// PC-G7 drain: evacuate a DPU by rehoming every ENI to a least-loaded
// uncordoned destination.
//
// Locked decisions:
//
//   * Drain cordons the source DPU first. If cordoning fails, the
//     drain returns without rehoming anything. This is the "no-new-
//     placements + evacuate" semantic operators expect; the source
//     stays cordoned even on partial failure so a retry only has to
//     finish what's left.
//
//   * Destinations are picked one ENI at a time via the supplied
//     Mover.PickDestination — production wires capacity.Tracker.
//     LeastLoadedDPU so each successive ENI lands on whatever is
//     currently emptiest. Tie-break is deterministic (lex DPU id).
//
//   * Parallelism caps concurrent rehome ops. Default 4 matches the
//     locked D5 decision in impl-phases.md. Zero or negative falls
//     back to serial (parallelism=1).
//
//   * On a per-ENI failure we record the reason and KEEP DRAINING
//     the other ENIs. The DrainResult lists Migrated + Failed; the
//     final source DPU may still host the Failed ENIs (operator
//     decides whether to retry or uncordon).
//
//   * Context cancellation interrupts the forward dispatch but does
//     NOT undo already-rehomed ENIs (drain is not transactional;
//     ApplyBatch is for that). The DrainResult reflects whatever made
//     it through.
package operations

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// EniRef identifies one ENI being drained. Mirrors capacity.EniRef so
// callers don't have to import that package just to construct one.
type EniRef struct {
	Namespace string
	Name      string
}

// Mover is the interface the drain coordinator needs from the rest of
// dashd. Production wires this in service.DrainDpu using the capacity
// tracker (EnisOn / PickDestination) + service.PutEni (Rehome).
//
// Tests substitute a fake to assert call order and parallelism.
type Mover interface {
	// EnisOn returns the ENIs currently placed on dpuID. Drain calls
	// this once at the start, post-cordon, so any new Put admitted
	// during the drain is not in scope (the cordon makes that
	// impossible for fleet-wide fan-out; explicit hints at the
	// cordoned DPU are rejected by service.PutEni). Order matters
	// for deterministic test assertions — implementations should
	// return a stable sort.
	EnisOn(dpuID string) []EniRef

	// PickDestination returns the DPU id that should host this ENI,
	// excluding `excluded` (which always contains the source DPU and
	// any DPUs already accepted by previous Rehomes in the current
	// batch — production may also exclude cordoned DPUs). Returns ""
	// when no destination is eligible; drain reports that as a
	// per-ENI failure with reason "no destination available".
	PickDestination(eni EniRef, excluded []string) string

	// Rehome moves the ENI to dst. This is a synchronous RPC: drain
	// waits for the verdict before counting the ENI as migrated.
	// Returns nil on success, error (often wrapped capacity /
	// schema / namespace errors) on failure.
	Rehome(ctx context.Context, eni EniRef, dst string) error
}

// DrainOpts tunes a drain operation.
type DrainOpts struct {
	// Parallelism caps concurrent Rehome calls. Default 4 (D5).
	Parallelism int
	// Reason is recorded in the cordon audit ring.
	Reason string
}

// DrainResult is the per-call outcome.
type DrainResult struct {
	DpuID      string            `json:"dpu_id"`
	Cordoned   bool              `json:"cordoned"`
	TotalEnis  int               `json:"total_enis"`
	Migrated   []EniMigration    `json:"migrated,omitempty"`
	Failed     []EniMigrationErr `json:"failed,omitempty"`
}

// EniMigration is one successful rehome row.
type EniMigration struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	From      string `json:"from"`
	To        string `json:"to"`
}

// EniMigrationErr is one failed rehome row.
type EniMigrationErr struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Reason    string `json:"reason"`
}

