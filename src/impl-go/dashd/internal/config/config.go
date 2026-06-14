// Package config loads dashd configuration from YAML, applies defaults,
// and validates constraints. Phase 1 supports only file-backed storage.
package config

import (
	"errors"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the top-level dashd configuration.
type Config struct {
	// NodeID identifies THIS dashd process within its cluster. Defaults to
	// the OS hostname when empty. Used as the etcd-lease key in controller
	// mode (PA-3) and the raft node id in controllerless mode (PF).
	NodeID string `yaml:"node_id"`

	// Mode selects the deployment-topology personality. Either
	// "controller" (default) or "controllerless" (PF; parsed but rejected
	// at startup until PF-3). See internal/config/mode.go for the full
	// rationale.
	Mode string `yaml:"mode"`

	Listen    ListenConfig    `yaml:"listen"`
	Storage   StorageConfig   `yaml:"storage"`
	Inventory InventoryConfig `yaml:"inventory"`
	Reconcile ReconcileConfig `yaml:"reconcile"`
	Log       LogConfig       `yaml:"log"`

	// Auth is the auth posture (none|token|mtls). Mode"none" is the
	// default and matches today's plaintext behaviour exactly. PD will
	// activate "token" and "mtls" semantics. See internal/config/auth.go.
	Auth AuthConfig `yaml:"auth"`

	// HA is the mode-specific HA block. Only the sub-block matching Mode
	// may be populated. See internal/config/ha.go.
	HA HAConfig `yaml:"ha"`

	// Observability holds the polling + caching knobs for the counter
	// ingestion pipeline (PE-3b / PE-G9). Defaults: enabled, poll every
	// 5s. Operators flip the gate or change the interval through this
	// block + the corresponding admin endpoints.
	Observability ObservabilityConfig `yaml:"observability"`
}

// ListenConfig holds network listener addresses.
type ListenConfig struct {
	GRPCAddr  string `yaml:"grpc_addr"`  // default ":9443"
	RESTAddr  string `yaml:"rest_addr"`  // default ":8443"
	AdminAddr string `yaml:"admin_addr"` // default ":7443"
}

// StorageConfig selects and configures the desired-state backend.
//
// `file` is the today-default, single-node-friendly backend. `etcd`
// (PA-1b) is the controller-mode production backend backed by an etcd
// cluster. `raft` (PF) is the controllerless backend that reuses
// HA.Controllerless.Raft for transport.
type StorageConfig struct {
	Backend string             `yaml:"backend"` // file | etcd | raft
	File    FileStoreConfig    `yaml:"file"`
	Etcd    EtcdStorageConfig  `yaml:"etcd"`
}

// FileStoreConfig holds settings for the file-based store backend.
type FileStoreConfig struct {
	StateDir string `yaml:"state_dir"`
}

// EtcdStorageConfig holds settings for the etcd-backed store. Used when
// Storage.Backend == "etcd" under Mode == "controller". Validation rejects
// any combination outside this scope.
//
// All keys land under KeyPrefix; default "/dashd/state/". The store
// derives per-spec keys as "<KeyPrefix><namespace>/<kind>/<name>".
type EtcdStorageConfig struct {
	Endpoints   []string      `yaml:"endpoints"`
	KeyPrefix   string        `yaml:"key_prefix"`   // default "/dashd/state/"
	DialTimeout time.Duration `yaml:"dial_timeout"` // default 5s
	TLS         TLSConfig     `yaml:"tls"`
}

// InventoryConfig selects and configures the DPU inventory source.
type InventoryConfig struct {
	Source string `yaml:"source"` // "file" | "api"
	File   string `yaml:"file"`   // required when Source=="file"
}

// ReconcileConfig tunes the reconciler and dispatch subsystems.
type ReconcileConfig struct {
	TickInterval      time.Duration `yaml:"tick_interval"`        // default 30s
	PerDPUInboxSize   int           `yaml:"per_dpu_inbox_size"`   // default 1
	ApplyRateLimit    float64       `yaml:"apply_rate_limit"`     // default 100
	ErrorBudgetPerMin int           `yaml:"error_budget_per_min"` // default 10
}

// LogConfig configures structured logging.
type LogConfig struct {
	Level  string `yaml:"level"`  // debug|info|warn|error, default info
	Format string `yaml:"format"` // json|text, default json
}

// ObservabilityConfig is the top-level observability block. Today it
// only holds counter polling knobs (PE-3b); diagnostics + audit moved
// here in future phases.
type ObservabilityConfig struct {
	Counters CountersConfig `yaml:"counters"`
}

// CountersConfig tunes the dashd → dash-sim/DPU counter polling loop
// AND the downstream PE-3c streaming surface (ObservabilityService.
// GetCounters + REST/SSE).
//
//   - Enabled defaults to true. Set to false to stop polling without
//     unwiring the pipeline (admin endpoints stay reachable and the
//     poller can be re-enabled at runtime).
//   - PollInterval default 5s; minimum 100ms (anything tighter just
//     thrashes sims with no observable benefit).
//   - PerDpuOverrides (PE-3c) maps a DPU id to a poll cadence that
//     trumps PollInterval for that one DPU. Each entry is also
//     validated against the 100ms floor. Useful for noisy edge DPUs
//     that need 1s sampling while the rest of the fleet polls at 5s.
//   - Stream (PE-3c) bundles every broadcaster + handler knob: cap,
//     ring, coalesce, keepalive, rate-limit, plus the per-protocol
//     minimum sample interval floors (gRPC vs SSE).
type CountersConfig struct {
	Enabled          bool                     `yaml:"enabled"`
	PollInterval     time.Duration            `yaml:"poll_interval"`
	PerDpuOverrides  map[string]time.Duration `yaml:"per_dpu_overrides"`
	Stream           StreamConfig             `yaml:"stream"`
}

// StreamConfig is the PE-3c broadcaster + handler tuning block.
//
// All fields default to production-sane values when zero. Validation
// enforces the relationships between them (e.g. min_interval_grpc
// <= default_interval). Operators MUST be free to tune every knob via
// YAML — this is the project's "ultra-flexible" config posture.
type StreamConfig struct {
	// MinIntervalGrpc clamps the sample cadence requested by gRPC
	// clients (dashctl + automation). Default 100ms (matches poller
	// floor — automation can keep up).
	MinIntervalGrpc time.Duration `yaml:"min_interval_grpc"`

	// MinIntervalSse clamps the sample cadence visible to REST/SSE
	// clients (browsers via dashw). Default 1s (browsers can't usefully
	// render faster; tighter values just burn CPU + battery).
	MinIntervalSse time.Duration `yaml:"min_interval_sse"`

	// DefaultInterval is the cadence used when a client sends
	// CounterRequest.interval_seconds = 0. Default 5s.
	DefaultInterval time.Duration `yaml:"default_interval"`

	// MaxSubscribers caps total in-flight WatchCounters subscribers
	// across both transports. Default 256.
	MaxSubscribers int `yaml:"max_subscribers"`

	// MaxSubscribersPerSubject caps subscribers per auth Subject so
	// one runaway operator can't hog the pool. Default 8.
	MaxSubscribersPerSubject int `yaml:"max_subscribers_per_subject"`

	// SubscriberBufferSize is the per-subscriber buffered channel
	// depth. Smaller = faster overflow detection (KIND_DROPPED fires
	// sooner); larger = more headroom for transient stalls. Default 64.
	SubscriberBufferSize int `yaml:"subscriber_buffer_size"`

	// RingSize is the number of recent events retained for
	// resume_after_event_id replay (mirrors PE-G7 cluster broadcaster).
	// Default 512.
	RingSize int `yaml:"ring_size"`

	// CoalesceWindow merges KIND_REPORT events for the same dpu_id that
	// arrive within this duration. Sentinels are NEVER coalesced.
	// 0 disables coalescing. Default 250ms.
	CoalesceWindow time.Duration `yaml:"coalesce_window"`

	// KeepaliveInterval is the cadence of the single global keepalive
	// goroutine. 0 disables. Default 30s.
	KeepaliveInterval time.Duration `yaml:"keepalive_interval"`

	// RateLimitPerSecond is the steady-state event ceiling per subscriber
	// enforced by a leaky bucket. Default 200.
	RateLimitPerSecond float64 `yaml:"rate_limit_per_second"`

	// RateLimitBurst is the leaky-bucket capacity. Default 400.
	RateLimitBurst float64 `yaml:"rate_limit_burst"`
}

// Default returns a Config with all production defaults filled in.
func Default() *Config {
	return &Config{
		NodeID: defaultNodeID(),
		Mode:   defaultMode,
		Listen: ListenConfig{
			GRPCAddr:  ":9443",
			RESTAddr:  ":8443",
			AdminAddr: ":7443",
		},
		Storage: StorageConfig{
			Backend: "file",
			File:    FileStoreConfig{StateDir: "/var/lib/dashd"},
		},
Inventory: InventoryConfig{
Source: "api",
},
		Reconcile: ReconcileConfig{
			TickInterval:      30 * time.Second,
			PerDPUInboxSize:   1,
			ApplyRateLimit:    100,
			ErrorBudgetPerMin: 10,
		},
		Log: LogConfig{
			Level:  "info",
			Format: "json",
		},
		Auth: defaultAuthConfig(),
		HA:   defaultHAConfig(),
		Observability: ObservabilityConfig{
			Counters: CountersConfig{
				Enabled:         true,
				PollInterval:    5 * time.Second,
				PerDpuOverrides: nil,
				Stream:          defaultStreamConfig(),
			},
		},
	}
}

// defaultStreamConfig is the production-ready PE-3c streaming tuning.
// Numbers chosen to mirror PE-G7's cluster broadcaster where they apply
// (max_subscribers, ring_size, keepalive_interval, rate_limit_*) and to
// match the (Q1/Q2/Q3/Q4) plan decisions where they diverge
// (min_interval_grpc, min_interval_sse, default_interval, coalesce).
func defaultStreamConfig() StreamConfig {
	return StreamConfig{
		MinIntervalGrpc:          100 * time.Millisecond,
		MinIntervalSse:           1 * time.Second,
		DefaultInterval:          5 * time.Second,
		MaxSubscribers:           256,
		MaxSubscribersPerSubject: 8,
		SubscriberBufferSize:     64,
		RingSize:                 512,
		CoalesceWindow:           250 * time.Millisecond,
		KeepaliveInterval:        30 * time.Second,
		RateLimitPerSecond:       200,
		RateLimitBurst:           400,
	}
}

// Load reads a YAML config file, applies defaults for any missing fields,
// and validates the result.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}

	applyDefaults(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: validate %s: %w", path, err)
	}
	return cfg, nil
}

