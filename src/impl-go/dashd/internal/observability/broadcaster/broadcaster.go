// Package broadcaster implements the dashd-side server-stream fan-out
// for ObservabilityService.GetCounters (PE-3c / PD-G5).
//
// Architecture
// ------------
// The broadcaster is a *passive* fan-out engine. It does NOT subscribe
// to counters.Store directly — a separate Bridge goroutine (bridge.go)
// owns that integration and calls Broadcaster.Publish on each store
// change. This separation matches the established PE-G7 pattern
// (cluster.Aggregator → cluster.Broadcaster) and keeps the broadcaster
// unit-testable in isolation.
//
// Hardening (mirrors cluster broadcaster's D1-D7 fixes)
// -----------------------------------------------------
//
//   D1. Marshal-once-send-many. Every Publish call protojson-marshals
//       the CounterEvent ONCE and shares the same []byte across every
//       matching subscriber + the resume ring. Per-event CPU is O(1)
//       in subscriber count. §8 of agent-operating-discipline.md is
//       the marshal-once invariant; broadcaster_test.go asserts
//       byte-equality of the shared payload across N subscribers.
//
//   D2. KIND_DROPPED sentinel on per-subscriber overflow. Buffer-full
//       drops set a counter rather than silently losing the event;
//       the handler synthesises a KIND_DROPPED notice on the next
//       successful send so the client knows to refetch a snapshot.
//
//   D3. Per-server + per-subject subscriber caps. Subscribe returns
//       ErrTooManySubscribers when either limit is breached so the
//       handler maps to RESOURCE_EXHAUSTED / HTTP 429.
//
//   D4. Single global keepalive ticker. One goroutine fires
//       KIND_KEEPALIVE on every active subscriber (filtered + sent),
//       O(1) regardless of subscriber count.
//
//   D5. Leaky-bucket rate limit + coalesce window. KIND_REPORT events
//       for the same dpu_id within CoalesceWindow collapse to the
//       latest. Burst over BurstSize fires a KIND_RATE_LIMITED notice
//       with the suppressed count. Sentinels are NEVER coalesced.
//
//   D6. Monotonic per-process event_id stamped at Publish time and
//       stored in a ring buffer. Subscribe with ResumeAfterEventID
//       replays cleanly when the cursor is still in-ring; older
//       cursors trigger KIND_RESYNC + fresh snapshot refetch.
//
//   D7. SubscribeOptions.DpuIDs is an allow-list filter applied
//       inside the fan-out loop so DPU-X reports never reach a
//       subscriber that watches only DPU-Y. KIND_REPORT carries
//       Body.Report.DpuId; sentinels bypass the filter (every
//       subscriber sees keepalive / dropped / rate-limit / resync).
//
// Concurrency
// -----------
// Broadcaster is safe for concurrent use. The subscriber map and the
// per-subject counter are guarded by `mu` (RWMutex; Publish takes
// RLock during the fan-out snapshot). The ring is guarded by `ringMu`
// to keep cursor-resume Subscribe calls cheap. The coalesce window and
// rate-bucket each have their own mutex so contention paths stay
// independent. Per-subscriber state owns its own `closedMu`. All
// channels follow PE-G7's "drop-on-slow" discipline — Publish NEVER
// blocks on a slow client.
package broadcaster

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── public errors ────────────────────────────────────────────────────────

// ErrTooManySubscribers is returned by Subscribe when either the global
// or per-subject cap is exhausted. Handlers MUST map this to gRPC
// RESOURCE_EXHAUSTED / HTTP 429.
var ErrTooManySubscribers = errors.New("observability.broadcaster: too many subscribers")

// ── tunables ────────────────────────────────────────────────────────────

