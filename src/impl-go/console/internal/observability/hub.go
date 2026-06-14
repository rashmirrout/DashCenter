// Package observability implements the dashw counter Hub multiplexer
// (PE-3c / PD-G5).
//
// Why this package exists
// -----------------------
// Browsers MUST NEVER talk to dashd directly (§7 of agent-operating-
// discipline.md). A naive browser→dashd architecture forces dashd to
// do per-event protojson marshalling × N browsers and ties dashd's
// connection budget to the number of human users. dashw is the
// multiplexer that fixes both:
//
//   * Hub holds ONE upstream gRPC GetCounters stream PER subscribed
//     DPU (lazy + GC'd after the last watcher leaves + a configurable
//     idle window). This bounds dashd cost to the set of DPUs anyone
//     is actually watching.
//   * Hub fans the upstream events out to N downstream SSE/WS clients
//     with marshal-once semantics (the same pre-decoded JSON bytes
//     are shared across every matching watcher).
//   * Per-IP + global caps defend against runaway browsers + scraper
//     loops.
//   * Upstream reconnect uses exponential backoff (UpstreamReconnectMin
//     → Max). On every reconnect a KIND_RESYNC notice fans out so
//     watchers re-fetch a snapshot.
//   * source/via byte-splice (PE-G7.1) stamps every frame with the
//     dashd that produced it and the dashw replica that relayed it.
//
// The package is **deliberately structurally similar** to
// console/internal/cluster (§0.2.1 Pattern reconnaissance). The
// post-GA cleanup window (T1.3) will extract the shared parts into a
// generic multiplexer; until then both packages register identically-
// shaped metric sets under different namespaces.
package observability

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── public errors ────────────────────────────────────────────────────────

// ErrTooManyWatchers is returned by Subscribe when global or per-IP
// caps are exhausted. Handlers MUST map this to HTTP 429 + Retry-After.
var ErrTooManyWatchers = errors.New("counter hub: too many watchers")

// ── tunables ────────────────────────────────────────────────────────────

// HubConfig binds the multiplexer's behaviour. Zero values fall back
// to DefaultHubConfig.
type HubConfig struct {
	MaxWatchers          int           // global watcher cap
	MaxWatchersPerIP     int           // per-IP cap (0 = disabled)
	WatcherBufferSize    int           // per-watcher channel depth
	RingSize             int           // resume replay buffer
	UpstreamReconnectMin time.Duration // exponential backoff floor
	UpstreamReconnectMax time.Duration // exponential backoff ceiling
	UpstreamIdleGC       time.Duration // close upstream after last watcher leaves + this
	UpstreamLabel        string        // dashd identity stamped on frames (`source`)
	SelfLabel            string        // dashw identity stamped on frames (`via`)
}

// DefaultHubConfig is the production-ready zero-config.
func DefaultHubConfig() HubConfig {
	return HubConfig{
		MaxWatchers:          512,
		MaxWatchersPerIP:     8,
		WatcherBufferSize:    128,
		RingSize:             1024,
		UpstreamReconnectMin: 500 * time.Millisecond,
		UpstreamReconnectMax: 15 * time.Second,
		UpstreamIdleGC:       30 * time.Second,
	}
}

// CountersClient is the slice of the generated gRPC client the hub
// uses. hub_test.go swaps in a fake.
type CountersClient interface {
	// GetCounters opens the streaming RPC. Caller MUST consume the
	// returned stream until io.EOF / error.
	GetCounters(ctx context.Context, req *dashcenterv1.CounterRequest) (CounterStream, error)
}

// CounterStream is the upstream stream interface. Implementations
// must respect ctx cancellation.
type CounterStream interface {
	Recv() (*dashcenterv1.CounterEvent, error)
}

// SubscribeOptions narrows a Hub.Subscribe call.
type SubscribeOptions struct {
	// ClientID is the per-IP key used by MaxWatchersPerIP.
	ClientID string
	// DpuIDs filters the watcher to a subset of DPUs. Empty = all (the
	// hub will open an upstream per known DPU on demand). Non-empty
	// lazily opens an upstream per DPU that doesn't already have one.
	DpuIDs []string
	// ResumeAfterEventID enables ring replay; cursors older than the
	// ring emit KIND_RESYNC immediately.
	ResumeAfterEventID uint64
}