// Validate checks all fields for correctness.
func (c *Config) Validate() error {
	var errs []error

	// Mode + node identity (locked: K1..K5).
	if err := validateMode(c.Mode, c.NodeID); err != nil {
		errs = append(errs, err)
	}

	// Storage backend selection.
	// PA-1 keeps the file backend as today's default. The etcd backend
	// (planned PA-1b/PA-2) parses but is rejected with a clean
	// "not yet implemented" message until its real implementation lands.
	switch c.Storage.Backend {
	case "file":
		if c.Storage.File.StateDir == "" {
			errs = append(errs, errors.New("storage.file.state_dir is required when backend=file"))
		}
	case "etcd":
		if c.Mode == ModeControllerless {
			errs = append(errs, errors.New(`storage.backend="etcd" is for controller mode; use "raft" when mode="controllerless"`))
		}
		if len(c.Storage.Etcd.Endpoints) == 0 {
			errs = append(errs, errors.New(`storage.backend="etcd" requires storage.etcd.endpoints`))
		}
		for i, ep := range c.Storage.Etcd.Endpoints {
			if ep == "" {
				errs = append(errs, fmt.Errorf("storage.etcd.endpoints[%d] is empty", i))
			}
		}
		if c.Storage.Etcd.DialTimeout < 0 {
			errs = append(errs, errors.New("storage.etcd.dial_timeout must be >= 0"))
		}
		if (c.Storage.Etcd.TLS.CertFile == "") != (c.Storage.Etcd.TLS.KeyFile == "") {
			errs = append(errs, errors.New("storage.etcd.tls.cert_file and storage.etcd.tls.key_file must be set together"))
		}
	case "raft":
		errs = append(errs, errors.New(`storage.backend="raft" is not yet implemented (planned for Phase 2 PF); use "file" until then`))
		if c.Mode == ModeController {
			errs = append(errs, errors.New(`storage.backend="raft" is for controllerless mode; use "file" or "etcd" when mode="controller"`))
		}
	default:
		errs = append(errs, fmt.Errorf("storage.backend: unsupported value %q (allowed: file, etcd, raft)", c.Storage.Backend))
	}

	// Inventory
	switch c.Inventory.Source {
	case "file":
		if c.Inventory.File == "" {
			errs = append(errs, errors.New("inventory.file is required when source=file"))
		}
	case "api":
		// valid
	default:
		errs = append(errs, fmt.Errorf("inventory.source: unsupported value %q (allowed: file, api)", c.Inventory.Source))
	}

	// Reconcile
	if c.Reconcile.TickInterval <= 0 {
		errs = append(errs, errors.New("reconcile.tick_interval must be > 0"))
	}
	if c.Reconcile.PerDPUInboxSize < 1 {
		errs = append(errs, errors.New("reconcile.per_dpu_inbox_size must be >= 1"))
	}
	if c.Reconcile.ApplyRateLimit <= 0 {
		errs = append(errs, errors.New("reconcile.apply_rate_limit must be > 0"))
	}
	if c.Reconcile.ErrorBudgetPerMin < 1 {
		errs = append(errs, errors.New("reconcile.error_budget_per_min must be >= 1"))
	}

	// Log
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
		// valid
	default:
		errs = append(errs, fmt.Errorf("log.level: unsupported value %q (allowed: debug, info, warn, error)", c.Log.Level))
	}
	switch c.Log.Format {
	case "json", "text":
		// valid
	default:
		errs = append(errs, fmt.Errorf("log.format: unsupported value %q (allowed: json, text)", c.Log.Format))
	}

	// Auth + HA (locked: D11..D15, K6..K8).
	if err := validateAuth(c.Auth); err != nil {
		errs = append(errs, err)
	}
	if err := validateHA(c.Mode, c.HA); err != nil {
		errs = append(errs, err)
	}

	// Observability.Counters. Enabled requires a non-zero PollInterval
	// that is >= 100ms. (Defaults already filled in by applyDefaults;
	// validation guards explicitly-bad YAML.)
	if c.Observability.Counters.Enabled {
		if c.Observability.Counters.PollInterval <= 0 {
			errs = append(errs, errors.New("observability.counters.poll_interval must be > 0 when enabled"))
		} else if c.Observability.Counters.PollInterval < 100*time.Millisecond {
			errs = append(errs, fmt.Errorf("observability.counters.poll_interval = %s; minimum is 100ms", c.Observability.Counters.PollInterval))
		}
	}

	// Observability.Counters.PerDpuOverrides (PE-3c). Each entry MUST
	// have a non-empty DPU id AND honour the same 100ms floor as the
	// global PollInterval. Empty map is fine — it just means no override.
	for dpu, d := range c.Observability.Counters.PerDpuOverrides {
		if dpu == "" {
			errs = append(errs, errors.New("observability.counters.per_dpu_overrides: empty DPU id"))
			continue
		}
		if d <= 0 {
			errs = append(errs, fmt.Errorf("observability.counters.per_dpu_overrides[%q]: must be > 0", dpu))
		} else if d < 100*time.Millisecond {
			errs = append(errs, fmt.Errorf("observability.counters.per_dpu_overrides[%q] = %s; minimum is 100ms", dpu, d))
		}
	}

	// Observability.Counters.Stream (PE-3c). Every knob has a default
	// applied by applyDefaults; validation guards explicit YAML that
	// breaks the invariants we depend on (e.g. min_interval_grpc must
	// be <= default_interval).
	if err := validateStreamConfig(c.Observability.Counters.Stream); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// validateStreamConfig validates every PE-3c streaming knob individually
