// EtcdElector implements leader.Elector using an etcd v3 lease.
//
// This is the leader-election backend for `mode: controller` + multi-node
// deployments. The pattern is the standard etcd concurrency primitive:
//
//  1. Grant a lease with TTL=cfg.LeaseTTL. The lease keep-alive runs as
//     long as we hold the session; on session loss (heartbeat failure,
//     etcd unreachable for longer than TTL, or our own Close) the lease
//     expires and any election we hold is revoked automatically.
//  2. Open a session over the lease (`concurrency.NewSession`).
//  3. Create a campaign with `concurrency.NewElection` keyed by
//     cfg.LeaderKey.
//  4. AwaitLeadership calls Campaign(ctx, nodeID). Campaign blocks until
//     this node is the leader (or ctx fires).
//  5. While we are leader, a background goroutine watches the session's
//     Done() channel. When the session expires, LostLeadership is
//     closed and leaderLoop in main.go tears down + re-campaigns.
//
// Notes on edge cases:
//
//   - Lease loss vs. explicit Close: both close LostLeadership so the
//     main.go select unblocks. Close also tears down the etcd session so
//     other nodes can elect immediately, instead of waiting for the
//     keep-alive deadline.
//   - Re-campaign after loss: leaderLoop calls AwaitLeadership again on
//     a fresh elector instance. We DON'T reuse a single EtcdElector
//     across re-campaigns because the etcd concurrency Session is
//     one-shot — once it expires, it's gone. A fresh elector with a
//     fresh session is the correct pattern.
package leader

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
	"go.etcd.io/etcd/client/v3/concurrency"
)

// EtcdConfig is the subset of internal/config.ElectorConfig that this
// package needs. We accept a purpose-built struct (rather than importing
// internal/config) to keep the dependency direction one-way and to
// allow tests to construct an elector without dragging in the full
// config validator.
type EtcdConfig struct {
	// Endpoints is the list of etcd peer URLs (same form as the
	// store/etcd backend's Endpoints).
	Endpoints []string

	// NodeID is what this process advertises as its identity in the
	// election. Two nodes with the same NodeID would shadow each other
	// in some etcd tooling; main.go defaults this to hostname.
	NodeID string

	// LeaseTTL is the lease TTL in seconds (rounded up). When the
	// keep-alive misses long enough for the lease to expire, leadership
	// is lost. D3 locks this at 15s by default.
	LeaseTTL time.Duration

	// LeaderKey is the etcd key prefix under which the election runs.
	// All candidates campaign under the same prefix.
	LeaderKey string

	// DialTimeout caps the time clientv3.New + initial session creation
	// will wait. Defaults to 5s when zero.
	DialTimeout time.Duration

	// TLS material. When CertFile + KeyFile are both empty the
	// connection is plaintext.
	CertFile string
	KeyFile  string
	CAFile   string
}

// EtcdElector implements leader.Elector against an etcd v3 cluster.
//
// Lifecycle:
//
//	NewEtcdElector(...)                 → connects + opens session
//	AwaitLeadership(ctx)                → blocks until elected (or ctx)
//	IsLeader()                          → true between elected and lost
//	LostLeadership()                    → closes on session loss or Close
//	Close()                             → releases session, closes client
//
// Concurrent calls to IsLeader / LeaderID / LostLeadership are safe.
// AwaitLeadership is meant to be called by leaderLoop only (single goroutine).
type EtcdElector struct {
	cli      *clientv3.Client
	session  *concurrency.Session
	election *concurrency.Election
	nodeID   string

	// sessionCancel cancels the session's context (separate from the
	// dial context, which is short-lived). Called from Close to tear the
	// session down deterministically.
	sessionCancel context.CancelFunc

	// observeCancel stops the background leader-observer goroutine on
	// Close. The goroutine watches election.Observe() so that LeaderID()
	// on a follower node reflects the current leader without callers
	// having to invoke ObserveCurrentLeader explicitly.
	observeCancel context.CancelFunc

	mu          sync.RWMutex
	currentLeader string
	isLeader      bool

	closeOnce sync.Once
	lostCh    chan struct{}
	closed    bool
}