// Config binds the broadcaster's behaviour. Zero-valued fields default
// to production-sane values inside NewBroadcaster.
//
// All knobs are wired from internal/config.StreamConfig in main.go so
// operators tune broadcaster behaviour purely through dashd.yaml. The
// translation lives in main.go (not here) so broadcaster has zero
// dependency on the config package — keeps unit tests dependency-free.
type Config struct {
	// MaxSubscribers caps total in-flight subscribers across all
	// transports (gRPC + REST/SSE + dashw hub). 0 → default (256).
	MaxSubscribers int

	// MaxSubscribersPerSubject caps watchers per auth Subject.Name so a
	// single operator/tenant can't hog the pool. 0 → disabled (no
	// per-subject check; single-tenant deployments).
	MaxSubscribersPerSubject int

	// SubscriberBufferSize is the per-stream buffered channel depth.
	// Smaller = faster overflow detection; larger = more headroom for
	// brief stalls. 0 → default (64).
	SubscriberBufferSize int

	// RingSize is the number of recent events retained for the
	// ResumeAfterEventID cursor. 0 → default (512).
	RingSize int

	// CoalesceWindow merges KIND_REPORT events for the same dpu_id
	// that arrive within this duration. Sentinels are NEVER coalesced.
	// 0 disables coalescing entirely.
	CoalesceWindow time.Duration

	// EventRatePerSec is the steady-state event ceiling enforced by
	// the leaky bucket. 0 → default (200).
	EventRatePerSec float64

	// BurstSize is the leaky-bucket capacity. MUST be ≥ EventRatePerSec
	// (validated upstream in config). 0 → default (400).
	BurstSize float64

	// KeepaliveInterval is the cadence of the single global keepalive
	// goroutine. 0 disables keepalive. Started by Run(ctx), torn down
	// by Stop().
	KeepaliveInterval time.Duration

	// SuppressedNoticeDelay is the time to wait after the first
	// suppression before emitting a KIND_RATE_LIMITED notice. Coalesces
	// many suppressions into one notice. 0 → default (250ms).
	SuppressedNoticeDelay time.Duration
}

// DefaultConfig returns a production-ready zero-config. Used by tests
// that don't care about tuning and as the safety net inside
// NewBroadcaster.
func DefaultConfig() Config {
	return Config{
		MaxSubscribers:           256,
		MaxSubscribersPerSubject: 8,
		SubscriberBufferSize:     64,
		RingSize:                 512,
		CoalesceWindow:           250 * time.Millisecond,
		EventRatePerSec:          200,
		BurstSize:                400,
		KeepaliveInterval:        30 * time.Second,
		SuppressedNoticeDelay:    250 * time.Millisecond,
	}
}

// SubscribeOptions narrows a Subscribe call.
type SubscribeOptions struct {
	// SubjectName is the auth.Subject.Name of the caller, used for the
	// per-subject cap. Empty = anonymous (counted as one bucket).
	SubjectName string

	// DpuIDs is an allow-list filter: KIND_REPORT events whose
	// Body.Report.DpuId is NOT in this list are skipped at fan-out time
	// for this subscriber. Empty = all DPUs. Sentinels bypass the
	// filter (every subscriber sees keepalive / dropped / rate-limit /
	// resync regardless of DpuIDs).
	DpuIDs []string

	// ResumeAfterEventID, when non-zero, asks the broadcaster to
	// replay events with id > cursor before relaying live events. If
	// the cursor predates the ring (or the broadcaster restarted), the
	// channel receives a single KIND_RESYNC sentinel and the caller
	// MUST refetch a snapshot before relying on subsequent deltas.
	ResumeAfterEventID uint64
}

// Frame is what every subscriber receives — the typed CounterEvent
// plus the SAME pre-marshalled protojson bytes (computed once at
// Publish; shared across every matching subscriber + the ring). The
// JSON slice is immutable after construction.
type Frame struct {
	Event *dashcenterv1.CounterEvent
	JSON  []byte
}

// Subscription is the handle returned by Subscribe. Callers use it to
// receive frames AND to query the dropped-event counter so handlers
// can synthesise KIND_DROPPED before relaying the next live event.
type Subscription struct {
	sub *subscription
}

// Recv returns the receive channel. Closed when the cancel func
// returned by Subscribe is invoked.
func (s *Subscription) Recv() <-chan *Frame { return s.sub.ch }

// TakeDroppedCount returns + atomically clears the dropped count. A
// non-zero return means the next read SHOULD be preceded by a
// synthetic KIND_DROPPED notice so the client knows to refetch.
func (s *Subscription) TakeDroppedCount() uint64 { return s.sub.droppedCount.Swap(0) }

// LastDeliveredEventID returns the highest event_id the broadcaster
// has successfully written to this subscriber's channel.
func (s *Subscription) LastDeliveredEventID() uint64 { return s.sub.lastDeliveredID.Load() }

// ── internal types ───────────────────────────────────────────────────────