// AND the cross-field invariants. Returned as a single joined error so
// the caller surfaces every problem in one shot.
func validateStreamConfig(s StreamConfig) error {
	var errs []error
	if s.MinIntervalGrpc <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.min_interval_grpc must be > 0"))
	} else if s.MinIntervalGrpc < 100*time.Millisecond {
		errs = append(errs, fmt.Errorf("observability.counters.stream.min_interval_grpc = %s; minimum is 100ms", s.MinIntervalGrpc))
	}
	if s.MinIntervalSse <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.min_interval_sse must be > 0"))
	} else if s.MinIntervalSse < 100*time.Millisecond {
		errs = append(errs, fmt.Errorf("observability.counters.stream.min_interval_sse = %s; minimum is 100ms", s.MinIntervalSse))
	}
	if s.DefaultInterval <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.default_interval must be > 0"))
	}
	if s.MinIntervalGrpc > 0 && s.DefaultInterval > 0 && s.MinIntervalGrpc > s.DefaultInterval {
		errs = append(errs, fmt.Errorf("observability.counters.stream: min_interval_grpc (%s) > default_interval (%s)", s.MinIntervalGrpc, s.DefaultInterval))
	}
	if s.MinIntervalSse > 0 && s.DefaultInterval > 0 && s.MinIntervalSse > s.DefaultInterval {
		errs = append(errs, fmt.Errorf("observability.counters.stream: min_interval_sse (%s) > default_interval (%s)", s.MinIntervalSse, s.DefaultInterval))
	}
	if s.MaxSubscribers <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.max_subscribers must be > 0"))
	}
	if s.MaxSubscribersPerSubject < 0 {
		errs = append(errs, errors.New("observability.counters.stream.max_subscribers_per_subject must be >= 0 (0 disables per-subject cap)"))
	}
	if s.MaxSubscribersPerSubject > 0 && s.MaxSubscribersPerSubject > s.MaxSubscribers {
		errs = append(errs, fmt.Errorf("observability.counters.stream: max_subscribers_per_subject (%d) > max_subscribers (%d)", s.MaxSubscribersPerSubject, s.MaxSubscribers))
	}
	if s.SubscriberBufferSize <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.subscriber_buffer_size must be > 0"))
	}
	if s.RingSize <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.ring_size must be > 0"))
	}
	if s.CoalesceWindow < 0 {
		errs = append(errs, errors.New("observability.counters.stream.coalesce_window must be >= 0 (0 disables coalescing)"))
	}
	if s.KeepaliveInterval < 0 {
		errs = append(errs, errors.New("observability.counters.stream.keepalive_interval must be >= 0 (0 disables keepalive)"))
	}
	if s.RateLimitPerSecond <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.rate_limit_per_second must be > 0"))
	}
	if s.RateLimitBurst <= 0 {
		errs = append(errs, errors.New("observability.counters.stream.rate_limit_burst must be > 0"))
	}
	if s.RateLimitPerSecond > 0 && s.RateLimitBurst > 0 && s.RateLimitBurst < s.RateLimitPerSecond {
		errs = append(errs, fmt.Errorf("observability.counters.stream: rate_limit_burst (%v) < rate_limit_per_second (%v); burst should be ≥ sustained rate", s.RateLimitBurst, s.RateLimitPerSecond))
	}
	return errors.Join(errs...)
}

