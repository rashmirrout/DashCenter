// TriggerResimulation tells one or more DPUs to re-evaluate active
// flows against the current installed policy. dashd's local action is
// to call the configured Resimulator (production: dispatch fan-out);
// the actual datapath re-eval is the DPU's responsibility (the sim
// records the request today, real DPUs perform the slow-path re-run).
package flow

import (
	"context"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

// TriggerResimulation validates the scope then delegates to the
// configured Resimulator. Validation rules mirror the proto comment:
// at least one of {dpu_ids, eni_names} must be non-empty so the
// operator is explicit. namespace defaults to "default" when blank.
//
// Returns the txn id surfaced by the Resimulator (e.g. for log
// correlation). The Ack.txn_id wraps it.
func (e *Engine) TriggerResimulation(ctx context.Context, req *dashcenterv1.ResimRequest) (*dashcenterv1.Ack, error) {
	if req == nil {
		return nil, invArgf("request is nil")
	}
	if len(req.GetDpuIds()) == 0 && len(req.GetEniNames()) == 0 {
		return nil, invArgf("at least one of dpu_ids or eni_names is required")
	}
	ns := req.GetNamespace()
	if ns == "" {
		ns = "default"
	}
	txn, err := e.resim.Resimulate(ctx, req.GetDpuIds(), req.GetEniNames(), ns, req.GetDropAllFlows())
	if err != nil {
		return nil, err
	}
	return &dashcenterv1.Ack{TxnId: txn}, nil
}
