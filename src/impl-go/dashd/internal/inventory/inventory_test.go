package inventory

import (
"errors"
"os"
"path/filepath"
"sync"
"testing"

dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func entry(id, ep string) DpuEntry {
return DpuEntry{ID: id, Endpoint: ep, Labels: map[string]string{"rack": "A1"}}
}

// 1. Register adds entry, state = REGISTERING
func TestRegisterState(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
e, _ := inv.Get("dpu-0")
if e.State != dashcenterv1.DpuState_DPU_STATE_REGISTERING {
t.Errorf("expected REGISTERING, got %v", e.State)
}
}

// 2. Register empty ID → error
func TestRegisterEmptyID(t *testing.T) {
inv := New()
if err := inv.Register(DpuEntry{Endpoint: "x"}); err == nil {
t.Error("expected error for empty ID")
}
}

// 3. Register empty Endpoint → error
func TestRegisterEmptyEndpoint(t *testing.T) {
inv := New()
if err := inv.Register(DpuEntry{ID: "dpu-0"}); err == nil {
t.Error("expected error for empty endpoint")
}
}

// 4. Re-register preserves State
func TestReRegisterPreservesState(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
inv.SetState("dpu-0", dashcenterv1.DpuState_DPU_STATE_UP)
inv.Register(DpuEntry{ID: "dpu-0", Endpoint: "localhost:60000"})
e, _ := inv.Get("dpu-0")
if e.State != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("expected UP after re-register, got %v", e.State)
}
if e.Endpoint != "localhost:60000" {
t.Errorf("expected updated endpoint, got %s", e.Endpoint)
}
}

// 5. Deregister removes
func TestDeregister(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
inv.Deregister("dpu-0")
_, err := inv.Get("dpu-0")
if !errors.Is(err, ErrNotFound) {
t.Error("expected ErrNotFound after deregister")
}
}

// 6. Deregister non-existent → ErrNotFound
func TestDeregisterNotFound(t *testing.T) {
inv := New()
if err := inv.Deregister("nope"); !errors.Is(err, ErrNotFound) {
t.Errorf("expected ErrNotFound, got %v", err)
}
}

// 7. Get non-existent → ErrNotFound
func TestGetNotFound(t *testing.T) {
inv := New()
_, err := inv.Get("nope")
if !errors.Is(err, ErrNotFound) {
t.Errorf("expected ErrNotFound, got %v", err)
}
}

// 8. List sorted by ID
func TestListSorted(t *testing.T) {
inv := New()
for _, id := range []string{"c", "a", "b"} {
inv.Register(DpuEntry{ID: id, Endpoint: "x:" + id})
}
list := inv.List()
if len(list) != 3 {
t.Fatalf("expected 3, got %d", len(list))
}
for i := 0; i < len(list)-1; i++ {
if list[i].ID >= list[i+1].ID {
t.Errorf("not sorted: %v", list)
break
}
}
}

// 9. SetState updates LastSeen
func TestSetStateUpdatesLastSeen(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
e1, _ := inv.Get("dpu-0")
inv.SetState("dpu-0", dashcenterv1.DpuState_DPU_STATE_UP)
e2, _ := inv.Get("dpu-0")
if !e2.LastSeen.After(e1.LastSeen) && !e2.LastSeen.Equal(e1.LastSeen) {
t.Error("expected LastSeen to be updated")
}
if e2.State != dashcenterv1.DpuState_DPU_STATE_UP {
t.Errorf("expected UP, got %v", e2.State)
}
}

// 10. SetCapabilities
func TestSetCapabilities(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
caps := &dashcenterv1.DpuCapabilities{Ipv6: true}
inv.SetCapabilities("dpu-0", caps)
e, _ := inv.Get("dpu-0")
if e.Capabilities == nil || !e.Capabilities.Ipv6 {
t.Error("expected capabilities with Ipv6=true")
}
}