// Frame is what every downstream watcher receives — the typed event +
// the SHARED pre-decoded JSON bytes (source/via injected once).
type Frame struct {
	Event *dashcenterv1.CounterEvent
	JSON  []byte
}

// Watcher is the public handle returned by Subscribe.
type Watcher struct {
	w *watcher
}

func (w *Watcher) Recv() <-chan *Frame      { return w.w.ch }
func (w *Watcher) TakeDroppedCount() uint64 { return w.w.dropped.Swap(0) }
func (w *Watcher) LastDelivered() uint64    { return w.w.lastID.Load() }

// ── internal types ──────────────────────────────────────────────────────

type watcher struct {
	ch       chan *Frame
	clientID string
	dpuSet   map[string]struct{} // nil = all DPUs
	dropped  atomic.Uint64
	lastID   atomic.Uint64
	closedMu sync.Mutex
	closed   bool
}

type ringEntry struct {
	id    uint64
	frame *Frame
}

// upstream owns one gRPC stream for one DPU id (or for the empty-id
// case "all DPUs"). Started lazily by Subscribe; GC'd by gcLoop after
// UpstreamIdleGC of zero watchers.
type upstream struct {
	dpuID    string
	refcount atomic.Int32   // # of watchers interested in this DPU
	idleAt   atomic.Int64   // unix-nano when refcount last hit 0
	cancel   context.CancelFunc
	doneCh   chan struct{}
}

// Hub is the per-dashw counter multiplexer.
type Hub struct {
	cfg    HubConfig
	logger *slog.Logger
	cli    CountersClient

	// watcher map + per-IP counter.
	mu         sync.RWMutex
	watchers   map[*watcher]struct{}
	byClientIP map[string]int

	// ring buffer for resume replay.
	ringMu      sync.RWMutex
	ring        []ringEntry
	ringHead    int
	ringWrapped bool
	highest     atomic.Uint64

	// per-DPU upstream registry.
	upMu      sync.Mutex
	upstreams map[string]*upstream

	// lifecycle.
	stopCh   chan struct{}
	stopOnce sync.Once
	startOnce sync.Once

	// metrics counters (atomic; mirrored to Prom via metrics.go).
	mPublished atomic.Uint64
	mDelivered atomic.Uint64
	mDropped   atomic.Uint64
}

// NewHub constructs an unstarted Hub. Call Start(ctx) to launch the
// GC goroutine.
func NewHub(cli CountersClient, cfg HubConfig, logger *slog.Logger) *Hub {
	if cfg.MaxWatchers <= 0 {
		cfg.MaxWatchers = 512
	}
	if cfg.WatcherBufferSize <= 0 {
		cfg.WatcherBufferSize = 128
	}
	if cfg.RingSize <= 0 {
		cfg.RingSize = 1024
	}
	if cfg.UpstreamReconnectMin <= 0 {
		cfg.UpstreamReconnectMin = 500 * time.Millisecond
	}
	if cfg.UpstreamReconnectMax <= 0 {
		cfg.UpstreamReconnectMax = 15 * time.Second
	}
	if cfg.UpstreamIdleGC <= 0 {
		cfg.UpstreamIdleGC = 30 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		cfg:        cfg,
		logger:     logger,
		cli:        cli,
		watchers:   map[*watcher]struct{}{},
		byClientIP: map[string]int{},
		ring:       make([]ringEntry, cfg.RingSize),
		upstreams:  map[string]*upstream{},
		stopCh:     make(chan struct{}),
	}
}

// Start launches the per-upstream GC goroutine. Idempotent.
func (h *Hub) Start(ctx context.Context) {
	h.startOnce.Do(func() {
		go h.gcLoop(ctx)
	})
}

// Stop shuts down every upstream and the GC goroutine.
func (h *Hub) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
		h.upMu.Lock()
		for _, up := range h.upstreams {
			if up.cancel != nil {
				up.cancel()
			}
		}
		h.upMu.Unlock()
	})
}

// ── Subscribe / cancel ───────────────────────────────────────────────────

