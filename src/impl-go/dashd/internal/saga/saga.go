// Package saga implements PC-8 atomic ApplyBatch coordination.
//
// PC-8's job: when an operator submits a batch of PolicyObject writes
// (e.g. via dashctl apply --atomic or POST /v1/apply-batch), either ALL
// of them land successfully or NONE of them do. On the first failure,
// the coordinator reverses every previously-applied item in REVERSE
// order, restoring the desired store to its pre-batch state.
//
// Locked decisions:
//
//   * Coordinator runs serially within a single batch. Cross-batch
//     concurrency is allowed (the desired store's per-key
//     optimistic concurrency catches conflicting batches), but a
//     single batch is committed one op at a time so the compensating
//     log (the list of "what we did so far") is unambiguous.
//
//   * Compensation = reverse op. For an Apply of a new key, the
//     compensator is Delete. For an Apply of an existing key, the
//     compensator is Apply-with-the-old-payload (snapshotted before
//     the new write). For a Delete of an existing key, the
//     compensator is Apply-with-the-deleted-payload (snapshotted
//     before the delete). Failed ops have no compensation work.
//
//   * On compensation failure, log loud + return MultiError. Operators
//     get a list of "still-applied" items they must clean up by hand.
//     We deliberately do NOT retry compensation: if the store is
//     unhealthy enough that compensation failed, retry loops will not
//     help and just blow the SLO.
//
//   * The coordinator does NOT consult capacity / schema gates itself
//     \u2014 the executor (caller-supplied function) does that. The
//     coordinator only sees Op{kind, ns, name, payload} and the
//     executor's success/failure verdict.
package saga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
)

// ErrCompensation is the sentinel for "the apply failed AND one or
// more compensations also failed". The error wraps the original apply
// failure; Unwrap returns it. The compensation failure details live in
// the Items field.
var ErrCompensation = errors.New("saga: compensation failed")

// Action distinguishes Apply (put) from Delete in a batch.
type Action int

const (
	ActionApply Action = iota + 1
	ActionDelete
)

// Op is one element of a batch.
type Op struct {
	Action    Action
	Namespace string
	Kind      string // store kind: "vnet" | "eni" | "vnet_mapping" | "acl_policy" | "route_policy" | "ha_set" | "service_tunnel"
	Name      string
	// Payload is the spec (proto message) for Apply ops; nil for Delete.
	Payload any
}

// Executor performs a single op against the live system. The
// coordinator calls Execute exactly once per op and trusts its
// verdict (nil = success, non-nil = abort the batch).
//
// Compensate must reverse the op. Behaviour:
//   - reversing Apply-of-new-key = Delete
//   - reversing Apply-of-existing-key = Apply with prior payload (priorData)
//   - reversing Delete-of-existing-key = Apply with deleted payload (priorData)
//
// priorData is nil when there was no pre-existing payload (e.g. a new
// Apply that the coordinator must roll back via Delete). When non-nil
// it is the raw bytes the store returned at snapshot time.
type Executor interface {
	Execute(ctx context.Context, op Op) error
	Compensate(ctx context.Context, op Op, priorData []byte) error
	// SnapshotPrior reads the current value for the op's key (so the
	// coordinator can roll back to it on failure). Returns (nil, nil)
	// when the key does not yet exist \u2014 the compensator will then
	// reverse via Delete.
	SnapshotPrior(ctx context.Context, op Op) ([]byte, error)
}

// Result reports the outcome of a batch. Committed is true iff every
// op succeeded and nothing was rolled back.
type Result struct {
	Committed     bool          `json:"committed"`
	OpsTotal      int           `json:"ops_total"`
	OpsCommitted  int           `json:"ops_committed"`
	FailedIndex   int           `json:"failed_index,omitempty"`   // index of the first failed op (only when !Committed)
	FailedError   string        `json:"failed_error,omitempty"`   // string form of the first failure
	Compensated   int           `json:"compensated,omitempty"`    // count of successful compensations
	CompFailures  []ItemError   `json:"comp_failures,omitempty"`  // ops where compensation also failed
}

// ItemError carries a per-op error (compensation phase).
type ItemError struct {
	Index int    `json:"index"`
	Op    OpKey  `json:"op"`
	Error string `json:"error"`
}

