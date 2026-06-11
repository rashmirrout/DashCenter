// LivePutEffect + DirectStoreRehomer + Broadcaster coverage tests.
package migration

import (
	"context"
	"errors"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func seedEni(t *testing.T, st store.DesiredStore, ns, name, hint string) {
	t.Helper()
	spec := &dashcenterv1.EniSpec{
		Name: name, VnetName: "v1", MacAddress: "00:11:22:00:00:01",
		UnderlayIp: "10.0.0.1", AdminState: "up",
		PlacementHintDpuIds: []string{hint},
	}
	if _, err := st.Put(context.Background(), store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}, spec, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func TestDirectStoreRehomer_RoundTrip(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	seedEni(t, st, "default", "eni-1", "dpu-source")
	rh := &DirectStoreRehomer{Store: st}

	prior, err := rh.Rehome(context.Background(), "default", "eni-1", "dpu-target")
	if err != nil {
		t.Fatalf("Rehome: %v", err)
	}
	if len(prior) != 1 || prior[0] != "dpu-source" {
		t.Errorf("prior=%v; want [dpu-source]", prior)
	}
	// Verify the store reflects the new placement.
	sp, _ := st.Get(context.Background(), store.ObjectKey{Namespace: "default", Kind: "eni", Name: "eni-1"})
	if !containsBytes(sp.Data, "dpu-target") {
		t.Errorf("post-rehome eni data missing dpu-target: %s", sp.Data)
	}
	// Restore.
	if err := rh.Restore(context.Background(), "default", "eni-1", prior); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	sp, _ = st.Get(context.Background(), store.ObjectKey{Namespace: "default", Kind: "eni", Name: "eni-1"})
	if !containsBytes(sp.Data, "dpu-source") {
		t.Errorf("post-restore eni data missing dpu-source: %s", sp.Data)
	}
}

func TestDirectStoreRehomer_UnknownEni(t *testing.T) {
	st, _ := filstore.Open(t.TempDir())
	defer st.Close()
	rh := &DirectStoreRehomer{Store: st}
	if _, err := rh.Rehome(context.Background(), "default", "no-such", "dst"); err == nil {
		t.Error("want error on missing ENI")
	}
	if err := rh.Restore(context.Background(), "default", "no-such", []string{"x"}); err == nil {
		t.Error("want error on missing ENI restore")
	}
}

func TestLivePutEffect_Cutover_CapturesSnapshot(t *testing.T) {
	st, _ := filstore.Open(t.TempDir())
	defer st.Close()
	seedEni(t, st, "default", "eni-A", "dpu-1")
	seedEni(t, st, "default", "eni-B", "dpu-1")

	e := &LivePutEffect{Rehomer: &DirectStoreRehomer{Store: st}}
	plan := dashcenterv1.MigrationPlan{
		Namespace: "default", EniNames: []string{"eni-A", "eni-B"},
		SourceDpuId: "dpu-1", TargetDpuId: "dpu-2",
	}
	snap, err := e.Cutover(context.Background(), plan)
	if err != nil {
		t.Fatalf("Cutover: %v", err)
	}
	if len(snap.PerEni) != 2 {
		t.Errorf("snapshot=%v; want 2 entries", snap.PerEni)
	}
	for eni, hint := range snap.PerEni {
		if len(hint) != 1 || hint[0] != "dpu-1" {
			t.Errorf("snapshot[%s]=%v; want [dpu-1]", eni, hint)
		}
	}
}

// brokenRehomer fails Rehome on the second call so we can test the
// rollback-on-first-failure path.
type brokenRehomer struct {
	inner   *DirectStoreRehomer
	failAt  int
	count   int
	restored map[string][]string
}

func (b *brokenRehomer) Rehome(ctx context.Context, ns, name, dst string) ([]string, error) {
	b.count++
	if b.count == b.failAt {
		return nil, errors.New("simulated dpu reject")
	}
	return b.inner.Rehome(ctx, ns, name, dst)
}
func (b *brokenRehomer) Restore(ctx context.Context, ns, name string, prior []string) error {
	if b.restored == nil {
		b.restored = map[string][]string{}
	}
	b.restored[name] = prior
	return b.inner.Restore(ctx, ns, name, prior)
}

func TestLivePutEffect_Cutover_FailureRestoresPriorRehomes(t *testing.T) {
	st, _ := filstore.Open(t.TempDir())
	defer st.Close()
	seedEni(t, st, "default", "e1", "dpu-1")
	seedEni(t, st, "default", "e2", "dpu-1")

	r := &brokenRehomer{inner: &DirectStoreRehomer{Store: st}, failAt: 2}
	e := &LivePutEffect{Rehomer: r}
	plan := dashcenterv1.MigrationPlan{
		Namespace: "default", EniNames: []string{"e1", "e2"},
		SourceDpuId: "dpu-1", TargetDpuId: "dpu-2",
	}
	_, err := e.Cutover(context.Background(), plan)
	if err == nil {
		t.Fatal("want error")
	}
	// e1 (rehomed before failure) must have been restored.
	if _, ok := r.restored["e1"]; !ok {
		t.Errorf("restored=%v; want e1 to have been undone", r.restored)
	}
}

func TestLivePutEffect_UndoCutover_Restores(t *testing.T) {
	st, _ := filstore.Open(t.TempDir())
	defer st.Close()
	seedEni(t, st, "default", "eni-A", "dpu-1")
	rh := &DirectStoreRehomer{Store: st}
	e := &LivePutEffect{Rehomer: rh}
	plan := dashcenterv1.MigrationPlan{
		Namespace: "default", EniNames: []string{"eni-A"},
		SourceDpuId: "dpu-1", TargetDpuId: "dpu-2",
	}
	snap, _ := e.Cutover(context.Background(), plan)
	if err := e.UndoCutover(context.Background(), plan, snap); err != nil {
		t.Fatalf("UndoCutover: %v", err)
	}
	sp, _ := st.Get(context.Background(), store.ObjectKey{Namespace: "default", Kind: "eni", Name: "eni-A"})
	if !containsBytes(sp.Data, "dpu-1") {
		t.Errorf("post-undo data missing dpu-1: %s", sp.Data)
	}
}

func TestLivePutEffect_NoRehomer_Errors(t *testing.T) {
	e := &LivePutEffect{}
	plan := dashcenterv1.MigrationPlan{EniNames: []string{"x"}}
	if _, err := e.Cutover(context.Background(), plan); err == nil {
		t.Error("want error with nil Rehomer")
	}
	if err := e.UndoCutover(context.Background(), plan, Snapshot{}); err == nil {
		t.Error("want error with nil Rehomer in UndoCutover")
	}
}

func TestLivePutEffect_NoOpPhases(t *testing.T) {
	// Prepare / Sync / Drain are no-ops today and must return nil.
	e := &LivePutEffect{Rehomer: nil}
	plan := dashcenterv1.MigrationPlan{}
	if err := e.PrepareTarget(context.Background(), plan); err != nil {
		t.Errorf("PrepareTarget: %v", err)
	}
	if err := e.SyncFlows(context.Background(), plan); err != nil {
		t.Errorf("SyncFlows: %v", err)
	}
	if err := e.DrainSource(context.Background(), plan); err != nil {
		t.Errorf("DrainSource: %v", err)
	}
}

func TestBroadcaster_Count(t *testing.T) {
	b := NewBroadcaster()
	_, c1 := b.Subscribe("")
	_, c2 := b.Subscribe("session-x")
	if b.Count() != 2 {
		t.Errorf("Count=%d; want 2", b.Count())
	}
	c1()
	c2()
}

func TestBroadcaster_FilterBySessionID(t *testing.T) {
	b := NewBroadcaster()
	chA, ca := b.Subscribe("session-a")
	defer ca()
	b.Publish(Session{ID: "session-b"})
	b.Publish(Session{ID: "session-a"})
	select {
	case s := <-chA:
		if s.ID != "session-a" {
			t.Errorf("got %v; want session-a", s.ID)
		}
	default:
		t.Error("filtered subscriber missed its event")
	}
}

func TestBroadcaster_SlowSubscriberDropsNonBlocking(t *testing.T) {
	b := NewBroadcaster()
	// Subscribe but never read.
	_, cancel := b.Subscribe("")
	defer cancel()
	for i := 0; i < 200; i++ {
		b.Publish(Session{ID: "x"})
	}
	// Reaching this line = pass.
}

// --- List filter coverage --------------------------------------------

func TestList_FilterByNamespaceAndSource(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	p1 := mkPlan() // ns=default, source=dpu-1
	p2 := *mkPlan()
	p2.Namespace = "team-a"
	p2.SourceDpuId = "dpu-other"
	_, _ = c.StartSession(context.Background(), p1)
	_, _ = c.StartSession(context.Background(), &p2)

	got := c.List(context.Background(), &ListFilter{Namespaces: []string{"team-a"}})
	if len(got) != 1 || got[0].Plan.Namespace != "team-a" {
		t.Errorf("ns filter got %v; want 1 row in team-a", got)
	}
	got = c.List(context.Background(), &ListFilter{SourceDPUIDs: []string{"dpu-other"}})
	if len(got) != 1 || got[0].Plan.SourceDpuId != "dpu-other" {
		t.Errorf("src filter got %v; want 1 row with dpu-other", got)
	}
	got = c.List(context.Background(), &ListFilter{TargetDPUIDs: []string{"dpu-2"}})
	if len(got) < 1 {
		t.Errorf("target filter got %v; want >=1 row", got)
	}
	// Phase filter.
	got = c.List(context.Background(), &ListFilter{Phases: []dashcenterv1.MigrationPhase{dashcenterv1.MigrationPhase_MIGRATION_PHASE_ADMISSION}})
	if len(got) != 2 {
		t.Errorf("phase filter got %v; want 2 in ADMISSION", got)
	}
}

func TestValidatePlan_NilPlan(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	if _, err := c.ValidatePlan(context.Background(), nil); err == nil {
		t.Error("want error on nil plan")
	}
}

func TestCreatePlan_MissingSource(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	if _, err := c.CreatePlan(context.Background(), "default", &dashcenterv1.CreateMigrationPlanRequest{
		EniNames: []string{"e1"}, TargetDpuId: "b",
	}); err == nil {
		t.Error("want error on missing source")
	}
}

func TestCreatePlan_MissingTarget(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	if _, err := c.CreatePlan(context.Background(), "default", &dashcenterv1.CreateMigrationPlanRequest{
		EniNames: []string{"e1"}, SourceDpuId: "a",
	}); err == nil {
		t.Error("want error on missing target")
	}
}

func TestCreatePlan_NilRequest(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	if _, err := c.CreatePlan(context.Background(), "", nil); err == nil {
		t.Error("want error on nil req")
	}
}

func TestNew_NilStore_Error(t *testing.T) {
	if _, err := New(context.Background(), nil, &NoOpEffect{}); err == nil {
		t.Error("want error on nil store")
	}
}

func TestNew_NilEffect_UsesNoOp(t *testing.T) {
	st, _ := filstore.Open(t.TempDir())
	defer st.Close()
	c, err := New(context.Background(), st, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Drive a happy path to prove NoOp is wired.
	s, _ := c.StartSession(context.Background(), mkPlan())
	if s == nil {
		t.Fatal("StartSession returned nil")
	}
}

func TestAdvance_UnknownSession(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	_, err := c.Advance(context.Background(), "no-such", 1, dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestRollback_UnknownSession(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	_, err := c.Rollback(context.Background(), "no-such", 1, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestAbort_UnknownSession(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	_, err := c.Abort(context.Background(), "no-such", 1, "")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("got %v; want ErrNotFound", err)
	}
}

func TestAbort_GenerationMismatch(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	_, err := c.Abort(context.Background(), s.ID, 99, "")
	if !errors.Is(err, ErrGenerationMismatch) {
		t.Errorf("got %v; want ErrGenerationMismatch", err)
	}
}

func TestAbort_AlreadyTerminal(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	s, _ = c.Abort(context.Background(), s.ID, s.Generation, "")
	_, err := c.Abort(context.Background(), s.ID, s.Generation, "")
	if !errors.Is(err, ErrTerminal) {
		t.Errorf("got %v; want ErrTerminal", err)
	}
}

func TestAdvance_AlreadyTerminal(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	s, _ = c.Abort(context.Background(), s.ID, s.Generation, "")
	_, err := c.Advance(context.Background(), s.ID, s.Generation, dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT, "")
	if !errors.Is(err, ErrTerminal) {
		t.Errorf("got %v; want ErrTerminal", err)
	}
}

func TestRollback_GenerationMismatch(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	_, err := c.Rollback(context.Background(), s.ID, 99, "")
	if !errors.Is(err, ErrGenerationMismatch) {
		t.Errorf("got %v; want ErrGenerationMismatch", err)
	}
}

// --- containsBytes helper for tests ---------------------------------

func containsBytes(b []byte, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(b); i++ {
		if string(b[i:i+len(sub)]) == sub {
			return true
		}
	}
	return false
}
