// CutoverEffect implementations:
//
//   * NoOpEffect — does nothing, returns nil. Used by tests that don't
//     care about southbound side effects.
//
//   * LivePutEffect — production wiring: at CUTOVER, rewrites each
//     ENI's placement_hint_dpu_ids from source → target via the
//     supplied EniRehomer (production wires service.PutEni so capacity
//     + schema + cordon admission fire on the target). At rollback,
//     restores the prior placement.
package migration

import (
	"context"
	"encoding/json"
	"fmt"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// NoOpEffect satisfies CutoverEffect with no side effects. Records
// call counts so tests can assert phase ordering.
type NoOpEffect struct {
	PrepareCalls int
	SyncCalls    int
	CutoverCalls int
	DrainCalls   int
	UndoCalls    int
	// Inject these to simulate per-phase failure in tests.
	PrepareErr error
	SyncErr    error
	CutoverErr error
	DrainErr   error
	UndoErr    error
}

func (n *NoOpEffect) PrepareTarget(_ context.Context, _ dashcenterv1.MigrationPlan) error {
	n.PrepareCalls++
	return n.PrepareErr
}
func (n *NoOpEffect) SyncFlows(_ context.Context, _ dashcenterv1.MigrationPlan) error {
	n.SyncCalls++
	return n.SyncErr
}
func (n *NoOpEffect) Cutover(_ context.Context, p dashcenterv1.MigrationPlan) (Snapshot, error) {
	n.CutoverCalls++
	if n.CutoverErr != nil {
		return Snapshot{}, n.CutoverErr
	}
	// Synthesize a snapshot so rollback tests can verify it's populated.
	snap := Snapshot{PerEni: map[string][]string{}}
	for _, eni := range p.GetEniNames() {
		snap.PerEni[eni] = []string{p.GetSourceDpuId()}
	}
	return snap, nil
}
func (n *NoOpEffect) DrainSource(_ context.Context, _ dashcenterv1.MigrationPlan) error {
	n.DrainCalls++
	return n.DrainErr
}
func (n *NoOpEffect) UndoCutover(_ context.Context, _ dashcenterv1.MigrationPlan, _ Snapshot) error {
	n.UndoCalls++
	return n.UndoErr
}

// EniRehomer is the seam LivePutEffect uses to rewrite ENI placements.
// Production wires service.PutEni via a thin adapter so capacity +
// schema + cordon admission all fire on the destination.
type EniRehomer interface {
	// Rehome moves the named ENI to dst by rewriting its
	// placement_hint_dpu_ids. The previous hint list is returned so
	// rollback can restore it. Uses the live store as the source of
	// truth (not a cached spec) so a concurrent edit doesn't get
	// silently overwritten — the implementation uses ExpectedGeneration.
	Rehome(ctx context.Context, namespace, eniName, dst string) (priorHints []string, err error)

	// Restore is the rollback inverse of Rehome.
	Restore(ctx context.Context, namespace, eniName string, priorHints []string) error
}

// LivePutEffect is the production CutoverEffect. PrepareTarget,
// SyncFlows, DrainSource are no-ops today (admission already happens
// at Rehome time; the sim has no flow state to sync or drain). PE
// will replace these with real DASH southbound calls.
type LivePutEffect struct {
	Rehomer EniRehomer
}

func (e *LivePutEffect) PrepareTarget(_ context.Context, _ dashcenterv1.MigrationPlan) error { return nil }
func (e *LivePutEffect) SyncFlows(_ context.Context, _ dashcenterv1.MigrationPlan) error     { return nil }
func (e *LivePutEffect) DrainSource(_ context.Context, _ dashcenterv1.MigrationPlan) error   { return nil }

func (e *LivePutEffect) Cutover(ctx context.Context, plan dashcenterv1.MigrationPlan) (Snapshot, error) {
	if e.Rehomer == nil {
		return Snapshot{}, fmt.Errorf("migration: LivePutEffect requires Rehomer")
	}
	snap := Snapshot{PerEni: map[string][]string{}}
	ns := plan.GetNamespace()
	if ns == "" {
		ns = "default"
	}
	for _, eni := range plan.GetEniNames() {
		prior, err := e.Rehomer.Rehome(ctx, ns, eni, plan.GetTargetDpuId())
		if err != nil {
			// First failure aborts: restore anything we already rehomed.
			for already, hint := range snap.PerEni {
				_ = e.Rehomer.Restore(ctx, ns, already, hint)
			}
			return Snapshot{}, fmt.Errorf("rehome %s -> %s: %w", eni, plan.GetTargetDpuId(), err)
		}
		snap.PerEni[eni] = prior
	}
	return snap, nil
}

func (e *LivePutEffect) UndoCutover(ctx context.Context, plan dashcenterv1.MigrationPlan, snap Snapshot) error {
	if e.Rehomer == nil {
		return fmt.Errorf("migration: LivePutEffect requires Rehomer")
	}
	ns := plan.GetNamespace()
	if ns == "" {
		ns = "default"
	}
	// Best-effort restore: record failures but keep going so we
	// don't leave half-migrated state.
	var firstErr error
	for eni, prior := range snap.PerEni {
		if err := e.Rehomer.Restore(ctx, ns, eni, prior); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// --- store-direct rehomer (used by tests that don't want the service layer) ---

// DirectStoreRehomer is an EniRehomer that mutates ENI specs in-place
// via the store. Used by migration unit tests; production wires a
// service-backed rehomer so admission gates fire.
type DirectStoreRehomer struct {
	Store store.DesiredStore
}

func (r *DirectStoreRehomer) Rehome(ctx context.Context, ns, name, dst string) ([]string, error) {
	key := store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}
	sp, err := r.Store.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	spec := &dashcenterv1.EniSpec{}
	if err := json.Unmarshal(sp.Data, spec); err != nil {
		return nil, fmt.Errorf("unmarshal eni: %w", err)
	}
	prior := append([]string(nil), spec.GetPlacementHintDpuIds()...)
	spec.PlacementHintDpuIds = []string{dst}
	if _, err := r.Store.Put(ctx, key, spec, sp.Generation); err != nil {
		return nil, err
	}
	return prior, nil
}

func (r *DirectStoreRehomer) Restore(ctx context.Context, ns, name string, prior []string) error {
	key := store.ObjectKey{Namespace: ns, Kind: "eni", Name: name}
	sp, err := r.Store.Get(ctx, key)
	if err != nil {
		return err
	}
	spec := &dashcenterv1.EniSpec{}
	if err := json.Unmarshal(sp.Data, spec); err != nil {
		return fmt.Errorf("unmarshal eni: %w", err)
	}
	spec.PlacementHintDpuIds = append([]string(nil), prior...)
	_, err = r.Store.Put(ctx, key, spec, sp.Generation)
	return err
}
