// Embedded-etcd tests for the EtcdElector.
//
// These tests boot an in-process etcd via embed and exercise the
// concurrency primitives over the real wire — same approach as
// store/etcd_test.go. A single etcd cluster is shared per `go test`
// invocation; per-test isolation comes from unique LeaderKey prefixes.
package leader

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
)

// --- harness ----------------------------------------------------------

var (
	sharedEtcd        *embed.Etcd
	sharedEndpoint    string
	sharedEtcdOnce    sync.Once
)

// setupEmbedded boots an in-process etcd on a free port. Reused across
// tests; we never explicitly close it — process exit reclaims the port
// and tempdir.
func setupEmbedded(t *testing.T) string {
	t.Helper()
	sharedEtcdOnce.Do(func() {
		clientPort := freePort(t)
		peerPort := freePort(t)

		cfg := embed.NewConfig()
		cfg.Dir = filepath.Join(os.TempDir(), "dashd-leader-test-"+strconv.Itoa(os.Getpid()))
		_ = os.RemoveAll(cfg.Dir)
		cfg.LogLevel = "error"

		cu, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(clientPort))
		pu, _ := url.Parse("http://127.0.0.1:" + strconv.Itoa(peerPort))
		cfg.ListenClientUrls = []url.URL{*cu}
		cfg.AdvertiseClientUrls = []url.URL{*cu}
		cfg.ListenPeerUrls = []url.URL{*pu}
		cfg.AdvertisePeerUrls = []url.URL{*pu}
		cfg.InitialCluster = cfg.Name + "=http://127.0.0.1:" + strconv.Itoa(peerPort)

		e, err := embed.StartEtcd(cfg)
		if err != nil {
			t.Fatalf("embed.StartEtcd: %v", err)
		}
		select {
		case <-e.Server.ReadyNotify():
		case <-time.After(15 * time.Second):
			e.Close()
			t.Fatal("embedded etcd not ready within 15s")
		}

		sharedEtcd = e
		sharedEndpoint = cu.String()
	})
	return sharedEndpoint
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// newElector opens a fresh elector against the shared etcd. Each test
// gets a unique LeaderKey so they don't race for the same election.
func newElector(t *testing.T, nodeID string) *EtcdElector {
	t.Helper()
	endpoint := setupEmbedded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	e, err := NewEtcdElector(ctx, EtcdConfig{
		Endpoints:   []string{endpoint},
		NodeID:      nodeID,
		LeaseTTL:    2 * time.Second, // short for tests; production default is 15s
		LeaderKey:   "/dashd-test/" + t.Name(),
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewEtcdElector: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// --- constructor validation -------------------------------------------

func TestNewEtcdElector_NoEndpoints(t *testing.T) {
	_, err := NewEtcdElector(context.Background(), EtcdConfig{
		NodeID: "n", LeaderKey: "/k",
	})
	if err == nil {
		t.Fatal("expected error for empty endpoints")
	}
}

func TestNewEtcdElector_NoNodeID(t *testing.T) {
	endpoint := setupEmbedded(t)
	_, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints: []string{endpoint}, LeaderKey: "/k",
	})
	if err == nil {
		t.Fatal("expected error for empty NodeID")
	}
}

func TestNewEtcdElector_NoLeaderKey(t *testing.T) {
	endpoint := setupEmbedded(t)
	_, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints: []string{endpoint}, NodeID: "n",
	})
	if err == nil {
		t.Fatal("expected error for empty LeaderKey")
	}
}

func TestNewEtcdElector_DialFailure(t *testing.T) {
	_, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints:   []string{"http://127.0.0.1:1"},
		NodeID:      "n",
		LeaderKey:   "/k",
		LeaseTTL:    2 * time.Second,
		DialTimeout: 300 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected dial failure")
	}
}

// --- campaign + leadership --------------------------------------------

