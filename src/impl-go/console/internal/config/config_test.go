package config

import (
"log/slog"
"os"
"testing"
"time"
)

func TestDefaultConfig(t *testing.T) {
cfg := DefaultConfig()

if cfg.Listen != ":8080" {
t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
}
if cfg.DashdRestAddr != "http://localhost:8443" {
t.Errorf("DashdRestAddr = %q, want %q", cfg.DashdRestAddr, "http://localhost:8443")
}
if cfg.DashdGrpcAddr != "localhost:9443" {
t.Errorf("DashdGrpcAddr = %q, want %q", cfg.DashdGrpcAddr, "localhost:9443")
}
if cfg.DashdAdminAddr != "http://localhost:7443" {
t.Errorf("DashdAdminAddr = %q, want %q", cfg.DashdAdminAddr, "http://localhost:7443")
}
if cfg.ProxyTimeout != 30*time.Second {
t.Errorf("ProxyTimeout = %v, want 30s", cfg.ProxyTimeout)
}
if cfg.CacheFastTTL != 5*time.Second {
t.Errorf("CacheFastTTL = %v, want 5s", cfg.CacheFastTTL)
}
if cfg.CacheSlowTTL != 30*time.Second {
t.Errorf("CacheSlowTTL = %v, want 30s", cfg.CacheSlowTTL)
}
if cfg.CacheStaleWindow != 30*time.Second {
t.Errorf("CacheStaleWindow = %v, want 30s", cfg.CacheStaleWindow)
}
if cfg.WriteBurstPerIP != 10 {
t.Errorf("WriteBurstPerIP = %d, want 10", cfg.WriteBurstPerIP)
}
if cfg.WriteRatePerMin != 100 {
t.Errorf("WriteRatePerMin = %d, want 100", cfg.WriteRatePerMin)
}
if cfg.BatchSizeLimit != 50 {
t.Errorf("BatchSizeLimit = %d, want 50", cfg.BatchSizeLimit)
}
if cfg.CBFailureThreshold != 5 {
t.Errorf("CBFailureThreshold = %d, want 5", cfg.CBFailureThreshold)
}
if cfg.CBResetTimeout != 30*time.Second {
t.Errorf("CBResetTimeout = %v, want 30s", cfg.CBResetTimeout)
}
if cfg.EnableMetrics {
t.Error("EnableMetrics should default to false")
}
if cfg.EnableCORS {
t.Error("EnableCORS should default to false")
}
if !cfg.DashdInsecure {
t.Error("DashdInsecure should default to true")
}
if cfg.LogLevel != slog.LevelInfo {
t.Errorf("LogLevel = %v, want Info", cfg.LogLevel)
}
if cfg.Version != "dev" {
t.Errorf("Version = %q, want %q", cfg.Version, "dev")
}
}

func TestParse_Defaults(t *testing.T) {
// Clear env vars that could interfere
for _, k := range []string{
"DASHW_LISTEN", "DASHD_REST_ADDR", "DASHD_GRPC_ADDR",
"DASHD_ADMIN_ADDR", "DASHW_SIM_BASE", "DASHW_METRICS",
"DASHW_CORS", "DASHW_LOG_LEVEL",
"DASHD_AUTH_TOKEN", "DASHD_TLS_CERT", "DASHD_TLS_KEY", "DASHD_TLS_CA",
} {
os.Unsetenv(k)
}

cfg := Parse([]string{})

if cfg.Listen != ":8080" {
t.Errorf("Listen = %q, want %q", cfg.Listen, ":8080")
}
if cfg.DashdRestAddr != "http://localhost:8443" {
t.Errorf("DashdRestAddr = %q, want %q", cfg.DashdRestAddr, "http://localhost:8443")
}
if cfg.LogLevel != slog.LevelInfo {
t.Errorf("LogLevel = %v, want Info", cfg.LogLevel)
}
}

