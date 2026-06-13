// counters_test.go covers the Observability.Counters config block
// (PE-3b / PE-G9): defaults, validation, override semantics.

package config

import (
	"strings"
	"testing"
	"time"
)

func TestObservabilityCounters_Defaults(t *testing.T) {
	cfg := Default()
	if !cfg.Observability.Counters.Enabled {
		t.Errorf("counters.enabled = false, want true (default)")
	}
	if got := cfg.Observability.Counters.PollInterval; got != 5*time.Second {
		t.Errorf("counters.poll_interval = %v, want 5s", got)
	}
}

func TestObservabilityCounters_ApplyDefaults_FillsZeroValue(t *testing.T) {
	cfg := &Config{}
	applyDefaults(cfg)
	if !cfg.Observability.Counters.Enabled {
		t.Errorf("zero-value config did not inherit Enabled=true")
	}
	if cfg.Observability.Counters.PollInterval != 5*time.Second {
		t.Errorf("zero-value config did not inherit poll_interval=5s")
	}
}

func TestObservabilityCounters_ApplyDefaults_PreservesEnabled(t *testing.T) {
	cfg := &Config{Observability: ObservabilityConfig{Counters: CountersConfig{
		Enabled:      true,
		PollInterval: 0, // operator forgot to set
	}}}
	applyDefaults(cfg)
	if cfg.Observability.Counters.PollInterval != 5*time.Second {
		t.Errorf("explicit Enabled=true with zero interval should fill default, got %v",
			cfg.Observability.Counters.PollInterval)
	}
}

func TestObservabilityCounters_ApplyDefaults_PreservesDisable(t *testing.T) {
	cfg := &Config{Observability: ObservabilityConfig{Counters: CountersConfig{
		Enabled:      false,
		PollInterval: 10 * time.Second,
	}}}
	applyDefaults(cfg)
	// Operator explicitly set poll_interval; preserve it even though
	// Enabled=false. (Re-enabling later via admin should use the
	// configured value, not the global default.)
	if cfg.Observability.Counters.PollInterval != 10*time.Second {
		t.Errorf("explicit poll_interval lost: %v, want 10s", cfg.Observability.Counters.PollInterval)
	}
	if cfg.Observability.Counters.Enabled {
		t.Errorf("explicit Enabled=false flipped to true")
	}
}

func TestObservabilityCounters_Validate_RejectsZeroIntervalWhenEnabled(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PollInterval = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "poll_interval must be > 0") {
		t.Errorf("err = %v, want 'poll_interval must be > 0'", err)
	}
}

func TestObservabilityCounters_Validate_RejectsTooSmallInterval(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PollInterval = 10 * time.Millisecond
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "minimum is 100ms") {
		t.Errorf("err = %v, want 'minimum is 100ms'", err)
	}
}

func TestObservabilityCounters_Validate_DisabledAllowsZero(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Enabled = false
	cfg.Observability.Counters.PollInterval = 0
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled + zero interval should pass validation, got %v", err)
	}
}
