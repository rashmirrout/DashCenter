// PC-1 operations tests: cordon admission + audit ring.
package operations

import (
	"errors"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// fakeInv is a minimal Inventory impl. The Cordoned flag is stored
// per ID; SetCordoned mutates it; Get/List return clones.
type fakeInv struct {
	m map[string]*inventory.DpuEntry
}

func newFakeInv(ids ...string) *fakeInv {
	m := map[string]*inventory.DpuEntry{}
	for _, id := range ids {
		m[id] = &inventory.DpuEntry{ID: id, Endpoint: id + ":50051"}
	}
	return &fakeInv{m: m}
}

func (f *fakeInv) Get(id string) (inventory.DpuEntry, error) {
	e, ok := f.m[id]
	if !ok {
		return inventory.DpuEntry{}, errors.New("not found")
	}
	return *e, nil
}

func (f *fakeInv) List() []inventory.DpuEntry {
	out := make([]inventory.DpuEntry, 0, len(f.m))
	for _, e := range f.m {
		out = append(out, *e)
	}
	return out
}

func (f *fakeInv) SetCordoned(id string, cordoned bool) error {
	e, ok := f.m[id]
	if !ok {
		return errors.New("not found")
	}
	e.Cordoned = cordoned
	return nil
}

func TestCordon_Idempotent(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	for i := 0; i < 3; i++ {
		if err := mgr.Cordon("dpu-1", "maintenance"); err != nil {
			t.Fatalf("cordon iter %d: %v", i, err)
		}
	}
	if !mgr.IsCordoned("dpu-1") {
		t.Error("dpu-1 should be cordoned")
	}
	// Three calls → three audit entries.
	if got := len(mgr.AuditRecent(0)); got != 3 {
		t.Errorf("audit entries=%d; want 3", got)
	}
}

func TestUncordon_Idempotent(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	mgr.Cordon("dpu-1", "")
	mgr.Uncordon("dpu-1", "")
	mgr.Uncordon("dpu-1", "second")
	if mgr.IsCordoned("dpu-1") {
		t.Error("dpu-1 should not be cordoned")
	}
}

func TestCordon_UnknownDPU(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	err := mgr.Cordon("dpu-typo", "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestCordon_EmptyID(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	if err := mgr.Cordon("", ""); !errors.Is(err, ErrNotFound) {
		t.Errorf("empty id: %v; want ErrNotFound", err)
	}
}

func TestListCordoned(t *testing.T) {
	mgr := New(newFakeInv("dpu-1", "dpu-2", "dpu-3"))
	mgr.Cordon("dpu-1", "maint")
	mgr.Cordon("dpu-3", "drain")
	got := mgr.ListCordoned()
	if len(got) != 2 {
		t.Fatalf("ListCordoned=%v; want 2 entries", got)
	}
	// inventory.List returns entries sorted by ID, so we can assert order.
	if got[0] != "dpu-1" || got[1] != "dpu-3" {
		t.Errorf("ListCordoned=%v; want [dpu-1 dpu-3]", got)
	}
}

func TestIsCordoned_Unknown_False(t *testing.T) {
	mgr := New(newFakeInv("dpu-1"))
	if mgr.IsCordoned("dpu-typo") {
		t.Error("IsCordoned(typo) should be false")
	}
}

func TestAuditRecent_NewestFirst(t *testing.T) {
	mgr := New(newFakeInv("a", "b", "c"))
	mgr.Cordon("a", "one")
	mgr.Cordon("b", "two")
	mgr.Cordon("c", "three")
	got := mgr.AuditRecent(2)
	if len(got) != 2 {
		t.Fatalf("len=%d; want 2", len(got))
	}
	if got[0].DpuID != "c" || got[1].DpuID != "b" {
		t.Errorf("order wrong: %v", got)
	}
	if got[0].Reason != "three" {
		t.Errorf("reason mismatch: %q", got[0].Reason)
	}
}

func TestAuditRecent_RingTrim(t *testing.T) {
	mgr := New(newFakeInv("a"))
	// Exceed cap → ring trims to auditCap.
	for i := 0; i < auditCap+50; i++ {
		mgr.Cordon("a", "spam")
	}
	if got := len(mgr.AuditRecent(0)); got != auditCap {
		t.Errorf("audit cap = %d; want %d", got, auditCap)
	}
}

func TestNilManager_Safe(t *testing.T) {
	var mgr *Manager
	if err := mgr.Cordon("a", ""); err == nil {
		t.Error("nil Manager Cordon should error")
	}
	if err := mgr.Uncordon("a", ""); err == nil {
		t.Error("nil Manager Uncordon should error")
	}
	if mgr.IsCordoned("a") {
		t.Error("nil Manager IsCordoned should be false")
	}
	if mgr.ListCordoned() != nil {
		t.Error("nil Manager ListCordoned should be nil")
	}
}