// Drain cordons srcDpuID and then rehomes every ENI on it.
//
// Returns DrainResult describing every ENI's outcome. The error is
// non-nil only when the drain could not even start (unknown DPU,
// cordon failed) or when ctx was cancelled before any rehome ran.
// Per-ENI failures are reported in DrainResult.Failed; the caller
// inspects the result for "did every ENI move?" rather than a single
// error.
func (m *Manager) Drain(ctx context.Context, srcDpuID string, opts DrainOpts, mover Mover) (DrainResult, error) {
	if m == nil {
		return DrainResult{}, errors.New("operations: nil Manager")
	}
	if mover == nil {
		return DrainResult{}, errors.New("operations: nil Mover")
	}
	if srcDpuID == "" {
		return DrainResult{}, fmt.Errorf("%w: dpu id is required", ErrNotFound)
	}
	if _, err := m.inv.Get(srcDpuID); err != nil {
		return DrainResult{}, fmt.Errorf("%w: %s", ErrNotFound, srcDpuID)
	}

	// 1. Cordon the source first so no new ENIs land here mid-drain.
	if err := m.Cordon(srcDpuID, opts.Reason); err != nil {
		return DrainResult{DpuID: srcDpuID}, fmt.Errorf("drain: cordon %s: %w", srcDpuID, err)
	}

	// 2. Enumerate ENIs to move. We snapshot once — any ENI that
	//    appears later via a successful Put cannot land on the
	//    cordoned source (fleet-wide fan-out skips it; explicit hint
	//    is rejected), so the snapshot is the complete drain set.
	enis := mover.EnisOn(srcDpuID)
	res := DrainResult{
		DpuID:     srcDpuID,
		Cordoned:  true,
		TotalEnis: len(enis),
	}
	if len(enis) == 0 {
		return res, nil
	}

	// 3. Rehome in parallel with a worker pool bounded by Parallelism.
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = 4 // D5 default
	}
	if parallelism > len(enis) {
		parallelism = len(enis)
	}

	type outcome struct {
		eni    EniRef
		dst    string
		err    error
	}
	in := make(chan EniRef, len(enis))
	out := make(chan outcome, len(enis))

	// Track destinations chosen so far so PickDestination can spread
	// load across the surviving fleet rather than dog-piling on the
	// currently-emptiest DPU. We hold a small mutex around this slice
	// because the worker pool reads + writes it concurrently.
	var (
		chosenMu    sync.Mutex
		chosenSoFar []string
	)

	excludedFor := func() []string {
		chosenMu.Lock()
		defer chosenMu.Unlock()
		// Always exclude the source; spread load across remaining DPUs
		// by passing the chosen-so-far slice as additional exclusions.
		// This is best-effort fairness — if every other DPU has been
		// hit once already, PickDestination will return "" and we
		// allow re-use by removing chosenSoFar (see below).
		out := make([]string, 0, len(chosenSoFar)+1)
		out = append(out, srcDpuID)
		out = append(out, chosenSoFar...)
		return out
	}

	recordChoice := func(dst string) {
		chosenMu.Lock()
		chosenSoFar = append(chosenSoFar, dst)
		chosenMu.Unlock()
	}

	resetChoicesIfFull := func(totalDPUs int) bool {
		chosenMu.Lock()
		defer chosenMu.Unlock()
		// We've exhausted distinct destinations — clear and recycle.
		if len(chosenSoFar) >= totalDPUs-1 {
			chosenSoFar = chosenSoFar[:0]
			return true
		}
		return false
	}

	totalDPUs := len(m.inv.List())

	for w := 0; w < parallelism; w++ {
		go func() {
			for eni := range in {
				if err := ctx.Err(); err != nil {
					out <- outcome{eni: eni, err: err}
					continue
				}
				excluded := excludedFor()
				dst := mover.PickDestination(eni, excluded)
				if dst == "" {
					// Try once more with chosenSoFar wiped — allows
					// re-using destinations when every uncordoned DPU
					// has already received one ENI this drain.
					if resetChoicesIfFull(totalDPUs) {
						dst = mover.PickDestination(eni, []string{srcDpuID})
					}
				}
				if dst == "" {
					out <- outcome{eni: eni, err: errors.New("no destination available (every other DPU cordoned or absent)")}
					continue
				}
				if err := mover.Rehome(ctx, eni, dst); err != nil {
					out <- outcome{eni: eni, dst: dst, err: err}
					continue
				}
				recordChoice(dst)
				out <- outcome{eni: eni, dst: dst}
			}
		}()
	}

	for _, e := range enis {
		in <- e
	}
	close(in)

	for i := 0; i < len(enis); i++ {
		o := <-out
		if o.err != nil {
			res.Failed = append(res.Failed, EniMigrationErr{
				Namespace: o.eni.Namespace, Name: o.eni.Name, Reason: o.err.Error(),
			})
			continue
		}
		res.Migrated = append(res.Migrated, EniMigration{
			Namespace: o.eni.Namespace, Name: o.eni.Name, From: srcDpuID, To: o.dst,
		})
	}

	return res, nil
}