// Subscribe registers a new downstream watcher. Lazily opens an
// upstream per DPU in opts.DpuIDs (empty = "all"; conventionally this
// opens one upstream with empty dpu_ids that receives every DPU).
func (h *Hub) Subscribe(opts SubscribeOptions) (*Watcher, func(), error) {
	w := &watcher{
		ch:       make(chan *Frame, h.cfg.WatcherBufferSize),
		clientID: opts.ClientID,
	}
	if len(opts.DpuIDs) > 0 {
		w.dpuSet = make(map[string]struct{}, len(opts.DpuIDs))
		for _, id := range opts.DpuIDs {
			if id == "" {
				continue
			}
			w.dpuSet[id] = struct{}{}
		}
		if len(w.dpuSet) == 0 {
			w.dpuSet = nil
		}
	}

	h.mu.Lock()
	if len(h.watchers) >= h.cfg.MaxWatchers {
		h.mu.Unlock()
		hubMetrics.subscribeRejected.WithLabelValues("global").Inc()
		return nil, nil, fmt.Errorf("%w: global cap=%d reached", ErrTooManyWatchers, h.cfg.MaxWatchers)
	}
	if h.cfg.MaxWatchersPerIP > 0 && opts.ClientID != "" {
		if h.byClientIP[opts.ClientID] >= h.cfg.MaxWatchersPerIP {
			h.mu.Unlock()
			hubMetrics.subscribeRejected.WithLabelValues("per_ip").Inc()
			return nil, nil, fmt.Errorf("%w: per-IP cap=%d reached for %s", ErrTooManyWatchers, h.cfg.MaxWatchersPerIP, opts.ClientID)
		}
		h.byClientIP[opts.ClientID]++
	}
	h.watchers[w] = struct{}{}
	count := len(h.watchers)
	h.mu.Unlock()
	hubMetrics.watchers.WithLabelValues().Set(float64(count))

	// Open / refcount upstream(s).
	h.refUpstreams(w)

	// Resume replay (after refUpstreams so the ring is current).
	if opts.ResumeAfterEventID > 0 {
		h.replayResume(w, opts.ResumeAfterEventID)
	}

	cancel := func() {
		w.closedMu.Lock()
		if !w.closed {
			w.closed = true
			close(w.ch)
		}
		w.closedMu.Unlock()
		h.mu.Lock()
		if _, ok := h.watchers[w]; ok {
			delete(h.watchers, w)
			if w.clientID != "" {
				if c := h.byClientIP[w.clientID]; c > 1 {
					h.byClientIP[w.clientID] = c - 1
				} else {
					delete(h.byClientIP, w.clientID)
				}
			}
		}
		newCount := len(h.watchers)
		h.mu.Unlock()
		hubMetrics.watchers.WithLabelValues().Set(float64(newCount))
		h.unrefUpstreams(w)
	}
	return &Watcher{w: w}, cancel, nil
}

// refUpstreams increments refcount for every DPU the watcher needs;
// opens new upstreams lazily. An "all DPUs" watcher (dpuSet=nil)
// refcounts the empty-key upstream.
func (h *Hub) refUpstreams(w *watcher) {
	keys := h.upstreamKeysFor(w)
	for _, key := range keys {
		h.upMu.Lock()
		up, ok := h.upstreams[key]
		if !ok {
			ctx, cancel := context.WithCancel(context.Background())
			up = &upstream{
				dpuID:  key,
				cancel: cancel,
				doneCh: make(chan struct{}),
			}
			h.upstreams[key] = up
			go h.runUpstream(ctx, up)
		}
		up.refcount.Add(1)
		up.idleAt.Store(0) // active
		h.upMu.Unlock()
	}
}

// unrefUpstreams decrements refcount. The GC goroutine closes upstream
// after UpstreamIdleGC of refcount=0.
func (h *Hub) unrefUpstreams(w *watcher) {
	keys := h.upstreamKeysFor(w)
	now := time.Now().UnixNano()
	for _, key := range keys {
		h.upMu.Lock()
		up, ok := h.upstreams[key]
		h.upMu.Unlock()
		if !ok {
			continue
		}
		if r := up.refcount.Add(-1); r == 0 {
			up.idleAt.Store(now)
		}
	}
}