func TestParse_FlagsOverrideDefaults(t *testing.T) {
// Clear env vars
for _, k := range []string{
"DASHW_LISTEN", "DASHD_REST_ADDR", "DASHD_GRPC_ADDR",
"DASHD_ADMIN_ADDR", "DASHW_LOG_LEVEL",
} {
os.Unsetenv(k)
}

cfg := Parse([]string{
"--listen", ":3000",
"--dashd-rest", "http://dashd-1:8443",
"--dashd-grpc", "dashd-1:9443",
"--dashd-admin", "http://dashd-1:7443",
"--log-level", "debug",
"--cors",
"--metrics",
"--proxy-timeout", "15s",
"--cache-fast-ttl", "3s",
"--cache-slow-ttl", "60s",
"--cache-stale-window", "120s",
"--write-burst", "20",
"--write-rate-min", "200",
"--batch-size-limit", "100",
"--cb-threshold", "10",
"--cb-timeout", "60s",
})

if cfg.Listen != ":3000" {
t.Errorf("Listen = %q, want %q", cfg.Listen, ":3000")
}
if cfg.DashdRestAddr != "http://dashd-1:8443" {
t.Errorf("DashdRestAddr = %q, want %q", cfg.DashdRestAddr, "http://dashd-1:8443")
}
if cfg.DashdGrpcAddr != "dashd-1:9443" {
t.Errorf("DashdGrpcAddr = %q, want %q", cfg.DashdGrpcAddr, "dashd-1:9443")
}
if cfg.DashdAdminAddr != "http://dashd-1:7443" {
t.Errorf("DashdAdminAddr = %q, want %q", cfg.DashdAdminAddr, "http://dashd-1:7443")
}
if cfg.LogLevel != slog.LevelDebug {
t.Errorf("LogLevel = %v, want Debug", cfg.LogLevel)
}
if !cfg.EnableCORS {
t.Error("EnableCORS should be true")
}
if !cfg.EnableMetrics {
t.Error("EnableMetrics should be true")
}
if cfg.ProxyTimeout != 15*time.Second {
t.Errorf("ProxyTimeout = %v, want 15s", cfg.ProxyTimeout)
}
if cfg.CacheFastTTL != 3*time.Second {
t.Errorf("CacheFastTTL = %v, want 3s", cfg.CacheFastTTL)
}
if cfg.CacheSlowTTL != 60*time.Second {
t.Errorf("CacheSlowTTL = %v, want 60s", cfg.CacheSlowTTL)
}
if cfg.CacheStaleWindow != 120*time.Second {
t.Errorf("CacheStaleWindow = %v, want 120s", cfg.CacheStaleWindow)
}
if cfg.WriteBurstPerIP != 20 {
t.Errorf("WriteBurstPerIP = %d, want 20", cfg.WriteBurstPerIP)
}
if cfg.WriteRatePerMin != 200 {
t.Errorf("WriteRatePerMin = %d, want 200", cfg.WriteRatePerMin)
}
if cfg.BatchSizeLimit != 100 {
t.Errorf("BatchSizeLimit = %d, want 100", cfg.BatchSizeLimit)
}
if cfg.CBFailureThreshold != 10 {
t.Errorf("CBFailureThreshold = %d, want 10", cfg.CBFailureThreshold)
}
if cfg.CBResetTimeout != 60*time.Second {
t.Errorf("CBResetTimeout = %v, want 60s", cfg.CBResetTimeout)
}
}

func TestParse_EnvVarFallback(t *testing.T) {
// Set env vars
t.Setenv("DASHW_LISTEN", ":9090")
t.Setenv("DASHD_REST_ADDR", "http://env-dashd:8443")
t.Setenv("DASHD_GRPC_ADDR", "env-dashd:9443")
t.Setenv("DASHD_ADMIN_ADDR", "http://env-dashd:7443")
t.Setenv("DASHW_LOG_LEVEL", "warn")
t.Setenv("DASHW_METRICS", "true")
t.Setenv("DASHW_CORS", "1")

cfg := Parse([]string{})

if cfg.Listen != ":9090" {
t.Errorf("Listen = %q, want %q (from env)", cfg.Listen, ":9090")
}
if cfg.DashdRestAddr != "http://env-dashd:8443" {
t.Errorf("DashdRestAddr = %q, want %q (from env)", cfg.DashdRestAddr, "http://env-dashd:8443")
}
if cfg.DashdGrpcAddr != "env-dashd:9443" {
t.Errorf("DashdGrpcAddr = %q, want %q (from env)", cfg.DashdGrpcAddr, "env-dashd:9443")
}
if cfg.LogLevel != slog.LevelWarn {
t.Errorf("LogLevel = %v, want Warn (from env)", cfg.LogLevel)
}
if !cfg.EnableMetrics {
t.Error("EnableMetrics should be true from env DASHW_METRICS=true")
}
if !cfg.EnableCORS {
t.Error("EnableCORS should be true from env DASHW_CORS=1")
}
}

