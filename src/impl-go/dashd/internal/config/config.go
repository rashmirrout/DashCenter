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

// CountersConfig tunes the dashd → dash-sim/DPU counter polling loop.
//
//   - Enabled defaults to true. Set to false to stop polling without
//     unwiring the pipeline (admin endpoints stay reachable and the
//     poller can be re-enabled at runtime).
//   - PollInterval default 5s; minimum 100ms (anything tighter just
//     thrashes sims with no observable benefit).
//
// Per-DPU overrides are deferred to PE-3c — the runtime
// SetInterval admin endpoint covers the fleet-wide knob today and
// the per-DPU branch slots in cleanly when needed.
type CountersConfig struct {
	Enabled      bool          `yaml:"enabled"`
	PollInterval time.Duration `yaml:"poll_interval"`
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
				Enabled:      true,
				PollInterval: 5 * time.Second,
			},
		},
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

	// Observability — only the counters sub-block exists today.
	if !c.Observability.Counters.Enabled && c.Observability.Counters.PollInterval == 0 {
		// Zero value: never specified in YAML — inherit full defaults.
		c.Observability.Counters = d.Observability.Counters
	} else if c.Observability.Counters.PollInterval == 0 {
		c.Observability.Counters.PollInterval = d.Observability.Counters.PollInterval
	}
}