// NewEtcdElector dials the etcd cluster and opens a concurrency session.
// Returns an error if the dial or session creation fails within
// cfg.DialTimeout. On success the elector holds an active lease but has
// not yet campaigned — call AwaitLeadership next.
func NewEtcdElector(ctx context.Context, cfg EtcdConfig) (*EtcdElector, error) {
	if len(cfg.Endpoints) == 0 {
		return nil, errors.New("leader.etcd: at least one endpoint is required")
	}
	if cfg.NodeID == "" {
		return nil, errors.New("leader.etcd: NodeID is required")
	}
	if cfg.LeaderKey == "" {
		return nil, errors.New("leader.etcd: LeaderKey is required")
	}

	dialTimeout := cfg.DialTimeout
	if dialTimeout == 0 {
		dialTimeout = 5 * time.Second
	}
	leaseTTL := cfg.LeaseTTL
	if leaseTTL <= 0 {
		leaseTTL = 15 * time.Second
	}

	clientCfg := clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: dialTimeout,
		Context:     ctx,
	}
	if cfg.CertFile != "" || cfg.CAFile != "" {
		tlsCfg, err := buildClientTLS(cfg)
		if err != nil {
			return nil, fmt.Errorf("leader.etcd: TLS config: %w", err)
		}
		clientCfg.TLS = tlsCfg
	}

	cli, err := clientv3.New(clientCfg)
	if err != nil {
		return nil, fmt.Errorf("leader.etcd: dial %v: %w", cfg.Endpoints, err)
	}

	// Fail-fast probe: clientv3.New succeeds even when no etcd is
	// reachable (the client connects lazily). Without an explicit probe,
	// NewSession below would block forever on a dead endpoint. We use a
	// bounded ctx for the probe so the caller's DialTimeout actually
	// means something.
	probeCtx, probeCancel := context.WithTimeout(ctx, dialTimeout)
	if _, err := cli.Get(probeCtx, "/dashd-probe", clientv3.WithLimit(1)); err != nil {
		probeCancel()
		_ = cli.Close()
		return nil, fmt.Errorf("leader.etcd: probe %v: %w", cfg.Endpoints, err)
	}
	probeCancel()

	// concurrency.WithTTL expects seconds, ceiling.
	ttlSec := int(leaseTTL.Round(time.Second).Seconds())
	if ttlSec < 1 {
		ttlSec = 1
	}
	// Bind the session to its OWN cancellable context, NOT the short-lived
	// dial ctx. The dial ctx exists only to bound NewSession's connection
	// setup; the session itself must live for the lifetime of the elector
	// (until either Close() or lease expiry). Using the dial ctx here
	// previously caused sessions to orphan as soon as the caller's 5s
	// dial context expired, and any subsequent campaign or Close would
	// hang on the dead session.
	sessCtx, sessCancel := context.WithCancel(context.Background())
	sess, err := concurrency.NewSession(cli,
		concurrency.WithTTL(ttlSec),
		concurrency.WithContext(sessCtx),
	)
	if err != nil {
		sessCancel()
		_ = cli.Close()
		return nil, fmt.Errorf("leader.etcd: new session: %w", err)
	}

	e := &EtcdElector{
		cli:           cli,
		session:       sess,
		election:      concurrency.NewElection(sess, cfg.LeaderKey),
		nodeID:        cfg.NodeID,
		sessionCancel: sessCancel,
		lostCh:        make(chan struct{}),
	}

	// Background goroutine: detect session loss. When the etcd session
	// orphans (lease expired, etcd unreachable too long, or our Close
	// torn it down), close lostCh so leaderLoop unblocks.
	go e.watchSession()

	// Background goroutine: keep `currentLeader` fresh on followers.
	// concurrency.Election.Observe streams the value at the leader key
	// every time it changes (campaign / resign / lease expiry on the
	// holder). Without this, LeaderID() returns the empty string until
	// somebody calls ObserveCurrentLeader explicitly — which is the
	// PE-G6 known limitation that the ClusterService topology reply
	// surfaced as `leader_id: ""` on follower nodes.
	observeCtx, observeCancel := context.WithCancel(context.Background())
	e.observeCancel = observeCancel
	go e.observeLoop(observeCtx)

	return e, nil
}

// watchSession waits for the session's Done channel. When it fires,
// leadership is lost (either because we explicitly Close'd or because
// the lease expired). We close lostCh exactly once so leaderLoop's
// select can unblock and re-campaign.
func (e *EtcdElector) watchSession() {
	<-e.session.Done()

	e.mu.Lock()
	wasLeader := e.isLeader
	e.isLeader = false
	e.currentLeader = ""
	e.mu.Unlock()

	if wasLeader {
		slog.Warn("leader.etcd: session expired while leader; signalling LostLeadership")
	}

	// Close lostCh idempotently. closeOnce coordinates between
	// watchSession and Close — whichever fires first owns the close.
	e.closeOnce.Do(func() {
		close(e.lostCh)
	})
}

