// Package cluster implements the dashw multiplexer for dashd's
// ClusterService streaming surface (PE-G7).
//
// Why this package exists
// -----------------------
// Browsers MUST NEVER talk to dashd directly. A naive
// browser→dashd architecture forces dashd to do per-event protojson
// marshalling × N watchers and ties dashd's connection budget to the
// number of human users. dashw is the multiplexer that fixes both:
//
//   - dashw holds exactly ONE upstream gRPC WatchTopology stream to
//     dashd per replica (the Client).
//
//   - dashw's own Hub fans the upstream events out to N downstream
//     SSE/WebSocket clients. Each downstream client gets the SAME
//     pre-decoded bytes (marshal-once-send-many — same pattern as the
//     dashd broadcaster).
//
//   - A ring buffer of the last K events lets reconnecting clients
//     resume from a Last-Event-ID cursor without re-fetching the full
//     snapshot.
//
//   - A 1-second snapshot cache deduplicates the GET /topology fan-out
//     when many tabs mount simultaneously (typical after a deploy or
//     after a reverse proxy bounces).
//
//   - Per-IP and per-tenant connection caps defend against runaway
//     browsers + Prometheus scrape loops.
//
// See docs/dashd-features/topology-streaming-design.md for the full
// architecture + Future Scopes.
package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// ── public errors ────────────────────────────────────────────────────────

// ErrTooManyWatchers is returned by Hub.Subscribe when the per-replica
// or per-IP cap is exhausted. Handlers map this to HTTP 429.
var ErrTooManyWatchers = errors.New("dashw.cluster.Hub: too many watchers")

// ── tunables ─────────────────────────────────────────────────────────────

// HubConfig binds the multiplexer's behaviour. Zero values fall back
// to DefaultHubConfig.
type HubConfig struct {
	// MaxWatchers caps the in-flight downstream SSE/WS connections.
	// Replicas can be scaled horizontally; this is the per-process
	// cap defending against pathological behaviour on one node.
	MaxWatchers int

	// MaxWatchersPerIP caps watchers per source IP (so a single
	// browser tab cannot open more than N concurrent EventSource
	// connections).
	MaxWatchersPerIP int

	// WatcherBufferSize is the per-watcher buffered channel size.
	WatcherBufferSize int

	// RingSize is the number of recent frames retained for
	// Last-Event-ID resume.
	RingSize int

	// SnapshotCacheTTL deduplicates concurrent GetTopology fan-outs.
	// Set to 0 to disable caching.
	SnapshotCacheTTL time.Duration

	// IdleTimeout closes downstream connections that have not received
	// any byte for this duration (defends against abandoned tabs).
	// 0 disables the idle check.
	IdleTimeout time.Duration

	// UpstreamReconnectMin / Max bound the backoff schedule for the
	// upstream gRPC WatchTopology stream.
	UpstreamReconnectMin time.Duration
	UpstreamReconnectMax time.Duration
}

// DefaultHubConfig is the production-ready zero-config.
func DefaultHubConfig() HubConfig {
	return HubConfig{
		MaxWatchers:          512,
		MaxWatchersPerIP:     8,
		WatcherBufferSize:    128,
		RingSize:             2048,
		SnapshotCacheTTL:     1 * time.Second,
		IdleTimeout:          5 * time.Minute,
		UpstreamReconnectMin: 500 * time.Millisecond,
		UpstreamReconnectMax: 15 * time.Second,
	}
}

// SubscribeOptions narrows a Hub.Subscribe call. Mirrors the dashd
// SubscribeOptions but extends with a per-IP key.
type SubscribeOptions struct {
	// ClientID is the per-IP key used by MaxWatchersPerIP. Typically
	// the X-Real-IP / RemoteAddr stripped of port.
	ClientID string

	// SubjectName is the auth subject; counted toward dashd's
	// per-subject cap on the upstream side.
	SubjectName string

	// ResumeAfterEventID, when non-zero, asks the hub to replay
	// events with id > cursor from its local ring buffer. If the
	// cursor is older than the oldest retained event, the watcher
	// receives a KIND_RESYNC notice and MUST re-fetch GetTopology.
	ResumeAfterEventID uint64

	// IncludeEnis controls payload size for snapshot replies + for the
	// upstream stream subscription on first connect. Once the upstream
	// stream is open, this flag is best-effort — the hub multiplexes a
	// single shared stream regardless.
	IncludeEnis bool
}

// Frame is what every downstream watcher receives — the typed event +
// the SHARED pre-decoded JSON bytes.
type Frame struct {
	Event *dashcenterv1.TopologyEvent
	JSON  []byte
}

