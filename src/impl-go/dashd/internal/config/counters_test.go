// counters_test.go covers the Observability.Counters config block:
//   * PE-3b / PE-G9: Enabled + PollInterval defaults + validation.
//   * PE-3c / PD-G5: PerDpuOverrides map + StreamConfig sub-block
//     defaults + per-knob validation + cross-field invariants.
//
// Coverage target: every branch of every Validate / applyDefaults
// path for these fields. The CountersConfig + StreamConfig structures
// are operator-facing — bad YAML must always surface a clear, actionable
// error, never a silent miscalibration.

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

// ── PE-3c additions ──────────────────────────────────────────────────────

func TestStreamConfig_Defaults(t *testing.T) {
	s := Default().Observability.Counters.Stream
	want := defaultStreamConfig()
	if s != want {
		t.Errorf("Default().Stream = %+v, want %+v", s, want)
	}
}

func TestStreamConfig_ApplyDefaults_PerFieldOverride(t *testing.T) {
	// Operator overrides only the SSE floor; everything else should
	// inherit defaults. This is the "ultra-flexible" guarantee.
	cfg := &Config{Observability: ObservabilityConfig{Counters: CountersConfig{
		Enabled:      true,
		PollInterval: 5 * time.Second,
		Stream:       StreamConfig{MinIntervalSse: 2 * time.Second},
	}}}
	applyDefaults(cfg)
	def := defaultStreamConfig()
	got := cfg.Observability.Counters.Stream
	if got.MinIntervalSse != 2*time.Second {
		t.Errorf("MinIntervalSse override lost: got %v", got.MinIntervalSse)
	}
	if got.MinIntervalGrpc != def.MinIntervalGrpc {
		t.Errorf("MinIntervalGrpc default not applied: got %v want %v", got.MinIntervalGrpc, def.MinIntervalGrpc)
	}
	if got.MaxSubscribers != def.MaxSubscribers {
		t.Errorf("MaxSubscribers default not applied: got %v want %v", got.MaxSubscribers, def.MaxSubscribers)
	}
	if got.RingSize != def.RingSize {
		t.Errorf("RingSize default not applied: got %v want %v", got.RingSize, def.RingSize)
	}
	if got.KeepaliveInterval != def.KeepaliveInterval {
		t.Errorf("KeepaliveInterval default not applied: got %v want %v", got.KeepaliveInterval, def.KeepaliveInterval)
	}
}

func TestStreamConfig_ApplyDefaults_FillsAllZeroFields(t *testing.T) {
	// Sanity: every field defaults independently. Walk the field list
	// explicitly so adding a new field forces the test to be updated.
	cfg := &Config{Observability: ObservabilityConfig{Counters: CountersConfig{
		Enabled:      true,
		PollInterval: 5 * time.Second,
		Stream:       StreamConfig{}, // all zero
	}}}
	applyDefaults(cfg)
	def := defaultStreamConfig()
	got := cfg.Observability.Counters.Stream
	if got != def {
		t.Errorf("Stream defaults not applied. got=%+v want=%+v", got, def)
	}
}

