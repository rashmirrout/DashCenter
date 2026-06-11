// PC-G4 / PC-G5 / PC-G6 migration coordinator tests.
package migration

import (
	"context"
	"errors"
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func newCoord(t *testing.T, effect CutoverEffect) (*Coordinator, *filstore.FileStore) {
	t.Helper()
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	c, err := New(context.Background(), st, effect)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c, st
}

func mkPlan() *dashcenterv1.MigrationPlan {
	return &dashcenterv1.MigrationPlan{
		Namespace:   "default",
		EniNames:    []string{"eni-1", "eni-2"},
		SourceDpuId: "dpu-1",
		TargetDpuId: "dpu-2",
		Strategy:    dashcenterv1.MigrationStrategy_MIGRATION_STRATEGY_NEW_FLOWS_FIRST_DRAIN,
	}
}

// --- PC-G4: 10-phase happy path -------------------------------------

func TestHappyPath_AllTenPhases_PC_G4(t *testing.T) {
	effect := &NoOpEffect{}
	c, _ := newCoord(t, effect)

	s, err := c.StartSession(context.Background(), mkPlan())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_ADMISSION {
		t.Fatalf("initial phase=%s; want ADMISSION", s.Phase)
	}

	// Walk ADMISSION -> SNAPSHOT -> PREPARE -> SYNC -> READY -> CUTOVER
	// -> DRAIN -> COMMIT -> FINALIZE -> COMPLETED.
	phases := []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_DRAIN,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMMIT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_FINALIZE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMPLETED,
	}
	for _, next := range phases {
		s, err = c.Advance(context.Background(), s.ID, s.Generation, next, "")
		if err != nil {
			t.Fatalf("Advance -> %s: %v", next, err)
		}
		if s.Phase != next {
			t.Fatalf("after Advance want %s got %s", next, s.Phase)
		}
	}
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMPLETED {
		t.Errorf("final phase=%s; want COMPLETED", s.Phase)
	}
	// PrepareTarget, SyncFlows, Cutover, DrainSource each called exactly once.
	if effect.PrepareCalls != 1 || effect.SyncCalls != 1 || effect.CutoverCalls != 1 || effect.DrainCalls != 1 {
		t.Errorf("effect calls = prepare=%d sync=%d cutover=%d drain=%d; want 1/1/1/1",
			effect.PrepareCalls, effect.SyncCalls, effect.CutoverCalls, effect.DrainCalls)
	}
	// And UndoCutover NEVER called on happy path.
	if effect.UndoCalls != 0 {
		t.Errorf("UndoCalls=%d; want 0 (no rollback)", effect.UndoCalls)
	}
	// Generation should have ticked once per advance.
	if s.Generation != 10 {
		t.Errorf("final generation=%d; want 10 (1 start + 9 advances)", s.Generation)
	}
}

func TestAdvance_GenerationMismatch_PC_G4(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	// Pass the wrong generation.
	_, err := c.Advance(context.Background(), s.ID, 99, dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT, "")
	if !errors.Is(err, ErrGenerationMismatch) {
		t.Errorf("got %v; want ErrGenerationMismatch", err)
	}
}

func TestAdvance_SkipPhase_Rejected(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	// Try to skip from ADMISSION directly to CUTOVER.
	_, err := c.Advance(context.Background(), s.ID, s.Generation, dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER, "")
	if !errors.Is(err, ErrInvalidTransition) {
		t.Errorf("got %v; want ErrInvalidTransition", err)
	}
}

func TestAdvance_EffectFailure_AbortsAdvance(t *testing.T) {
	effect := &NoOpEffect{CutoverErr: errors.New("dpu unreachable")}
	c, _ := newCoord(t, effect)
	s, _ := c.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	// Now Cutover will fail.
	_, err := c.Advance(context.Background(), s.ID, s.Generation, dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER, "")
	if err == nil {
		t.Fatal("want error from cutover")
	}
	// Session must still be at READY; failure did not advance.
	live, _ := c.Get(context.Background(), s.ID)
	if live.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY {
		t.Errorf("after failure phase=%s; want READY", live.Phase)
	}
}

// --- PC-G5: rollback from CUTOVER restores original placement -------