// Watcher is the handle returned by Hub.Subscribe. Recv() yields
// frames; Cancel() releases resources. TakeDroppedCount + LastDelivered
// mirror the dashd broadcaster surface so handlers can synthesise
// KIND_DROPPED before relaying the next live event.
type Watcher struct {
	w *watcher
}

func (w *Watcher) Recv() <-chan *Frame      { return w.w.ch }
func (w *Watcher) TakeDroppedCount() uint64 { return w.w.dropped.Swap(0) }
func (w *Watcher) LastDelivered() uint64    { return w.w.lastID.Load() }

type watcher struct {
	ch       chan *Frame
	clientID string
	dropped  atomic.Uint64
	lastID   atomic.Uint64
	closedMu sync.Mutex
	closed   bool
}

// ringEntry stores a (id, frame) pair for resume replay.
type ringEntry struct {
	id    uint64
	frame *Frame
}

// snapshotEntry is the cached topology snapshot.
type snapshotEntry struct {
	resp        *dashcenterv1.TopologyResponse
	at          time.Time
	includeEnis bool
}

// Hub is the per-dashw multiplexer. Construct with NewHub; the dashw
// server owns exactly one. Safe for concurrent use.
type Hub struct {
	cfg    HubConfig
	logger *slog.Logger
	cli    ClusterClient // upstream gRPC client

	// watcher map + per-IP counter.
	mu          sync.RWMutex
	watchers    map[*watcher]struct{}
	byClientIP  map[string]int

	// ring buffer for resume.
	ringMu      sync.RWMutex
	ring        []ringEntry
	ringHead    int
	ringWrapped bool
	highest     atomic.Uint64

	// snapshot cache.
	snapMu sync.RWMutex
	snap   *snapshotEntry

	// upstream stream lifecycle.
	upMu       sync.Mutex
	upRunning  bool
	upCancel   context.CancelFunc
	upRestart  atomic.Uint64
	upHealthy  atomic.Bool

	// metrics counters (read by Stats; also fed to Prom in metrics.go).
	mPublished atomic.Uint64
	mDelivered atomic.Uint64
	mDropped   atomic.Uint64
}

// NewHub returns a hub. The upstream stream is NOT started until
// Start(ctx) is called.
func NewHub(cli ClusterClient, cfg HubConfig, logger *slog.Logger) *Hub {
	if cfg.MaxWatchers <= 0 {
		cfg.MaxWatchers = 512
	}
	if cfg.WatcherBufferSize <= 0 {
		cfg.WatcherBufferSize = 128
	}
	if cfg.RingSize <= 0 {
		cfg.RingSize = 2048
	}
	if cfg.SnapshotCacheTTL < 0 {
		cfg.SnapshotCacheTTL = 0
	}
	if cfg.UpstreamReconnectMin <= 0 {
		cfg.UpstreamReconnectMin = 500 * time.Millisecond
	}
	if cfg.UpstreamReconnectMax <= 0 {
		cfg.UpstreamReconnectMax = 15 * time.Second
	}
	return &Hub{
		cfg:        cfg,
		logger:     logger,
		cli:        cli,
		watchers:   map[*watcher]struct{}{},
		byClientIP: map[string]int{},
		ring:       make([]ringEntry, cfg.RingSize),
	}
}

// Start launches the upstream stream goroutine. Returns immediately;
// the goroutine runs until ctx is cancelled. Safe to call multiple
// times — only the first start is effective.
func (h *Hub) Start(ctx context.Context) {
	h.upMu.Lock()
	defer h.upMu.Unlock()
	if h.upRunning {
		return
	}
	streamCtx, cancel := context.WithCancel(ctx)
	h.upCancel = cancel
	h.upRunning = true
	go h.runUpstream(streamCtx)
}

// Stop cancels the upstream stream. Idempotent.
func (h *Hub) Stop() {
	h.upMu.Lock()
	defer h.upMu.Unlock()
	if !h.upRunning {
		return
	}
	if h.upCancel != nil {
		h.upCancel()
	}
	h.upRunning = false
}

// IsHealthy reports whether the upstream stream is currently
// connected. Used by /readyz and /metrics.
func (h *Hub) IsHealthy() bool { return h.upHealthy.Load() }

// ── GetTopology with snapshot cache ──────────────────────────────────────