type subscription struct {
	ch          chan *Frame
	closedMu    sync.Mutex
	closed      bool
	subjectName string

	// dpuFilter is nil when SubscribeOptions.DpuIDs is empty; otherwise
	// a set for O(1) membership check during fan-out. Sentinels bypass
	// the filter (filterMatches returns true when ev.Body.Report is nil).
	dpuFilter map[string]struct{}

	droppedCount    atomic.Uint64
	lastDeliveredID atomic.Uint64
}

type ringEntry struct {
	id    uint64
	frame *Frame
}

// ── Broadcaster ──────────────────────────────────────────────────────────

// Broadcaster owns the GetCounters fan-out for one dashd process. One
// per dashd. Construct with NewBroadcaster; start the keepalive
// goroutine with Run; tear down with Stop. Safe for concurrent use.
type Broadcaster struct {
	cfg    Config
	logger *slog.Logger

	// monotonic event_id counter (per-process; resets on restart).
	nextID atomic.Uint64

	// subscriber map + per-subject counter.
	mu             sync.RWMutex
	subscribers    map[*subscription]struct{}
	bySubjectCount map[string]int

	// ring buffer for ResumeAfterEventID replay.
	ringMu      sync.RWMutex
	ring        []ringEntry
	ringHead    int  // slot to overwrite next
	ringWrapped bool // true once the ring has cycled through head

	// coalescing window state. pendingByDpu keyed by dpu_id (only
	// KIND_REPORT coalesces; sentinels never enter this map).
	coalesceMu  sync.Mutex
	pendingByDpu map[string]*dashcenterv1.CounterEvent
	coalesceT   *time.Timer
	coalesceOn  atomic.Bool

	// leaky bucket state.
	rateMu                    sync.Mutex
	tokens                    float64
	lastFill                  time.Time
	suppressedSinceLastNotice atomic.Uint64
	rateNoticeT               *time.Timer

	// goroutine lifecycle.
	stopCh   chan struct{}
	stopOnce sync.Once
	runOnce  sync.Once

	// metrics counters (atomics; Stats() reads).
	mTotalPublished atomic.Uint64
	mTotalDelivered atomic.Uint64
	mTotalDropped   atomic.Uint64
	mTotalCoalesced atomic.Uint64
	mTotalSuppress  atomic.Uint64
}

// NewBroadcaster returns a broadcaster with the supplied config. Pass
// DefaultConfig() for production defaults. The logger is used for the
// single best-effort warning path (protojson marshal failure on our
// own protos — should never happen, but observability-first §0.2.1
// requires we surface it).
//
// NOTE: NewBroadcaster does NOT start the keepalive goroutine. Call
// Run(ctx) once you have a lifecycle context. Tests that don't need
// keepalive can skip Run entirely.
func NewBroadcaster(cfg Config, logger *slog.Logger) *Broadcaster {
	if cfg.MaxSubscribers <= 0 {
		cfg.MaxSubscribers = 256
	}
	if cfg.SubscriberBufferSize <= 0 {
		cfg.SubscriberBufferSize = 64
	}
	if cfg.RingSize <= 0 {
		cfg.RingSize = 512
	}
	if cfg.EventRatePerSec <= 0 {
		cfg.EventRatePerSec = 200
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = 400
	}
	if cfg.SuppressedNoticeDelay <= 0 {
		cfg.SuppressedNoticeDelay = 250 * time.Millisecond
	}
	if logger == nil {
		logger = slog.Default()
	}
	b := &Broadcaster{
		cfg:            cfg,
		logger:         logger,
		subscribers:    map[*subscription]struct{}{},
		bySubjectCount: map[string]int{},
		ring:           make([]ringEntry, cfg.RingSize),
		pendingByDpu:   map[string]*dashcenterv1.CounterEvent{},
		tokens:         cfg.BurstSize,
		lastFill:       time.Now(),
		stopCh:         make(chan struct{}),
	}
	metrics.maxSubscribers.WithLabelValues().Set(float64(cfg.MaxSubscribers))
	return b
}

// Run starts the keepalive ticker goroutine. Idempotent — second call
// is a no-op. Stop() (or ctx cancellation) tears it down. Callers SHOULD
// invoke Run during dashd startup AFTER NewBroadcaster + admin wiring.
func (b *Broadcaster) Run(ctx context.Context) {
	b.runOnce.Do(func() {
		if b.cfg.KeepaliveInterval > 0 {
			go b.runKeepalive(ctx)
		}
	})
}