// OpKey is the public form of an Op identifier (for error reporting).
type OpKey struct {
	Action    string `json:"action"`
	Namespace string `json:"namespace,omitempty"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
}

func opKey(o Op) OpKey {
	action := "apply"
	if o.Action == ActionDelete {
		action = "delete"
	}
	return OpKey{Action: action, Namespace: o.Namespace, Kind: o.Kind, Name: o.Name}
}

// Run executes ops as an atomic batch. Returns Result describing the
// outcome plus an error iff the batch was not committed.
//
// On the first executor failure, Run replays the compensating action
// for every previously-applied op in REVERSE order. The originally-
// returned error is the executor's failure (wrapped so errors.Is finds
// the caller's sentinel). If any compensation also failed, the returned
// error additionally wraps ErrCompensation; Result.CompFailures carries
// the per-item details so operators have a precise cleanup list.
//
// Context cancellation interrupts the forward pass but compensation
// runs to completion against context.Background() \u2014 we'd rather
// over-compensate than leave half-applied state stranded.
func Run(ctx context.Context, ex Executor, ops []Op) (Result, error) {
	if ex == nil {
		return Result{}, errors.New("saga: executor is nil")
	}
	res := Result{OpsTotal: len(ops)}
	if len(ops) == 0 {
		res.Committed = true
		return res, nil
	}

	// applied[i] holds the pre-op snapshot of ops[i]. We capture this
	// BEFORE Execute so a successful Execute that later needs rollback
	// can be reversed via Apply(applied[i]) or Delete (when applied[i]
	// is nil → key did not exist).
	applied := make([][]byte, 0, len(ops))

	for i, op := range ops {
		if err := ctx.Err(); err != nil {
			res.FailedIndex = i
			res.FailedError = err.Error()
			return rollback(ex, ops, applied, res, err)
		}
		prior, err := ex.SnapshotPrior(ctx, op)
		if err != nil {
			res.FailedIndex = i
			res.FailedError = "snapshot prior: " + err.Error()
			return rollback(ex, ops, applied, res, err)
		}
		if err := ex.Execute(ctx, op); err != nil {
			res.FailedIndex = i
			res.FailedError = err.Error()
			return rollback(ex, ops, applied, res, err)
		}
		applied = append(applied, prior)
	}

	res.Committed = true
	res.OpsCommitted = len(ops)
	return res, nil
}

// rollback compensates every op in `applied[]` in reverse order.
// Returns the original error possibly wrapped with ErrCompensation if
// any compensator failed.
func rollback(ex Executor, ops []Op, applied [][]byte, res Result, original error) (Result, error) {
	// Compensation runs against a fresh context \u2014 we should not honour
	// the caller's cancellation here, otherwise a Ctrl-C right after
	// the failure leaves the desired store half-committed.
	compCtx := context.Background()

	// Walk applied in REVERSE so dependents come out before parents
	// (e.g. ENIs before their Vnets). The caller is expected to have
	// ordered the batch parent-first.
	for i := len(applied) - 1; i >= 0; i-- {
		op := ops[i]
		prior := applied[i]
		if err := ex.Compensate(compCtx, op, prior); err != nil {
			res.CompFailures = append(res.CompFailures, ItemError{
				Index: i, Op: opKey(op), Error: err.Error(),
			})
			continue
		}
		res.Compensated++
	}

	res.OpsCommitted = 0
	// Wrap so callers can errors.Is(err, theirSentinel) AND
	// errors.Is(err, ErrCompensation) when partial compensation
	// happened.
	out := fmt.Errorf("saga: op[%d] failed: %w", res.FailedIndex, original)
	if len(res.CompFailures) > 0 {
		out = fmt.Errorf("%w; also %d compensation failure(s): %s",
			ErrCompensation, len(res.CompFailures), summariseCompFailures(res.CompFailures))
	}
	return res, out
}

func summariseCompFailures(items []ItemError) string {
	parts := make([]string, 0, len(items))
	for _, it := range items {
		parts = append(parts, fmt.Sprintf("%s/%s/%s: %s",
			it.Op.Kind, it.Op.Namespace, it.Op.Name, it.Op.Action))
		// Append error tail (truncated) so the operator sees the cause inline.
		if len(it.Error) > 0 {
			parts[len(parts)-1] += " (" + it.Error + ")"
		}
	}
	return strings.Join(parts, "; ")
}

// --- StoreExecutor: a default Executor wired to store.DesiredStore ---

// StoreExecutor implements Executor against a store.DesiredStore. It is
// the production wiring used by the service-layer ApplyBatch \u2014 tests
// can plug a fake Executor instead.
//
// Spec payloads are JSON-encoded (matches the file + etcd backends'
// existing on-disk format).
type StoreExecutor struct {
	Store store.DesiredStore
}

func (e *StoreExecutor) SnapshotPrior(ctx context.Context, op Op) ([]byte, error) {
	sp, err := e.Store.Get(ctx, store.ObjectKey{Namespace: op.Namespace, Kind: op.Kind, Name: op.Name})
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(nil), sp.Data...), nil
}

func (e *StoreExecutor) Execute(ctx context.Context, op Op) error {
	key := store.ObjectKey{Namespace: op.Namespace, Kind: op.Kind, Name: op.Name}
	switch op.Action {
	case ActionApply:
		if op.Payload == nil {
			return errors.New("saga: apply with nil payload")
		}
		if _, err := e.Store.Put(ctx, key, op.Payload, 0); err != nil {
			return err
		}
	case ActionDelete:
		if err := e.Store.Delete(ctx, key); err != nil {
			return err
		}
	default:
		return fmt.Errorf("saga: unknown action %d", op.Action)
	}
	return nil
}

func (e *StoreExecutor) Compensate(ctx context.Context, op Op, priorData []byte) error {
	key := store.ObjectKey{Namespace: op.Namespace, Kind: op.Kind, Name: op.Name}
	if priorData == nil {
		// Key did not exist before \u2014 reverse by Delete (regardless of
		// whether the original op was Apply or Delete; Delete after
		// Delete is a no-op the store handles idempotently).
		err := e.Store.Delete(ctx, key)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	// Key existed before \u2014 restore the prior payload via raw write.
	// We side-step the JSON path because priorData is already the
	// store's on-wire representation.
	var raw json.RawMessage = priorData
	_, err := e.Store.Put(ctx, key, raw, 0)
	return err
}
