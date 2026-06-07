package file

import (
"context"
"errors"
"fmt"
"os"
"path/filepath"
"sort"
"sync"
"testing"
"time"

"google.golang.org/protobuf/types/known/wrapperspb"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// testSpec returns a simple proto.Message for testing.
// Using wrapperspb.StringValue since it's always available.
func testSpec(val string) *wrapperspb.StringValue {
return wrapperspb.String(val)
}

func testKey(name string) store.ObjectKey {
return store.ObjectKey{Namespace: store.DefaultNamespace, Kind: "vnet", Name: name}
}

func openTemp(t *testing.T) *FileStore {
t.Helper()
dir := t.TempDir()
fs, err := Open(dir)
if err != nil {
t.Fatalf("Open(%s) failed: %v", dir, err)
}
t.Cleanup(func() { _ = fs.Close() })
return fs
}

// 1. Open empty dir → List returns empty slice
func TestOpenEmptyDir(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
specs, err := fs.List(ctx, store.DefaultNamespace, "vnet")
if err != nil {
t.Fatalf("List: %v", err)
}
if len(specs) != 0 {
t.Errorf("expected 0 specs, got %d", len(specs))
}
}

// 2. First Put returns generation 1
func TestFirstPutReturnsGen1(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
gen, err := fs.Put(ctx, testKey("v1"), testSpec("hello"), 0)
if err != nil {
t.Fatalf("Put: %v", err)
}
if gen != 1 {
t.Errorf("expected gen=1, got %d", gen)
}
}

// 3. Two Puts of same key (expected=0) → generations 1, 2
func TestTwoPutsSameKey(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
g1, _ := fs.Put(ctx, testKey("v1"), testSpec("a"), 0)
g2, _ := fs.Put(ctx, testKey("v1"), testSpec("b"), 0)
if g1 != 1 || g2 != 2 {
t.Errorf("expected gens 1,2; got %d,%d", g1, g2)
}
}

// 4. Put with expectedGeneration=99 against gen=1 → ErrGenerationMismatch
func TestPutGenMismatch(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
fs.Put(ctx, testKey("v1"), testSpec("a"), 0)
_, err := fs.Put(ctx, testKey("v1"), testSpec("b"), 99)
if !errors.Is(err, store.ErrGenerationMismatch) {
t.Errorf("expected ErrGenerationMismatch, got: %v", err)
}
}

// 5. Put with matching expectedGeneration=1 → new gen=2
func TestPutGenMatch(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
fs.Put(ctx, testKey("v1"), testSpec("a"), 0)
gen, err := fs.Put(ctx, testKey("v1"), testSpec("b"), 1)
if err != nil {
t.Fatalf("Put with matching gen: %v", err)
}
if gen != 2 {
t.Errorf("expected gen=2, got %d", gen)
}
}

// 6. Get non-existent → ErrNotFound
func TestGetNotFound(t *testing.T) {
fs := openTemp(t)
_, err := fs.Get(context.Background(), testKey("nope"))
if !errors.Is(err, store.ErrNotFound) {
t.Errorf("expected ErrNotFound, got: %v", err)
}
}

// 7. Get after Put → returns same spec
func TestGetAfterPut(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
fs.Put(ctx, testKey("v1"), testSpec("hello"), 0)
sp, err := fs.Get(ctx, testKey("v1"))
if err != nil {
t.Fatalf("Get: %v", err)
}
if sp.Generation != 1 {
t.Errorf("expected gen=1, got %d", sp.Generation)
}
if sp.Key.Name != "v1" {
t.Errorf("expected name=v1, got %s", sp.Key.Name)
}
}

// 8. Delete non-existent → ErrNotFound
func TestDeleteNotFound(t *testing.T) {
fs := openTemp(t)
err := fs.Delete(context.Background(), testKey("nope"))
if !errors.Is(err, store.ErrNotFound) {
t.Errorf("expected ErrNotFound, got: %v", err)
}
}

// 9. Delete after Put → Get returns ErrNotFound
func TestDeleteThenGet(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
fs.Put(ctx, testKey("v1"), testSpec("a"), 0)
if err := fs.Delete(ctx, testKey("v1")); err != nil {
t.Fatalf("Delete: %v", err)
}
_, err := fs.Get(ctx, testKey("v1"))
if !errors.Is(err, store.ErrNotFound) {
t.Errorf("expected ErrNotFound after delete, got: %v", err)
}
}

// 10. List returns specs sorted by Name
func TestListSorted(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()
for _, name := range []string{"c", "a", "b"} {
fs.Put(ctx, testKey(name), testSpec(name), 0)
}
specs, err := fs.List(ctx, store.DefaultNamespace, "vnet")
if err != nil {
t.Fatalf("List: %v", err)
}
if len(specs) != 3 {
t.Fatalf("expected 3 specs, got %d", len(specs))
}
names := make([]string, 3)
for i, s := range specs {
names[i] = s.Key.Name
}
if !sort.StringsAreSorted(names) {
t.Errorf("expected sorted names, got %v", names)
}
}

// 11. List unknown kind → empty slice (not nil)
func TestListUnknownKind(t *testing.T) {
fs := openTemp(t)
specs, err := fs.List(context.Background(), store.DefaultNamespace, "unknown_kind")
if err != nil {
t.Fatalf("List: %v", err)
}
if specs == nil {
t.Error("expected non-nil empty slice")
}
if len(specs) != 0 {
t.Errorf("expected 0 specs, got %d", len(specs))
}
}

// 12. Restart: Open, Put 3, Close, Re-Open → List returns same 3
func TestRestartPersistence(t *testing.T) {
dir := t.TempDir()
ctx := context.Background()

// First session.
fs1, err := Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
for _, name := range []string{"v1", "v2", "v3"} {
fs1.Put(ctx, testKey(name), testSpec(name), 0)
}
fs1.Close()

// Second session.
fs2, err := Open(dir)
if err != nil {
t.Fatalf("Re-Open: %v", err)
}
defer fs2.Close()

specs, err := fs2.List(ctx, store.DefaultNamespace, "vnet")
if err != nil {
t.Fatalf("List: %v", err)
}
if len(specs) != 3 {
t.Errorf("expected 3 specs after restart, got %d", len(specs))
}
// Check generations are preserved.
for _, sp := range specs {
if sp.Generation != 1 {
t.Errorf("spec %s: expected gen=1, got %d", sp.Key.Name, sp.Generation)
}
}
}

// 13. Watch: snapshot + live events
func TestWatch(t *testing.T) {
fs := openTemp(t)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

// Pre-populate.
fs.Put(ctx, testKey("v1"), testSpec("a"), 0)
fs.Put(ctx, testKey("v2"), testSpec("b"), 0)

ch, err := fs.Watch(ctx)
if err != nil {
t.Fatalf("Watch: %v", err)
}

// Should receive 2 snapshot events.
snapshots := drainN(t, ch, 2, 2*time.Second)
if len(snapshots) != 2 {
t.Fatalf("expected 2 snapshot events, got %d", len(snapshots))
}
for _, ev := range snapshots {
if ev.Type != store.EventPut {
t.Errorf("snapshot event type should be PUT, got %v", ev.Type)
}
}

// Now do a Put → should receive live EventPut.
fs.Put(ctx, testKey("v3"), testSpec("c"), 0)
live := drainN(t, ch, 1, 2*time.Second)
if len(live) != 1 || live[0].Type != store.EventPut || live[0].Key.Name != "v3" {
t.Errorf("expected PUT for v3, got %+v", live)
}

// Delete → should receive EventDelete.
fs.Delete(ctx, testKey("v1"))
del := drainN(t, ch, 1, 2*time.Second)
if len(del) != 1 || del[0].Type != store.EventDelete || del[0].Key.Name != "v1" {
t.Errorf("expected DELETE for v1, got %+v", del)
}
}

// 14. Watch ctx cancel closes channel
func TestWatchCancel(t *testing.T) {
fs := openTemp(t)
ctx, cancel := context.WithCancel(context.Background())
ch, err := fs.Watch(ctx)
if err != nil {
t.Fatalf("Watch: %v", err)
}
cancel()

// Channel should close within 1s.
timer := time.NewTimer(2 * time.Second)
defer timer.Stop()
for {
select {
case _, ok := <-ch:
if !ok {
return // success
}
case <-timer.C:
t.Fatal("channel not closed within 2s after cancel")
}
}
}

// 15. Multiple Watch subscribers each receive each event
func TestMultipleSubscribers(t *testing.T) {
fs := openTemp(t)
ctx, cancel := context.WithCancel(context.Background())
defer cancel()

ch1, _ := fs.Watch(ctx)
ch2, _ := fs.Watch(ctx)

fs.Put(ctx, testKey("v1"), testSpec("a"), 0)

ev1 := drainN(t, ch1, 1, 2*time.Second)
ev2 := drainN(t, ch2, 1, 2*time.Second)
if len(ev1) != 1 || len(ev2) != 1 {
t.Errorf("expected 1 event on each subscriber; got %d and %d", len(ev1), len(ev2))
}
}

// 16. After successful Put, no .tmp file remains
func TestNoTmpFileRemains(t *testing.T) {
dir := t.TempDir()
fs, _ := Open(dir)
defer fs.Close()

fs.Put(context.Background(), testKey("v1"), testSpec("a"), 0)

// Walk dir for any .tmp files.
filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
if err != nil {
return err
}
if filepath.Ext(path) == ".tmp" {
t.Errorf("found leftover tmp file: %s", path)
}
return nil
})
}

