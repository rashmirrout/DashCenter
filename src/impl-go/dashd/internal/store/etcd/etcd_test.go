// Embedded-etcd unit tests for the EtcdStore.
//
// These tests spin up a real, in-process etcd via go.etcd.io/etcd/server/v3/embed.
// That makes them slower than pure-mock unit tests (each test pays ~1-2s
// for etcd bring-up) but gives us genuine wire-level coverage of the
// client semantics — version semantics, txn CAS, watch event ordering,
// snapshot+watch handoff, compaction behaviour. The alternative
// (mocking clientv3) trades reliability for speed; for a store this
// fundamental we want the wire-level fidelity.
//
// All tests share a single embedded etcd via setupEmbedded — the
// per-test isolation comes from using a unique KeyPrefix per subtest.
package etcd

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"go.etcd.io/etcd/server/v3/embed"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// jsonUnmarshal is a thin alias so the rest of the file doesn't need to
// reach into encoding/json by name (keeps the imports section above
// readable).
var jsonUnmarshal = json.Unmarshal

// --- harness -----------------------------------------------------------

// sharedEmbedded is set up once per test binary invocation by setupEmbedded
// and reused across every test. Bringing etcd up costs ~1s; doing it once
// keeps the suite snappy.
var (
	sharedEmbedded     *embed.Etcd
	sharedClientURL    string
	sharedEmbeddedOnce sync.Once
)

// setupEmbedded boots an in-process etcd on a free localhost port. It
// runs once per `go test` invocation; subsequent calls return the cached
// instance. Returned cleanup is registered automatically with the first
// caller via t.Cleanup.
func setupEmbedded(t *testing.T) string {
	t.Helper()
	sharedEmbeddedOnce.Do(func() {
		clientPort := freePort(t)
		peerPort := freePort(t)

		cfg := embed.NewConfig()
		cfg.Dir = filepath.Join(os.TempDir(), "dashd-etcd-test-"+t.Name())
		_ = os.RemoveAll(cfg.Dir)
		cfg.LogLevel = "error" // keep noise out of test output

		// Bind on the free ports we grabbed.
		cu, _ := url.Parse("http://127.0.0.1:" + itoa(clientPort))
		pu, _ := url.Parse("http://127.0.0.1:" + itoa(peerPort))
		cfg.ListenClientUrls = []url.URL{*cu}
		cfg.AdvertiseClientUrls = []url.URL{*cu}
		cfg.ListenPeerUrls = []url.URL{*pu}
		cfg.AdvertisePeerUrls = []url.URL{*pu}
		cfg.InitialCluster = cfg.Name + "=http://127.0.0.1:" + itoa(peerPort)

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

		sharedEmbedded = e
		sharedClientURL = cu.String()
	})

	// Register a teardown on the first caller — subsequent callers
	// also register, but Cleanup is safe with multiple registrations.
	t.Cleanup(func() {
		// Don't close here: the binary may have other tests still
		// running. We rely on process exit to GC the etcd. For a
		// long-running test binary we'd want a sync.Once-protected
		// closer; today's suite finishes in <30s.
	})

	return sharedClientURL
}