// Stop releases the keepalive goroutine + any pending coalesce/rate
// notice timer. Idempotent. Subscribers are NOT force-closed — callers
// own their cancel funcs.
func (b *Broadcaster) Stop() {
	b.stopOnce.Do(func() {
		close(b.stopCh)
		b.coalesceMu.Lock()
		if b.coalesceT != nil {
			b.coalesceT.Stop()
			b.coalesceT = nil
		}
		b.coalesceMu.Unlock()
		b.rateMu.Lock()
		if b.rateNoticeT != nil {
			b.rateNoticeT.Stop()
			b.rateNoticeT = nil
		}
		b.rateMu.Unlock()
	})
}

// Subscribe registers a new stream and returns the handle + a cancel
// function. The cancel func MUST be invoked (typically deferred in the
// handler) to release the subscriber slot and the per-subject quota.
//
// If ResumeAfterEventID is set, the channel is pre-loaded with either:
//
//   * the buffered frames with id > cursor (if cursor is still in ring), OR
//   * a single KIND_RESYNC notice (if cursor is stale) followed by no
//     further history — the caller MUST then refetch a snapshot before
//     consuming live events.
//
// Returns ErrTooManySubscribers when global or per-subject caps are
// exhausted (wrapped so callers can errors.Is + extract diagnostics).
func (b *Broadcaster) Subscribe(opts SubscribeOptions) (*Subscription, func(), error) {
	sub := &subscription{
		ch:          make(chan *Frame, b.cfg.SubscriberBufferSize),
		subjectName: opts.SubjectName,
	}
	if len(opts.DpuIDs) > 0 {
		sub.dpuFilter = make(map[string]struct{}, len(opts.DpuIDs))
		for _, id := range opts.DpuIDs {
			if id == "" {
				continue
			}
			sub.dpuFilter[id] = struct{}{}
		}
		// Edge case: every entry was empty → fall back to "all DPUs"
		// rather than silently delivering zero events.
		if len(sub.dpuFilter) == 0 {
			sub.dpuFilter = nil
		}
	}

	b.mu.Lock()
	if len(b.subscribers) >= b.cfg.MaxSubscribers {
		b.mu.Unlock()
		metrics.subscribeRejected.WithLabelValues("global").Inc()
		return nil, nil, fmt.Errorf("%w: global cap=%d reached", ErrTooManySubscribers, b.cfg.MaxSubscribers)
	}
	if b.cfg.MaxSubscribersPerSubject > 0 && opts.SubjectName != "" {
		if b.bySubjectCount[opts.SubjectName] >= b.cfg.MaxSubscribersPerSubject {
			b.mu.Unlock()
			metrics.subscribeRejected.WithLabelValues("per_subject").Inc()
			return nil, nil, fmt.Errorf("%w: per-subject cap=%d reached for %q",
				ErrTooManySubscribers, b.cfg.MaxSubscribersPerSubject, opts.SubjectName)
		}
		b.bySubjectCount[opts.SubjectName]++
	}
	b.subscribers[sub] = struct{}{}
	subCount := len(b.subscribers)
	b.mu.Unlock()
	metrics.subscribers.WithLabelValues().Set(float64(subCount))

	if opts.ResumeAfterEventID > 0 {
		b.replayResume(sub, opts.ResumeAfterEventID)
	}

	cancel := func() {
		sub.closedMu.Lock()
		if !sub.closed {
			sub.closed = true
			close(sub.ch)
		}
		sub.closedMu.Unlock()
		b.mu.Lock()
		if _, ok := b.subscribers[sub]; ok {
			delete(b.subscribers, sub)
			if sub.subjectName != "" {
				if c := b.bySubjectCount[sub.subjectName]; c > 1 {
					b.bySubjectCount[sub.subjectName] = c - 1
				} else {
					delete(b.bySubjectCount, sub.subjectName)
				}
			}
		}
		newCount := len(b.subscribers)
		b.mu.Unlock()
		metrics.subscribers.WithLabelValues().Set(float64(newCount))
	}
	return &Subscription{sub: sub}, cancel, nil
}