// 11. Concurrent IncrementErrors
func TestConcurrentIncrementErrors(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))

var wg sync.WaitGroup
for i := 0; i < 100; i++ {
wg.Add(1)
go func() {
defer wg.Done()
inv.IncrementErrors("dpu-0")
}()
}
wg.Wait()

e, _ := inv.Get("dpu-0")
if e.ConsecErrors != 100 {
t.Errorf("expected 100 errors, got %d", e.ConsecErrors)
}
}

// 12. ResetErrors
func TestResetErrors(t *testing.T) {
inv := New()
inv.Register(entry("dpu-0", "localhost:50051"))
inv.IncrementErrors("dpu-0")
inv.IncrementErrors("dpu-0")
inv.ResetErrors("dpu-0")
e, _ := inv.Get("dpu-0")
if e.ConsecErrors != 0 {
t.Errorf("expected 0 errors, got %d", e.ConsecErrors)
}
}

// 13. LoadFromFile valid YAML
func TestLoadFromFileValid(t *testing.T) {
yaml := `dpus:
  - id: dpu-0
    endpoint: localhost:50051
    labels:
      rack: A1
  - id: dpu-1
    endpoint: localhost:50052
`
path := writeTemp(t, "inv.yaml", yaml)
inv := New()
if err := LoadFromFile(path, inv); err != nil {
t.Fatalf("LoadFromFile: %v", err)
}
list := inv.List()
if len(list) != 2 {
t.Errorf("expected 2 DPUs, got %d", len(list))
}
}

// 14. LoadFromFile missing file → error
func TestLoadFromFileMissing(t *testing.T) {
inv := New()
if err := LoadFromFile("/nonexistent/path", inv); err == nil {
t.Error("expected error for missing file")
}
}

// 15. LoadFromFile malformed YAML → error
func TestLoadFromFileMalformed(t *testing.T) {
path := writeTemp(t, "bad.yaml", "dpus: [broken")
inv := New()
if err := LoadFromFile(path, inv); err == nil {
t.Error("expected error for malformed YAML")
}
}

// 16. LoadFromFile duplicate ID → last wins (no error)
func TestLoadFromFileDuplicateID(t *testing.T) {
yaml := `dpus:
  - id: dpu-0
    endpoint: localhost:50051
  - id: dpu-0
    endpoint: localhost:60000
`
path := writeTemp(t, "dup.yaml", yaml)
inv := New()
if err := LoadFromFile(path, inv); err != nil {
t.Fatalf("LoadFromFile: %v", err)
}
e, _ := inv.Get("dpu-0")
if e.Endpoint != "localhost:60000" {
t.Errorf("expected last endpoint, got %s", e.Endpoint)
}
}

// 17. Clone deep-copies Labels
func TestCloneDeepCopies(t *testing.T) {
e := DpuEntry{
ID:       "dpu-0",
Endpoint: "x",
Labels:   map[string]string{"a": "1"},
}
c := e.Clone()
c.Labels["a"] = "changed"
if e.Labels["a"] != "1" {
t.Error("clone mutated original labels")
}
}

// 18. Subscribe callback fires
func TestSubscribeCallback(t *testing.T) {
inv := New()
var called bool
var calledID string
var calledRemoved bool
inv.Subscribe(func(id, ep string, removed bool) {
called = true
calledID = id
calledRemoved = removed
})
inv.Register(entry("dpu-0", "localhost:50051"))
if !called || calledID != "dpu-0" || calledRemoved {
t.Error("subscribe callback not fired correctly on register")
}

called = false
inv.Deregister("dpu-0")
if !called || calledID != "dpu-0" || !calledRemoved {
t.Error("subscribe callback not fired correctly on deregister")
}
}

func writeTemp(t *testing.T, name, content string) string {
t.Helper()
dir := t.TempDir()
path := filepath.Join(dir, name)
os.WriteFile(path, []byte(content), 0o644)
return path
}