func TestParse_FlagOverridesEnv(t *testing.T) {
t.Setenv("DASHW_LISTEN", ":9090")

cfg := Parse([]string{"--listen", ":4000"})

if cfg.Listen != ":4000" {
t.Errorf("Listen = %q, want %q (flag should override env)", cfg.Listen, ":4000")
}
}

func TestParse_AuthFromEnvOnly(t *testing.T) {
t.Setenv("DASHD_AUTH_TOKEN", "secret-token-123")
t.Setenv("DASHD_TLS_CERT", "/path/to/cert.pem")
t.Setenv("DASHD_TLS_KEY", "/path/to/key.pem")
t.Setenv("DASHD_TLS_CA", "/path/to/ca.pem")

cfg := Parse([]string{})

if cfg.DashdAuthToken != "secret-token-123" {
t.Errorf("DashdAuthToken = %q, want %q", cfg.DashdAuthToken, "secret-token-123")
}
if cfg.DashdTLSCert != "/path/to/cert.pem" {
t.Errorf("DashdTLSCert = %q, want %q", cfg.DashdTLSCert, "/path/to/cert.pem")
}
if cfg.DashdTLSKey != "/path/to/key.pem" {
t.Errorf("DashdTLSKey = %q, want %q", cfg.DashdTLSKey, "/path/to/key.pem")
}
if cfg.DashdTLSCA != "/path/to/ca.pem" {
t.Errorf("DashdTLSCA = %q, want %q", cfg.DashdTLSCA, "/path/to/ca.pem")
}
}

func TestParseLevel(t *testing.T) {
tests := []struct {
input string
want  slog.Level
}{
{"debug", slog.LevelDebug},
{"info", slog.LevelInfo},
{"warn", slog.LevelWarn},
{"warning", slog.LevelWarn},
{"error", slog.LevelError},
{"", slog.LevelInfo},
{"unknown", slog.LevelInfo},
{"DEBUG", slog.LevelInfo}, // case-sensitive: uppercase not recognized → default
}

for _, tt := range tests {
t.Run(tt.input, func(t *testing.T) {
got := parseLevel(tt.input)
if got != tt.want {
t.Errorf("parseLevel(%q) = %v, want %v", tt.input, got, tt.want)
}
})
}
}

func TestEnv(t *testing.T) {
// Unset to test default
os.Unsetenv("TEST_DASHW_ABSENT")
got := env("TEST_DASHW_ABSENT", "fallback")
if got != "fallback" {
t.Errorf("env(absent) = %q, want %q", got, "fallback")
}

// Set to test override
t.Setenv("TEST_DASHW_PRESENT", "override")
got = env("TEST_DASHW_PRESENT", "fallback")
if got != "override" {
t.Errorf("env(present) = %q, want %q", got, "override")
}
}

func TestEnvBool(t *testing.T) {
tests := []struct {
envVal  string
set     bool
def     bool
want    bool
}{
{"", false, false, false},    // not set, default false
{"", false, true, true},      // not set, default true
{"true", true, false, true},
{"1", true, false, true},
{"yes", true, false, true},
{"false", true, false, false},
{"0", true, false, false},
{"no", true, false, false},
{"anything", true, false, false},
}

for i, tt := range tests {
key := "TEST_DASHW_BOOL"
if tt.set {
os.Setenv(key, tt.envVal)
} else {
os.Unsetenv(key)
}
got := envBool(key, tt.def)
if got != tt.want {
t.Errorf("case %d: envBool(%q, %v) = %v, want %v (envVal=%q, set=%v)",
i, key, tt.def, got, tt.want, tt.envVal, tt.set)
}
}
}