// Publish ingests one CounterReport and fans it out as KIND_REPORT.
// Callers (typically the Bridge goroutine) supply the report; the
// broadcaster owns wrapping, event_id stamping, marshalling, ring
// storage, and dispatch. nil reports are no-ops.
//
// Coalescing + rate-limit apply to KIND_REPORT.
func (b *Broadcaster) Publish(report *dashcenterv1.CounterReport) {
	if report == nil {
		return
	}
	ev := &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_REPORT,
		Body: &dashcenterv1.CounterEvent_Report{Report: report},
	}
	b.publish(ev)
}

// PublishSentinel emits a sentinel event (KIND_KEEPALIVE / RATE_LIMITED).
// KIND_DROPPED and KIND_RESYNC are per-subscriber and are NEVER fanned
// out via Publish — see enqueueResyncLocked + the handler's
// TakeDroppedCount workflow.
//
// Exposed for tests; production callers go through Run's keepalive
// loop and the internal rate-notice timer.
func (b *Broadcaster) PublishSentinel(ev *dashcenterv1.CounterEvent) {
	if ev == nil {
		return
	}
	switch ev.GetKind() {
	case dashcenterv1.CounterEvent_KIND_DROPPED,
		dashcenterv1.CounterEvent_KIND_RESYNC:
		// Per-subscriber; refuse to broadcast.
		return
	}
	b.publish(ev)
}

// publish is the shared internal entry. Applies rate-limit + coalesce
// only to KIND_REPORT; sentinels bypass both.
func (b *Broadcaster) publish(ev *dashcenterv1.CounterEvent) {
	b.mTotalPublished.Add(1)
	metrics.published.WithLabelValues(kindLabel(ev)).Inc()

	if !canCoalesce(ev.GetKind()) {
		b.publishImmediate(ev)
		return
	}

	if !b.takeToken() {
		b.suppressedSinceLastNotice.Add(1)
		b.mTotalSuppress.Add(1)
		metrics.suppressed.WithLabelValues().Inc()
		b.scheduleRateNotice()
		return
	}

	if b.cfg.CoalesceWindow > 0 {
		b.coalesceMu.Lock()
		dpu := ev.GetReport().GetDpuId()
		if dpu != "" {
			if _, exists := b.pendingByDpu[dpu]; exists {
				b.mTotalCoalesced.Add(1)
				metrics.coalesced.WithLabelValues().Inc()
			}
			b.pendingByDpu[dpu] = ev
			if !b.coalesceOn.Load() {
				b.coalesceOn.Store(true)
				b.coalesceT = time.AfterFunc(b.cfg.CoalesceWindow, b.flushCoalesce)
			}
			b.coalesceMu.Unlock()
			return
		}
		b.coalesceMu.Unlock()
	}

	b.publishImmediate(ev)
}

// canCoalesce reports whether a kind participates in the coalesce + rate
// limit pipeline. Snapshots, keepalives, and the rate-limit sentinel
// itself always emit immediately.
func canCoalesce(k dashcenterv1.CounterEvent_Kind) bool {
	switch k {
	case dashcenterv1.CounterEvent_KIND_SNAPSHOT,
		dashcenterv1.CounterEvent_KIND_KEEPALIVE,
		dashcenterv1.CounterEvent_KIND_RATE_LIMITED:
		return false
	}
	return true
}

// publishImmediate stamps event_id + ts, ring-stores, and fans out.
// Filters per subscriber by DpuIDs before send.
func (b *Broadcaster) publishImmediate(ev *dashcenterv1.CounterEvent) {
	id := b.nextID.Add(1)
	if ev.GetTs() == nil {
		ev.Ts = timestamppb.Now()
	}
	ev.EventId = id

	frame, err := buildFrame(ev)
	if err != nil {
		// UNREACHABLE in production: protojson.Marshal cannot fail on
		// our own generated CounterEvent type (all field types are
		// well-formed). The branch is intentionally retained as
		// defensive belt-and-suspenders + an observability marker — if
		// it ever fires, dashd has a generated-code bug that we want
		// to surface immediately. Excluded from UT coverage per
		// §0.2.1 "Backward compatibility" / defensive-code allowance.
		b.logger.Warn("counter broadcaster: marshal failed", "kind", kindLabel(ev), "error", err)
		metrics.dropped.WithLabelValues("marshal_error").Inc()
		return
	}

	// Store in ring BEFORE fan-out so a fresh subscribe-with-cursor
	// during fan-out sees the new event.
	b.ringMu.Lock()
	b.ring[b.ringHead] = ringEntry{id: id, frame: frame}
	b.ringHead++
	if b.ringHead >= len(b.ring) {
		b.ringHead = 0
		b.ringWrapped = true
	}
	b.ringMu.Unlock()

	b.mu.RLock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		if !sub.matches(ev) {
			continue
		}
		sub.closedMu.Lock()
		if sub.closed {
			sub.closedMu.Unlock()
			continue
		}
		select {
		case sub.ch <- frame:
			metrics.delivered.WithLabelValues(kindLabel(frame.Event)).Inc()
			b.mTotalDelivered.Add(1)
			sub.lastDeliveredID.Store(id)
		default:
			sub.droppedCount.Add(1)
			b.mTotalDropped.Add(1)
			metrics.dropped.WithLabelValues("buffer_full").Inc()
		}
		sub.closedMu.Unlock()
	}
}

