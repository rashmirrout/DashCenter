// PC-8 saga tests: commit-all happy path, mid-batch failure with
// reverse-order compensation, no-op compensation when nothing applied,
// compensation failure surfacing, StoreExecutor round-trip.
package saga

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

// --- scriptable fake executor -----------------------------------------

// fakeExecutor records every call. `failAt` (1-indexed) makes Execute
// return the configured error on that op. compFail makes Compensate
// return an error on the same index.
type fakeExecutor struct {
	executed     []Op
	compensated  []Op
	priorReturn  [][]byte
	failAt       int
	failErr      error
	compFailIdx  int
	compFailErr  error
}

func (f *fakeExecutor) SnapshotPrior(ctx context.Context, op Op) ([]byte, error) {
	idx := len(f.executed)
	if idx < len(f.priorReturn) {
		return f.priorReturn[idx], nil
	}
	return nil, nil
}

func (f *fakeExecutor) Execute(ctx context.Context, op Op) error {
	f.executed = append(f.executed, op)
	if f.failAt > 0 && len(f.executed) == f.failAt {
		return f.failErr
	}
	return nil
}

func (f *fakeExecutor) Compensate(ctx context.Context, op Op, prior []byte) error {
	f.compensated = append(f.compensated, op)
	if f.compFailIdx > 0 && len(f.compensated) == f.compFailIdx {
		return f.compFailErr
	}
	return nil
}

// --- happy path -------------------------------------------------------

