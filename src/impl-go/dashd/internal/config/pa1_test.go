// PA-1 tests for the new mode/auth/ha config knob. Verifies the locked
// behaviour from specs/Impl-Plan/impl-phases.md § Configuration &
// forward-compatibility contract.
package config

import (
	"strings"
	"testing"
)

// --- Mode ---------------------------------------------------------------

func TestModeDefault(t *testing.T) {
	cfg := Default()
	if cfg.Mode != ModeController {
		t.Errorf("mode default = %q; want %q", cfg.Mode, ModeController)
	}
	if cfg.NodeID == "" {
		t.Error("node_id default should be non-empty (hostname or dashd-local)")
	}
}

func TestModeControllerlessRejected(t *testing.T) {
	y := `
mode: controllerless
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
`
	_, err := Load(writeTemp(t, "controllerless.yaml", y))
	if err == nil {
		t.Fatal("expected error for mode=controllerless")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention 'not yet implemented', got: %v", err)
	}
	if !strings.Contains(err.Error(), "Phase 2 PF") {
		t.Errorf("error should point at the PF milestone, got: %v", err)
	}
}

func TestModeUnknown(t *testing.T) {
	y := `
mode: cluster
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
`
	_, err := Load(writeTemp(t, "unknown-mode.yaml", y))
	if err == nil {
		t.Fatal("expected error for unknown mode")
	}
	if !strings.Contains(err.Error(), "unsupported value") {
		t.Errorf("error should mention 'unsupported value', got: %v", err)
	}
}

func TestNodeIDOverride(t *testing.T) {
	y := `
node_id: dashd-test-7
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
`
	cfg, err := Load(writeTemp(t, "node.yaml", y))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.NodeID != "dashd-test-7" {
		t.Errorf("node_id = %q; want dashd-test-7", cfg.NodeID)
	}
}

// --- Auth ---------------------------------------------------------------

func TestAuthDefault(t *testing.T) {
	cfg := Default()
	if cfg.Auth.Mode != AuthModeNone {
		t.Errorf("auth.mode default = %q; want %q", cfg.Auth.Mode, AuthModeNone)
	}
}

func TestAuthModeTokenAcceptedInPD(t *testing.T) {
	y := `
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
auth:
  mode: token
  tokens:
    - token: t1
      role: admin
      name: alice
`
	_, err := Load(writeTemp(t, "auth-token.yaml", y))
	if err != nil {
		t.Fatalf("PD: token mode should be accepted; got %v", err)
	}
}

func TestAuthModeMTLSAcceptedInPD(t *testing.T) {
	y := `
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
auth:
  mode: mtls
  tls:
    cert_file: /tmp/c.pem
    key_file:  /tmp/k.pem
    ca_file:   /tmp/ca.pem
`
	_, err := Load(writeTemp(t, "auth-mtls.yaml", y))
	if err != nil {
		t.Fatalf("PD: mtls mode should be accepted; got %v", err)
	}
}

func TestAuthModeMTLSRequiresCA(t *testing.T) {
	cfg := Default()
	cfg.Auth.Mode = AuthModeMTLS
	cfg.Auth.TLS = TLSConfig{CertFile: "c.pem", KeyFile: "k.pem"}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when mtls + no ca_file")
	}
	if !strings.Contains(err.Error(), "ca_file") {
		t.Errorf("error should mention ca_file, got: %v", err)
	}
}

func TestAuthRoleUnsupported(t *testing.T) {
	cfg := Default()
	cfg.Auth.Mode = AuthModeNone // stay valid on the mode itself
	cfg.Auth.Roles = map[string][]string{"superuser": {"*"}}
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for unsupported role")
	}
	if !strings.Contains(err.Error(), "unsupported role") {
		t.Errorf("error should mention 'unsupported role', got: %v", err)
	}
}

func TestAuthUnknownModeRejected(t *testing.T) {
	cfg := Default()
	cfg.Auth.Mode = "kerberos"
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for auth.mode=kerberos")
	}
	if !strings.Contains(err.Error(), "unsupported value") {
		t.Errorf("error should mention 'unsupported value', got: %v", err)
	}
}

// --- HA -----------------------------------------------------------------

func TestHADefault(t *testing.T) {
	cfg := Default()
	if cfg.HA.Controller.Elector.Backend != ElectorBackendNone {
		t.Errorf("ha.controller.elector.backend default = %q; want %q", cfg.HA.Controller.Elector.Backend, ElectorBackendNone)
	}
	if cfg.HA.Controller.Elector.LeaseTTL == 0 {
		t.Error("ha.controller.elector.lease_ttl default should be non-zero")
	}
}