// matches reports whether this subscription is interested in ev. The
// filter applies ONLY to KIND_REPORT / KIND_SNAPSHOT carrying a Report
// body; sentinels (KIND_KEEPALIVE / KIND_RATE_LIMITED) always pass.
func (s *subscription) matches(ev *dashcenterv1.CounterEvent) bool {
	if s.dpuFilter == nil {
		return true
	}
	report := ev.GetReport()
	if report == nil {
		return true // sentinel — always delivered
	}
	_, ok := s.dpuFilter[report.GetDpuId()]
	return ok
}

// flushCoalesce fires from the coalesce timer. Drains pendingByDpu and
// publishes each surviving event in deterministic dpu_id order for
// test stability.
func (b *Broadcaster) flushCoalesce() {
	b.coalesceMu.Lock()
	pending := b.pendingByDpu
	b.pendingByDpu = map[string]*dashcenterv1.CounterEvent{}
	b.coalesceT = nil
	b.coalesceOn.Store(false)
	b.coalesceMu.Unlock()

	keys := make([]string, 0, len(pending))
	for k := range pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.publishImmediate(pending[k])
	}
}

// takeToken implements the leaky bucket. Returns true if a token was
// available.
func (b *Broadcaster) takeToken() bool {
	b.rateMu.Lock()
	defer b.rateMu.Unlock()
	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens += elapsed * b.cfg.EventRatePerSec
	if b.tokens > b.cfg.BurstSize {
		b.tokens = b.cfg.BurstSize
	}
	b.lastFill = now
	if b.tokens >= 1 {
		b.tokens--
		return true
	}
	return false
}

// scheduleRateNotice emits a KIND_RATE_LIMITED notice
// SuppressedNoticeDelay after the first suppression. Coalesces multiple
// suppressions into one notice.
func (b *Broadcaster) scheduleRateNotice() {
	b.rateMu.Lock()
	if b.rateNoticeT != nil {
		b.rateMu.Unlock()
		return
	}
	b.rateNoticeT = time.AfterFunc(b.cfg.SuppressedNoticeDelay, b.emitRateNotice)
	b.rateMu.Unlock()
}

func (b *Broadcaster) emitRateNotice() {
	b.rateMu.Lock()
	b.rateNoticeT = nil
	b.rateMu.Unlock()
	n := b.suppressedSinceLastNotice.Swap(0)
	if n == 0 {
		return
	}
	ev := newRateLimitedNotice(n, b.cfg.EventRatePerSec)
	b.publishImmediate(ev)
}

// runKeepalive emits KIND_KEEPALIVE on a single global cadence. Exits
// on ctx cancel or Stop().
func (b *Broadcaster) runKeepalive(ctx context.Context) {
	t := time.NewTicker(b.cfg.KeepaliveInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stopCh:
			return
		case <-t.C:
			b.publishImmediate(newKeepaliveNotice())
		}
	}
}

// ── resume cursor ───────────────────────────────────────────────────────

func (b *Broadcaster) replayResume(sub *subscription, cursor uint64) {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()

	currentID := b.nextID.Load()
	oldestID := b.oldestIDLocked()

	if currentID <= cursor {
		// Server restarted (cursor from a previous process) OR client
		// cursor is from the future. Tell them to resync.
		b.enqueueResyncLocked(sub, currentID, "cursor exceeds current event_id (server restart?)")
		return
	}
	if oldestID == 0 || cursor < oldestID-1 {
		// Cursor predates the ring buffer.
		b.enqueueResyncLocked(sub, currentID, "cursor predates ring buffer; refetch snapshot")
		return
	}
	walk := func(start, end int) {
		for i := start; i < end; i++ {
			entry := b.ring[i]
			if entry.frame == nil || entry.id <= cursor {
				continue
			}
			if !sub.matches(entry.frame.Event) {
				continue
			}
			b.enqueueLocked(sub, entry.frame)
		}
	}
	if b.ringWrapped {
		walk(b.ringHead, len(b.ring))
	}
	walk(0, b.ringHead)
}

