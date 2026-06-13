// Package cluster provides the dashd peer-membership registry and the
// pure-function topology aggregator that powers the
// dashcenter.v1.ClusterService surface.
//
// This file (registry.go) is the membership half: every dashd process
// publishes its own PeerInfo under its OWN etcd lease, watches the
// /peers/<id> prefix to keep an in-memory peer map, and surfaces
// add/remove/update events to subscribers.
//
// Design choices (per docs/dashd-features/cluster-topology-design.md):
//
//   - Independent failure domain: this package holds its own
//     *clientv3.Client + lease. Losing leadership in the elector does
//     NOT depublish this node from the peer registry, and a registry
//     etcd outage does NOT crash the elector.
//
//   - Crash-safe semantics: the peer key is bound to the lease; process
//     death → lease expiry within 1×TTL → key auto-deleted → every peer's
//     WATCH fires within ms. Graceful Close DELETEs the key explicitly
//     first so peers see us disappear immediately rather than waiting TTL.
//
//   - Single-writer watch loop: peers map is mutated only from one
//     goroutine (runWatch). Reads (Snapshot) take an RLock. Subscribers'
//     OnChange callbacks fire from the watch goroutine — they MUST be
//     non-blocking (long work goes on the caller's own goroutine).
//
//   - Self-only fallback: when no etcd endpoints are configured (e.g.
//     `mode: file` single-node), OpenSelfOnly returns a registry that
//     exposes only `self` and emits no events. The aggregator works
//     unchanged.
package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

// PeerInfo is the payload published under /peers/<node_id>. The JSON
// shape is the public wire format on etcd — bump a `schema_version`
// field here if it ever changes shape.
type PeerInfo struct {
	NodeID    string            `json:"node_id"`
	RESTAddr  string            `json:"rest_addr"`
	GRPCAddr  string            `json:"grpc_addr"`
	AdminAddr string            `json:"admin_addr"`
	Version   string            `json:"version"`
	BuildSHA  string            `json:"build_sha,omitempty"`
	StartedAt time.Time         `json:"started_at"`
	Labels    map[string]string `json:"labels,omitempty"`
}

// TLSConfig carries the same TLS material the leader elector + storage
// backend accept. Empty CertFile + CAFile = plaintext.
type TLSConfig struct {
	CertFile string
	KeyFile  string
	CAFile   string
}

// Config is what Open needs to dial etcd and publish self. KeyPrefix
// is the FULL prefix the peers live under; the registry appends the
// node_id directly. Example: "/dashd/console/peers/" produces
// "/dashd/console/peers/dashd-1".
type Config struct {
	Endpoints   []string
	KeyPrefix   string
	DialTimeout time.Duration // default 5s
	LeaseTTL    time.Duration // default 8s; min 1s (etcd lower bound)
	TLS         *TLSConfig
}

// ChangeKind is the discriminator for OnChange events.
type ChangeKind int

const (
	ChangeAdded ChangeKind = iota + 1
	ChangeRemoved
	ChangeUpdated
)

// String makes log lines readable.
func (k ChangeKind) String() string {
	switch k {
	case ChangeAdded:
		return "added"
	case ChangeRemoved:
		return "removed"
	case ChangeUpdated:
		return "updated"
	}
	return "unknown"
}

// OnChange is the subscriber callback signature. Invoked from the
// registry's single watch goroutine — implementations MUST be
// non-blocking (defer heavy work to a separate goroutine, or use a
// buffered channel).
type OnChange func(kind ChangeKind, peer PeerInfo)

// Registry is the live peer-membership view. Safe for concurrent use.
// Construct via Open (production) or OpenSelfOnly (no-etcd single-node).
type Registry struct {
	self      PeerInfo
	keyPrefix string

	// cli + lease are nil when running in self-only mode.
	cli      *clientv3.Client
	leaseID  clientv3.LeaseID
	watchCtx context.Context
	cancel   context.CancelFunc

	mu    sync.RWMutex
	peers map[string]PeerInfo
	subs  []OnChange

	// running guards watch goroutine lifecycle. Closed by Close().
	running chan struct{}

	// onceClose makes Close idempotent.
	onceClose sync.Once
}