func TestAwaitLeadership_FirstCandidateWins(t *testing.T) {
	e := newElector(t, "node-1")

	if e.IsLeader() {
		t.Fatal("IsLeader=true before AwaitLeadership")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.AwaitLeadership(ctx); err != nil {
		t.Fatalf("AwaitLeadership: %v", err)
	}
	if !e.IsLeader() {
		t.Fatal("IsLeader=false after winning campaign")
	}
	if e.LeaderID() != "node-1" {
		t.Errorf("LeaderID = %q; want node-1", e.LeaderID())
	}
}

func TestAwaitLeadership_SecondCandidateBlocks(t *testing.T) {
	// Two electors campaigning under the same LeaderKey — only one wins;
	// the other blocks until the first resigns or its lease expires.
	endpoint := setupEmbedded(t)
	leaderKey := "/dashd-test/" + t.Name()

	mkElector := func(nodeID string) *EtcdElector {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		e, err := NewEtcdElector(ctx, EtcdConfig{
			Endpoints:   []string{endpoint},
			NodeID:      nodeID,
			LeaseTTL:    2 * time.Second,
			LeaderKey:   leaderKey,
			DialTimeout: 5 * time.Second,
		})
		if err != nil {
			t.Fatalf("NewEtcdElector(%s): %v", nodeID, err)
		}
		t.Cleanup(func() { _ = e.Close() })
		return e
	}

	first := mkElector("node-A")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := first.AwaitLeadership(ctx); err != nil {
		t.Fatalf("first AwaitLeadership: %v", err)
	}

	second := mkElector("node-B")
	// Second.AwaitLeadership must block — verify with a short ctx that
	// expires while it's still blocked.
	blockCtx, blockCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer blockCancel()
	err := second.AwaitLeadership(blockCtx)
	if err == nil {
		t.Fatal("second elector won immediately; first should still hold the lease")
	}
	if err != context.DeadlineExceeded {
		t.Errorf("expected DeadlineExceeded; got %v", err)
	}
}

func TestClose_BeforeCampaign(t *testing.T) {
	e := newElector(t, "node-1")
	if err := e.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// LostLeadership must fire after Close even if we never campaigned.
	select {
	case <-e.LostLeadership():
	case <-time.After(2 * time.Second):
		t.Fatal("LostLeadership did not fire after Close")
	}
	// Post-close ops behave correctly.
	if e.IsLeader() {
		t.Error("IsLeader=true after Close")
	}
	if err := e.AwaitLeadership(context.Background()); err != context.Canceled {
		t.Errorf("AwaitLeadership after Close = %v; want context.Canceled", err)
	}
}

func TestClose_WhileLeaderResignsCleanly(t *testing.T) {
	// One elector wins, Close is called → second elector should win
	// quickly (well before the 2s lease TTL would naturally elapse),
	// because Resign explicitly hands off.
	endpoint := setupEmbedded(t)
	leaderKey := "/dashd-test/" + t.Name()

	first, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints:   []string{endpoint},
		NodeID:      "node-A",
		LeaseTTL:    5 * time.Second,
		LeaderKey:   leaderKey,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("first NewEtcdElector: %v", err)
	}

	campCtx, campCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer campCancel()
	if err := first.AwaitLeadership(campCtx); err != nil {
		t.Fatalf("first AwaitLeadership: %v", err)
	}

	second, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints:   []string{endpoint},
		NodeID:      "node-B",
		LeaseTTL:    5 * time.Second,
		LeaderKey:   leaderKey,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("second NewEtcdElector: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	// Spawn second's campaign — it MUST block while first holds the lease.
	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		done <- second.AwaitLeadership(ctx)
	}()

	// Verify it's still blocked before we resign.
	select {
	case err := <-done:
		t.Fatalf("second won before first resigned: %v", err)
	case <-time.After(300 * time.Millisecond):
		// Expected.
	}

	// First resigns (via Close). Lease=5s — without Resign, second
	// would wait up to 5s. We assert second wins within 3s.
	if err := first.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("second AwaitLeadership: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second did not win within 3s of first's Resign")
	}

	if !second.IsLeader() {
		t.Error("second.IsLeader=false after winning")
	}
}