func (b *Broadcaster) oldestIDLocked() uint64 {
	if !b.ringWrapped {
		if b.ringHead == 0 {
			return 0
		}
		return b.ring[0].id
	}
	return b.ring[b.ringHead].id
}

func (b *Broadcaster) enqueueResyncLocked(sub *subscription, currentID uint64, msg string) {
	ev := newResyncNotice(currentID, msg)
	frame, _ := buildFrame(ev) // best-effort; protojson never fails on our own protos
	b.enqueueLocked(sub, frame)
}

func (b *Broadcaster) enqueueLocked(sub *subscription, frame *Frame) {
	sub.closedMu.Lock()
	defer sub.closedMu.Unlock()
	if sub.closed {
		return
	}
	select {
	case sub.ch <- frame:
		metrics.delivered.WithLabelValues(kindLabel(frame.Event)).Inc()
		b.mTotalDelivered.Add(1)
		if id := frame.Event.GetEventId(); id > 0 {
			sub.lastDeliveredID.Store(id)
		}
	default:
		sub.droppedCount.Add(1)
		metrics.dropped.WithLabelValues("resume_replay").Inc()
		b.mTotalDropped.Add(1)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

// buildFrame pre-marshals the event into a shared protojson byte
// slice. Returns the typed event + JSON; both are immutable after
// this call. This is the marshal-once point (§8 invariant).
//
// The err branch is UNREACHABLE in production — protojson.Marshal
// cannot fail on our own generated CounterEvent type. The check is
// retained as a defensive guard against future schema regressions;
// excluded from UT coverage per §0.2.1 defensive-code allowance.
func buildFrame(ev *dashcenterv1.CounterEvent) (*Frame, error) {
	js, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(ev)
	if err != nil {
		return nil, err
	}
	return &Frame{Event: ev, JSON: js}, nil
}

// kindLabel returns the metrics-safe label for a CounterEvent kind.
func kindLabel(ev *dashcenterv1.CounterEvent) string {
	if ev == nil {
		return "nil"
	}
	name := ev.GetKind().String()
	return strings.ToLower(strings.TrimPrefix(name, "KIND_"))
}

// ── Stats ────────────────────────────────────────────────────────────────

// Stats is a snapshot of broadcaster activity for /admin/health.
type Stats struct {
	Subscribers     int
	BySubjectCount  map[string]int
	TotalPublished  uint64
	TotalDelivered  uint64
	TotalDropped    uint64
	TotalCoalesced  uint64
	TotalSuppressed uint64
	NextEventID     uint64
	RingSize        int
	OldestEventID   uint64
	NewestEventID   uint64
}

// Stats returns a snapshot. Safe for concurrent use.
func (b *Broadcaster) Stats() Stats {
	b.mu.RLock()
	subs := len(b.subscribers)
	bySubj := make(map[string]int, len(b.bySubjectCount))
	for k, v := range b.bySubjectCount {
		bySubj[k] = v
	}
	b.mu.RUnlock()

	b.ringMu.RLock()
	oldest := b.oldestIDLocked()
	newest := uint64(0)
	if !(b.ringHead == 0 && !b.ringWrapped) {
		idx := b.ringHead - 1
		if idx < 0 {
			idx = len(b.ring) - 1
		}
		newest = b.ring[idx].id
	}
	b.ringMu.RUnlock()

	return Stats{
		Subscribers:     subs,
		BySubjectCount:  bySubj,
		TotalPublished:  b.mTotalPublished.Load(),
		TotalDelivered:  b.mTotalDelivered.Load(),
		TotalDropped:    b.mTotalDropped.Load(),
		TotalCoalesced:  b.mTotalCoalesced.Load(),
		TotalSuppressed: b.mTotalSuppress.Load(),
		NextEventID:     b.nextID.Load(),
		RingSize:        len(b.ring),
		OldestEventID:   oldest,
		NewestEventID:   newest,
	}
}
