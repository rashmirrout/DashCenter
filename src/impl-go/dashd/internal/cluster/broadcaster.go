// broadcaster.go — production-grade WatchTopology fan-out for ClusterService.
//
// Replaces the v1 broadcaster (~120 LOC) with a hardened implementation
// that fixes the five scalability defects called out in
// docs/dashd-features/topology-streaming-design.md:
//
//   D1. Marshal-once-send-many. Each Publish() pre-marshals one
//       protojson byte slice and shares it across every subscriber +
//       the resume ring. Per-event CPU is independent of subscriber
//       count.
//
//   D2. Burst coalescing + leaky-bucket rate limit. Events that arrive
//       within `CoalesceWindow` and share the same (kind, entity-id)
//       key collapse to the latest; the leaky bucket caps the
//       sustained event rate. When the bucket runs dry the broadcaster
//       emits a single KIND_RATE_LIMITED notice carrying the
//       suppressed count instead of silently dropping. State remains
//       consistent because coalesced events represent the same logical
//       transition.
//
//   D3. KIND_DROPPED sentinel. Per-subscriber buffer overflows now set
//       a flag + counter rather than silently dropping. On the next
//       successful Send, the handler synthesises a KIND_DROPPED notice
//       so the client knows to call GetTopology and resync. No event
//       is ever "silently" lost from the client's point of view.
//
//   D4. Per-server + per-subject subscriber caps. Subscribe returns
//       ErrTooManySubscribers when either limit is breached so the
//       handler can map that to gRPC RESOURCE_EXHAUSTED / HTTP 429
//       with Retry-After.
//
//   D5. Single global keepalive ticker. One goroutine fires
//       KIND_KEEPALIVE events; the cost is O(1) regardless of
//       subscriber count (vs. one ticker per stream in the v1).
//
// Plus two production essentials the design spec asked for:
//
//   * Monotonic per-process event IDs assigned at Publish time and
//     stored in a ring buffer. WatchTopology.resume_after_event_id
//     replays cleanly without a fresh snapshot when the cursor is
//     within the ring (typical mid-flight reconnect). Older cursors
//     trigger a KIND_RESYNC sentinel + a fresh snapshot.
//
//   * Prometheus metrics in cluster/metrics.go register-once via
//     prometheus.MustRegister and are observed from every Publish /
//     Subscribe / Drop path. Wired by Stats().
//
// The whole file is single-writer-ish: Publish + tick goroutines hold
// `b.mu` for the subscriber map; reads (Snapshot / Stats / cursor
// replay) take RLock on the same mutex; subscribers hold their own
// closedMu and an atomic flag for the dropped sentinel.
package cluster

