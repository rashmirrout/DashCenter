// Package config provides dashctl's kubeconfig-equivalent configuration:
// persistent contexts, per-context auth/transport/TLS, and a deterministic
// resolver that merges flags, env vars, context, and defaults.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Default kind/apiVersion strings for the config file.
const (
	APIVersion = "dashctl/v1"
	Kind       = "Config"
)

// Built-in defaults (also documented in specs/LLD/dashctl-lld.md §4.4).
const (
	DefaultEndpointREST  = "http://localhost:8443"
	DefaultEndpointGRPC  = "localhost:9443"
	DefaultAdminEndpoint = "http://localhost:7443"
	DefaultNamespace     = "default"
	DefaultTimeout       = 30 * time.Second
	DefaultPageSize      = 100
	DefaultOutput        = "table"
)

// Transport selects which backend the SDK dials.
type Transport string

const (
	TransportREST Transport = "rest"
	TransportGRPC Transport = "grpc"
)

// Config is the on-disk schema persisted to ~/.config/dashctl/config (or
// %APPDATA%\dashctl\config on Windows).
type Config struct {
	APIVersion     string                  `yaml:"apiVersion"      json:"apiVersion"`
	Kind           string                  `yaml:"kind"            json:"kind"`
	CurrentContext string                  `yaml:"current-context" json:"current-context"`
	Contexts       map[string]ContextEntry `yaml:"contexts"        json:"contexts"`
	Preferences    Preferences             `yaml:"preferences"     json:"preferences"`
}

// ContextEntry is one named cluster/context.
type ContextEntry struct {
	Endpoint      string     `yaml:"endpoint"                  json:"endpoint"`
	AdminEndpoint string     `yaml:"admin-endpoint,omitempty"  json:"admin-endpoint,omitempty"`
	Transport     Transport  `yaml:"transport"                 json:"transport"`
	Namespace     string     `yaml:"namespace"                 json:"namespace"`
	Auth          AuthConfig `yaml:"auth,omitempty"            json:"auth,omitempty"`
	TLS           TLSConfig  `yaml:"tls,omitempty"             json:"tls,omitempty"`
	Timeout       string     `yaml:"timeout,omitempty"         json:"timeout,omitempty"` // duration string; "" → default
}

// AuthConfig — see specs/LLD/dashctl-lld.md §11.
type AuthConfig struct {
	Mode      string `yaml:"mode,omitempty"        json:"mode,omitempty"`       // "none" | "token" | "mtls"
	Token     string `yaml:"token,omitempty"       json:"token,omitempty"`      // discouraged; use TokenEnv
	TokenEnv  string `yaml:"token-env,omitempty"   json:"token-env,omitempty"`
	TokenFile string `yaml:"token-file,omitempty"  json:"token-file,omitempty"`
}

// TLSConfig holds optional TLS material.
type TLSConfig struct {
	CAFile             string `yaml:"ca-file,omitempty"               json:"ca-file,omitempty"`
	CertFile           string `yaml:"cert-file,omitempty"             json:"cert-file,omitempty"`
	KeyFile            string `yaml:"key-file,omitempty"              json:"key-file,omitempty"`
	Insecure           bool   `yaml:"insecure,omitempty"              json:"insecure,omitempty"`
	InsecureSkipVerify bool   `yaml:"insecure-skip-verify,omitempty" json:"insecure-skip-verify,omitempty"`
}

// Preferences are user-level defaults; lower priority than per-context.
type Preferences struct {
	Output   string `yaml:"output,omitempty"     json:"output,omitempty"`
	Color    string `yaml:"color,omitempty"      json:"color,omitempty"` // "auto" | "always" | "never"
	PageSize int    `yaml:"page-size,omitempty" json:"page-size,omitempty"`
}

// ResolvedConfig is the merged-and-validated view returned to commands.
type ResolvedConfig struct {
	Endpoint      string
	AdminEndpoint string
	Transport     Transport
	Namespace     string
	Timeout       time.Duration
	Token         string
	TLS           TLSConfig
	Output        string
	Color         string
	PageSize      int
	ContextName   string
}

// New returns an empty but valid Config.
func New() *Config {
	return &Config{
		APIVersion: APIVersion,
		Kind:       Kind,
		Contexts:   map[string]ContextEntry{},
	}
}

