package config

import (
"errors"
"os"
"path/filepath"
"strings"
"testing"
"time"
)

func TestDefaultPassesValidate(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default() config should pass Validate, got: %v", err)
	}
}

func TestLoadValidYAML(t *testing.T) {
	yaml := `
listen:
  grpc_addr: ":19443"
  rest_addr: ":18443"
  admin_addr: ":17443"
storage:
  backend: file
  file:
    state_dir: /tmp/dashd-test
inventory:
  source: api
reconcile:
  tick_interval: 10s
  per_dpu_inbox_size: 2
  apply_rate_limit: 50
  error_budget_per_min: 5
log:
  level: debug
  format: text
`
	path := writeTemp(t, "valid.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load valid YAML failed: %v", err)
	}
	if cfg.Listen.GRPCAddr != ":19443" {
		t.Errorf("expected grpc_addr=:19443, got %s", cfg.Listen.GRPCAddr)
	}
	if cfg.Reconcile.TickInterval != 10*time.Second {
		t.Errorf("expected tick_interval=10s, got %v", cfg.Reconcile.TickInterval)
	}
	if cfg.Inventory.Source != "api" {
		t.Errorf("expected source=api, got %s", cfg.Inventory.Source)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("expected level=debug, got %s", cfg.Log.Level)
	}
}

func TestLoadNonexistent(t *testing.T) {
	_, err := Load("/nonexistent/path/dashd.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
	if !os.IsNotExist(unwrapAll(err)) {
		// The error should wrap os.ErrNotExist somewhere.
		if !strings.Contains(err.Error(), "no such file") &&
			!strings.Contains(err.Error(), "cannot find") &&
			!strings.Contains(err.Error(), "not exist") {
			t.Errorf("expected os.ErrNotExist-like error, got: %v", err)
		}
	}
}

func TestLoadMalformedYAML(t *testing.T) {
	path := writeTemp(t, "bad.yaml", `listen: [broken`)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
	if !strings.Contains(err.Error(), "parse") {
		t.Errorf("expected parse error, got: %v", err)
	}
}

func TestPartialYAMLUsesDefaults(t *testing.T) {
	yaml := `
listen:
  grpc_addr: ":19443"
storage:
  backend: file
  file:
    state_dir: /tmp/dashd-partial
inventory:
  source: api
`
	path := writeTemp(t, "partial.yaml", yaml)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load partial YAML failed: %v", err)
	}
	if cfg.Listen.GRPCAddr != ":19443" {
		t.Errorf("grpc_addr should be overridden, got %s", cfg.Listen.GRPCAddr)
	}
	if cfg.Listen.RESTAddr != ":8443" {
		t.Errorf("rest_addr should default to :8443, got %s", cfg.Listen.RESTAddr)
	}
	if cfg.Listen.AdminAddr != ":7443" {
		t.Errorf("admin_addr should default to :7443, got %s", cfg.Listen.AdminAddr)
	}
	if cfg.Reconcile.TickInterval != 30*time.Second {
		t.Errorf("tick_interval should default to 30s, got %v", cfg.Reconcile.TickInterval)
	}
	if cfg.Log.Level != "info" {
		t.Errorf("log.level should default to info, got %s", cfg.Log.Level)
	}
}

func TestBackendEtcdError(t *testing.T) {
	// PA-1b: storage.backend=etcd is now a real backend. With no endpoints
	// configured, validation must reject — pointing the operator at the
	// required field. Previously (PA-1a) the whole backend was rejected
	// with "not yet implemented".
	yaml := `
storage:
  backend: etcd
inventory:
  source: api
`
	path := writeTemp(t, "etcd.yaml", yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for etcd backend without endpoints")
	}
	if !strings.Contains(err.Error(), "endpoints") {
		t.Errorf("error should mention missing endpoints, got: %v", err)
	}
}

func TestBackendRedisError(t *testing.T) {
	yaml := `
storage:
  backend: redis
  file:
    state_dir: /tmp/test
inventory:
  source: api
`
	path := writeTemp(t, "redis.yaml", yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for backend=redis")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported, got: %v", err)
	}
}

func TestLogLevelTraceError(t *testing.T) {
	yaml := `
storage:
  backend: file
  file:
    state_dir: /tmp/test
inventory:
  source: api
log:
  level: trace
`
	path := writeTemp(t, "trace.yaml", yaml)
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected error for log.level=trace")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported, got: %v", err)
	}
}

func TestTickIntervalZeroError(t *testing.T) {
	cfg := Default()
	cfg.Reconcile.TickInterval = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for tick_interval=0")
	}
	if !strings.Contains(err.Error(), "tick_interval") {
		t.Errorf("error should mention tick_interval, got: %v", err)
	}
}

// --- helpers ---

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func unwrapAll(err error) error {
	for {
		u := errors.Unwrap(err)
		if u == nil {
			return err
		}
		err = u
	}
}