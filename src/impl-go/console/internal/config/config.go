// Package config defines the dashw BFF configuration. Values are
// loaded from CLI flags (primary) with env-var fallbacks. Every
// exported field has a sensible default so the binary works
// out-of-the-box for local development.
package config

import (
	"flag"
	"log/slog"
	"os"
	"time"
)

// Config holds all dashw BFF settings.
type Config struct {
	// ── Networking ───────────────────────────────────────────────
	Listen string // BFF HTTP listen address (default ":8080")

	// ── dashd backends ──────────────────────────────────────────
	DashdRestAddr  string // dashd REST :8443
	DashdGrpcAddr  string // dashd gRPC :9443
	DashdAdminAddr string // dashd Admin :7443
	SimBaseAddr    string // dash-sim URL template (e.g. "http://sim-{id}:8080")

	// ── Timeouts ────────────────────────────────────────────────
	ProxyTimeout    time.Duration // reverse-proxy timeout
	GrpcDialTimeout time.Duration // gRPC dial timeout
	WsWriteTimeout  time.Duration // WebSocket write deadline
	WsPongTimeout   time.Duration // WebSocket pong wait
	WsPingInterval  time.Duration // WebSocket ping interval

	// ── Cache ───────────────────────────────────────────────────
	CacheFastTTL    time.Duration // TTL for fast-poll endpoints (fleet/summary, etc.)
	CacheSlowTTL    time.Duration // TTL for slow-poll endpoints (topology, etc.)
	CacheStaleWindow time.Duration // stale-while-revalidate window beyond TTL

	// ── Rate limiting ───────────────────────────────────────────
	WriteBurstPerIP  int // mutations/sec per source IP
	WriteRatePerMin  int // mutations/min per source IP
	BatchSizeLimit   int // max objects in ApplyBatch

	// ── Circuit breaker ─────────────────────────────────────────
	CBFailureThreshold int           // failures before OPEN
	CBResetTimeout     time.Duration // time in OPEN before HALF_OPEN

	// ── Feature flags ───────────────────────────────────────────
	EnableMetrics bool
	EnableCORS    bool
	DashdInsecure bool // skip dashd TLS verification

	// ── Logging ─────────────────────────────────────────────────
	LogLevel slog.Level

	// ── Build info ──────────────────────────────────────────────
	Version string

	// ── Auth (env-only, not flags) ──────────────────────────────
	DashdAuthToken string
	DashdTLSCert   string
	DashdTLSKey    string
	DashdTLSCA     string
}

// DefaultConfig returns a Config with all defaults suitable for
// local development against a dashd-fleet Docker setup.
func DefaultConfig() *Config {
	return &Config{
		Listen:         ":8080",
		DashdRestAddr:  "http://localhost:8443",
		DashdGrpcAddr:  "localhost:9443",
		DashdAdminAddr: "http://localhost:7443",
		SimBaseAddr:    "",

		ProxyTimeout:    30 * time.Second,
		GrpcDialTimeout: 10 * time.Second,
		WsWriteTimeout:  10 * time.Second,
		WsPongTimeout:   60 * time.Second,
		WsPingInterval:  30 * time.Second,

		CacheFastTTL:     5 * time.Second,
		CacheSlowTTL:     30 * time.Second,
		CacheStaleWindow: 30 * time.Second,

		WriteBurstPerIP:  10,
		WriteRatePerMin:  100,
		BatchSizeLimit:   50,

		CBFailureThreshold: 5,
		CBResetTimeout:     30 * time.Second,

		EnableMetrics: false,
		EnableCORS:    false,
		DashdInsecure: true,
		LogLevel:      slog.LevelInfo,
		Version:       "dev",
	}
}