// GetTopology returns a TopologyResponse, served from the snapshot
// cache when fresh. include_enis=true bypasses the cache (large
// payload).
func (h *Hub) GetTopology(ctx context.Context, includeEnis bool) (*dashcenterv1.TopologyResponse, error) {
	if !includeEnis && h.cfg.SnapshotCacheTTL > 0 {
		h.snapMu.RLock()
		if h.snap != nil && !h.snap.includeEnis && time.Since(h.snap.at) < h.cfg.SnapshotCacheTTL {
			cached := h.snap.resp
			h.snapMu.RUnlock()
			hubMetrics.snapshotCacheHits.WithLabelValues().Inc()
			return cached, nil
		}
		h.snapMu.RUnlock()
	}
	hubMetrics.snapshotCacheMisses.WithLabelValues().Inc()
	resp, err := h.cli.GetTopology(ctx, includeEnis)
	if err != nil {
		return nil, err
	}
	if h.cfg.SnapshotCacheTTL > 0 {
		h.snapMu.Lock()
		h.snap = &snapshotEntry{resp: resp, at: time.Now(), includeEnis: includeEnis}
		h.snapMu.Unlock()
	}
	return resp, nil
}

// ── Subscribe / cancel ───────────────────────────────────────────────────

// Subscribe registers a new downstream watcher. Returns
// ErrTooManyWatchers when the global or per-IP cap is exhausted.
// ResumeAfterEventID is honored locally from the hub's ring; cursors
// older than the ring trigger an immediate KIND_RESYNC notice.
func (h *Hub) Subscribe(opts SubscribeOptions) (*Watcher, func(), error) {
	w := &watcher{
		ch:       make(chan *Frame, h.cfg.WatcherBufferSize),
		clientID: opts.ClientID,
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

	// Resume cursor replay from local ring.
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
	}
	return &Watcher{w: w}, cancel, nil
}