// applyDefaults fills in zero-value fields from the production defaults.
func applyDefaults(c *Config) {
	d := Default()
	applyModeDefaults(c)
	applyAuthDefaults(&c.Auth)
	applyHADefaults(c)
	if c.Listen.GRPCAddr == "" {
		c.Listen.GRPCAddr = d.Listen.GRPCAddr
	}
	if c.Listen.RESTAddr == "" {
		c.Listen.RESTAddr = d.Listen.RESTAddr
	}
	if c.Listen.AdminAddr == "" {
		c.Listen.AdminAddr = d.Listen.AdminAddr
	}
	if c.Storage.Backend == "" {
		c.Storage.Backend = d.Storage.Backend
	}
	if c.Storage.Backend == "file" && c.Storage.File.StateDir == "" {
		c.Storage.File.StateDir = d.Storage.File.StateDir
	}
	if c.Storage.Backend == "etcd" {
		if c.Storage.Etcd.KeyPrefix == "" {
			c.Storage.Etcd.KeyPrefix = "/dashd/state/"
		}
		if c.Storage.Etcd.DialTimeout == 0 {
			c.Storage.Etcd.DialTimeout = 5 * time.Second
		}
	}
	if c.Inventory.Source == "" {
		c.Inventory.Source = d.Inventory.Source
	}
	if c.Reconcile.TickInterval == 0 {
		c.Reconcile.TickInterval = d.Reconcile.TickInterval
	}
	if c.Reconcile.PerDPUInboxSize == 0 {
		c.Reconcile.PerDPUInboxSize = d.Reconcile.PerDPUInboxSize
	}
	if c.Reconcile.ApplyRateLimit == 0 {
		c.Reconcile.ApplyRateLimit = d.Reconcile.ApplyRateLimit
	}
	if c.Reconcile.ErrorBudgetPerMin == 0 {
		c.Reconcile.ErrorBudgetPerMin = d.Reconcile.ErrorBudgetPerMin
	}
	if c.Log.Level == "" {
		c.Log.Level = d.Log.Level
	}
	if c.Log.Format == "" {
		c.Log.Format = d.Log.Format
	}

	// Observability — counters PollInterval + Stream sub-block.
	if !c.Observability.Counters.Enabled && c.Observability.Counters.PollInterval == 0 {
		// Zero value: never specified in YAML — inherit full defaults.
		c.Observability.Counters = d.Observability.Counters
	} else if c.Observability.Counters.PollInterval == 0 {
		c.Observability.Counters.PollInterval = d.Observability.Counters.PollInterval
	}
	// Per-field merge of Stream sub-block (PE-3c). Per-knob defaults
	// let operators override one field without re-specifying the entire
	// block.
	mergeStreamDefaults(&c.Observability.Counters.Stream, d.Observability.Counters.Stream)
}