// upstreamKeysFor returns the upstream keys this watcher is interested
// in. nil dpuSet → ["" ] (the "all DPUs" sentinel key).
func (h *Hub) upstreamKeysFor(w *watcher) []string {
	if w.dpuSet == nil {
		return []string{""}
	}
	out := make([]string, 0, len(w.dpuSet))
	for k := range w.dpuSet {
		out = append(out, k)
	}
	return out
}

// gcLoop closes upstreams whose refcount has been 0 for longer than
// UpstreamIdleGC. Ticks every UpstreamIdleGC/3 (min 10ms — keeps the
// loop responsive in tests and inexpensive in production where the
// default is 30s/3 = 10s).
func (h *Hub) gcLoop(ctx context.Context) {
	tick := h.cfg.UpstreamIdleGC / 3
	if tick < 10*time.Millisecond {
		tick = 10 * time.Millisecond
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.stopCh:
			return
		case <-t.C:
			h.gcOnce(time.Now().UnixNano())
		}
	}
}

func (h *Hub) gcOnce(nowNanos int64) {
	cutoff := nowNanos - int64(h.cfg.UpstreamIdleGC)
	var toClose []*upstream
	h.upMu.Lock()
	for key, up := range h.upstreams {
		if up.refcount.Load() == 0 {
			idle := up.idleAt.Load()
			if idle > 0 && idle <= cutoff {
				toClose = append(toClose, up)
				delete(h.upstreams, key)
			}
		}
	}
	h.upMu.Unlock()
	for _, up := range toClose {
		if up.cancel != nil {
			up.cancel()
		}
		<-up.doneCh
	}
}

// runUpstream maintains one gRPC GetCounters stream for `up.dpuID`.
// On disconnect, exponential backoff + reconnect + fanoutResync(dpuID).
// Exits when ctx is cancelled (Stop or GC).
func (h *Hub) runUpstream(ctx context.Context, up *upstream) {
	defer close(up.doneCh)
	backoff := h.cfg.UpstreamReconnectMin
	cursor := uint64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		err := h.runUpstreamOnce(ctx, up, &cursor)
		if ctx.Err() != nil {
			return
		}
		hubMetrics.upstreamReconnects.WithLabelValues().Inc()
		h.fanoutResyncForDpu(up.dpuID, "upstream stream reconnecting")
		if h.logger != nil {
			h.logger.Warn("counter hub: upstream ended; reconnecting",
				"dpu", up.dpuID, "error", err, "backoff", backoff)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > h.cfg.UpstreamReconnectMax {
			backoff = h.cfg.UpstreamReconnectMax
		}
	}
}

func (h *Hub) runUpstreamOnce(ctx context.Context, up *upstream, cursor *uint64) error {
	req := &dashcenterv1.CounterRequest{
		Follow:             true,
		ResumeAfterEventId: *cursor,
	}
	if up.dpuID != "" {
		req.DpuIds = []string{up.dpuID}
	}
	stream, err := h.cli.GetCounters(ctx, req)
	if err != nil {
		return fmt.Errorf("open upstream %q: %w", up.dpuID, err)
	}
	hubMetrics.upstreamConnected.WithLabelValues().Inc()
	defer hubMetrics.upstreamConnected.WithLabelValues().Dec()
	for {
		ev, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("recv: %w", err)
		}
		if id := ev.GetEventId(); id > *cursor {
			*cursor = id
		}
		frame, ferr := h.buildFrame(ev)
		if ferr != nil {
			hubMetrics.dropped.WithLabelValues("marshal_error").Inc()
			continue
		}
		h.publish(frame, up.dpuID)
	}
}

// publish appends to ring and fans out to matching watchers.
// originDpu is the dpu_id of the upstream that produced this frame
// (empty = "all DPUs" upstream); used to deliver only to watchers
// interested in that DPU.
func (h *Hub) publish(f *Frame, originDpu string) {
	if f == nil || f.Event == nil {
		return
	}
	id := f.Event.GetEventId()
	if id > 0 {
		h.ringMu.Lock()
		h.ring[h.ringHead] = ringEntry{id: id, frame: f}
		h.ringHead++
		if h.ringHead >= len(h.ring) {
			h.ringHead = 0
			h.ringWrapped = true
		}
		if id > h.highest.Load() {
			h.highest.Store(id)
		}
		h.ringMu.Unlock()
		h.mPublished.Add(1)
		hubMetrics.published.WithLabelValues(kindLabel(f.Event)).Inc()
	}
	h.mu.RLock()
	wlist := make([]*watcher, 0, len(h.watchers))
	for w := range h.watchers {
		wlist = append(wlist, w)
	}
	h.mu.RUnlock()
	for _, w := range wlist {
		if !h.watcherWants(w, f, originDpu) {
			continue
		}
		h.deliver(w, f)
	}
}