// replayResume copies ring frames with id > cursor into the watcher's
// channel. Stale cursor → KIND_RESYNC.
func (h *Hub) replayResume(w *watcher, cursor uint64) {
	h.ringMu.RLock()
	defer h.ringMu.RUnlock()

	current := h.highest.Load()
	if current <= cursor {
		// Cursor is from a previous process / future ID — resync.
		h.enqueueResyncLocked(w, current, "cursor exceeds current event_id")
		return
	}
	oldest := h.oldestIDLocked()
	if oldest == 0 || cursor < oldest-1 {
		h.enqueueResyncLocked(w, current, "cursor predates ring; refetch GetTopology")
		return
	}
	walk := func(start, end int) {
		for i := start; i < end; i++ {
			entry := h.ring[i]
			if entry.frame == nil || entry.id <= cursor {
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
	ev := &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_RESYNC,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
			Message:        msg,
			CurrentEventId: current,
		}},
	}
	frame, _ := buildFrame(ev)
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

// ── upstream stream owner ────────────────────────────────────────────────

// runUpstream maintains the single gRPC WatchTopology stream to dashd.
// On every disconnect it reconnects with exponential backoff and emits
// a synthetic KIND_RESYNC notice downstream so watchers re-fetch
// GetTopology (the ring's monotonic IDs may have reset).
func (h *Hub) runUpstream(ctx context.Context) {
	backoff := h.cfg.UpstreamReconnectMin
	cursor := uint64(0)
	for {
		if ctx.Err() != nil {
			return
		}
		streamCtx, cancel := context.WithCancel(ctx)
		err := h.runUpstreamOnce(streamCtx, &cursor)
		cancel()
		if ctx.Err() != nil {
			return
		}
		h.upHealthy.Store(false)
		hubMetrics.upstreamConnected.WithLabelValues().Set(0)
		hubMetrics.upstreamReconnects.WithLabelValues().Inc()
		h.upRestart.Add(1)
		// Notify all watchers — server-side state may have changed.
		h.fanoutResync("upstream stream reconnecting")

		if h.logger != nil {
			h.logger.Warn("topology hub: upstream stream ended; reconnecting",
				"error", err, "backoff", backoff)
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

// runUpstreamOnce opens one WatchTopology stream and pumps frames into
// the hub until the stream closes.
func (h *Hub) runUpstreamOnce(ctx context.Context, cursor *uint64) error {
	stream, err := h.cli.WatchTopology(ctx, *cursor, false)
	if err != nil {
		return fmt.Errorf("open upstream: %w", err)
	}
	h.upHealthy.Store(true)
	hubMetrics.upstreamConnected.WithLabelValues().Set(1)
	// Reset backoff in caller via successful first event.
	for {
		ev, err := stream.Recv()
		if err != nil {
			return fmt.Errorf("upstream recv: %w", err)
		}
		// Update cursor for next reconnect.
		if id := ev.GetEventId(); id > *cursor {
			*cursor = id
		}
		// Build the pre-marshalled frame ONCE and fan out.
		frame, ferr := buildFrame(ev)
		if ferr != nil {
			if h.logger != nil {
				h.logger.Warn("hub: marshal failed; dropping", "error", ferr)
			}
			hubMetrics.dropped.WithLabelValues("marshal_error").Inc()
			continue
		}
		h.publish(frame)
	}
}

// publish appends to the ring buffer + fans out to all watchers.
func (h *Hub) publish(f *Frame) {
	if f == nil || f.Event == nil {
		return
	}
	id := f.Event.GetEventId()
	// Snapshot frames carry id=0 (they're cold-start payloads); skip
	// them in the ring so resume-cursor logic stays sane.
	if id == 0 {
		// Still deliver to current watchers (they need the snapshot
		// on cold-start). Resume cursor flow uses GetTopology directly.
	} else {
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
		h.deliver(w, f)
	}
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

// fanoutResync sends a KIND_RESYNC notice to every active watcher.
// Used after an upstream reconnect to tell clients to re-fetch.
func (h *Hub) fanoutResync(msg string) {
	ev := &dashcenterv1.TopologyEvent{
		Kind: dashcenterv1.TopologyEvent_KIND_RESYNC,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.TopologyEvent_Notice{Notice: &dashcenterv1.Notice{
			Message: msg,
		}},
	}
	frame, _ := buildFrame(ev)
	if frame == nil {
		return
	}
	h.mu.RLock()
	wlist := make([]*watcher, 0, len(h.watchers))
	for w := range h.watchers {
		wlist = append(wlist, w)
	}
	h.mu.RUnlock()
	for _, w := range wlist {
		h.deliver(w, frame)
	}
}

// ── stats ────────────────────────────────────────────────────────────────

// Stats is a snapshot of hub activity for /admin/topology or metrics.
type Stats struct {
	Watchers           int
	ByClientIPCount    map[string]int
	TotalPublished     uint64
	TotalDelivered     uint64
	TotalDropped       uint64
	HighestEventID     uint64
	UpstreamReconnects uint64
	UpstreamHealthy    bool
}

func (h *Hub) Stats() Stats {
	h.mu.RLock()
	wc := len(h.watchers)
	by := make(map[string]int, len(h.byClientIP))
	for k, v := range h.byClientIP {
		by[k] = v
	}
	h.mu.RUnlock()
	return Stats{
		Watchers:           wc,
		ByClientIPCount:    by,
		TotalPublished:     h.mPublished.Load(),
		TotalDelivered:     h.mDelivered.Load(),
		TotalDropped:       h.mDropped.Load(),
		HighestEventID:     h.highest.Load(),
		UpstreamReconnects: h.upRestart.Load(),
		UpstreamHealthy:    h.upHealthy.Load(),
	}
}

// ── helpers ──────────────────────────────────────────────────────────────

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

func kindLabel(ev *dashcenterv1.TopologyEvent) string {
	if ev == nil {
		return "nil"
	}
	return strings.ToLower(strings.TrimPrefix(ev.GetKind().String(), "KIND_"))
}

// ── gRPC client helpers ──────────────────────────────────────────────────

// DialClusterService dials dashd and returns a ClusterClient. tls and
// authToken are applied uniformly (mTLS via env certs / bearer via env
// token). Caller owns the close.
func DialClusterService(addr string, insecureFlag bool, dialTimeout time.Duration,
	tlsCertPath, tlsKeyPath, tlsCAPath, bearerToken string,
) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption
	if insecureFlag {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	} else {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}
		if tlsCertPath != "" && tlsKeyPath != "" {
			cert, err := tls.LoadX509KeyPair(tlsCertPath, tlsKeyPath)
			if err != nil {
				return nil, fmt.Errorf("load client cert: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
		if tlsCAPath != "" {
			caBytes, err := os.ReadFile(tlsCAPath)
			if err != nil {
				return nil, fmt.Errorf("read CA: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(caBytes) {
				return nil, fmt.Errorf("CA %s had no certificates", tlsCAPath)
			}
			tlsCfg.RootCAs = pool
		}
		opts = append(opts, grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg)))
	}
	if bearerToken != "" {
		opts = append(opts, grpc.WithPerRPCCredentials(staticToken(bearerToken)))
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	conn, err := grpc.DialContext(dialCtx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", addr, err)
	}
	return conn, nil
}

// staticToken is a grpc.PerRPCCredentials implementation that attaches
// a single bearer token to every outgoing RPC.
type staticToken string

func (t staticToken) GetRequestMetadata(ctx context.Context, _ ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(t)}, nil
}

func (staticToken) RequireTransportSecurity() bool { return false }

// _ ensures the metadata import is used even when bearer auth is
// disabled (so future contributors don't strip it).
var _ = metadata.New