func TestStreamConfig_Validate_RejectsZeroMinIntervalGrpc(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalGrpc = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_grpc must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsTooSmallMinIntervalGrpc(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalGrpc = 50 * time.Millisecond
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_grpc = 50ms; minimum is 100ms") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroMinIntervalSse(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalSse = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_sse must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsTooSmallMinIntervalSse(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalSse = 50 * time.Millisecond
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_sse = 50ms; minimum is 100ms") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroDefaultInterval(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.DefaultInterval = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "default_interval must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsGrpcFloorAboveDefault(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalGrpc = 10 * time.Second
	cfg.Observability.Counters.Stream.DefaultInterval = 5 * time.Second
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_grpc (10s) > default_interval (5s)") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsSseFloorAboveDefault(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalSse = 30 * time.Second
	cfg.Observability.Counters.Stream.DefaultInterval = 5 * time.Second
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "min_interval_sse (30s) > default_interval (5s)") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroMaxSubscribers(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MaxSubscribers = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "max_subscribers must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsNegativeMaxSubscribersPerSubject(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MaxSubscribersPerSubject = -1
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "max_subscribers_per_subject must be >= 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_AllowsZeroMaxSubscribersPerSubject(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MaxSubscribersPerSubject = 0 // explicit "disable per-subject cap"
	if err := cfg.Validate(); err != nil {
		t.Errorf("0 should disable per-subject cap, not fail: %v", err)
	}
}

func TestStreamConfig_Validate_RejectsPerSubjectAboveGlobal(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MaxSubscribers = 10
	cfg.Observability.Counters.Stream.MaxSubscribersPerSubject = 100
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "max_subscribers_per_subject (100) > max_subscribers (10)") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroBuffer(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.SubscriberBufferSize = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "subscriber_buffer_size must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroRingSize(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.RingSize = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "ring_size must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsNegativeCoalesce(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.CoalesceWindow = -1 * time.Millisecond
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "coalesce_window must be >= 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_AllowsZeroCoalesce(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.CoalesceWindow = 0 // explicit disable
	if err := cfg.Validate(); err != nil {
		t.Errorf("0 should disable coalescing, not fail: %v", err)
	}
}

func TestStreamConfig_Validate_RejectsNegativeKeepalive(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.KeepaliveInterval = -1 * time.Second
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "keepalive_interval must be >= 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_AllowsZeroKeepalive(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.KeepaliveInterval = 0 // explicit disable
	if err := cfg.Validate(); err != nil {
		t.Errorf("0 should disable keepalive, not fail: %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroRateLimit(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.RateLimitPerSecond = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "rate_limit_per_second must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsZeroBurst(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.RateLimitBurst = 0
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "rate_limit_burst must be > 0") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_RejectsBurstBelowSustained(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.RateLimitPerSecond = 500
	cfg.Observability.Counters.Stream.RateLimitBurst = 100
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "rate_limit_burst (100) < rate_limit_per_second (500)") {
		t.Errorf("err = %v", err)
	}
}

func TestStreamConfig_Validate_AggregatesMultipleErrors(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.Stream.MinIntervalGrpc = 0
	cfg.Observability.Counters.Stream.MinIntervalSse = 0
	cfg.Observability.Counters.Stream.MaxSubscribers = 0
	cfg.Observability.Counters.Stream.RingSize = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("want aggregated errors, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"min_interval_grpc must be > 0",
		"min_interval_sse must be > 0",
		"max_subscribers must be > 0",
		"ring_size must be > 0",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("missing %q in aggregated error: %s", want, msg)
		}
	}
}

// ── PerDpuOverrides ─────────────────────────────────────────────────────

func TestPerDpuOverrides_DefaultEmpty(t *testing.T) {
	if got := Default().Observability.Counters.PerDpuOverrides; got != nil {
		t.Errorf("default PerDpuOverrides = %v, want nil", got)
	}
}

func TestPerDpuOverrides_Validate_Accepts(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PerDpuOverrides = map[string]time.Duration{
		"dpu-edge-01":     1 * time.Second,
		"dpu-fastpath-02": 500 * time.Millisecond,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid overrides rejected: %v", err)
	}
}

func TestPerDpuOverrides_Validate_RejectsEmptyKey(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PerDpuOverrides = map[string]time.Duration{
		"": 1 * time.Second,
	}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), "per_dpu_overrides: empty DPU id") {
		t.Errorf("err = %v", err)
	}
}

func TestPerDpuOverrides_Validate_RejectsZeroInterval(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PerDpuOverrides = map[string]time.Duration{"dpu-1": 0}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `per_dpu_overrides["dpu-1"]: must be > 0`) {
		t.Errorf("err = %v", err)
	}
}

func TestPerDpuOverrides_Validate_RejectsBelowFloor(t *testing.T) {
	cfg := Default()
	cfg.Observability.Counters.PerDpuOverrides = map[string]time.Duration{"dpu-1": 10 * time.Millisecond}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `per_dpu_overrides["dpu-1"] = 10ms; minimum is 100ms`) {
		t.Errorf("err = %v", err)
	}
}

func TestPerDpuOverrides_Validate_NegativeIntervalFiresGTZero(t *testing.T) {
	// Negative is < 100ms but ALSO <= 0; validation should fire the
	// "> 0" branch (not the "minimum is 100ms" branch). Confirms the
	// order of checks documented in validateStreamConfig and the
	// per_dpu_overrides loop.
	cfg := Default()
	cfg.Observability.Counters.PerDpuOverrides = map[string]time.Duration{"dpu-1": -5 * time.Second}
	err := cfg.Validate()
	if err == nil || !strings.Contains(err.Error(), `per_dpu_overrides["dpu-1"]: must be > 0`) {
		t.Errorf("err = %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "minimum is 100ms") {
		t.Errorf("negative interval should NOT trip the 100ms-min branch: %v", err)
	}
}