// DefaultPath returns the platform-appropriate config file location.
func DefaultPath() string {
	if runtime.GOOS == "windows" {
		if dir := os.Getenv("APPDATA"); dir != "" {
			return filepath.Join(dir, "dashctl", "config")
		}
		if home, _ := os.UserHomeDir(); home != "" {
			return filepath.Join(home, "AppData", "Roaming", "dashctl", "config")
		}
	}
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "dashctl", "config")
	}
	if home, _ := os.UserHomeDir(); home != "" {
		return filepath.Join(home, ".config", "dashctl", "config")
	}
	return "dashctl-config"
}

// Load reads cfg from path. If path is empty, [DefaultPath] is used. A
// missing file is NOT an error — an empty Config is returned so first-time
// users can still drive a localhost dashd without any setup.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return New(), nil
		}
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	cfg := New()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]ContextEntry{}
	}
	return cfg, nil
}

// Save persists cfg atomically to path (tmp + rename).
func Save(cfg *Config, path string) error {
	if path == "" {
		path = DefaultPath()
	}
	if cfg == nil {
		return fmt.Errorf("config: nil cfg")
	}
	cfg.APIVersion = APIVersion
	cfg.Kind = Kind
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]ContextEntry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("config: mkdir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("config: marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("config: write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: rename: %w", err)
	}
	return nil
}

// Flags holds explicit per-invocation overrides (typically populated from
// Cobra persistent flags). Empty string / zero means "not set".
type Flags struct {
	Context       string
	Endpoint      string
	AdminEndpoint string
	Transport     string
	Namespace     string
	Output        string
	Color         string
	Timeout       string
	Token         string
	CAFile        string
	CertFile      string
	KeyFile       string
	Insecure      bool   // explicit user request
	InsecureSet   bool   // distinguishes "false-by-default" from "user set false"
	SkipTLSVerify bool
	SkipTLSSet    bool
}

// Env captures the environment variables dashctl honors.
type Env struct {
	Context       string // DASHCTL_CONTEXT
	Endpoint      string // DASHCTL_ENDPOINT
	AdminEndpoint string // DASHCTL_ADMIN_ENDPOINT
	Transport     string // DASHCTL_TRANSPORT
	Namespace     string // DASHCTL_NAMESPACE
	Output        string // DASHCTL_OUTPUT
	Timeout       string // DASHCTL_TIMEOUT
	Token         string // DASHCTL_TOKEN
	Insecure      bool   // DASHCTL_INSECURE (truthy: 1/true/yes)
	NoColor       bool   // NO_COLOR
}

// ReadEnv populates Env from os.Getenv.
func ReadEnv() Env {
	return Env{
		Context:       os.Getenv("DASHCTL_CONTEXT"),
		Endpoint:      os.Getenv("DASHCTL_ENDPOINT"),
		AdminEndpoint: os.Getenv("DASHCTL_ADMIN_ENDPOINT"),
		Transport:     os.Getenv("DASHCTL_TRANSPORT"),
		Namespace:     os.Getenv("DASHCTL_NAMESPACE"),
		Output:        os.Getenv("DASHCTL_OUTPUT"),
		Timeout:        os.Getenv("DASHCTL_TIMEOUT"),
		Token:         os.Getenv("DASHCTL_TOKEN"),
		Insecure:      truthyEnv(os.Getenv("DASHCTL_INSECURE")),
		NoColor:       os.Getenv("NO_COLOR") != "",
	}
}

// truthyEnv recognises 1, true, yes (case-insensitive) as true.
func truthyEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "y", "on":
		return true
	}
	return false
}

