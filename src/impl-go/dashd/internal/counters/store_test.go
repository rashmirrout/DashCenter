// store_test.go covers Put/Get/List/Delete + Subscribe drop-on-slow +
// concurrent stress.

package counters

import (
	"sync"
	"testing"
	"time"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func sampleEntry(id string, decap int64) Entry {
	return Entry{
		DpuID: id,
		Report: &dashcenterv1.CounterReport{
			DpuId:      id,
			SampledAt:  timestamppb.Now(),
			VxlanDecap: decap,
		},
	}
}

func TestStore_PutGet(t *testing.T) {
	s := NewStore()
	s.Put(sampleEntry("dpu-a", 100))
	e, ok := s.Get("dpu-a")
	if !ok {
		t.Fatalf("Get(dpu-a) not found")
	}
	if e.DpuID != "dpu-a" || e.Report.GetVxlanDecap() != 100 {
		t.Errorf("entry = %+v, want dpu-a/decap=100", e)
	}
	if e.UpdateAt.IsZero() {
		t.Errorf("UpdateAt should be stamped by Put")
	}
}

// TestStore_GetReport covers the PE-3c streaming-surface accessor: it
// returns just the typed *CounterReport without the surrounding Entry
// (no PerEni/PerVnet sub-rollups) and gracefully handles missing keys.
func TestStore_GetReport_Found(t *testing.T) {
	s := NewStore()
	s.Put(sampleEntry("dpu-a", 42))
	rep, ok := s.GetReport("dpu-a")
	if !ok {
		t.Fatalf("GetReport(dpu-a) not found")
	}
	if rep == nil || rep.GetDpuId() != "dpu-a" || rep.GetVxlanDecap() != 42 {
		t.Errorf("report = %+v, want dpu-a/decap=42", rep)
	}
}

func TestStore_GetReport_Missing(t *testing.T) {
	s := NewStore()
	rep, ok := s.GetReport("nope")
	if ok || rep != nil {
		t.Errorf("GetReport(missing) = (%v,%v); want (nil,false)", rep, ok)
	}
}

func TestStore_Put_RejectsEmptyOrNil(t *testing.T) {
	s := NewStore()
	s.Put(Entry{DpuID: "", Report: &dashcenterv1.CounterReport{}})
	s.Put(Entry{DpuID: "x", Report: nil})
	if s.Len() != 0 {
		t.Errorf("invalid Puts populated the store: %d entries", s.Len())
	}
}

func TestStore_Put_Replaces(t *testing.T) {
	s := NewStore()
	s.Put(sampleEntry("dpu-a", 100))
	s.Put(sampleEntry("dpu-a", 200))
	e, _ := s.Get("dpu-a")
	if got := e.Report.GetVxlanDecap(); got != 200 {
		t.Errorf("decap = %d, want 200 (second Put replaces)", got)
	}
	if s.Len() != 1 {
		t.Errorf("Len = %d, want 1", s.Len())
	}
}

func TestStore_List_Sorted(t *testing.T) {
	s := NewStore()
	s.Put(sampleEntry("dpu-c", 3))
	s.Put(sampleEntry("dpu-a", 1))
	s.Put(sampleEntry("dpu-b", 2))
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	want := []string{"dpu-a", "dpu-b", "dpu-c"}
	for i, e := range got {
		if e.DpuID != want[i] {
			t.Errorf("List[%d] = %q, want %q", i, e.DpuID, want[i])
		}
	}
}

func TestStore_Delete(t *testing.T) {
	s := NewStore()
	s.Put(sampleEntry("dpu-a", 1))
	if !s.Delete("dpu-a") {
		t.Errorf("Delete returned false, want true")
	}
	if _, ok := s.Get("dpu-a"); ok {
		t.Errorf("entry still present after Delete")
	}
	if s.Delete("dpu-a") {
		t.Errorf("Delete on missing returned true")
	}
}

func TestStore_Subscribe_Notify(t *testing.T) {
	s := NewStore()
	ch := make(chan string, 4)
	unsub := s.Subscribe(ch)
	defer unsub()

	s.Put(sampleEntry("dpu-a", 1))
	s.Put(sampleEntry("dpu-b", 2))

	got := make([]string, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case id := <-ch:
			got = append(got, id)
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for notification %d", i)
		}
	}
	if got[0] != "dpu-a" || got[1] != "dpu-b" {
		t.Errorf("notifications = %v, want [dpu-a dpu-b]", got)
	}
}

func TestStore_Subscribe_DropOnSlow(t *testing.T) {
	s := NewStore()
	ch := make(chan string, 1) // full after first
	unsub := s.Subscribe(ch)
	defer unsub()

	// 3 puts → 1 enqueued, 2 dropped silently. No deadlock, no panic.
	s.Put(sampleEntry("dpu-a", 1))
	s.Put(sampleEntry("dpu-b", 2))
	s.Put(sampleEntry("dpu-c", 3))

	select {
	case id := <-ch:
		if id == "" {
			t.Errorf("got empty notification")
		}
	case <-time.After(time.Second):
		t.Fatalf("first notification was dropped — expected drop-on-slow to keep it")
	}
	// Drain any extras; we only assert that we did not block above.
	select {
	case <-ch:
	default:
	}
}

func TestStore_Unsubscribe(t *testing.T) {
	s := NewStore()
	ch := make(chan string, 4)
	unsub := s.Subscribe(ch)
	unsub() // remove

	s.Put(sampleEntry("dpu-a", 1))
	select {
	case id := <-ch:
		t.Errorf("got %q after unsubscribe", id)
	case <-time.After(50 * time.Millisecond):
		// expected
	}

	// Idempotent: calling unsub() twice is fine.
	unsub()
}

func TestStore_Unsubscribe_AfterReorder(t *testing.T) {
	s := NewStore()
	chA := make(chan string, 1)
	chB := make(chan string, 1)
	chC := make(chan string, 1)
	unsubA := s.Subscribe(chA)
	unsubB := s.Subscribe(chB)
	unsubC := s.Subscribe(chC)

	// Unsub the middle one — chA now sits at idx 0, chC moves to idx 1.
	unsubB()

	// chB's original idx is stale; calling its unsub MUST NOT remove
	// the wrong subscriber via the fast path.
	unsubB()

	s.Put(sampleEntry("dpu-a", 1))
	for _, ch := range []chan string{chA, chC} {
		select {
		case <-ch:
		case <-time.After(time.Second):
			t.Fatalf("a non-unsubscribed subscriber missed a notification")
		}
	}

	unsubA()
	unsubC()
}

func TestStore_Concurrent(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup
	const N = 100

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "dpu-" + string(rune('a'+i%26))
			s.Put(sampleEntry(id, int64(i)))
			_, _ = s.Get(id)
			_ = s.List()
		}(i)
	}
	wg.Wait()
	if s.Len() == 0 || s.Len() > 26 {
		t.Errorf("Len = %d after concurrent stress, want 1..26", s.Len())
	}
}