// Open dials etcd, grants a lease, publishes self under the lease, and
// starts the watch goroutine. Returns an error if any of those fail —
// callers (main.go) can fall back to OpenSelfOnly to keep dashd up
// without cluster visibility.
//
// The supplied ctx is used only for the initial dial + Put + Watch
// setup. The registry's background loops run on their own internal
// context that is cancelled by Close.
func Open(ctx context.Context, cfg Config, self PeerInfo) (*Registry, error) {
	if err := validateConfig(cfg, self); err != nil {
		return nil, err
	}
	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = 8 * time.Second
	}
	ttlSec := int64(leaseTTL.Round(time.Second).Seconds())
	if ttlSec < 1 {
		ttlSec = 1
	}

	clientCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: dialTimeout,
	}
	if cfg.TLS != nil && (cfg.TLS.CertFile != "" || cfg.TLS.CAFile != "") {
		tlsCfg, err := buildTLS(*cfg.TLS)
		if err != nil {
			return nil, fmt.Errorf("cluster.Open: TLS: %w", err)
		}
		clientCfg.TLS = tlsCfg
	}
	cli, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("cluster.Open: dial %v: %w", cfg.Endpoints, err)
	}

	// Fail-fast probe (clientv3.New connects lazily; lease ops below
	// would hang on a dead endpoint). Use a bounded ctx tied to the
	// caller's DialTimeout.
	probeCtx, probeCancel := context.WithTimeout(ctx, dialTimeout)
	if _, err := cli.Get(probeCtx, "/dashd-probe", clientv3.WithLimit(1)); err != nil {
		probeCancel()
		_ = cli.Close()
		return nil, fmt.Errorf("cluster.Open: probe %v: %w", cfg.Endpoints, err)
	}
	probeCancel()

	// Grant lease + start keep-alive. KeepAlive returns a channel that
	// we must drain — etcd documents that an undrained KeepAlive
	// channel will eventually back-pressure the lease keep-alive
	// goroutine and the lease will expire. We discard the responses
	// in a small drain goroutine bound to the registry's lifetime.
	grantCtx, grantCancel := context.WithTimeout(ctx, dialTimeout)
	lease, err := cli.Grant(grantCtx, ttlSec)
	grantCancel()
	if err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("cluster.Open: grant lease: %w", err)
	}

	// Internal long-lived context for keep-alive + watch.
	watchCtx, cancel := context.WithCancel(context.Background())

	kaCh, err := cli.KeepAlive(watchCtx, lease.ID)
	if err != nil {
		cancel()
		_ = cli.Close()
		return nil, fmt.Errorf("cluster.Open: KeepAlive: %w", err)
	}

	// PUT self under the lease. If this fails we tear everything down
	// (including the lease) — partial publication is worse than a
	// clean error to main.go.
	if self.StartedAt.IsZero() {
		self.StartedAt = time.Now().UTC()
	}
	if self.NodeID == "" {
		// Shouldn't happen — validateConfig caught it. Belt + braces.
		cancel()
		_ = cli.Close()
		return nil, errors.New("cluster.Open: self.NodeID is empty")
	}
	jsonSelf, err := json.Marshal(self)
	if err != nil {
		cancel()
		_ = cli.Close()
		return nil, fmt.Errorf("cluster.Open: marshal self: %w", err)
	}
	putCtx, putCancel := context.WithTimeout(ctx, dialTimeout)
	key := peerKey(cfg.KeyPrefix, self.NodeID)
	if _, err := cli.Put(putCtx, key, string(jsonSelf), clientv3.WithLease(lease.ID)); err != nil {
		putCancel()
		_, _ = cli.Revoke(context.Background(), lease.ID)
		cancel()
		_ = cli.Close()
		return nil, fmt.Errorf("cluster.Open: put %s: %w", key, err)
	}
	putCancel()

	r := &Registry{
		self:      self,
		keyPrefix: cfg.KeyPrefix,
		cli:       cli,
		leaseID:   lease.ID,
		watchCtx:  watchCtx,
		cancel:    cancel,
		peers:     map[string]PeerInfo{self.NodeID: self}, // seed with self
		running:   make(chan struct{}),
	}

	// Drain keep-alive responses; log when the lease is lost so
	// operators see it.
	go r.drainKeepAlive(kaCh)

	// Initial Get of all existing peers + start the watch.
	if err := r.seedAndWatch(ctx, dialTimeout); err != nil {
		// seed failure isn't fatal — the watch loop will retry from
		// inside. Log loudly and continue with self-only view.
		slog.Warn("cluster: initial seed failed; continuing with self-only view, watch will retry",
			"error", err, "node_id", self.NodeID)
	}

	slog.Info("cluster: registry open",
		"node_id", self.NodeID,
		"key", key,
		"lease_id", lease.ID,
		"ttl_seconds", ttlSec,
		"endpoints", cfg.Endpoints)
	return r, nil
}