// Resolve merges flags, env, the active context, and built-in defaults
// per the precedence rules in specs/LLD/dashctl-lld.md §4.3.
//
// Resolution rules per field:
//  1. Explicit flag wins.
//  2. Env var.
//  3. Active context.
//  4. Built-in default.
//
// An error is returned only if explicit flag/env names a context that
// does not exist; missing config + no flags is fine (everything defaults).
func (c *Config) Resolve(flags Flags, env Env) (*ResolvedConfig, error) {
	ctxName := firstNonEmpty(flags.Context, env.Context, c.CurrentContext)
	var ctx ContextEntry
	if ctxName != "" {
		ce, ok := c.Contexts[ctxName]
		if !ok {
			// Only error if the name came from explicit flag or env.
			if flags.Context != "" || env.Context != "" {
				return nil, fmt.Errorf("config: context %q not found", ctxName)
			}
			// current-context names a missing entry: treat as empty.
			ctxName = ""
		} else {
			ctx = ce
		}
	}

	rc := &ResolvedConfig{ContextName: ctxName}

	// Endpoint.
	rc.Endpoint = firstNonEmpty(flags.Endpoint, env.Endpoint, ctx.Endpoint)
	// Admin endpoint: explicit, then env, then ctx, then derive from main.
	rc.AdminEndpoint = firstNonEmpty(flags.AdminEndpoint, env.AdminEndpoint, ctx.AdminEndpoint)
	if rc.AdminEndpoint == "" && rc.Endpoint != "" {
		rc.AdminEndpoint = deriveAdminEndpoint(rc.Endpoint)
	}

	// Transport.
	tr := firstNonEmpty(flags.Transport, env.Transport, string(ctx.Transport))
	switch Transport(tr) {
	case TransportREST, TransportGRPC:
		rc.Transport = Transport(tr)
	case "":
		rc.Transport = TransportREST // Phase 1 default
	default:
		return nil, fmt.Errorf("config: unknown transport %q (want rest|grpc)", tr)
	}

	// Defaults that depend on transport.
	if rc.Endpoint == "" {
		if rc.Transport == TransportGRPC {
			rc.Endpoint = DefaultEndpointGRPC
		} else {
			rc.Endpoint = DefaultEndpointREST
		}
	}
	if rc.AdminEndpoint == "" {
		rc.AdminEndpoint = DefaultAdminEndpoint
	}

	// Namespace.
	rc.Namespace = firstNonEmpty(flags.Namespace, env.Namespace, ctx.Namespace, DefaultNamespace)

	// Timeout.
	tout := firstNonEmpty(flags.Timeout, env.Timeout, ctx.Timeout)
	if tout == "" {
		rc.Timeout = DefaultTimeout
	} else {
		d, err := time.ParseDuration(tout)
		if err != nil {
			return nil, fmt.Errorf("config: invalid timeout %q: %w", tout, err)
		}
		if d < 0 {
			return nil, fmt.Errorf("config: timeout must be >= 0")
		}
		rc.Timeout = d
	}

	// Output.
	rc.Output = firstNonEmpty(flags.Output, env.Output, c.Preferences.Output)
	// (caller may still default this to table/json based on TTY.)

	// Color.
	rc.Color = firstNonEmpty(flags.Color, c.Preferences.Color)
	if rc.Color == "" {
		rc.Color = "auto"
	}
	if env.NoColor {
		rc.Color = "never"
	}

	// Page size.
	rc.PageSize = c.Preferences.PageSize
	if rc.PageSize <= 0 {
		rc.PageSize = DefaultPageSize
	}

	// Token resolution (flag > env > context TokenEnv > context TokenFile > context Token literal).
	tok, err := resolveToken(flags.Token, env.Token, ctx.Auth)
	if err != nil {
		return nil, err
	}
	rc.Token = tok

	// TLS material.
	rc.TLS = ctx.TLS
	if flags.CAFile != "" {
		rc.TLS.CAFile = flags.CAFile
	}
	if flags.CertFile != "" {
		rc.TLS.CertFile = flags.CertFile
	}
	if flags.KeyFile != "" {
		rc.TLS.KeyFile = flags.KeyFile
	}
	if flags.InsecureSet {
		rc.TLS.Insecure = flags.Insecure
	} else if env.Insecure {
		// Env-supplied DASHCTL_INSECURE=true is honoured when no explicit
		// flag was set, so containers and CI can opt-in without injecting
		// --insecure on every invocation.
		rc.TLS.Insecure = true
	}
	if flags.SkipTLSSet {
		rc.TLS.InsecureSkipVerify = flags.SkipTLSVerify
	}
	// Safety: plaintext non-localhost is refused unless user opted in.
	if err := enforceInsecureSafety(rc); err != nil {
		return nil, err
	}

	return rc, nil
}

func resolveToken(flagToken, envToken string, auth AuthConfig) (string, error) {
	if flagToken != "" {
		return flagToken, nil
	}
	if envToken != "" {
		return envToken, nil
	}
	if auth.TokenEnv != "" {
		if v := os.Getenv(auth.TokenEnv); v != "" {
			return v, nil
		}
	}
	if auth.TokenFile != "" {
		data, err := os.ReadFile(auth.TokenFile)
		if err != nil {
			return "", fmt.Errorf("config: token file %s: %w", auth.TokenFile, err)
		}
		return strings.TrimRight(strings.TrimRight(string(data), "\n"), "\r"), nil
	}
	if auth.Token != "" {
		return auth.Token, nil
	}
	return "", nil
}