func TestRun_CommitAll(t *testing.T) {
	ex := &fakeExecutor{}
	ops := []Op{
		{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v1", Payload: map[string]any{"vni": 100}},
		{Action: ActionApply, Namespace: "default", Kind: "eni", Name: "e1", Payload: map[string]any{"vnet_name": "v1"}},
		{Action: ActionApply, Namespace: "default", Kind: "acl_policy", Name: "p1", Payload: map[string]any{"rules": []any{}}},
	}
	res, err := Run(context.Background(), ex, ops)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Committed {
		t.Error("Committed=false; want true")
	}
	if res.OpsCommitted != 3 {
		t.Errorf("OpsCommitted=%d; want 3", res.OpsCommitted)
	}
	if len(ex.compensated) != 0 {
		t.Errorf("compensated=%d; want 0", len(ex.compensated))
	}
}

// --- PC-G8: 10-item batch, op#5 fails, all rolled back ----------------

func TestRun_FailureMidBatch_ReverseOrderRollback_PC_G8(t *testing.T) {
	ex := &fakeExecutor{
		failAt:  5,
		failErr: errors.New("simulated put failure"),
	}
	ops := make([]Op, 10)
	for i := range ops {
		ops[i] = Op{
			Action:    ActionApply,
			Namespace: "default",
			Kind:      "eni",
			Name:      "e" + string(rune('0'+i)),
			Payload:   map[string]any{"i": i},
		}
	}
	res, err := Run(context.Background(), ex, ops)
	if err == nil {
		t.Fatal("Run: want error; got nil")
	}
	if res.Committed {
		t.Error("Committed=true; want false")
	}
	if res.FailedIndex != 4 { // 0-indexed → op #5
		t.Errorf("FailedIndex=%d; want 4", res.FailedIndex)
	}
	if !strings.Contains(res.FailedError, "simulated put failure") {
		t.Errorf("FailedError=%q; want to contain 'simulated put failure'", res.FailedError)
	}
	// Compensation should have run for ops 0..3 in REVERSE order.
	if len(ex.compensated) != 4 {
		t.Fatalf("compensated count=%d; want 4", len(ex.compensated))
	}
	wantOrder := []string{"e3", "e2", "e1", "e0"}
	for i, name := range wantOrder {
		if ex.compensated[i].Name != name {
			t.Errorf("compensated[%d].Name=%q; want %q", i, ex.compensated[i].Name, name)
		}
	}
	if res.Compensated != 4 {
		t.Errorf("res.Compensated=%d; want 4", res.Compensated)
	}
}

func TestRun_FailureOnFirstOp_NoCompensation(t *testing.T) {
	ex := &fakeExecutor{failAt: 1, failErr: errors.New("boom")}
	ops := []Op{{Action: ActionApply, Kind: "eni", Name: "x"}}
	res, err := Run(context.Background(), ex, ops)
	if err == nil {
		t.Fatal("want error")
	}
	if res.Committed || res.OpsCommitted != 0 {
		t.Errorf("res=%+v; want not-committed, 0 ops", res)
	}
	if len(ex.compensated) != 0 {
		t.Errorf("compensated=%d; want 0 (no prior ops to undo)", len(ex.compensated))
	}
}

// --- compensation failure surfacing -----------------------------------

func TestRun_CompensationFailure_SurfacedInResult(t *testing.T) {
	ex := &fakeExecutor{
		failAt:      3,
		failErr:     errors.New("op3 failed"),
		compFailIdx: 1, // the first compensation (reversed op 1 = ops[1])
		compFailErr: errors.New("compensation flaky"),
	}
	ops := []Op{
		{Action: ActionApply, Namespace: "default", Kind: "eni", Name: "a"},
		{Action: ActionApply, Namespace: "default", Kind: "eni", Name: "b"},
		{Action: ActionApply, Namespace: "default", Kind: "eni", Name: "c"},
	}
	res, err := Run(context.Background(), ex, ops)
	if err == nil {
		t.Fatal("want error")
	}
	if !errors.Is(err, ErrCompensation) {
		t.Errorf("err=%v; want errors.Is(ErrCompensation)", err)
	}
	if len(res.CompFailures) != 1 {
		t.Fatalf("CompFailures=%v; want 1 entry", res.CompFailures)
	}
	if res.Compensated != 1 {
		t.Errorf("Compensated=%d; want 1 (one of two succeeded)", res.Compensated)
	}
}

// --- empty batch is a successful no-op --------------------------------

func TestRun_EmptyOps_OK(t *testing.T) {
	ex := &fakeExecutor{}
	res, err := Run(context.Background(), ex, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Committed {
		t.Error("Committed=false; want true for empty batch")
	}
}

func TestRun_NilExecutor(t *testing.T) {
	_, err := Run(context.Background(), nil, []Op{{Action: ActionApply, Kind: "eni"}})
	if err == nil {
		t.Error("want error for nil executor")
	}
}

// --- StoreExecutor round-trip against the file store ------------------

func TestStoreExecutor_ApplyDelete_RoundTrip(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ex := &StoreExecutor{Store: st}

	op := Op{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v1", Payload: map[string]any{"vni": 100}}
	if err := ex.Execute(context.Background(), op); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Snapshot prior should now return the just-written bytes.
	prior, err := ex.SnapshotPrior(context.Background(), op)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(prior) == 0 {
		t.Error("snapshot returned empty payload")
	}
	// Delete the key.
	if err := ex.Execute(context.Background(), Op{Action: ActionDelete, Namespace: "default", Kind: "vnet", Name: "v1"}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// SnapshotPrior of a missing key returns nil, nil.
	prior2, err := ex.SnapshotPrior(context.Background(), op)
	if err != nil {
		t.Fatalf("snapshot after delete: %v", err)
	}
	if prior2 != nil {
		t.Errorf("snapshot after delete=%v; want nil", prior2)
	}
}

func TestStoreExecutor_Compensate_RestoresPrior(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ex := &StoreExecutor{Store: st}

	// Pre-seed an existing payload.
	key := store.ObjectKey{Namespace: "default", Kind: "vnet", Name: "v1"}
	if _, err := st.Put(context.Background(), key, map[string]any{"vni": 100}, 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	op := Op{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v1", Payload: map[string]any{"vni": 200}}
	prior, _ := ex.SnapshotPrior(context.Background(), op)
	if err := ex.Execute(context.Background(), op); err != nil {
		t.Fatalf("apply: %v", err)
	}
	// Compensate restores prior.
	if err := ex.Compensate(context.Background(), op, prior); err != nil {
		t.Fatalf("compensate: %v", err)
	}
	got, err := st.Get(context.Background(), key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !strings.Contains(string(got.Data), "100") {
		t.Errorf("after compensate Data=%s; want to contain 100 (prior vni)", string(got.Data))
	}
}

func TestStoreExecutor_Compensate_NewKey_DeletesIt(t *testing.T) {
	st, err := filstore.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer st.Close()
	ex := &StoreExecutor{Store: st}

	op := Op{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v1", Payload: map[string]any{"vni": 100}}
	// SnapshotPrior returns nil (key didn't exist).
	prior, _ := ex.SnapshotPrior(context.Background(), op)
	if prior != nil {
		t.Fatal("expected nil prior")
	}
	if err := ex.Execute(context.Background(), op); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if err := ex.Compensate(context.Background(), op, prior); err != nil {
		t.Fatalf("compensate: %v", err)
	}
	if _, err := st.Get(context.Background(), store.ObjectKey{Namespace: "default", Kind: "vnet", Name: "v1"}); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("after compensate of new-key apply: err=%v; want ErrNotFound", err)
	}
}

// --- e2e: full Run via StoreExecutor with mid-batch failure -----------

// brittleExecutor wraps StoreExecutor and fails on a configured op
// index. Used to prove end-to-end rollback with the real store.
type brittleExecutor struct {
	inner   *StoreExecutor
	failAt  int
	count   int
	failErr error
}

func (b *brittleExecutor) SnapshotPrior(ctx context.Context, op Op) ([]byte, error) {
	return b.inner.SnapshotPrior(ctx, op)
}
func (b *brittleExecutor) Execute(ctx context.Context, op Op) error {
	b.count++
	if b.count == b.failAt {
		return b.failErr
	}
	return b.inner.Execute(ctx, op)
}
func (b *brittleExecutor) Compensate(ctx context.Context, op Op, prior []byte) error {
	return b.inner.Compensate(ctx, op, prior)
}

func TestRun_FailureMidBatch_StoreLeftUnchanged_PC_G8_E2E(t *testing.T) {
st, err := filstore.Open(t.TempDir())
if err != nil {
t.Fatalf("open: %v", err)
}
defer st.Close()
ex := &brittleExecutor{inner: &StoreExecutor{Store: st}, failAt: 3, failErr: errors.New("disk full")}

ops := []Op{
{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v-a", Payload: map[string]any{"vni": 100}},
{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v-b", Payload: map[string]any{"vni": 101}},
{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v-c", Payload: map[string]any{"vni": 102}},
}
res, err := Run(context.Background(), ex, ops)
if err == nil || res.Committed {
t.Fatalf("expected failure; res=%+v err=%v", res, err)
}
for _, name := range []string{"v-a", "v-b", "v-c"} {
_, err := st.Get(context.Background(), store.ObjectKey{Namespace: "default", Kind: "vnet", Name: name})
if !errors.Is(err, store.ErrNotFound) {
t.Errorf("post-rollback Get(%s) err=%v; want ErrNotFound", name, err)
}
}
}

// --- coverage extras ---------------------------------------------------

func TestStoreExecutor_Execute_NilPayload(t *testing.T) {
st, _ := filstore.Open(t.TempDir())
defer st.Close()
ex := &StoreExecutor{Store: st}
err := ex.Execute(context.Background(), Op{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "x"})
if err == nil || !strings.Contains(err.Error(), "nil payload") {
t.Errorf("got %v; want nil-payload error", err)
}
}

func TestStoreExecutor_Execute_UnknownAction(t *testing.T) {
st, _ := filstore.Open(t.TempDir())
defer st.Close()
ex := &StoreExecutor{Store: st}
err := ex.Execute(context.Background(), Op{Action: Action(99), Namespace: "default", Kind: "vnet", Name: "x"})
if err == nil || !strings.Contains(err.Error(), "unknown action") {
t.Errorf("got %v; want unknown-action error", err)
}
}

func TestRun_CtxCancelled_TriggersRollback(t *testing.T) {
ex := &fakeExecutor{}
ops := []Op{
{Action: ActionApply, Kind: "eni", Name: "a"},
{Action: ActionApply, Kind: "eni", Name: "b"},
}
ctx, cancel := context.WithCancel(context.Background())
cancel() // cancel before Run starts
res, err := Run(ctx, ex, ops)
if err == nil || res.Committed {
t.Errorf("cancelled ctx: res=%+v err=%v; want failure", res, err)
}
}

func TestSnapshotPrior_ExistingKey_ReturnsBytes(t *testing.T) {
st, _ := filstore.Open(t.TempDir())
defer st.Close()
ex := &StoreExecutor{Store: st}
key := store.ObjectKey{Namespace: "default", Kind: "vnet", Name: "v1"}
if _, err := st.Put(context.Background(), key, map[string]any{"vni": 42}, 0); err != nil {
t.Fatal(err)
}
op := Op{Action: ActionApply, Namespace: "default", Kind: "vnet", Name: "v1"}
got, err := ex.SnapshotPrior(context.Background(), op)
if err != nil || len(got) == 0 {
t.Errorf("SnapshotPrior: got=%v err=%v", got, err)
}
}

func TestSummariseCompFailures_FormatsAllItems(t *testing.T) {
items := []ItemError{
{Index: 1, Op: OpKey{Action: "apply", Kind: "eni", Namespace: "default", Name: "a"}, Error: "boom"},
{Index: 2, Op: OpKey{Action: "delete", Kind: "vnet", Namespace: "default", Name: "v"}, Error: ""},
}
got := summariseCompFailures(items)
if !strings.Contains(got, "eni/default/a") || !strings.Contains(got, "boom") {
t.Errorf("summarise=%q; missing first item details", got)
}
}