func TestHAControllerEtcdAccepted(t *testing.T) {
	// PA-3 landed: ha.controller.elector.backend=etcd is now a real
	// elector wired through cmd/dashd/main.go. Validator must accept it
	// when endpoints are supplied.
	y := `
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
ha:
  controller:
    elector:
      backend: etcd
      endpoints: ["http://etcd-0:2379"]
`
	if _, err := Load(writeTemp(t, "ha-etcd.yaml", y)); err != nil {
		t.Fatalf("unexpected error for ha.controller.elector.backend=etcd: %v", err)
	}
}

func TestHAControllerEtcdMissingEndpoints(t *testing.T) {
	y := `
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
ha:
  controller:
    elector:
      backend: etcd
`
	_, err := Load(writeTemp(t, "ha-etcd-no-eps.yaml", y))
	if err == nil {
		t.Fatal("expected error for etcd elector without endpoints")
	}
	if !strings.Contains(err.Error(), "endpoints") {
		t.Errorf("error should mention endpoints, got: %v", err)
	}
}

func TestHAControllerlessUnderControllerMode(t *testing.T) {
	y := `
mode: controller
storage:
  backend: file
  file: { state_dir: /tmp/test }
inventory: { source: api }
ha:
  controllerless:
    bind_addr: 0.0.0.0
    gossip:
      port: 7946
      seeds: ["dpu-1:7946"]
`
	_, err := Load(writeTemp(t, "ha-controllerless-under-controller.yaml", y))
	if err == nil {
		t.Fatal("expected error for controllerless block under mode=controller")
	}
	if !strings.Contains(err.Error(), "ha.controllerless is set but mode=") {
		t.Errorf("error should call out the block/mode mismatch, got: %v", err)
	}
}

func TestStorageRaftRejected(t *testing.T) {
	y := `
mode: controller
storage:
  backend: raft
inventory: { source: api }
`
	_, err := Load(writeTemp(t, "storage-raft.yaml", y))
	if err == nil {
		t.Fatal("expected error for storage.backend=raft")
	}
	if !strings.Contains(err.Error(), "not yet implemented") {
		t.Errorf("error should mention 'not yet implemented', got: %v", err)
	}
}

func TestStorageRaftWrongMode(t *testing.T) {
	cfg := Default()
	cfg.Storage.Backend = "raft"
	cfg.Mode = ModeController
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for raft backend under controller mode")
	}
	// Two errors expected — both the "not yet implemented" rejection AND
	// the mode/backend mismatch. We assert only on the mismatch text here.
	if !strings.Contains(err.Error(), "controllerless mode") {
		t.Errorf("error should mention mode mismatch, got: %v", err)
	}
}

// --- Regression: today's dashd.yaml keeps working ---

func TestPreExistingConfigUnchanged(t *testing.T) {
	// This is the exact shape of deploy/dashctl-fleet/configs/dashd.yaml —
	// the live fleet that drives every integration test and tutorial. It
	// MUST keep loading and validating after PA-1.
	y := `
listen:
  rest_addr:  ":8443"
  grpc_addr:  ":9443"
  admin_addr: ":7443"
storage:
  backend: file
  file:
    state_dir: /var/lib/dashd
inventory:
  source: file
  file:   /etc/dashd/inventory.yaml
reconcile:
  tick_interval:        15s
  per_dpu_inbox_size:   1
  apply_rate_limit:     100
  error_budget_per_min: 10
log:
  level:  info
  format: json
`
	cfg, err := Load(writeTemp(t, "fleet.yaml", y))
	if err != nil {
		t.Fatalf("pre-PA-1 dashd.yaml MUST still load: %v", err)
	}
	// New defaults are applied.
	if cfg.Mode != ModeController {
		t.Errorf("mode default = %q; want %q", cfg.Mode, ModeController)
	}
	if cfg.Auth.Mode != AuthModeNone {
		t.Errorf("auth.mode default = %q; want %q", cfg.Auth.Mode, AuthModeNone)
	}
	if cfg.HA.Controller.Elector.Backend != ElectorBackendNone {
		t.Errorf("ha.controller.elector.backend default = %q; want %q", cfg.HA.Controller.Elector.Backend, ElectorBackendNone)
	}
	// Existing fields preserved.
	if cfg.Listen.RESTAddr != ":8443" {
		t.Errorf("listen.rest_addr = %q; want :8443", cfg.Listen.RESTAddr)
	}
}