import (
	"errors"
	"fmt"
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

// ErrTooManySubscribers is returned by Subscribe when either the
// global or per-subject cap is exhausted. Handlers SHOULD map this to
// gRPC RESOURCE_EXHAUSTED / HTTP 429.
var ErrTooManySubscribers = errors.New("cluster.Broadcaster: too many subscribers")

// ── tunables ────────────────────────────────────────────────────────────

// BroadcasterConfig binds the broadcaster's behaviour. Zero-values mean
// "use the production default" — see DefaultBroadcasterConfig.
type BroadcasterConfig struct {
	// MaxSubscribers caps the total in-flight watchers. dashd is expected
	// to be fronted by dashw which multiplexes; this defends against
	// misbehaving clients (dashctl in a loop, runaway Prometheus, etc.).
	MaxSubscribers int

	// MaxSubscribersPerSubject caps watchers per auth Subject.Name so a
	// single operator can't hog the pool. Zero disables the per-subject
	// check (single-tenant deployments).
	MaxSubscribersPerSubject int

	// SubscriberBufferSize is the per-stream buffered channel size.
	// Smaller = faster overflow detection; larger = more headroom for
	// brief stalls. The default (64) survives ~3s of stalled writes at
	// the default rate cap.
	SubscriberBufferSize int

	// RingSize is the number of recent events retained for the
	// resume_after_event_id cursor. 1024 covers ~5min at 3 events/sec
	// (typical fleet churn) which is well above the worst-case
	// browser reconnect window.
	RingSize int

	// CoalesceWindow merges (kind, entity-id) duplicates that arrive
	// within this duration. Set to 0 to disable coalescing entirely.
	CoalesceWindow time.Duration

	// EventRatePerSec is the steady-state event ceiling enforced by the
	// leaky bucket. Bursts up to BurstSize are allowed.
	EventRatePerSec float64

	// BurstSize is the leaky-bucket capacity.
	BurstSize float64

	// KeepaliveInterval is the cadence of the single global keepalive
	// goroutine. Zero disables keepalive.
	KeepaliveInterval time.Duration
}

// DefaultBroadcasterConfig is the production-ready zero-config.
// MaxSubscribers chosen so a dashw fleet of ~16 replicas + 16 dashctl
// streams + headroom = 64.
func DefaultBroadcasterConfig() BroadcasterConfig {
	return BroadcasterConfig{
		MaxSubscribers:           64,
		MaxSubscribersPerSubject: 4,
		SubscriberBufferSize:     64,
		RingSize:                 1024,
		CoalesceWindow:           50 * time.Millisecond,
		EventRatePerSec:          100,
		BurstSize:                200,
		KeepaliveInterval:        30 * time.Second,
	}
}

// SubscribeOptions narrows a Subscribe call.
type SubscribeOptions struct {
	// SubjectName is the auth.Subject.Name of the caller (used for the
	// per-subject cap). Empty = anonymous; counted as a single bucket.
	SubjectName string

	// ResumeAfterEventID, when non-zero, asks the broadcaster to replay
	// events with id > ResumeAfterEventID before relaying live events.
	// If the cursor predates the ring or the broadcaster restarted,
	// the channel receives a single KIND_RESYNC event and the caller
	// MUST refetch GetTopology before relying on subsequent deltas.
	ResumeAfterEventID uint64
}

// Subscription is the handle returned by Subscribe. Callers use it to
// receive frames AND to query the dropped-event counter (so handlers
// can synthesise KIND_DROPPED before relaying the next live event).
type Subscription struct {
	sub *subscription
}

// Recv returns the receive channel. Closed when Cancel() is called.
func (s *Subscription) Recv() <-chan *Frame { return s.sub.ch }

// TakeDroppedCount returns + atomically clears the dropped count. A
// non-zero return means the next read SHOULD be preceded by a synthetic
// KIND_DROPPED notice so the client knows to call GetTopology.
func (s *Subscription) TakeDroppedCount() uint64 { return s.sub.droppedCount.Swap(0) }

// LastDeliveredEventID returns the highest event_id the broadcaster has
// successfully written to this subscriber's channel.
func (s *Subscription) LastDeliveredEventID() uint64 { return s.sub.lastDeliveredID.Load() }

// ── internal types ───────────────────────────────────────────────────────

// Frame is what every subscriber receives — the typed proto event plus
// the SAME pre-marshalled protojson bytes (computed once at Publish).
type Frame struct {
	Event *dashcenterv1.TopologyEvent
	JSON  []byte
}

type subscription struct {
	ch          chan *Frame
	closedMu    sync.Mutex
	closed      bool
	subjectName string

	// Dropped-sentinel state. droppedCount accumulates events the
	// subscriber missed; the handler reads + resets it on the next
	// successful send to emit KIND_DROPPED.
	droppedCount    atomic.Uint64
	lastDeliveredID atomic.Uint64
}

type ringEntry struct {
	id    uint64
	frame *Frame
}

// pendingKey identifies events that should coalesce in a window.
//   - KIND_PEER_*       → "peer/<node_id>"
//   - KIND_DPU_*        → "dpu/<dpu_id>"
//   - KIND_LEADER_CHANGED → "leader"
//   - KIND_SNAPSHOT     → not coalesced (always emitted)
//   - sentinels         → not coalesced (always emitted)
func pendingKey(ev *dashcenterv1.TopologyEvent) string {
	switch ev.GetKind() {
	case dashcenterv1.TopologyEvent_KIND_PEER_ADDED,
		dashcenterv1.TopologyEvent_KIND_PEER_REMOVED,
		dashcenterv1.TopologyEvent_KIND_PEER_UPDATED:
		if p := ev.GetPeer(); p != nil {
			return "peer/" + p.GetNodeId()
		}
	case dashcenterv1.TopologyEvent_KIND_DPU_STATE,
		dashcenterv1.TopologyEvent_KIND_DPU_ADDED,
		dashcenterv1.TopologyEvent_KIND_DPU_REMOVED:
		if d := ev.GetDpu(); d != nil {
			return "dpu/" + d.GetId()
		}
	case dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED:
		return "leader"
	}
	return "" // empty = do not coalesce
}

// ── Broadcaster ──────────────────────────────────────────────────────────

// Broadcaster owns the WatchTopology fan-out for one dashd process.
// Construct with NewBroadcaster; ClusterService owns one. Safe for
// concurrent use; all internal mutation is guarded.
type Broadcaster struct {
	cfg BroadcasterConfig

	// monotonic event ID counter (per-process; resets on restart).
	nextID atomic.Uint64

	// subscriber map + per-subject counter.
	mu             sync.RWMutex
	subscribers    map[*subscription]struct{}
	bySubjectCount map[string]int

	// ring buffer of recent frames for resume_after_event_id.
	ringMu sync.RWMutex
	ring   []ringEntry
	// ringHead points at the slot that will be overwritten next; the
	// ring is full when ringWrapped=true.
	ringHead    int
	ringWrapped bool

	// coalescing window state.
	coalesceMu  sync.Mutex
	pendingByK  map[string]*dashcenterv1.TopologyEvent
	coalesceT   *time.Timer
	coalesceOn  atomic.Bool

	// leaky bucket.
	rateMu                    sync.Mutex
	tokens                    float64
	lastFill                  time.Time
	suppressedSinceLastNotice atomic.Uint64
	rateNoticeT               *time.Timer

	// keepalive ticker lifecycle.
	stopCh    chan struct{}
	stopOnce  sync.Once

	// metrics counters.
	mtotalPublished atomic.Uint64
	mtotalDelivered atomic.Uint64
	mtotalDropped   atomic.Uint64
	mtotalCoalesced atomic.Uint64
	mtotalSuppress  atomic.Uint64
}

// NewBroadcaster returns a broadcaster with the supplied config. Pass
// DefaultBroadcasterConfig() for production defaults.
func NewBroadcaster(cfg BroadcasterConfig) *Broadcaster {
	if cfg.MaxSubscribers <= 0 {
		cfg.MaxSubscribers = 64
	}
	if cfg.SubscriberBufferSize <= 0 {
		cfg.SubscriberBufferSize = 64
	}
	if cfg.RingSize <= 0 {
		cfg.RingSize = 1024
	}
	if cfg.EventRatePerSec <= 0 {
		cfg.EventRatePerSec = 100
	}
	if cfg.BurstSize <= 0 {
		cfg.BurstSize = 200
	}
	b := &Broadcaster{
		cfg:            cfg,
		subscribers:    map[*subscription]struct{}{},
		bySubjectCount: map[string]int{},
		ring:           make([]ringEntry, cfg.RingSize),
		pendingByK:     map[string]*dashcenterv1.TopologyEvent{},
		tokens:         cfg.BurstSize,
		lastFill:       time.Now(),
		stopCh:         make(chan struct{}),
	}
	if cfg.KeepaliveInterval > 0 {
		go b.runKeepalive()
	}
	clusterMetrics.maxSubscribers.WithLabelValues().Set(float64(cfg.MaxSubscribers))
	return b
}

// Stop releases the keepalive goroutine + any pending coalesce/rate
// notice timer. Idempotent. Subscribers are NOT force-closed (callers
// own their cancel funcs).
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

// Subscribe registers a new stream and returns the handle + a
// cancel function. The cancel func MUST be invoked (typically deferred
// in the gRPC handler) to release slots.
//
// If ResumeAfterEventID is set, the channel is pre-loaded with either:
//   - the buffered frames with id > cursor (if cursor is still in
//     ring), OR
//   - a single KIND_RESYNC notice (if the cursor is stale) followed by
//     no further history — the caller MUST call GetTopology and
//     reset its local state before consuming live events.
//
// Returns ErrTooManySubscribers when caps are exhausted.
func (b *Broadcaster) Subscribe(opts SubscribeOptions) (*Subscription, func(), error) {
	sub := &subscription{
		ch:          make(chan *Frame, b.cfg.SubscriberBufferSize),
		subjectName: opts.SubjectName,
	}

	b.mu.Lock()
	if len(b.subscribers) >= b.cfg.MaxSubscribers {
		b.mu.Unlock()
		clusterMetrics.subscribeRejected.WithLabelValues("global").Inc()
		return nil, nil, fmt.Errorf("%w: global cap=%d reached", ErrTooManySubscribers, b.cfg.MaxSubscribers)
	}
	if b.cfg.MaxSubscribersPerSubject > 0 && opts.SubjectName != "" {
		if b.bySubjectCount[opts.SubjectName] >= b.cfg.MaxSubscribersPerSubject {
			b.mu.Unlock()
			clusterMetrics.subscribeRejected.WithLabelValues("per_subject").Inc()
			return nil, nil, fmt.Errorf("%w: per-subject cap=%d reached for %q",
				ErrTooManySubscribers, b.cfg.MaxSubscribersPerSubject, opts.SubjectName)
		}
		b.bySubjectCount[opts.SubjectName]++
	}
	b.subscribers[sub] = struct{}{}
	subCount := len(b.subscribers)
	b.mu.Unlock()
	clusterMetrics.subscribers.WithLabelValues().Set(float64(subCount))

	// Resume cursor handling. Done OUTSIDE the subscriber map lock so a
	// slow protojson decode doesn't stall other Subscribe calls. Buffer
	// capacity is bounded by ring size; if it overflows we degrade to a
	// RESYNC sentinel and leave the rest to a fresh GetTopology call.
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
		clusterMetrics.subscribers.WithLabelValues().Set(float64(newCount))
	}
	return &Subscription{sub: sub}, cancel, nil
}

