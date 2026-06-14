// Tests for cluster.Registry. Boots an in-process etcd (same pattern
// as internal/ha/leader/etcd_test.go) and exercises lease publish,
// watch, OnChange callbacks, and graceful shutdown.
package cluster

import (
	"context"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.etcd.io/etcd/server/v3/embed"
)

// --- harness ----------------------------------------------------------

var (
	sharedEtcdOnce sync.Once
	sharedEndpoint string
	sharedEtcd     *embed.Etcd
)

func setupEmbedded(t *testing.T) string {
	t.Helper()
	sharedEtcdOnce.Do(func() {
		clientPort := freePort(t)
		peerPort := freePort(t)

		cfg := embed.NewConfig()
		cfg.Dir = filepath.Join(os.TempDir(), "dashd-cluster-test-"+strconv.Itoa(os.Getpid()))
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

// Each test gets a unique key prefix so they don't interfere when run
// in -race / -count=2.
func newPrefix(t *testing.T) string {
	t.Helper()
	return "/dashd-cluster-test/" + t.Name() + "/peers/"
}

func newSelf(id string) PeerInfo {
	return PeerInfo{
		NodeID:    id,
		RESTAddr:  id + ":8443",
		GRPCAddr:  id + ":9443",
		AdminAddr: id + ":7443",
		Version:   "test-1.0",
		Labels:    map[string]string{"zone": "us-west-2a"},
		StartedAt: time.Now().UTC(),
	}
}

func openRegistry(t *testing.T, prefix, id string) *Registry {
	t.Helper()
	endpoint := setupEmbedded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := Open(ctx, Config{
		Endpoints:   []string{endpoint},
		KeyPrefix:   prefix,
		DialTimeout: 3 * time.Second,
		LeaseTTL:    2 * time.Second, // short for tests
	}, newSelf(id))
	if err != nil {
		t.Fatalf("Open(%s): %v", id, err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

// --- tests ------------------------------------------------------------

// TestOpen_SelfPublishedAndVisible verifies the boot path: Open returns
// a registry whose Snapshot contains exactly self, with the expected
// fields preserved.
func TestOpen_SelfPublishedAndVisible(t *testing.T) {
	r := openRegistry(t, newPrefix(t), "dashd-1")
	peers := r.Snapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer (self), got %d", len(peers))
	}
	if peers[0].NodeID != "dashd-1" {
		t.Errorf("expected NodeID=dashd-1, got %q", peers[0].NodeID)
	}
	if peers[0].Version != "test-1.0" {
		t.Errorf("expected Version=test-1.0, got %q", peers[0].Version)
	}
	if peers[0].StartedAt.IsZero() {
		t.Error("StartedAt must be set")
	}
	if r.PeerCount() != 1 {
		t.Errorf("PeerCount = %d; want 1", r.PeerCount())
	}
}

// TestOpen_PeerVisibleAcrossRegistries verifies the multi-node path:
// two registries on the same prefix see each other within the watch
// propagation window.
func TestOpen_PeerVisibleAcrossRegistries(t *testing.T) {
	prefix := newPrefix(t)

	added := make(chan PeerInfo, 4)
	r1 := openRegistry(t, prefix, "dashd-1")
	r1.Subscribe(func(kind ChangeKind, p PeerInfo) {
		if kind == ChangeAdded && p.NodeID != "dashd-1" {
			added <- p
		}
	})

	// Open the second registry — r1's watch should fire ChangeAdded.
	r2 := openRegistry(t, prefix, "dashd-2")
	_ = r2

	select {
	case got := <-added:
		if got.NodeID != "dashd-2" {
			t.Errorf("got peer %q; want dashd-2", got.NodeID)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("r1 did not observe dashd-2 within 5s")
	}

	// Both registries should now report 2 peers.
	if got := r1.PeerCount(); got != 2 {
		t.Errorf("r1.PeerCount = %d; want 2", got)
	}
	// r2's snapshot may take a moment to seed; poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	for r2.PeerCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if got := r2.PeerCount(); got != 2 {
		t.Errorf("r2.PeerCount = %d; want 2", got)
	}
}

// TestClose_ExplicitDeletePropagates verifies graceful shutdown: r2.Close
// causes r1 to see ChangeRemoved within ms (no wait for TTL).
func TestClose_ExplicitDeletePropagates(t *testing.T) {
	prefix := newPrefix(t)

	removed := make(chan PeerInfo, 4)
	r1 := openRegistry(t, prefix, "dashd-1")
	r1.Subscribe(func(kind ChangeKind, p PeerInfo) {
		if kind == ChangeRemoved {
			removed <- p
		}
	})

	r2 := openRegistry(t, prefix, "dashd-2")
	// Wait for r1 to see r2 first.
	deadline := time.Now().Add(5 * time.Second)
	for r1.PeerCount() != 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r1.PeerCount() != 2 {
		t.Fatalf("r1 did not see r2; PeerCount = %d", r1.PeerCount())
	}

	// Graceful close — should fire ChangeRemoved within ms (well under TTL).
	closeStart := time.Now()
	if err := r2.Close(); err != nil {
		t.Fatalf("r2.Close: %v", err)
	}

	select {
	case got := <-removed:
		if got.NodeID != "dashd-2" {
			t.Errorf("removed = %q; want dashd-2", got.NodeID)
		}
		if d := time.Since(closeStart); d > 1*time.Second {
			t.Errorf("ChangeRemoved took %v; expected <1s (well under TTL=2s)", d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("r1 did not observe ChangeRemoved within 3s")
	}

	if got := r1.PeerCount(); got != 1 {
		t.Errorf("after r2.Close, r1.PeerCount = %d; want 1", got)
	}
}

// TestClose_Idempotent verifies Close can be called twice without panicking
// or returning an error from the second call.
func TestClose_Idempotent(t *testing.T) {
	r := openRegistry(t, newPrefix(t), "dashd-1")
	if err := r.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

// TestSubscribe_Unsubscribe verifies the returned cancel func stops
// future events from reaching the callback.
func TestSubscribe_Unsubscribe(t *testing.T) {
	prefix := newPrefix(t)
	r1 := openRegistry(t, prefix, "dashd-1")

	var calls atomic.Int32
	unsub := r1.Subscribe(func(kind ChangeKind, p PeerInfo) {
		calls.Add(1)
	})

	// First peer event — should fire.
	r2 := openRegistry(t, prefix, "dashd-2")
	deadline := time.Now().Add(3 * time.Second)
	for calls.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if calls.Load() == 0 {
		t.Fatal("subscriber never fired for first peer add")
	}
	before := calls.Load()

	// Unsubscribe.
	unsub()

	// r2.Close should NOT fire the unsubscribed callback.
	_ = r2.Close()
	time.Sleep(500 * time.Millisecond)
	if got := calls.Load(); got != before {
		t.Errorf("post-unsubscribe call count = %d; want %d (no further fires)", got, before)
	}
}

// TestOpenSelfOnly_NoEtcd verifies the fallback registry exposes self
// without contacting etcd.
func TestOpenSelfOnly_NoEtcd(t *testing.T) {
	r := OpenSelfOnly(newSelf("solo-node"))
	defer r.Close()

	if r.PeerCount() != 1 {
		t.Errorf("PeerCount = %d; want 1", r.PeerCount())
	}
	peers := r.Snapshot()
	if peers[0].NodeID != "solo-node" {
		t.Errorf("got %q; want solo-node", peers[0].NodeID)
	}
	// Subscribe should not panic even though we never get callbacks.
	unsub := r.Subscribe(func(kind ChangeKind, p PeerInfo) { t.Error("should not fire") })
	unsub()
}

// TestOpen_ValidatesConfig verifies bad configs are rejected up front
// instead of producing a half-broken registry.
func TestOpen_ValidatesConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
		self PeerInfo
	}{
		{"no endpoints", Config{KeyPrefix: "/x/"}, PeerInfo{NodeID: "n"}},
		{"missing prefix", Config{Endpoints: []string{"http://127.0.0.1:2379"}}, PeerInfo{NodeID: "n"}},
		{"prefix missing slash", Config{Endpoints: []string{"http://127.0.0.1:2379"}, KeyPrefix: "/x"}, PeerInfo{NodeID: "n"}},
		{"empty node id", Config{Endpoints: []string{"http://127.0.0.1:2379"}, KeyPrefix: "/x/"}, PeerInfo{}},
		{"slash in node id", Config{Endpoints: []string{"http://127.0.0.1:2379"}, KeyPrefix: "/x/"}, PeerInfo{NodeID: "a/b"}},
		{"whitespace in node id", Config{Endpoints: []string{"http://127.0.0.1:2379"}, KeyPrefix: "/x/"}, PeerInfo{NodeID: "a b"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, err := Open(ctx, c.cfg, c.self); err == nil {
				t.Error("expected error, got nil")
			}
		})
	}
}

// TestPeerEqual covers the label/timestamp comparison.
func TestPeerEqual(t *testing.T) {
	now := time.Now().UTC()
	a := PeerInfo{NodeID: "n", RESTAddr: "x", StartedAt: now, Labels: map[string]string{"a": "1"}}
	b := PeerInfo{NodeID: "n", RESTAddr: "x", StartedAt: now, Labels: map[string]string{"a": "1"}}
	if !peerEqual(a, b) {
		t.Fatal("expected equal")
	}
	c := b
	c.Labels = map[string]string{"a": "2"}
	if peerEqual(a, c) {
		t.Error("expected label diff to be unequal")
	}
	d := b
	d.Labels = nil
	if peerEqual(a, d) {
		t.Error("expected len-mismatch to be unequal")
	}
	e := b
	e.RESTAddr = "y"
	if peerEqual(a, e) {
		t.Error("expected RESTAddr diff to be unequal")
	}
}