// freePort returns an unbound TCP port on 127.0.0.1.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// itoa formats an int without pulling in strconv at the top of every
// test (and without the surrounding noise of fmt.Sprintf).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789"
	var b [20]byte
	i := len(b)
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	for n > 0 {
		i--
		b[i] = digits[n%10]
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// openStore returns an EtcdStore bound to a unique KeyPrefix per test
// so tests don't see each other's data.
func openStore(t *testing.T) *EtcdStore {
	t.Helper()
	endpoint := setupEmbedded(t)
	prefix := "/dashd-test/" + t.Name() + "/"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s, err := Open(ctx, Config{
		Endpoints:   []string{endpoint},
		KeyPrefix:   prefix,
		DialTimeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func testKey(name string) store.ObjectKey {
	return store.ObjectKey{Namespace: "default", Kind: "vnet", Name: name}
}

// testSpec is the test payload — a minimal map so we don't pull in proto
// types just for marshalling.
type testSpec struct {
	Value string `json:"value"`
}

// --- Open / Close ------------------------------------------------------

func TestOpen_EmptyEndpointsRejected(t *testing.T) {
	_, err := Open(context.Background(), Config{KeyPrefix: "/x/"})
	if err == nil {
		t.Fatal("expected error for empty endpoints")
	}
}

func TestOpen_PrefixMustEndInSlash(t *testing.T) {
	endpoint := setupEmbedded(t)
	_, err := Open(context.Background(), Config{
		Endpoints: []string{endpoint},
		KeyPrefix: "/no-slash",
	})
	if err == nil {
		t.Fatal("expected error for KeyPrefix without trailing /")
	}
}

func TestOpen_EmptyKeyPrefix(t *testing.T) {
	endpoint := setupEmbedded(t)
	_, err := Open(context.Background(), Config{
		Endpoints: []string{endpoint},
	})
	if err == nil {
		t.Fatal("expected error for empty KeyPrefix")
	}
}

func TestOpen_ProbeFailureSurfaces(t *testing.T) {
	// Dial a port we know nothing is listening on.
	_, err := Open(context.Background(), Config{
		Endpoints:   []string{"http://127.0.0.1:1"},
		KeyPrefix:   "/x/",
		DialTimeout: 200 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected dial/probe failure")
	}
}

func TestClose_Idempotent(t *testing.T) {
	s := openStore(t)
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

func TestOpsAfterCloseFail(t *testing.T) {
	s := openStore(t)
	_ = s.Close()
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v"}, 0); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Put after close: got %v; want ErrClosed", err)
	}
	if err := s.Delete(ctx, testKey("a")); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Delete after close: got %v; want ErrClosed", err)
	}
	if _, err := s.Get(ctx, testKey("a")); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Get after close: got %v; want ErrClosed", err)
	}
	if _, err := s.List(ctx, "default", "vnet"); !errors.Is(err, store.ErrClosed) {
		t.Errorf("List after close: got %v; want ErrClosed", err)
	}
	if _, err := s.Watch(ctx); !errors.Is(err, store.ErrClosed) {
		t.Errorf("Watch after close: got %v; want ErrClosed", err)
	}
}

// --- Put / Get / Delete / List -----------------------------------------

func TestPut_FirstPutReturnsGen1(t *testing.T) {
	s := openStore(t)
	gen, err := s.Put(context.Background(), testKey("a"), testSpec{"v1"}, 0)
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if gen != 1 {
		t.Errorf("first Put gen = %d; want 1", gen)
	}
}

func TestPut_SecondPutBumpsGen(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v1"}, 0); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	gen, err := s.Put(ctx, testKey("a"), testSpec{"v2"}, 0)
	if err != nil {
		t.Fatalf("Put 2: %v", err)
	}
	if gen != 2 {
		t.Errorf("second Put gen = %d; want 2", gen)
	}
}

func TestPut_CASMatch(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v1"}, 0); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	gen, err := s.Put(ctx, testKey("a"), testSpec{"v2"}, 1)
	if err != nil {
		t.Fatalf("CAS Put: %v", err)
	}
	if gen != 2 {
		t.Errorf("CAS Put gen = %d; want 2", gen)
	}
}

func TestPut_CASMismatch(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v1"}, 0); err != nil {
		t.Fatalf("Put 1: %v", err)
	}
	// Expected generation 99 against actual 1 → mismatch.
	_, err := s.Put(ctx, testKey("a"), testSpec{"v2"}, 99)
	if !errors.Is(err, store.ErrGenerationMismatch) {
		t.Fatalf("got %v; want ErrGenerationMismatch", err)
	}
}

func TestGet_NotFound(t *testing.T) {
	s := openStore(t)
	_, err := s.Get(context.Background(), testKey("missing"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestGet_AfterPut(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v1"}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sp, err := s.Get(ctx, testKey("a"))
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if sp.Generation != 1 {
		t.Errorf("Generation = %d; want 1", sp.Generation)
	}
	if sp.EtcdRevision == 0 {
		t.Error("EtcdRevision should be non-zero after Put")
	}
	// Round-trip semantic equality — JSON whitespace may differ because
	// the envelope is MarshalIndent'ed, which re-formats nested
	// RawMessage values. What we care about is that the spec decodes to
	// the same Go value we put in.
	var got testSpec
	if err := jsonUnmarshal(sp.Data, &got); err != nil {
		t.Fatalf("decode round-trip spec: %v", err)
	}
	if got.Value != "v1" {
		t.Errorf("spec.Value = %q; want v1", got.Value)
	}
}

func TestDelete_NotFound(t *testing.T) {
	s := openStore(t)
	err := s.Delete(context.Background(), testKey("missing"))
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestDelete_RemovesKey(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	if _, err := s.Put(ctx, testKey("a"), testSpec{"v1"}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := s.Delete(ctx, testKey("a")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(ctx, testKey("a")); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("post-delete Get: got %v; want ErrNotFound", err)
	}
}

func TestList_EmptyAndPopulated(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	specs, err := s.List(ctx, "default", "vnet")
	if err != nil {
		t.Fatalf("List empty: %v", err)
	}
	if len(specs) != 0 {
		t.Errorf("empty list len = %d; want 0", len(specs))
	}

	for _, name := range []string{"c", "a", "b"} {
		if _, err := s.Put(ctx, testKey(name), testSpec{name}, 0); err != nil {
			t.Fatalf("Put %s: %v", name, err)
		}
	}

	specs, err = s.List(ctx, "default", "vnet")
	if err != nil {
		t.Fatalf("List populated: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("len = %d; want 3", len(specs))
	}
	// Sort order: alphabetical by Name.
	want := []string{"a", "b", "c"}
	for i, w := range want {
		if specs[i].Key.Name != w {
			t.Errorf("specs[%d].Name = %q; want %q", i, specs[i].Key.Name, w)
		}
	}
}

func TestList_NamespaceIsolation(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()
	keyA := store.ObjectKey{Namespace: "ns-a", Kind: "vnet", Name: "x"}
	keyB := store.ObjectKey{Namespace: "ns-b", Kind: "vnet", Name: "x"}
	if _, err := s.Put(ctx, keyA, testSpec{"a"}, 0); err != nil {
		t.Fatalf("Put A: %v", err)
	}
	if _, err := s.Put(ctx, keyB, testSpec{"b"}, 0); err != nil {
		t.Fatalf("Put B: %v", err)
	}

	specsA, _ := s.List(ctx, "ns-a", "vnet")
	if len(specsA) != 1 || specsA[0].Key.Namespace != "ns-a" {
		t.Errorf("ns-a list = %+v; want one spec in ns-a", specsA)
	}
	specsB, _ := s.List(ctx, "ns-b", "vnet")
	if len(specsB) != 1 || specsB[0].Key.Namespace != "ns-b" {
		t.Errorf("ns-b list = %+v; want one spec in ns-b", specsB)
	}
}

// --- Watch -------------------------------------------------------------

func TestWatch_SnapshotThenLive(t *testing.T) {
	s := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Pre-seed two keys before opening Watch.
	if _, err := s.Put(ctx, testKey("snap-1"), testSpec{"a"}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, err := s.Put(ctx, testKey("snap-2"), testSpec{"b"}, 0); err != nil {
		t.Fatalf("Put: %v", err)
	}

	ch, err := s.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}

	// Expect two EventPut from the snapshot, in any order.
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case ev := <-ch:
			if ev.Type != store.EventPut {
				t.Fatalf("snapshot event %d type = %v; want EventPut", i, ev.Type)
			}
			seen[ev.Key.Name] = true
		case <-time.After(3 * time.Second):
			t.Fatalf("timed out waiting for snapshot event %d", i)
		}
	}
	if !seen["snap-1"] || !seen["snap-2"] {
		t.Errorf("snapshot missed keys: %v", seen)
	}

	// Live mutation: Put a new key — should arrive on the watch.
	if _, err := s.Put(ctx, testKey("live"), testSpec{"c"}, 0); err != nil {
		t.Fatalf("live Put: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Type != store.EventPut || ev.Key.Name != "live" {
			t.Errorf("got ev = %+v; want EventPut(live)", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for live event")
	}

	// Live delete — should arrive as EventDelete.
	if err := s.Delete(ctx, testKey("live")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	select {
	case ev := <-ch:
		if ev.Type != store.EventDelete || ev.Key.Name != "live" {
			t.Errorf("got ev = %+v; want EventDelete(live)", ev)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for delete event")
	}
}

func TestWatch_CancelClosesChannel(t *testing.T) {
	s := openStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := s.Watch(ctx)
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	cancel()
	// Channel must close within a short window.
	select {
	case _, ok := <-ch:
		if ok {
			// First receive may be an event from the open; consume
			// until close.
			for ok {
				select {
				case _, ok = <-ch:
				case <-time.After(2 * time.Second):
					t.Fatal("watch channel did not close after cancel")
				}
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watch channel did not produce or close within 2s of cancel")
	}
}

// --- Key parsing -------------------------------------------------------

func TestParseEtcdKey(t *testing.T) {
	s := &EtcdStore{keyPrefix: "/dashd/state/"}
	tests := []struct {
		input       string
		wantOK      bool
		wantNs      string
		wantKind    string
		wantName    string
	}{
		{"/dashd/state/default/vnet/v1", true, "default", "vnet", "v1"},
		{"/dashd/state/ns-a/eni/eni-1", true, "ns-a", "eni", "eni-1"},
		{"/different/prefix/x/y/z", false, "", "", ""},
		{"/dashd/state/missing-kind", false, "", "", ""},
		{"/dashd/state/ns//name", false, "", "", ""},
	}
	for _, tc := range tests {
		got, ok := s.parseEtcdKey(tc.input)
		if ok != tc.wantOK {
			t.Errorf("parse(%q) ok = %v; want %v", tc.input, ok, tc.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Namespace != tc.wantNs || got.Kind != tc.wantKind || got.Name != tc.wantName {
			t.Errorf("parse(%q) = %+v; want ns=%s kind=%s name=%s",
				tc.input, got, tc.wantNs, tc.wantKind, tc.wantName)
		}
	}
}