// Parse creates a Config from CLI flags and environment variables.
// Flags take precedence over env vars. Defaults are applied first.
func Parse(args []string) *Config {
	cfg := DefaultConfig()

	fs := flag.NewFlagSet("dashw", flag.ExitOnError)

	// Networking
	fs.StringVar(&cfg.Listen, "listen", env("DASHW_LISTEN", cfg.Listen), "HTTP listen address")

	// dashd backends
	fs.StringVar(&cfg.DashdRestAddr, "dashd-rest", env("DASHD_REST_ADDR", cfg.DashdRestAddr), "dashd REST address")
	fs.StringVar(&cfg.DashdGrpcAddr, "dashd-grpc", env("DASHD_GRPC_ADDR", cfg.DashdGrpcAddr), "dashd gRPC address")
	fs.StringVar(&cfg.DashdAdminAddr, "dashd-admin", env("DASHD_ADMIN_ADDR", cfg.DashdAdminAddr), "dashd Admin address")
	fs.StringVar(&cfg.SimBaseAddr, "sim-base", env("DASHW_SIM_BASE", cfg.SimBaseAddr), "dash-sim URL template")

	// Timeouts
	fs.DurationVar(&cfg.ProxyTimeout, "proxy-timeout", cfg.ProxyTimeout, "proxy timeout")
	fs.DurationVar(&cfg.GrpcDialTimeout, "grpc-dial-timeout", cfg.GrpcDialTimeout, "gRPC dial timeout")
	fs.DurationVar(&cfg.WsWriteTimeout, "ws-write-timeout", cfg.WsWriteTimeout, "WebSocket write timeout")
	fs.DurationVar(&cfg.WsPongTimeout, "ws-pong-timeout", cfg.WsPongTimeout, "WebSocket pong timeout")
	fs.DurationVar(&cfg.WsPingInterval, "ws-ping-interval", cfg.WsPingInterval, "WebSocket ping interval")

	// Cache
	fs.DurationVar(&cfg.CacheFastTTL, "cache-fast-ttl", cfg.CacheFastTTL, "TTL for fast-poll cache entries")
	fs.DurationVar(&cfg.CacheSlowTTL, "cache-slow-ttl", cfg.CacheSlowTTL, "TTL for slow-poll cache entries")
	fs.DurationVar(&cfg.CacheStaleWindow, "cache-stale-window", cfg.CacheStaleWindow, "stale-while-revalidate window")

	// Rate limiting
	fs.IntVar(&cfg.WriteBurstPerIP, "write-burst", cfg.WriteBurstPerIP, "max mutations/sec per IP")
	fs.IntVar(&cfg.WriteRatePerMin, "write-rate-min", cfg.WriteRatePerMin, "max mutations/min per IP")
	fs.IntVar(&cfg.BatchSizeLimit, "batch-size-limit", cfg.BatchSizeLimit, "max objects in ApplyBatch")

	// Circuit breaker
	fs.IntVar(&cfg.CBFailureThreshold, "cb-threshold", cfg.CBFailureThreshold, "circuit breaker failure threshold")
	fs.DurationVar(&cfg.CBResetTimeout, "cb-timeout", cfg.CBResetTimeout, "circuit breaker reset timeout")

	// Feature flags
	fs.BoolVar(&cfg.EnableMetrics, "metrics", envBool("DASHW_METRICS", cfg.EnableMetrics), "enable /metrics")
	fs.BoolVar(&cfg.EnableCORS, "cors", envBool("DASHW_CORS", cfg.EnableCORS), "enable CORS")
	fs.BoolVar(&cfg.DashdInsecure, "dashd-insecure", cfg.DashdInsecure, "skip dashd TLS verify")

	// Logging
	var logLvl string
	fs.StringVar(&logLvl, "log-level", env("DASHW_LOG_LEVEL", "info"), "log level (debug|info|warn|error)")

	_ = fs.Parse(args)

	cfg.LogLevel = parseLevel(logLvl)

	// Auth (env-only — never on the command line)
	cfg.DashdAuthToken = os.Getenv("DASHD_AUTH_TOKEN")
	cfg.DashdTLSCert = os.Getenv("DASHD_TLS_CERT")
	cfg.DashdTLSKey = os.Getenv("DASHD_TLS_KEY")
	cfg.DashdTLSCA = os.Getenv("DASHD_TLS_CA")

	return cfg
}

// env returns the environment variable value if set, otherwise the
// default. This enables env-var fallback without overriding explicit
// flag values (flags are parsed after env defaults are applied).
func env(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

// envBool returns the env var as bool if set, otherwise the default.
func envBool(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v == "true" || v == "1" || v == "yes"
}

// parseLevel converts a string to slog.Level.
func parseLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}