// enforceInsecureSafety refuses plaintext HTTP / gRPC to non-localhost
// endpoints unless the user explicitly set --insecure.
func enforceInsecureSafety(rc *ResolvedConfig) error {
	endpoint := rc.Endpoint
	if strings.HasPrefix(endpoint, "https://") {
		return nil
	}
	if rc.Transport == TransportGRPC {
		// gRPC without TLS material → "plaintext"; safe only if local.
		hasTLS := rc.TLS.CAFile != "" || rc.TLS.CertFile != ""
		if !hasTLS && !rc.TLS.Insecure && !isLocalEndpoint(endpoint) {
			return fmt.Errorf("config: plaintext gRPC to %q refused; pass --insecure or provide TLS material", endpoint)
		}
		return nil
	}
	// REST.
	if strings.HasPrefix(endpoint, "http://") && !isLocalEndpoint(endpoint) && !rc.TLS.Insecure {
		return fmt.Errorf("config: plaintext HTTP to %q refused; pass --insecure or use https://", endpoint)
	}
	return nil
}

func isLocalEndpoint(endpoint string) bool {
	hp := stripScheme(endpoint)
	host := hp
	// Bracketed IPv6: [host]:port or [host]
	if strings.HasPrefix(hp, "[") {
		if i := strings.Index(hp, "]"); i > 0 {
			host = hp[1:i]
		}
	} else if i := strings.LastIndex(hp, ":"); i >= 0 {
		// Heuristic: only treat as host:port if the FIRST half contains
		// no further colons — otherwise this is a bare IPv6 address.
		if !strings.Contains(hp[:i], ":") {
			host = hp[:i]
		}
	}
	host = strings.TrimSuffix(host, ".")
	switch strings.ToLower(host) {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func stripScheme(s string) string {
	if i := strings.Index(s, "://"); i >= 0 {
		return s[i+3:]
	}
	return s
}

// deriveAdminEndpoint converts a REST endpoint (e.g. http://h:8443) into a
// best-guess admin endpoint (http://h:7443). Returns "" if the input is
// not a parsable host:port URL.
func deriveAdminEndpoint(restEndpoint string) string {
	if restEndpoint == "" {
		return ""
	}
	scheme := "http"
	rest := restEndpoint
	if strings.HasPrefix(restEndpoint, "https://") {
		scheme = "https"
		rest = strings.TrimPrefix(restEndpoint, "https://")
	} else if strings.HasPrefix(restEndpoint, "http://") {
		rest = strings.TrimPrefix(restEndpoint, "http://")
	}
	host, port := rest, ""
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		host = rest[:i]
		port = rest[i+1:]
	}
	if port == "8443" {
		return scheme + "://" + host + ":7443"
	}
	if port == "" {
		return scheme + "://" + host + ":7443"
	}
	// Fall back: no derivation; let caller use DefaultAdminEndpoint.
	return ""
}

// SetCurrentContext mutates the config in place; returns error if name is
// empty or unknown.
func (c *Config) SetCurrentContext(name string) error {
	if name == "" {
		return fmt.Errorf("context name must not be empty")
	}
	if _, ok := c.Contexts[name]; !ok {
		return fmt.Errorf("context %q does not exist", name)
	}
	c.CurrentContext = name
	return nil
}

// PutContext inserts or replaces ctx by name.
func (c *Config) PutContext(name string, ctx ContextEntry) error {
	if name == "" {
		return fmt.Errorf("context name must not be empty")
	}
	if c.Contexts == nil {
		c.Contexts = map[string]ContextEntry{}
	}
	c.Contexts[name] = ctx
	if c.CurrentContext == "" {
		c.CurrentContext = name
	}
	return nil
}

// DeleteContext removes by name; clears current-context if it pointed to it.
func (c *Config) DeleteContext(name string) error {
	if _, ok := c.Contexts[name]; !ok {
		return fmt.Errorf("context %q does not exist", name)
	}
	delete(c.Contexts, name)
	if c.CurrentContext == name {
		c.CurrentContext = ""
	}
	return nil
}

// RenameContext renames old → new; new must be free.
func (c *Config) RenameContext(old, new string) error {
	if old == new {
		return nil
	}
	ce, ok := c.Contexts[old]
	if !ok {
		return fmt.Errorf("context %q does not exist", old)
	}
	if _, exists := c.Contexts[new]; exists {
		return fmt.Errorf("context %q already exists", new)
	}
	delete(c.Contexts, old)
	c.Contexts[new] = ce
	if c.CurrentContext == old {
		c.CurrentContext = new
	}
	return nil
}

// ContextNames returns the sorted list of context names.
func (c *Config) ContextNames() []string {
	out := make([]string, 0, len(c.Contexts))
	for k := range c.Contexts {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// Minimal stable string sort (avoids importing "sort" for one call).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