// OpenSelfOnly returns a no-etcd registry that exposes only `self`. Used
// by main.go when storage.backend != etcd, or as the fallback when
// Open fails and the operator still wants dashd up.
func OpenSelfOnly(self PeerInfo) *Registry {
	if self.StartedAt.IsZero() {
		self.StartedAt = time.Now().UTC()
	}
	return &Registry{
		self:    self,
		peers:   map[string]PeerInfo{self.NodeID: self},
		running: make(chan struct{}),
	}
}

// Self returns the published PeerInfo.
func (r *Registry) Self() PeerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.self
}

// Snapshot returns a sorted, deterministic copy of the current peer map.
// O(N) in the peer count; safe to call from any goroutine.
func (r *Registry) Snapshot() []PeerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PeerInfo, 0, len(r.peers))
	for _, p := range r.peers {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

// PeerCount returns the live peer count (including self).
func (r *Registry) PeerCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.peers)
}

// Subscribe registers an OnChange callback. Returns an unsubscribe
// function. The callback fires from the registry's watch goroutine
// and MUST be non-blocking (long work belongs on a goroutine the
// subscriber owns).
func (r *Registry) Subscribe(fn OnChange) func() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subs = append(r.subs, fn)
	idx := len(r.subs) - 1
	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		// Replace the slot with a no-op rather than re-slicing —
		// keeps idx-based bookkeeping stable for other subscribers.
		if idx < len(r.subs) {
			r.subs[idx] = nil
		}
	}
}

// Close DELETEs self from etcd, revokes the lease, cancels the watch,
// and closes the client. Idempotent. Returns the first non-nil error
// encountered (later errors are logged but not surfaced).
func (r *Registry) Close() error {
	var firstErr error
	r.onceClose.Do(func() {
		// Signal that we're stopping so the watch goroutine drains
		// before we close the client. running is closed by the watch
		// goroutine after it observes ctx cancel.
		if r.cli != nil {
			// Explicit DELETE so peers see us disappear instantly
			// rather than waiting for the lease to time out. Use a
			// short bounded context — shutdown must not hang on
			// etcd being unreachable.
			delCtx, delCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := r.cli.Delete(delCtx, peerKey(r.keyPrefix, r.self.NodeID)); err != nil {
				slog.Warn("cluster.Close: explicit Delete failed (proceeding with Revoke)",
					"error", err, "node_id", r.self.NodeID)
				if firstErr == nil {
					firstErr = err
				}
			}
			delCancel()

			// Revoke the lease — belt-and-suspenders. Even if the
			// Delete above succeeded, Revoke makes the lease ID
			// invalid immediately so any future KeepAlive returns
			// instead of looping.
			revCtx, revCancel := context.WithTimeout(context.Background(), 2*time.Second)
			if _, err := r.cli.Revoke(revCtx, r.leaseID); err != nil {
				slog.Warn("cluster.Close: Revoke failed (proceeding)", "error", err)
				if firstErr == nil {
					firstErr = err
				}
			}
			revCancel()
		}

		// Stop the watch + keep-alive loops.
		if r.cancel != nil {
			r.cancel()
		}

		// Wait briefly for the watch goroutine to finish so the
		// client close below doesn't race with in-flight RPCs.
		if r.cli != nil {
			select {
			case <-r.running:
			case <-time.After(2 * time.Second):
				slog.Warn("cluster.Close: watch goroutine did not stop within 2s; forcing client close")
			}
			if err := r.cli.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		} else {
			// Self-only registry: nothing to wait for, just signal done.
			close(r.running)
		}
	})
	return firstErr
}

// ── internals ───────────────────────────────────────────────────────────

func validateConfig(cfg Config, self PeerInfo) error {
	if len(cfg.Endpoints) == 0 {
		return errors.New("cluster.Open: at least one etcd endpoint is required (use OpenSelfOnly for no-etcd mode)")
	}
	if cfg.KeyPrefix == "" {
		return errors.New("cluster.Open: KeyPrefix is required")
	}
	if !strings.HasSuffix(cfg.KeyPrefix, "/") {
		return fmt.Errorf("cluster.Open: KeyPrefix must end with '/', got %q", cfg.KeyPrefix)
	}
	if self.NodeID == "" {
		return errors.New("cluster.Open: self.NodeID is required")
	}
	// Reject node IDs containing path separators — they'd let a node
	// publish under another node's key by accident.
	if strings.ContainsAny(self.NodeID, "/ ") {
		return fmt.Errorf("cluster.Open: self.NodeID %q must not contain '/' or whitespace", self.NodeID)
	}
	return nil
}