func TestCtxCancel_DuringCampaign(t *testing.T) {
	// First elector holds the lease; second's AwaitLeadership blocks;
	// cancelling its ctx must return ctx.Err() cleanly.
	endpoint := setupEmbedded(t)
	leaderKey := "/dashd-test/" + t.Name()

	first, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints: []string{endpoint}, NodeID: "A",
		LeaseTTL: 5 * time.Second, LeaderKey: leaderKey, DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	t.Cleanup(func() { _ = first.Close() })
	cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer ccancel()
	if err := first.AwaitLeadership(cctx); err != nil {
		t.Fatalf("first AwaitLeadership: %v", err)
	}

	second, err := NewEtcdElector(context.Background(), EtcdConfig{
		Endpoints: []string{endpoint}, NodeID: "B",
		LeaseTTL: 5 * time.Second, LeaderKey: leaderKey, DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- second.AwaitLeadership(ctx) }()

	// Let it block briefly, then cancel.
	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Errorf("AwaitLeadership after ctx cancel = %v; want context.Canceled", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("AwaitLeadership did not return within 3s of ctx cancel")
	}
}

// --- ObserveCurrentLeader ---------------------------------------------

func TestObserveCurrentLeader_NoLeader(t *testing.T) {
	e := newElector(t, "node-1")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	leader, err := e.ObserveCurrentLeader(ctx)
	if err != nil {
		t.Fatalf("ObserveCurrentLeader: %v", err)
	}
	if leader != "" {
		t.Errorf("leader = %q; want \"\"", leader)
	}
}

func TestObserveCurrentLeader_AfterCampaign(t *testing.T) {
	e := newElector(t, "node-1")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.AwaitLeadership(ctx); err != nil {
		t.Fatalf("AwaitLeadership: %v", err)
	}

	leader, err := e.ObserveCurrentLeader(ctx)
	if err != nil {
		t.Fatalf("ObserveCurrentLeader: %v", err)
	}
	if leader != "node-1" {
		t.Errorf("observed leader = %q; want node-1", leader)
	}
	if e.LeaderID() != "node-1" {
		t.Errorf("LeaderID = %q; want node-1", e.LeaderID())
	}
}

func TestObserveCurrentLeader_AfterClose(t *testing.T) {
	e := newElector(t, "node-1")
	_ = e.Close()
	_, err := e.ObserveCurrentLeader(context.Background())
	if err != context.Canceled {
		t.Errorf("ObserveCurrentLeader after Close = %v; want context.Canceled", err)
	}
}

// --- Close idempotency ------------------------------------------------

func TestClose_Idempotent(t *testing.T) {
	e := newElector(t, "node-1")
	if err := e.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// --- interface assertion ----------------------------------------------

func TestEtcdElector_SatisfiesInterface(t *testing.T) {
	var _ Elector = (*EtcdElector)(nil)
}

// --- TLS builder ------------------------------------------------------

func TestBuildClientTLS_BadCertKeyPair(t *testing.T) {
	_, err := buildClientTLS(EtcdConfig{CertFile: "/nonexistent.crt", KeyFile: "/nonexistent.key"})
	if err == nil {
		t.Fatal("expected error for nonexistent cert/key files")
	}
}

func TestBuildClientTLS_MismatchedPair(t *testing.T) {
	_, err := buildClientTLS(EtcdConfig{CertFile: "x.crt"}) // KeyFile empty
	if err == nil {
		t.Fatal("expected error for mismatched cert/key")
	}
}

func TestBuildClientTLS_BadCAFile(t *testing.T) {
	_, err := buildClientTLS(EtcdConfig{CAFile: "/nonexistent/ca.pem"})
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestBuildClientTLS_InvalidCAContents(t *testing.T) {
	// Write a non-PEM CA file and confirm we reject it.
	dir := t.TempDir()
	caPath := filepath.Join(dir, "bad-ca.pem")
	if err := os.WriteFile(caPath, []byte("not a PEM cert"), 0o644); err != nil {
		t.Fatalf("write CA: %v", err)
	}
	_, err := buildClientTLS(EtcdConfig{CAFile: caPath})
	if err == nil {
		t.Fatal("expected error for invalid CA contents")
	}
}