// watcherWants reports whether watcher w should receive frame f given
// the originating upstream's dpu key.
//
// Logic:
//   * Sentinel frames (no Report body) ALWAYS go to every watcher who
//     subscribed to the originating upstream's key set. We cannot know
//     which DPUs a KEEPALIVE applies to, so we deliver to anyone whose
//     dpuSet is nil OR contains the originDpu (if non-empty).
//   * Report frames are filtered by Report.dpu_id against watcher's set.
func (h *Hub) watcherWants(w *watcher, f *Frame, originDpu string) bool {
	if w.dpuSet == nil {
		return true // "all DPUs" watcher
	}
	report := f.Event.GetReport()
	if report != nil {
		_, ok := w.dpuSet[report.GetDpuId()]
		return ok
	}
	// Sentinel: deliver if watcher subscribed to the upstream's key.
	if originDpu == "" {
		return false // empty-key upstream sentinel for an "all" watcher only
	}
	_, ok := w.dpuSet[originDpu]
	return ok
}

func (h *Hub) deliver(w *watcher, f *Frame) {
	w.closedMu.Lock()
	defer w.closedMu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.ch <- f:
		hubMetrics.delivered.WithLabelValues().Inc()
		h.mDelivered.Add(1)
		w.lastID.Store(f.Event.GetEventId())
	default:
		w.dropped.Add(1)
		hubMetrics.dropped.WithLabelValues("buffer_full").Inc()
		h.mDropped.Add(1)
	}
}

// fanoutResyncForDpu emits a KIND_RESYNC notice to every watcher whose
// dpu filter includes originDpu (or has no filter).
func (h *Hub) fanoutResyncForDpu(originDpu, msg string) {
	ev := &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_RESYNC,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			Message: msg,
		}},
	}
	frame, _ := h.buildFrame(ev)
	if frame == nil {
		return
	}
	h.publish(frame, originDpu)
}

// ── resume ──────────────────────────────────────────────────────────────

func (h *Hub) replayResume(w *watcher, cursor uint64) {
	h.ringMu.RLock()
	defer h.ringMu.RUnlock()
	current := h.highest.Load()
	if current <= cursor {
		h.enqueueResyncLocked(w, current, "cursor exceeds current event_id")
		return
	}
	oldest := h.oldestIDLocked()
	if oldest == 0 || cursor < oldest-1 {
		h.enqueueResyncLocked(w, current, "cursor predates ring; refetch snapshot")
		return
	}
	walk := func(start, end int) {
		for i := start; i < end; i++ {
			entry := h.ring[i]
			if entry.frame == nil || entry.id <= cursor {
				continue
			}
			if !h.watcherWants(w, entry.frame, "") {
				continue
			}
			h.enqueueLocked(w, entry.frame)
		}
	}
	if h.ringWrapped {
		walk(h.ringHead, len(h.ring))
	}
	walk(0, h.ringHead)
}

func (h *Hub) oldestIDLocked() uint64 {
	if !h.ringWrapped {
		if h.ringHead == 0 {
			return 0
		}
		return h.ring[0].id
	}
	return h.ring[h.ringHead].id
}

func (h *Hub) enqueueResyncLocked(w *watcher, current uint64, msg string) {
	ev := &dashcenterv1.CounterEvent{
		Kind: dashcenterv1.CounterEvent_KIND_RESYNC,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.CounterEvent_Notice{Notice: &dashcenterv1.Notice{
			Message:        msg,
			CurrentEventId: current,
		}},
	}
	frame, _ := h.buildFrame(ev)
	h.enqueueLocked(w, frame)
}

