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
	Listen    ListenConfig    `yaml:"listen"`
	Storage   StorageConfig   `yaml:"storage"`
	Inventory InventoryConfig `yaml:"inventory"`
	Reconcile ReconcileConfig `yaml:"reconcile"`
	Log       LogConfig       `yaml:"log"`
}

// ListenConfig holds network listener addresses.
type ListenConfig struct {
	GRPCAddr  string `yaml:"grpc_addr"`  // default ":9443"
	RESTAddr  string `yaml:"rest_addr"`  // default ":8443"
	AdminAddr string `yaml:"admin_addr"` // default ":7443"
}

// StorageConfig selects and configures the desired-state backend.
type StorageConfig struct {
	Backend string          `yaml:"backend"` // "file" only in Phase 1
	File    FileStoreConfig `yaml:"file"`
}

// FileStoreConfig holds settings for the file-based store backend.
type FileStoreConfig struct {
	StateDir string `yaml:"state_dir"`
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

// Default returns a Config with all production defaults filled in.
func Default() *Config {
	return &Config{
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

	// Storage
	switch c.Storage.Backend {
	case "file":
		if c.Storage.File.StateDir == "" {
			errs = append(errs, errors.New("storage.file.state_dir is required when backend=file"))
		}
	case "etcd":
		errs = append(errs, errors.New("storage.backend=etcd is not supported in Phase 1"))
	default:
		errs = append(errs, fmt.Errorf("storage.backend: unsupported value %q (allowed: file)", c.Storage.Backend))
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

	return errors.Join(errs...)
}

// applyDefaults fills in zero-value fields from the production defaults.
func applyDefaults(c *Config) {
	d := Default()
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
}