// observeLoop streams `concurrency.Election.Observe` and caches every
// observed leader value in `currentLeader`. The etcd client's Observe
// implementation auto-reconnects on transient failures and emits the
// current value whenever the leader key changes (including campaign +
// resign + lease-expiry handovers). On any unexpected channel close we
// briefly back off and re-Observe so a flaky connection doesn't leave
// followers reporting a stale leader_id forever. Exits cleanly when
// the parent context is cancelled by Close.
func (e *EtcdElector) observeLoop(ctx context.Context) {
	backoff := 200 * time.Millisecond
	const maxBackoff = 5 * time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		ch := e.election.Observe(ctx)
		for resp := range ch {
			leader := ""
			if len(resp.Kvs) > 0 {
				leader = string(resp.Kvs[0].Value)
			}
			e.mu.Lock()
			e.currentLeader = leader
			e.mu.Unlock()
			backoff = 200 * time.Millisecond // healthy stream resets backoff
		}
		// Channel closed; either ctx is done or the Observe stream
		// died. Sleep with capped exponential backoff, then re-observe.
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// AwaitLeadership campaigns for leadership and blocks until elected.
// Returns ctx.Err() on cancellation, or a wrapped error on session
// failure mid-campaign. Re-callable on a fresh elector if leadership
// was lost (the underlying concurrency.Election is single-shot per
// session — leaderLoop creates a new EtcdElector after every loss).
func (e *EtcdElector) AwaitLeadership(ctx context.Context) error {
	if e.isClosed() {
		return context.Canceled
	}

	// Campaign blocks until this node is the leader OR ctx fires.
	// The advertised value is the node id; observers can call
	// election.Leader() to retrieve it.
	if err := e.election.Campaign(ctx, e.nodeID); err != nil {
		// Distinguish "ctx fired" from "session died" — the former is
		// graceful shutdown; the latter is the same as LostLeadership.
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("leader.etcd: campaign: %w", err)
	}

	e.mu.Lock()
	e.isLeader = true
	e.currentLeader = e.nodeID
	e.mu.Unlock()

	slog.Info("leader.etcd: assumed leadership", "node_id", e.nodeID)
	return nil
}

// LostLeadership returns a channel that closes when this process is no
// longer the leader. Closed either because the etcd session expired or
// because Close() was called. The leaderLoop in main.go selects on
// this channel to tear down leader-only goroutines.
func (e *EtcdElector) LostLeadership() <-chan struct{} {
	return e.lostCh
}

// IsLeader returns a non-blocking snapshot of leadership state.
func (e *EtcdElector) IsLeader() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.isLeader
}

// LeaderID returns the configured NodeID when we are leader; the
// observed leader's NodeID when we know it; or "" before the first
// campaign.
func (e *EtcdElector) LeaderID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	if e.currentLeader != "" {
		return e.currentLeader
	}
	return ""
}

// ObserveCurrentLeader performs a one-shot lookup of the current
// leader (whether or not it's us) and caches the result for LeaderID.
// Useful for follower nodes that want to report "leader: dashd-2" on
// /admin/health without themselves campaigning. Returns the leader's
// NodeID or empty string if no leader is currently held.
//
// This call uses ctx for the etcd round-trip; pass a short-timeout ctx
// to keep /admin/health fast.
func (e *EtcdElector) ObserveCurrentLeader(ctx context.Context) (string, error) {
	if e.isClosed() {
		return "", context.Canceled
	}
	resp, err := e.election.Leader(ctx)
	if err != nil {
		if errors.Is(err, concurrency.ErrElectionNoLeader) {
			e.mu.Lock()
			e.currentLeader = ""
			e.mu.Unlock()
			return "", nil
		}
		return "", err
	}
	leader := ""
	if len(resp.Kvs) > 0 {
		leader = string(resp.Kvs[0].Value)
	}
	e.mu.Lock()
	e.currentLeader = leader
	e.mu.Unlock()
	return leader, nil
}

// Close releases the etcd session and closes the underlying client.
// Idempotent. After Close: LostLeadership closes (if it hasn't
// already), IsLeader returns false, AwaitLeadership returns
// context.Canceled.
//
// We deliberately Resign before closing the session when we hold
// leadership — that way other nodes elect immediately instead of
// waiting LeaseTTL seconds for our lease to expire.
func (e *EtcdElector) Close() error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	wasLeader := e.isLeader
	e.isLeader = false
	e.mu.Unlock()

	// Resign cleanly if we held the lease. Use a short bounded ctx —
	// shutdown should not block on etcd being reachable.
	if wasLeader {
		resignCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := e.election.Resign(resignCtx); err != nil {
			slog.Warn("leader.etcd: Resign failed (proceeding with Close)", "error", err)
		}
		cancel()
	}

	// Closing the session triggers watchSession (if it hasn't already
	// observed lease loss) which will close lostCh via closeOnce.
	if err := e.session.Close(); err != nil {
		slog.Warn("leader.etcd: session.Close failed (proceeding)", "error", err)
	}
	// Cancel the session context so any background keep-alive goroutine
	// returns instead of leaking until lease expiry.
	if e.sessionCancel != nil {
		e.sessionCancel()
	}
	// Stop the leader-observer goroutine.
	if e.observeCancel != nil {
		e.observeCancel()
	}

	// Belt-and-suspenders: ensure lostCh is closed even if
	// watchSession hasn't fired yet (e.g. very fast Close path).
	e.closeOnce.Do(func() {
		close(e.lostCh)
	})

	return e.cli.Close()
}

// isClosed reports whether Close has been called. Fast non-blocking
// check used by all read paths.
func (e *EtcdElector) isClosed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.closed
}

// buildClientTLS constructs a *tls.Config from the supplied cert/key/CA
// material. Mirrors the store/etcd helper to keep the two leader/store
// connections configured identically. Kept private here (rather than
// shared) because the two packages must remain independent — a future
// refactor that extracts a shared transport helper is welcome but
// not required.
func buildClientTLS(cfg EtcdConfig) (*tls.Config, error) {
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

// Compile-time assertion that EtcdElector satisfies the Elector
// interface. Catches signature drift the moment Elector evolves.
var _ Elector = (*EtcdElector)(nil)