func (h *Hub) enqueueLocked(w *watcher, f *Frame) {
	w.closedMu.Lock()
	defer w.closedMu.Unlock()
	if w.closed {
		return
	}
	select {
	case w.ch <- f:
		hubMetrics.delivered.WithLabelValues().Inc()
		h.mDelivered.Add(1)
		w.lastID.Store(f.Event.GetEventId())
	default:
		w.dropped.Add(1)
		hubMetrics.dropped.WithLabelValues("resume_replay").Inc()
		h.mDropped.Add(1)
	}
}

// ── frame builder + source/via splice ───────────────────────────────────

func (h *Hub) buildFrame(ev *dashcenterv1.CounterEvent) (*Frame, error) {
	js, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}.Marshal(ev)
	if err != nil {
		return nil, err
	}
	js = injectSourceVia(js, h.cfg.UpstreamLabel, h.cfg.SelfLabel)
	return &Frame{Event: ev, JSON: js}, nil
}

// injectSourceVia mirrors cluster.injectSourceVia exactly (PE-G7.1).
// Splices `,"source":"X","via":"Y"` before the trailing `}` of a
// protojson object. Empty labels passthrough cleanly.
func injectSourceVia(js []byte, source, via string) []byte {
	if source == "" && via == "" {
		return js
	}
	n := len(js)
	if n < 2 || js[n-1] != '}' {
		return js
	}
	buf := make([]byte, 0, n+len(source)+len(via)+24)
	buf = append(buf, js[:n-1]...)
	if n > 2 {
		buf = append(buf, ',')
	}
	wrote := false
	if source != "" {
		buf = append(buf, `"source":`...)
		buf = strconv.AppendQuote(buf, source)
		wrote = true
	}
	if via != "" {
		if wrote {
			buf = append(buf, ',')
		}
		buf = append(buf, `"via":`...)
		buf = strconv.AppendQuote(buf, via)
	}
	buf = append(buf, '}')
	return buf
}

func kindLabel(ev *dashcenterv1.CounterEvent) string {
	if ev == nil {
		return "nil"
	}
	return strings.ToLower(strings.TrimPrefix(ev.GetKind().String(), "KIND_"))
}

// ── Stats ───────────────────────────────────────────────────────────────

type Stats struct {
	Watchers       int
	UpstreamCount  int
	TotalPublished uint64
	TotalDelivered uint64
	TotalDropped   uint64
	NewestEventID  uint64
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	w := len(h.watchers)
	h.mu.RUnlock()
	h.upMu.Lock()
	u := len(h.upstreams)
	h.upMu.Unlock()
	return Stats{
		Watchers:       w,
		UpstreamCount:  u,
		TotalPublished: h.mPublished.Load(),
		TotalDelivered: h.mDelivered.Load(),
		TotalDropped:   h.mDropped.Load(),
		NewestEventID:  h.highest.Load(),
	}
}

// LatestPerDpu walks the ring buffer and returns the most-recent
// CounterReport for each dpu_id. Used by HTTP snapshot endpoints
// (the Hub does not proactively pull from dashd on demand). Returns
// reports sorted by dpu_id for stable output.
func (h *Hub) LatestPerDpu() []*dashcenterv1.CounterReport {
	h.ringMu.RLock()
	defer h.ringMu.RUnlock()
	byDpu := map[string]ringEntry{}
	consider := func(e ringEntry) {
		if e.frame == nil || e.frame.Event == nil {
			return
		}
		rep := e.frame.Event.GetReport()
		if rep == nil || rep.GetDpuId() == "" {
			return
		}
		cur, ok := byDpu[rep.GetDpuId()]
		if !ok || e.id > cur.id {
			byDpu[rep.GetDpuId()] = e
		}
	}
	walk := func(start, end int) {
		for i := start; i < end; i++ {
			consider(h.ring[i])
		}
	}
	if h.ringWrapped {
		walk(h.ringHead, len(h.ring))
	}
	walk(0, h.ringHead)
	out := make([]*dashcenterv1.CounterReport, 0, len(byDpu))
	for _, e := range byDpu {
		out = append(out, e.frame.Event.GetReport())
	}
	// Stable sort by dpu_id (avoid map iteration noise in clients).
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].GetDpuId() < out[i].GetDpuId() {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