func TestRollback_FromCutover_RestoresOriginal_PC_G5(t *testing.T) {
	effect := &NoOpEffect{}
	c, _ := newCoord(t, effect)
	s, _ := c.StartSession(context.Background(), mkPlan())
	// Walk up to CUTOVER (inclusive).
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	// Snapshot must be populated.
	if len(s.Snapshot.PerEni) != 2 {
		t.Errorf("snapshot=%v; want 2 ENIs captured", s.Snapshot.PerEni)
	}
	// Rollback.
	s, err := c.Rollback(context.Background(), s.ID, s.Generation, "PC-G5 test")
	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED {
		t.Errorf("after Rollback phase=%s; want ABORTED", s.Phase)
	}
	if s.FailureReason == "" {
		t.Error("FailureReason empty; want populated by Rollback")
	}
	if effect.UndoCalls != 1 {
		t.Errorf("UndoCalls=%d; want 1 (rollback after CUTOVER triggers UndoCutover)", effect.UndoCalls)
	}
}

func TestRollback_BeforeCutover_NoUndoCall(t *testing.T) {
	// Rolling back before CUTOVER must NOT call UndoCutover —
	// nothing was committed downstream.
	effect := &NoOpEffect{}
	c, _ := newCoord(t, effect)
	s, _ := c.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	s, _ = c.Rollback(context.Background(), s.ID, s.Generation, "PC-G5 early rollback")
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED {
		t.Errorf("phase=%s; want ABORTED", s.Phase)
	}
	if effect.UndoCalls != 0 {
		t.Errorf("UndoCalls=%d; want 0 (pre-CUTOVER rollback)", effect.UndoCalls)
	}
}

func TestRollback_PostCommit_Rejected(t *testing.T) {
	effect := &NoOpEffect{}
	c, _ := newCoord(t, effect)
	s, _ := c.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_DRAIN,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMMIT,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	_, err := c.Rollback(context.Background(), s.ID, s.Generation, "too late")
	if !errors.Is(err, ErrCommitted) {
		t.Errorf("got %v; want ErrCommitted (rollback past COMMIT is forbidden)", err)
	}
}

func TestRollback_UndoCutoverFailure_TerminatesAborted(t *testing.T) {
	effect := &NoOpEffect{UndoErr: errors.New("dpu unreachable during undo")}
	c, _ := newCoord(t, effect)
	s, _ := c.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	_, err := c.Rollback(context.Background(), s.ID, s.Generation, "PC-G5 undo fails")
	if err == nil {
		t.Fatal("want error when UndoCutover fails")
	}
	live, _ := c.Get(context.Background(), s.ID)
	if live.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED {
		t.Errorf("phase=%s; want ABORTED even after undo failure", live.Phase)
	}
	if !strings.Contains(live.FailureReason, "undo-cutover failed") {
		t.Errorf("FailureReason=%q; want to mention undo failure", live.FailureReason)
	}
}

// --- PC-G6: restart recovery ----------------------------------------

func TestRestartRecovery_HydratesSessions_PC_G6(t *testing.T) {
	// 1. Start a session, advance it to SYNC, then "restart" by
	//    constructing a new Coordinator pointed at the same store.
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	defer st.Close()

	c1, err := New(context.Background(), st, &NoOpEffect{})
	if err != nil {
		t.Fatalf("New first: %v", err)
	}
	s, _ := c1.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
	} {
		s, _ = c1.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	idBefore, genBefore, phaseBefore := s.ID, s.Generation, s.Phase

	// 2. Simulate dashd restart by creating a fresh Coordinator
	//    against the same store. Hydration loads persisted sessions.
	c2, err := New(context.Background(), st, &NoOpEffect{})
	if err != nil {
		t.Fatalf("New restart: %v", err)
	}
	recovered, err := c2.Get(context.Background(), idBefore)
	if err != nil {
		t.Fatalf("Get after restart: %v", err)
	}
	if recovered.Phase != phaseBefore {
		t.Errorf("recovered phase=%s; want %s", recovered.Phase, phaseBefore)
	}
	if recovered.Generation != genBefore {
		t.Errorf("recovered generation=%d; want %d", recovered.Generation, genBefore)
	}
	// 3. And we can keep advancing it.
	advanced, err := c2.Advance(context.Background(), idBefore, genBefore, dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY, "post-restart")
	if err != nil {
		t.Fatalf("Advance post-restart: %v", err)
	}
	if advanced.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY {
		t.Errorf("post-restart phase=%s; want READY", advanced.Phase)
	}
}

