// Coverage-pushing tests for the PA-1 config knob. Targets default-fill
// paths and a couple of validateHA branches the main suite doesn't hit.
package config

import (
	"strings"
	"testing"
)

// TestApplyDefaults_ZeroStruct exercises every applyDefaults branch by
// passing a fully empty Config — every field should end up defaulted.
func TestApplyDefaults_ZeroStruct(t *testing.T) {
	c := &Config{}
	applyDefaults(c)

	if c.Mode != ModeController {
		t.Errorf("Mode = %q; want %q", c.Mode, ModeController)
	}
	if c.NodeID == "" {
		t.Error("NodeID empty after applyDefaults")
	}
	if c.Auth.Mode != AuthModeNone {
		t.Errorf("Auth.Mode = %q; want %q", c.Auth.Mode, AuthModeNone)
	}
	if c.HA.Controller.Elector.Backend != ElectorBackendNone {
		t.Errorf("HA.Controller.Elector.Backend = %q; want %q",
			c.HA.Controller.Elector.Backend, ElectorBackendNone)
	}
	if c.HA.Controller.Elector.LeaseTTL == 0 {
		t.Error("HA.Controller.Elector.LeaseTTL still zero after defaults")
	}
	if c.HA.Controller.Elector.LeaderKey == "" {
		t.Error("HA.Controller.Elector.LeaderKey empty after defaults")
	}
	if c.HA.Controller.Elector.DialTimeout == 0 {
		t.Error("HA.Controller.Elector.DialTimeout zero after defaults")
	}
	if c.Listen.GRPCAddr == "" || c.Listen.RESTAddr == "" || c.Listen.AdminAddr == "" {
		t.Error("Listen.* should be defaulted")
	}
}

// TestValidateHA_ControllerlessRejectsControllerBlock covers the
// validateHA branch where mode=controllerless + controller block set.
// (Note: mode=controllerless itself is rejected by validateMode, but
// validateHA still walks its own path and produces an additional error.)
func TestValidateHA_ControllerlessRejectsControllerBlock(t *testing.T) {
	ha := HAConfig{
		Controller: ControllerHAConfig{
			Elector: ElectorConfig{
				Backend:   ElectorBackendEtcd,
				Endpoints: []string{"http://etcd-0:2379"},
			},
		},
	}
	err := validateHA(ModeControllerless, ha)
	if err == nil {
		t.Fatal("expected error when controller block populated under controllerless mode")
	}
	if !strings.Contains(err.Error(), "ha.controller is set but mode=") {
		t.Errorf("error should call out the mismatch, got: %v", err)
	}
}

// TestValidateHA_EtcdBackendMissingEndpoints covers the "endpoints
// required" branch of the etcd-elector validation path.
func TestValidateHA_EtcdBackendMissingEndpoints(t *testing.T) {
	ha := HAConfig{
		Controller: ControllerHAConfig{
			Elector: ElectorConfig{Backend: ElectorBackendEtcd /* no Endpoints */},
		},
	}
	err := validateHA(ModeController, ha)
	if err == nil {
		t.Fatal("expected error when etcd backend has no endpoints")
	}
	if !strings.Contains(err.Error(), "requires ha.controller.elector.endpoints") {
		t.Errorf("error should call out missing endpoints, got: %v", err)
	}
}

// TestValidateHA_ElectorBackendUnknown covers the default branch of the
// elector-backend switch.
func TestValidateHA_ElectorBackendUnknown(t *testing.T) {
	ha := HAConfig{
		Controller: ControllerHAConfig{
			Elector: ElectorConfig{Backend: "zookeeper"},
		},
	}
	err := validateHA(ModeController, ha)
	if err == nil {
		t.Fatal("expected error for unknown elector backend")
	}
	if !strings.Contains(err.Error(), "unsupported value") {
		t.Errorf("error should mention unsupported value, got: %v", err)
	}
}

// TestValidateHA_NegativeLeaseTTL covers the lease_ttl bounds check.
func TestValidateHA_NegativeLeaseTTL(t *testing.T) {
	ha := HAConfig{
		Controller: ControllerHAConfig{
			Elector: ElectorConfig{Backend: ElectorBackendNone, LeaseTTL: -1},
		},
	}
	err := validateHA(ModeController, ha)
	if err == nil {
		t.Fatal("expected error for negative lease_ttl")
	}
	if !strings.Contains(err.Error(), "lease_ttl") {
		t.Errorf("error should mention lease_ttl, got: %v", err)
	}
}

// TestControllerHAIsSet_ByTLS covers the TLS branch of controllerHAIsSet.
func TestControllerHAIsSet_ByTLS(t *testing.T) {
	c := ControllerHAConfig{Elector: ElectorConfig{
		TLS: TLSConfig{CertFile: "x.pem"},
	}}
	if !controllerHAIsSet(c) {
		t.Error("controllerHAIsSet should return true when TLS material is set")
	}
}

// TestControllerlessHAIsSet_RaftFields covers the raft branch of
// controllerlessHAIsSet.
func TestControllerlessHAIsSet_RaftFields(t *testing.T) {
	c := ControllerlessHAConfig{Raft: RaftConfig{Port: 7947}}
	if !controllerlessHAIsSet(c) {
		t.Error("controllerlessHAIsSet should return true when Raft.Port is set")
	}
	c = ControllerlessHAConfig{Raft: RaftConfig{DataDir: "/var/lib/raft"}}
	if !controllerlessHAIsSet(c) {
		t.Error("controllerlessHAIsSet should return true when Raft.DataDir is set")
	}
}

// TestValidateAuth_TokenWithMissingFields covers the inner validation
// branches: empty token, bad role, mismatched cert/key pairing.
func TestValidateAuth_TokenWithBadEntries(t *testing.T) {
	a := AuthConfig{
		Mode: AuthModeToken,
		Tokens: []TokenEntry{
			{Token: "", Role: AuthRoleAdmin},                   // empty token
			{Token: "good", Role: "superuser"},                  // bad role
		},
		TLS: TLSConfig{CertFile: "c.pem" /* missing KeyFile */},
	}
	err := validateAuth(a)
	if err == nil {
		t.Fatal("expected aggregated errors")
	}
	msg := err.Error()
	for _, want := range []string{"tokens[0].token", "tokens[1].role", "cert_file and auth.tls.key_file"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should contain %q, got: %v", want, err)
		}
	}
}

// TestApplyAuthDefaults_PreservesNonEmpty asserts we don't overwrite a
// user-set Mode (the half of applyAuthDefaults that isn't exercised by
// other tests).
func TestApplyAuthDefaults_PreservesNonEmpty(t *testing.T) {
	a := &AuthConfig{Mode: AuthModeMTLS}
	applyAuthDefaults(a)
	if a.Mode != AuthModeMTLS {
		t.Errorf("applyAuthDefaults overwrote user Mode: got %q; want %q", a.Mode, AuthModeMTLS)
	}
}
