// PD-G4 audit Writer + tail-follow tests.
package audit

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
)

// contextWithSubject is a test helper that wraps auth.WithSubject so
// audit_test code reads cleanly.
func contextWithSubject(name, role string) context.Context {
	return auth.WithSubject(context.Background(), auth.Subject{Name: name, Role: role})
}

func newWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	dir := t.TempDir()
	w, err := Open(Config{Dir: dir, MaxBytes: 1024, SyncEveryWrite: true})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w, dir
}

func readAllEntries(t *testing.T, path string) []Entry {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	var out []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("unmarshal %q: %v", sc.Text(), err)
		}
		out = append(out, e)
	}
	return out
}

func TestAppend_OneEntry(t *testing.T) {
	w, dir := newWriter(t)
	if err := w.Append(Entry{Actor: "alice", Role: "admin", Method: "/x", OK: true}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	entries := readAllEntries(t, filepath.Join(dir, "audit.jsonl"))
	if len(entries) != 1 || entries[0].Actor != "alice" || !entries[0].OK {
		t.Errorf("entries=%v", entries)
	}
	if entries[0].Timestamp.IsZero() {
		t.Error("Append must set Timestamp when zero")
	}
}

func TestAppend_ManyEntries_ConcurrentWrites(t *testing.T) {
	w, dir := newWriter(t)
	const N = 200
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = w.Append(Entry{Actor: "u", Method: "/x", OK: true})
		}(i)
	}
	wg.Wait()
	// Sum across audit.jsonl + any rotated files (MaxBytes is small
	// for this test so rotation kicks in mid-run).
	total := 0
	matches, _ := filepath.Glob(filepath.Join(dir, "audit*.jsonl"))
	for _, m := range matches {
		total += len(readAllEntries(t, m))
	}
	if total != N {
		t.Errorf("total entries=%d; want %d (audit + rotated)", total, N)
	}
}

func TestRotation_PreservesOldEntries(t *testing.T) {
	dir := t.TempDir()
	w, err := Open(Config{Dir: dir, MaxBytes: 80, SyncEveryWrite: true})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	for i := 0; i < 5; i++ {
		_ = w.Append(Entry{Actor: "u", Method: "/x", OK: true})
	}
	// Should have produced at least one rotated file.
	matches, _ := filepath.Glob(filepath.Join(dir, "audit-*.jsonl"))
	if len(matches) == 0 {
		t.Errorf("expected at least one rotated file; got %v", matches)
	}
}

func TestOpen_TwoProcessesFailFast(t *testing.T) {
	dir := t.TempDir()
	w1, err := Open(Config{Dir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer w1.Close()
	_, err = Open(Config{Dir: dir})
	if err == nil {
		t.Error("second Open must error on the lockfile")
	}
}

func TestClose_Idempotent(t *testing.T) {
	w, _ := newWriter(t)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Errorf("double Close: %v", err)
	}
}

// --- Tail-follow ----------------------------------------------------

func TestTail_FromBeginning_StreamsExisting(t *testing.T) {
	w, dir := newWriter(t)
	_ = w.Append(Entry{Method: "/a", OK: true})
	_ = w.Append(Entry{Method: "/b", OK: true})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got := []string{}
	var mu sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = Tail(ctx, dir, true, func(e Entry) error {
			mu.Lock()
			got = append(got, e.Method)
			if len(got) >= 2 {
				cancel()
			}
			mu.Unlock()
			return nil
		})
	}()
	<-done
	if len(got) < 2 || got[0] != "/a" || got[1] != "/b" {
		t.Errorf("got=%v; want [/a /b]", got)
	}
}

func TestTail_FollowsNewAppends(t *testing.T) {
	w, dir := newWriter(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	got := make(chan string, 4)
	go func() {
		_ = Tail(ctx, dir, true, func(e Entry) error {
			got <- e.Method
			return nil
		})
	}()
	// Append after Tail has started.
	time.Sleep(100 * time.Millisecond)
	_ = w.Append(Entry{Method: "/late-1"})
	_ = w.Append(Entry{Method: "/late-2"})
	seen := []string{<-got, <-got}
	if !contains(seen, "/late-1") || !contains(seen, "/late-2") {
		t.Errorf("late appends not observed; got=%v", seen)
	}
}

func TestTail_RespectsCtxCancel(t *testing.T) {
	_, dir := newWriter(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Tail(ctx, dir, false, func(Entry) error { return nil })
	if err != context.Canceled {
		t.Errorf("got %v; want context.Canceled", err)
	}
}

// --- Interceptor ---------------------------------------------------

func TestInterceptor_SkipsReadsByDefault(t *testing.T) {
	w, dir := newWriter(t)
	cfg := InterceptorConfig{Writer: w}
	// Method matching: shouldAudit consults RoleMap; without a Roles
	// pointer it uses auth.DefaultRoleMap. We rely on the live
	// registry having read-classified GetDpuStatus.
	writeEntry(cfg, contextWithSubject("alice", "admin"), "/dashcenter.v1.ObservabilityService/GetDpuStatus", nil)
	writeEntry(cfg, contextWithSubject("alice", "admin"), "/dashcenter.v1.ControlPlane/PutEni", nil)
	_ = w.Close()
	entries := readAllEntries(t, filepath.Join(dir, "audit.jsonl"))
	if len(entries) != 1 || !strings.Contains(entries[0].Method, "PutEni") {
		t.Errorf("expected only mutating entry; got %v", entries)
	}
}

func TestInterceptor_IncludeReads(t *testing.T) {
	w, dir := newWriter(t)
	cfg := InterceptorConfig{Writer: w, IncludeReads: true}
	writeEntry(cfg, contextWithSubject("alice", "viewer"), "/dashcenter.v1.ObservabilityService/GetDpuStatus", nil)
	_ = w.Close()
	entries := readAllEntries(t, filepath.Join(dir, "audit.jsonl"))
	if len(entries) != 1 {
		t.Errorf("got %d entries; want 1 with IncludeReads", len(entries))
	}
}

// --- helpers ---

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