// mergeStreamDefaults fills any zero-value field in cur from def. Each
// knob is independently defaulted so operators can override just the
// one they care about.
func mergeStreamDefaults(cur *StreamConfig, def StreamConfig) {
	if cur.MinIntervalGrpc == 0 {
		cur.MinIntervalGrpc = def.MinIntervalGrpc
	}
	if cur.MinIntervalSse == 0 {
		cur.MinIntervalSse = def.MinIntervalSse
	}
	if cur.DefaultInterval == 0 {
		cur.DefaultInterval = def.DefaultInterval
	}
	if cur.MaxSubscribers == 0 {
		cur.MaxSubscribers = def.MaxSubscribers
	}
	if cur.MaxSubscribersPerSubject == 0 {
		cur.MaxSubscribersPerSubject = def.MaxSubscribersPerSubject
	}
	if cur.SubscriberBufferSize == 0 {
		cur.SubscriberBufferSize = def.SubscriberBufferSize
	}
	if cur.RingSize == 0 {
		cur.RingSize = def.RingSize
	}
	if cur.CoalesceWindow == 0 {
		cur.CoalesceWindow = def.CoalesceWindow
	}
	if cur.KeepaliveInterval == 0 {
		cur.KeepaliveInterval = def.KeepaliveInterval
	}
	if cur.RateLimitPerSecond == 0 {
		cur.RateLimitPerSecond = def.RateLimitPerSecond
	}
	if cur.RateLimitBurst == 0 {
		cur.RateLimitBurst = def.RateLimitBurst
	}
}