func peerKey(prefix, id string) string {
	return path.Clean(prefix + id)
}

// seedAndWatch does the initial Get-all-peers and starts the
// background watch loop. The watch loop calls itself on error so a
// transient etcd glitch doesn't lose the subscription permanently.
func (r *Registry) seedAndWatch(ctx context.Context, dialTimeout time.Duration) error {
	seedCtx, seedCancel := context.WithTimeout(ctx, dialTimeout)
	resp, err := r.cli.Get(seedCtx, r.keyPrefix, clientv3.WithPrefix())
	seedCancel()
	if err != nil {
		go r.runWatch(0) // watch loop will retry the initial Get
		return fmt.Errorf("seed Get: %w", err)
	}
	for _, kv := range resp.Kvs {
		var p PeerInfo
		if err := json.Unmarshal(kv.Value, &p); err != nil {
			slog.Warn("cluster: seed decode failed", "key", string(kv.Key), "error", err)
			continue
		}
		if p.NodeID == r.self.NodeID {
			continue // already seeded with our own canonical copy
		}
		r.mu.Lock()
		r.peers[p.NodeID] = p
		r.mu.Unlock()
	}
	go r.runWatch(resp.Header.Revision + 1)
	return nil
}

// runWatch is the background watch loop. Restarts on any error. Closes
// r.running when the parent context is cancelled.
func (r *Registry) runWatch(startRev int64) {
	defer close(r.running)

	backoff := 250 * time.Millisecond
	const maxBackoff = 5 * time.Second

	for {
		if err := r.watchCtx.Err(); err != nil {
			return
		}

		opts := []clientv3.OpOption{clientv3.WithPrefix()}
		if startRev > 0 {
			opts = append(opts, clientv3.WithRev(startRev))
		}
		ch := r.cli.Watch(r.watchCtx, r.keyPrefix, opts...)

		// Consume until the channel closes (error or cancel).
		for ev := range ch {
			if err := ev.Err(); err != nil {
				slog.Warn("cluster: watch error, will resync",
					"error", err, "key_prefix", r.keyPrefix)
				break // outer for re-establishes from a fresh revision
			}
			for _, kv := range ev.Events {
				r.handleEvent(kv)
			}
		}

		// If parent context cancelled, exit cleanly.
		if r.watchCtx.Err() != nil {
			return
		}

		// Resync: re-Get the prefix and restart Watch from the new
		// revision. Mirrors the pattern used by store/etcd's
		// consumeWatch.
		newRev, err := r.resync()
		if err != nil {
			slog.Warn("cluster: resync failed, backing off", "error", err, "backoff", backoff)
			select {
			case <-r.watchCtx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = 250 * time.Millisecond
		startRev = newRev
	}
}

// resync re-Gets the prefix and reconciles the in-memory peers map.
// Emits OnChange events for anything that changed since the last view.
// Returns the etcd revision to resume Watch from.
func (r *Registry) resync() (int64, error) {
	getCtx, getCancel := context.WithTimeout(r.watchCtx, 5*time.Second)
	resp, err := r.cli.Get(getCtx, r.keyPrefix, clientv3.WithPrefix())
	getCancel()
	if err != nil {
		return 0, err
	}

	newPeers := make(map[string]PeerInfo, len(resp.Kvs)+1)
	newPeers[r.self.NodeID] = r.self
	for _, kv := range resp.Kvs {
		var p PeerInfo
		if err := json.Unmarshal(kv.Value, &p); err != nil {
			slog.Warn("cluster: resync decode failed", "key", string(kv.Key), "error", err)
			continue
		}
		newPeers[p.NodeID] = p
	}

	// Compute diff vs current map and fire callbacks.
	r.mu.Lock()
	old := r.peers
	r.peers = newPeers
	subs := append([]OnChange(nil), r.subs...)
	r.mu.Unlock()

	for id, p := range newPeers {
		op, existed := old[id]
		switch {
		case !existed:
			fireSubs(subs, ChangeAdded, p)
		case !peerEqual(op, p):
			fireSubs(subs, ChangeUpdated, p)
		}
	}
	for id, op := range old {
		if _, still := newPeers[id]; !still {
			fireSubs(subs, ChangeRemoved, op)
		}
	}

	return resp.Header.Revision + 1, nil
}

// handleEvent applies a single etcd watch event to the in-memory map
// and fires subscribers.
func (r *Registry) handleEvent(ev *clientv3.Event) {
	// Self events are ignored — the canonical local copy of `self`
	// trumps anything coming back through the watch.
	keyStr := string(ev.Kv.Key)
	id := strings.TrimPrefix(keyStr, r.keyPrefix)
	if id == r.self.NodeID {
		return
	}

	switch ev.Type {
	case clientv3.EventTypePut:
		var p PeerInfo
		if err := json.Unmarshal(ev.Kv.Value, &p); err != nil {
			slog.Warn("cluster: event decode failed", "key", keyStr, "error", err)
			return
		}
		r.mu.Lock()
		old, existed := r.peers[p.NodeID]
		r.peers[p.NodeID] = p
		subs := append([]OnChange(nil), r.subs...)
		r.mu.Unlock()
		if !existed {
			fireSubs(subs, ChangeAdded, p)
		} else if !peerEqual(old, p) {
			fireSubs(subs, ChangeUpdated, p)
		}

	case clientv3.EventTypeDelete:
		r.mu.Lock()
		old, existed := r.peers[id]
		if existed {
			delete(r.peers, id)
		}
		subs := append([]OnChange(nil), r.subs...)
		r.mu.Unlock()
		if existed {
			fireSubs(subs, ChangeRemoved, old)
		}
	}
}

// drainKeepAlive consumes the keep-alive response channel. When the
// channel closes (lease lost / etcd unreachable past TTL), we log
// loudly — the watch loop will resync and re-Put on the next tick
// once etcd comes back, but during the outage the peer key on etcd
// has expired and OTHER nodes will have removed us from their map.
//
// Re-publication is handled implicitly: when seedAndWatch's resync
// notices our key is gone and we still believe we're "self", we re-PUT
// in a future improvement. For PE-G6 v1 we rely on operator restart
// to recover from total etcd loss — same semantic as the elector.
func (r *Registry) drainKeepAlive(ch <-chan *clientv3.LeaseKeepAliveResponse) {
	for resp := range ch {
		// Just drain; etcd uses this for back-pressure detection.
		_ = resp
	}
	if r.watchCtx.Err() == nil {
		// Channel closed by etcd, not by us → lease lost mid-flight.
		slog.Warn("cluster: lease keep-alive channel closed; lease likely expired",
			"node_id", r.self.NodeID, "lease_id", r.leaseID)
	}
}

func fireSubs(subs []OnChange, kind ChangeKind, p PeerInfo) {
	for _, fn := range subs {
		if fn == nil {
			continue
		}
		fn(kind, p)
	}
}

// peerEqual reports whether two PeerInfo values describe the same
// observable peer state. StartedAt is compared via Equal because JSON
// round-trip can swap monotonic for wall-clock.
func peerEqual(a, b PeerInfo) bool {
	if a.NodeID != b.NodeID || a.RESTAddr != b.RESTAddr || a.GRPCAddr != b.GRPCAddr ||
		a.AdminAddr != b.AdminAddr || a.Version != b.Version || a.BuildSHA != b.BuildSHA {
		return false
	}
	if !a.StartedAt.Equal(b.StartedAt) {
		return false
	}
	if len(a.Labels) != len(b.Labels) {
		return false
	}
	for k, v := range a.Labels {
		if b.Labels[k] != v {
			return false
		}
	}
	return true
}

// buildTLS mirrors leader/etcd.go::buildClientTLS. Kept private to the
// cluster package so a future shared transport helper doesn't have to
// cross internal/ boundaries.
func buildTLS(cfg TLSConfig) (*tls.Config, error) {
	tlsCfg := &tls.Config{MinVersion: tls.VersionTLS12}

	if cfg.CertFile != "" || cfg.KeyFile != "" {
		if cfg.CertFile == "" || cfg.KeyFile == "" {
			return nil, errors.New("CertFile and KeyFile must be set together")
		}
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load x509 keypair: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	if cfg.CAFile != "" {
		caBytes, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("CA file %s contained no usable certificates", cfg.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	return tlsCfg, nil
}