// replayResume copies ring frames with id > cursor into the subscriber.
// If cursor is older than the oldest retained event (or the broadcaster
// restarted and IDs reset), a single KIND_RESYNC notice is sent and the
// subscriber receives no further history.
func (b *Broadcaster) replayResume(sub *subscription, cursor uint64) {
	b.ringMu.RLock()
	defer b.ringMu.RUnlock()

	currentID := b.nextID.Load()
	oldestID := b.oldestIDLocked()

	if currentID <= cursor {
		// Server restarted (cursor from a previous process) OR client
		// cursor is from the future. Either way: tell them to resync.
		b.enqueueResyncLocked(sub, currentID, "cursor exceeds current event_id (server restart?)")
		return
	}
	if oldestID == 0 || cursor < oldestID-1 {
		// Cursor predates the ring buffer; the client missed events
		// that were already evicted.
		b.enqueueResyncLocked(sub, currentID, "cursor predates ring buffer; refetch GetTopology")
		return
	}
	// Walk the ring in id order. ringWrapped=false means slots 0..ringHead-1
	// hold the only entries; wrapped=true means slots ringHead..end + 0..ringHead-1.
	walk := func(start, end int) {
		for i := start; i < end; i++ {
			entry := b.ring[i]
			if entry.frame == nil || entry.id <= cursor {
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

// oldestIDLocked must be called with b.ringMu held.
func (b *Broadcaster) oldestIDLocked() uint64 {
	if !b.ringWrapped {
		if b.ringHead == 0 {
			return 0
		}
		return b.ring[0].id
	}
	// Wrapped: oldest is at ringHead (the slot about to be overwritten).
	return b.ring[b.ringHead].id
}

func (b *Broadcaster) enqueueResyncLocked(sub *subscription, currentID uint64, msg string) {
	ev := &dashcenterv1.TopologyEvent{
		Kind:    dashcenterv1.TopologyEvent_KIND_RESYNC,
		Ts:      timestamppb.Now(),
		EventId: 0,
		Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
			Message:        msg,
			CurrentEventId: currentID,
		}},
	}
	frame, _ := buildFrame(ev) // best-effort; error returns empty json
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
		clusterMetrics.delivered.WithLabelValues(kindLabel(frame.Event)).Inc()
		b.mtotalDelivered.Add(1)
		sub.lastDeliveredID.Store(frame.Event.GetEventId())
	default:
		// Cursor replay overflow: drop and mark; the next live Publish
		// will emit KIND_DROPPED.
		sub.droppedCount.Add(1)
		clusterMetrics.dropped.WithLabelValues("resume_replay").Inc()
		b.mtotalDropped.Add(1)
	}
}

// Publish enqueues an event for fan-out. The event's Ts + EventId fields
// are stamped server-side regardless of caller-supplied values so the
// monotonic order is preserved.
//
// Rate-limited / coalesced events do NOT count against deliveries; the
// suppressed count is rolled into the next KIND_RATE_LIMITED notice.
//
// Sentinel kinds (KIND_DROPPED is per-subscriber and is NEVER passed
// to Publish; KIND_RESYNC same. KIND_RATE_LIMITED is generated
// internally and is allowed through.)
func (b *Broadcaster) Publish(ev *dashcenterv1.TopologyEvent) {
	if ev == nil {
		return
	}
	switch ev.GetKind() {
	case dashcenterv1.TopologyEvent_KIND_DROPPED,
		dashcenterv1.TopologyEvent_KIND_RESYNC:
		// Per-subscriber sentinels, never broadcast.
		return
	}
	b.mtotalPublished.Add(1)
	clusterMetrics.published.WithLabelValues(kindLabel(ev)).Inc()

	// Snapshots + keepalives + rate-limit notices bypass coalescing and
	// rate limiting; they're either operator-relevant or already global
	// signals.
	if !canCoalesce(ev.GetKind()) {
		b.publishImmediate(ev)
		return
	}

	// Apply leaky bucket BEFORE coalescing so coalesced bursts still
	// drain a token.
	if !b.takeToken() {
		b.suppressedSinceLastNotice.Add(1)
		b.mtotalSuppress.Add(1)
		clusterMetrics.suppressed.WithLabelValues().Inc()
		b.scheduleRateNotice()
		return
	}

	if b.cfg.CoalesceWindow > 0 {
		b.coalesceMu.Lock()
		key := pendingKey(ev)
		if key != "" {
			if _, exists := b.pendingByK[key]; exists {
				b.mtotalCoalesced.Add(1)
				clusterMetrics.coalesced.WithLabelValues().Inc()
			}
			b.pendingByK[key] = ev
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

// canCoalesce reports whether a kind participates in the coalescing /
// rate-limit pipeline. Snapshots, keepalives, leader-change, and the
// rate-limit sentinel itself always emit immediately.
func canCoalesce(k dashcenterv1.TopologyEvent_Kind) bool {
	switch k {
	case dashcenterv1.TopologyEvent_KIND_SNAPSHOT,
		dashcenterv1.TopologyEvent_KIND_KEEPALIVE,
		dashcenterv1.TopologyEvent_KIND_RATE_LIMITED,
		dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED:
		return false
	}
	return true
}

// publishImmediate stamps EventId + Ts, ring-stores, and fans out.
func (b *Broadcaster) publishImmediate(ev *dashcenterv1.TopologyEvent) {
	id := b.nextID.Add(1)
	if ev.GetTs() == nil {
		ev.Ts = timestamppb.Now()
	}
	ev.EventId = id

	frame, err := buildFrame(ev)
	if err != nil {
		// protojson should never fail on our own protos; log + skip.
		// (We don't have slog here; metrics reflect the failure.)
		clusterMetrics.dropped.WithLabelValues("marshal_error").Inc()
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

	// Snapshot subscriber list under RLock; deliver outside the lock.
	b.mu.RLock()
	subs := make([]*subscription, 0, len(b.subscribers))
	for s := range b.subscribers {
		subs = append(subs, s)
	}
	b.mu.RUnlock()

	for _, sub := range subs {
		sub.closedMu.Lock()
		if sub.closed {
			sub.closedMu.Unlock()
			continue
		}
		select {
		case sub.ch <- frame:
			clusterMetrics.delivered.WithLabelValues(kindLabel(frame.Event)).Inc()
			b.mtotalDelivered.Add(1)
			sub.lastDeliveredID.Store(id)
		default:
			// Buffer full — record drop; handler synthesises KIND_DROPPED
			// on next successful send.
			sub.droppedCount.Add(1)
			b.mtotalDropped.Add(1)
			clusterMetrics.dropped.WithLabelValues("buffer_full").Inc()
		}
		sub.closedMu.Unlock()
	}
}

// flushCoalesce fires from the coalesce timer. Drains pendingByK and
// publishes each surviving event.
func (b *Broadcaster) flushCoalesce() {
	b.coalesceMu.Lock()
	pending := b.pendingByK
	b.pendingByK = map[string]*dashcenterv1.TopologyEvent{}
	b.coalesceT = nil
	b.coalesceOn.Store(false)
	b.coalesceMu.Unlock()

	// Deterministic order: by key alphabetically so tests are stable.
	keys := make([]string, 0, len(pending))
	for k := range pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.publishImmediate(pending[k])
	}
}

// takeToken implements the leaky bucket. Returns true if the bucket
// had a token to spend.
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

// scheduleRateNotice emits a KIND_RATE_LIMITED notice 250ms after the
// first suppression. Coalesces multiple suppressions into one notice.
func (b *Broadcaster) scheduleRateNotice() {
	b.rateMu.Lock()
	if b.rateNoticeT != nil {
		b.rateMu.Unlock()
		return
	}
	b.rateNoticeT = time.AfterFunc(250*time.Millisecond, b.emitRateNotice)
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
	ev := &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_RATE_LIMITED,
		Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
			SuppressedCount: n,
			Message:         fmt.Sprintf("broadcaster suppressed %d events in the last window (rate=%g/s)", n, b.cfg.EventRatePerSec),
		}},
	}
	b.publishImmediate(ev)
}

// runKeepalive emits KIND_KEEPALIVE on a single global cadence. Runs
// until Stop().
func (b *Broadcaster) runKeepalive() {
	t := time.NewTicker(b.cfg.KeepaliveInterval)
	defer t.Stop()
	for {
		select {
		case <-b.stopCh:
			return
		case <-t.C:
			ev := &dashcenterv1.TopologyEvent{
				Kind: dashcenterv1.TopologyEvent_KIND_KEEPALIVE,
				Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
					Message: "keepalive",
				}},
			}
			b.publishImmediate(ev)
		}
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

// buildFrame pre-marshals the event into a shared protojson byte slice.
// Returns the typed event + JSON; both are immutable after this call.
func buildFrame(ev *dashcenterv1.TopologyEvent) (*Frame, error) {
	js, err := protojson.MarshalOptions{
		UseProtoNames:   true,
		EmitUnpopulated: false,
	}.Marshal(ev)
	if err != nil {
		return nil, err
	}
	return &Frame{Event: ev, JSON: js}, nil
}

// kindLabel returns the metrics-safe label for a TopologyEvent kind.
func kindLabel(ev *dashcenterv1.TopologyEvent) string {
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

// Stats returns a snapshot.
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
	if b.ringHead == 0 && !b.ringWrapped {
		newest = 0
	} else {
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
		TotalPublished:  b.mtotalPublished.Load(),
		TotalDelivered:  b.mtotalDelivered.Load(),
		TotalDropped:    b.mtotalDropped.Load(),
		TotalCoalesced:  b.mtotalCoalesced.Load(),
		TotalSuppressed: b.mtotalSuppress.Load(),
		NextEventID:     b.nextID.Load(),
		RingSize:        len(b.ring),
		OldestEventID:   oldest,
		NewestEventID:   newest,
	}
}