func TestRestartRecovery_PreservesSnapshot(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("filstore.Open: %v", err)
	}
	defer st.Close()
	c1, _ := New(context.Background(), st, &NoOpEffect{})
	s, _ := c1.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
	} {
		s, _ = c1.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	// Simulate restart.
	c2, _ := New(context.Background(), st, &NoOpEffect{})
	recovered, _ := c2.Get(context.Background(), s.ID)
	if len(recovered.Snapshot.PerEni) != 2 {
		t.Errorf("recovered snapshot=%v; want 2 ENIs", recovered.Snapshot.PerEni)
	}
	// Rollback on c2 should still call UndoCutover because the
	// snapshot survived restart.
	effect2 := &NoOpEffect{}
	c3, _ := New(context.Background(), st, effect2)
	_, _ = c3.Rollback(context.Background(), s.ID, recovered.Generation, "post-restart undo")
	if effect2.UndoCalls != 1 {
		t.Errorf("UndoCalls after restart-rollback=%d; want 1 (snapshot survived hydration)", effect2.UndoCalls)
	}
}

// --- Plan validation ------------------------------------------------

func TestCreatePlan_EmptyEnis(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	_, err := c.CreatePlan(context.Background(), "default", &dashcenterv1.CreateMigrationPlanRequest{
		SourceDpuId: "a", TargetDpuId: "b",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument", err)
	}
}

func TestCreatePlan_SameSourceTarget(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	_, err := c.CreatePlan(context.Background(), "default", &dashcenterv1.CreateMigrationPlanRequest{
		EniNames: []string{"e1"}, SourceDpuId: "a", TargetDpuId: "a",
	})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("got %v; want ErrInvalidArgument", err)
	}
}

func TestCreatePlan_DefaultStrategy(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	p, err := c.CreatePlan(context.Background(), "default", &dashcenterv1.CreateMigrationPlanRequest{
		EniNames: []string{"e1"}, SourceDpuId: "a", TargetDpuId: "b",
	})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	if p.GetStrategy() != dashcenterv1.MigrationStrategy_MIGRATION_STRATEGY_NEW_FLOWS_FIRST_DRAIN {
		t.Errorf("strategy=%v; want default NEW_FLOWS_FIRST_DRAIN", p.GetStrategy())
	}
}

// --- Abort + List + Filter ------------------------------------------

func TestAbort_ImmediateTerminal(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	s, _ = c.Advance(context.Background(), s.ID, s.Generation, dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT, "")
	s, err := c.Abort(context.Background(), s.ID, s.Generation, "operator killed it")
	if err != nil {
		t.Fatalf("Abort: %v", err)
	}
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_ABORTED {
		t.Errorf("phase=%s; want ABORTED", s.Phase)
	}
}

func TestList_FilterByPhase_ExcludesTerminalByDefault(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	// Start 3 sessions.
	for i := 0; i < 3; i++ {
		_, _ = c.StartSession(context.Background(), mkPlan())
	}
	// All 3 should be at ADMISSION.
	all := c.List(context.Background(), nil)
	if len(all) != 3 {
		t.Errorf("List nil=%d; want 3", len(all))
	}
	// Abort one.
	s := all[0]
	_, _ = c.Abort(context.Background(), s.ID, s.Generation, "")
	// Default filter excludes terminals.
	out := c.List(context.Background(), &ListFilter{})
	if len(out) != 2 {
		t.Errorf("List default=%d; want 2 (terminal excluded)", len(out))
	}
	// IncludeTerminal=true returns all 3.
	out = c.List(context.Background(), &ListFilter{IncludeTerminal: true})
	if len(out) != 3 {
		t.Errorf("List include-terminal=%d; want 3", len(out))
	}
}

func TestCommit_WalksFinalizeAndCompleted(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	s, _ := c.StartSession(context.Background(), mkPlan())
	for _, p := range []dashcenterv1.MigrationPhase{
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SNAPSHOT,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_PREPARE,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_SYNC,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_READY,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_CUTOVER,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_DRAIN,
		dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMMIT,
	} {
		s, _ = c.Advance(context.Background(), s.ID, s.Generation, p, "")
	}
	s, err := c.Commit(context.Background(), s.ID, s.Generation, "done")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if s.Phase != dashcenterv1.MigrationPhase_MIGRATION_PHASE_COMPLETED {
		t.Errorf("phase=%s; want COMPLETED", s.Phase)
	}
}

// --- Broadcaster -----------------------------------------------------

func TestBroadcaster_PublishOnAdvance(t *testing.T) {
	c, _ := newCoord(t, &NoOpEffect{})
	ch, cancel := c.Broadcaster().Subscribe("")
	defer cancel()
	s, _ := c.StartSession(context.Background(), mkPlan())
	saw := false
	for i := 0; i < 5; i++ {
		select {
		case ev := <-ch:
			if ev.ID == s.ID {
				saw = true
			}
		default:
		}
		if saw {
			break
		}
	}
	if !saw {
		t.Error("broadcaster never published the StartSession event")
	}
}
