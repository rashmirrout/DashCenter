// Package operations implements PC-1 cordon / uncordon admission and
// (later, PC-7) DPU drain.
//
// Locked decisions:
//
//   * Cordon is a simple boolean on inventory.DpuEntry.Cordoned. It
//     EXCLUDES the DPU from the placement-engine's fleet-wide fallback
//     (capacity.placementForEni), but does NOT migrate existing ENIs.
//     Operators must call DrainDpu (PC-7) to evacuate first.
//
//   * An explicit placement_hint_dpu_ids that names a cordoned DPU is
//     REJECTED at admission time (the operator is being explicit and
//     deserves a hard error, not silent placement on a different DPU).
//     This is enforced in the service-layer PutEni wrapper, not here.
//
//   * Cordon is idempotent. Cordon-then-cordon is a no-op; same for
//     uncordon. The Reason field is recorded in a small audit ring
//     buffer (Phase 2; PD's audit log will swap this in).
//
//   * No persistence to DesiredStore in PC-1. Cordon state lives on
//     inventory.DpuEntry and is rebuilt on dashd boot via the
//     RegisterDpu flow (Operators re-cordon after restart). PD adds a
//     dedicated "cordon set" in the desired store so cordon survives
//     restarts.
package operations

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
)

// ErrNotFound is returned when the target DPU is unknown.
var ErrNotFound = errors.New("operations: dpu not found")

// Inventory is the subset of *inventory.Inventory that operations
// needs. Defined as an interface so tests can swap a fake.
type Inventory interface {
	Get(id string) (inventory.DpuEntry, error)
	List() []inventory.DpuEntry
	SetCordoned(id string, cordoned bool) error
}

// auditEntry records one cordon/uncordon action.
type auditEntry struct {
	DpuID    string
	Cordoned bool
	Reason   string
	At       time.Time
}

// Manager owns the cordon admission surface. Construct with New; pass
// to service.NewControlPlane via a new constructor arg.
//
// Thread-safe.
type Manager struct {
	inv Inventory

	mu    sync.Mutex
	audit []auditEntry // small ring buffer; trimmed at auditCap
}

// auditCap caps the in-memory audit ring (PD will replace with the
// persistent audit log). 1k is enough to keep ~a day of typical drain
// activity around for forensics without bounded growth.
const auditCap = 1024

// New constructs a Manager bound to the given inventory.
func New(inv Inventory) *Manager {
	return &Manager{inv: inv}
}

// Cordon marks dpuID as cordoned and records the reason. Idempotent:
// calling Cordon twice with different reasons records both audit
// entries but the inventory flag stays true.
func (m *Manager) Cordon(dpuID, reason string) error {
	if m == nil {
		return errors.New("operations: nil Manager")
	}
	if dpuID == "" {
		return fmt.Errorf("%w: dpu id is required", ErrNotFound)
	}
	if _, err := m.inv.Get(dpuID); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, dpuID)
	}
	if err := m.inv.SetCordoned(dpuID, true); err != nil {
		return fmt.Errorf("cordon %s: %w", dpuID, err)
	}
	m.recordAudit(auditEntry{DpuID: dpuID, Cordoned: true, Reason: reason, At: time.Now()})
	return nil
}

// Uncordon clears the cordon flag. Idempotent.
func (m *Manager) Uncordon(dpuID, reason string) error {
	if m == nil {
		return errors.New("operations: nil Manager")
	}
	if dpuID == "" {
		return fmt.Errorf("%w: dpu id is required", ErrNotFound)
	}
	if _, err := m.inv.Get(dpuID); err != nil {
		return fmt.Errorf("%w: %s", ErrNotFound, dpuID)
	}
	if err := m.inv.SetCordoned(dpuID, false); err != nil {
		return fmt.Errorf("uncordon %s: %w", dpuID, err)
	}
	m.recordAudit(auditEntry{DpuID: dpuID, Cordoned: false, Reason: reason, At: time.Now()})
	return nil
}

// IsCordoned returns the live cordon flag for the DPU. Returns false
// if the DPU is unknown (caller decides whether to fail open or
// closed — service layer fails closed when the operator hints
// explicitly at an unknown DPU via capacity admission).
func (m *Manager) IsCordoned(dpuID string) bool {
	if m == nil {
		return false
	}
	entry, err := m.inv.Get(dpuID)
	if err != nil {
		return false
	}
	return entry.Cordoned
}

// ListCordoned returns the IDs of all currently-cordoned DPUs, sorted
// (inventory.List already sorts). Used by /admin/health and by drain
// to short-circuit when there are no destinations.
func (m *Manager) ListCordoned() []string {
	if m == nil {
		return nil
	}
	out := []string{}
	for _, e := range m.inv.List() {
		if e.Cordoned {
			out = append(out, e.ID)
		}
	}
	return out
}

// AuditRecent returns up to n most recent cordon/uncordon entries,
// newest first. Pass 0 for the whole ring.
func (m *Manager) AuditRecent(n int) []AuditEntry {
	m.mu.Lock()
	defer m.mu.Unlock()

	total := len(m.audit)
	if n <= 0 || n > total {
		n = total
	}
	out := make([]AuditEntry, n)
	for i := 0; i < n; i++ {
		e := m.audit[total-1-i]
		out[i] = AuditEntry{DpuID: e.DpuID, Cordoned: e.Cordoned, Reason: e.Reason, At: e.At}
	}
	return out
}

// AuditEntry is the exported form of an audit row.
type AuditEntry struct {
	DpuID    string    `json:"dpu_id"`
	Cordoned bool      `json:"cordoned"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at"`
}

func (m *Manager) recordAudit(e auditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.audit = append(m.audit, e)
	if len(m.audit) > auditCap {
		// Trim oldest. Allocate a fresh slice so the underlying array
		// is GC'd — otherwise the cap growth balloons over time.
		trimmed := make([]auditEntry, auditCap)
		copy(trimmed, m.audit[len(m.audit)-auditCap:])
		m.audit = trimmed
	}
}