// 17. 100 concurrent goroutines Put with different names → all succeed
func TestConcurrentPut(t *testing.T) {
fs := openTemp(t)
ctx := context.Background()

var wg sync.WaitGroup
errCh := make(chan error, 100)
for i := 0; i < 100; i++ {
wg.Add(1)
go func(n int) {
defer wg.Done()
name := fmt.Sprintf("item-%03d", n)
_, err := fs.Put(ctx, testKey(name), testSpec(name), 0)
if err != nil {
errCh <- err
}
}(i)
}
wg.Wait()
close(errCh)
for err := range errCh {
t.Errorf("concurrent Put error: %v", err)
}

specs, _ := fs.List(ctx, store.DefaultNamespace, "vnet")
if len(specs) != 100 {
t.Errorf("expected 100 specs, got %d", len(specs))
}
}

// 18. Open with malformed JSON → error mentioning path
func TestOpenMalformedJSON(t *testing.T) {
dir := t.TempDir()

// Create a malformed file in the right directory structure.
badDir := filepath.Join(dir, "default", "vnet")
os.MkdirAll(badDir, 0o750)
os.WriteFile(filepath.Join(badDir, "bad.json"), []byte("{broken"), 0o644)

_, err := Open(dir)
if err == nil {
t.Fatal("expected error for malformed JSON")
}
if !containsAny(err.Error(), "parse", "bad.json") {
t.Errorf("error should mention file path, got: %v", err)
}
}

// --- helpers ---

func drainN(t *testing.T, ch <-chan store.DesiredEvent, n int, timeout time.Duration) []store.DesiredEvent {
t.Helper()
var events []store.DesiredEvent
timer := time.NewTimer(timeout)
defer timer.Stop()
for i := 0; i < n; i++ {
select {
case ev, ok := <-ch:
if !ok {
t.Fatalf("channel closed after %d events (wanted %d)", i, n)
}
events = append(events, ev)
case <-timer.C:
t.Fatalf("timeout after %d events (wanted %d)", i, n)
}
}
return events
}

func containsAny(s string, subs ...string) bool {
for _, sub := range subs {
if contains(s, sub) {
return true
}
}
return false
}

func contains(s, sub string) bool {
return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
for i := 0; i <= len(s)-len(sub); i++ {
if s[i:i+len(sub)] == sub {
return true
}
}
return false
}