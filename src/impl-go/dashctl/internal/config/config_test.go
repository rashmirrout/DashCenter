package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewIsValid(t *testing.T) {
	c := New()
	if c.APIVersion != APIVersion || c.Kind != Kind || c.Contexts == nil {
		t.Fatal("New() should be populated and valid")
	}
}

func TestDefaultPath(t *testing.T) {
	p := DefaultPath()
	if p == "" {
		t.Fatal("path must not be empty")
	}
}

func TestLoadMissingFileReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(filepath.Join(dir, "nope"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Contexts) != 0 {
		t.Fatal("missing file → empty contexts")
	}
}

func TestLoadParsesYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	body := `apiVersion: dashctl/v1
kind: Config
current-context: dev
contexts:
  dev:
    endpoint: http://localhost:8443
    transport: rest
    namespace: ns-a
  prod:
    endpoint: https://api.example.com:8443
    transport: rest
    namespace: prod
preferences:
  output: yaml
  page-size: 50
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CurrentContext != "dev" || len(cfg.Contexts) != 2 {
		t.Fatalf("unexpected: %+v", cfg)
	}
	if cfg.Preferences.Output != "yaml" || cfg.Preferences.PageSize != 50 {
		t.Fatalf("prefs wrong: %+v", cfg.Preferences)
	}
}

func TestLoadInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad")
	_ = os.WriteFile(path, []byte("not: : valid:"), 0o600)
	if _, err := Load(path); err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadReadError(t *testing.T) {
	// Pass a directory as path to provoke a non-IsNotExist read error.
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("expected error on dir-as-file")
	}
}

func TestSaveAtomicAndReadBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "config")
	cfg := New()
	if err := cfg.PutContext("dev", ContextEntry{Endpoint: "http://localhost:8443", Transport: TransportREST}); err != nil {
		t.Fatal(err)
	}
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentContext != "dev" {
		t.Fatal("PutContext should set current-context on first add")
	}
}

func TestSaveNilCfgFails(t *testing.T) {
	if err := Save(nil, filepath.Join(t.TempDir(), "x")); err == nil {
		t.Fatal("nil cfg")
	}
}

func TestPutContextEmptyName(t *testing.T) {
	c := New()
	if err := c.PutContext("", ContextEntry{}); err == nil {
		t.Fatal()
	}
}

func TestDeleteContext(t *testing.T) {
	c := New()
	_ = c.PutContext("a", ContextEntry{})
	if err := c.DeleteContext("missing"); err == nil {
		t.Fatal("missing → err")
	}
	if err := c.DeleteContext("a"); err != nil {
		t.Fatal(err)
	}
	if c.CurrentContext != "" {
		t.Fatal("current-context should clear")
	}
}

func TestRenameContext(t *testing.T) {
	c := New()
	_ = c.PutContext("a", ContextEntry{Endpoint: "x"})
	if err := c.RenameContext("a", "a"); err != nil {
		t.Fatal("self-rename no-op")
	}
	if err := c.RenameContext("missing", "x"); err == nil {
		t.Fatal()
	}
	_ = c.PutContext("b", ContextEntry{})
	if err := c.RenameContext("a", "b"); err == nil {
		t.Fatal("target exists → err")
	}
	if err := c.RenameContext("a", "c"); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Contexts["c"]; !ok {
		t.Fatal("renamed gone")
	}
	if c.CurrentContext != "c" {
		t.Fatal("current-context should follow")
	}
}

func TestSetCurrentContext(t *testing.T) {
	c := New()
	if err := c.SetCurrentContext(""); err == nil {
		t.Fatal("empty")
	}
	if err := c.SetCurrentContext("missing"); err == nil {
		t.Fatal("missing")
	}
	_ = c.PutContext("a", ContextEntry{})
	if err := c.SetCurrentContext("a"); err != nil {
		t.Fatal(err)
	}
}

func TestResolveDefaults(t *testing.T) {
	c := New()
	rc, err := c.Resolve(Flags{}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.Endpoint != DefaultEndpointREST || rc.Transport != TransportREST {
		t.Fatalf("default transport/endpoint wrong: %+v", rc)
	}
	if rc.Namespace != DefaultNamespace || rc.Timeout != DefaultTimeout {
		t.Fatalf("default ns/timeout wrong: %+v", rc)
	}
	if rc.AdminEndpoint == "" {
		t.Fatal("admin endpoint defaulted")
	}
	if rc.Color != "auto" {
		t.Fatal("color default")
	}
	if rc.PageSize != DefaultPageSize {
		t.Fatal("page size default")
	}
}

func TestResolveContextSelection(t *testing.T) {
	c := New()
	_ = c.PutContext("dev", ContextEntry{Endpoint: "http://localhost:8443", Namespace: "ns-d", Transport: TransportREST})
	_ = c.PutContext("prod", ContextEntry{Endpoint: "https://api:8443", Namespace: "ns-p", Transport: TransportREST})
	c.CurrentContext = "dev"

	// Flag wins over env wins over current.
	rc, err := c.Resolve(Flags{Context: "prod"}, Env{Context: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	if rc.ContextName != "prod" {
		t.Fatalf("flag should win: %s", rc.ContextName)
	}
	rc, err = c.Resolve(Flags{}, Env{Context: "prod"})
	if err != nil {
		t.Fatal(err)
	}
	if rc.ContextName != "prod" {
		t.Fatal("env should win over current")
	}
	rc, err = c.Resolve(Flags{}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.ContextName != "dev" {
		t.Fatal("current should be used")
	}
}

func TestResolveExplicitMissingContextErrors(t *testing.T) {
	c := New()
	if _, err := c.Resolve(Flags{Context: "missing"}, Env{}); err == nil {
		t.Fatal("explicit missing context must error")
	}
	if _, err := c.Resolve(Flags{}, Env{Context: "missing"}); err == nil {
		t.Fatal("env-named missing context must error")
	}
}

func TestResolveDanglingCurrentContextTolerated(t *testing.T) {
	c := New()
	c.CurrentContext = "ghost" // not in map
	rc, err := c.Resolve(Flags{}, Env{})
	if err != nil {
		t.Fatal("dangling current-context must not error")
	}
	if rc.ContextName != "" {
		t.Fatal("ContextName should be cleared")
	}
}

func TestResolveTransportValidation(t *testing.T) {
	c := New()
	if _, err := c.Resolve(Flags{Transport: "thrift"}, Env{}); err == nil {
		t.Fatal("invalid transport must error")
	}
}

func TestResolveTimeoutValidation(t *testing.T) {
	c := New()
	if _, err := c.Resolve(Flags{Timeout: "nope"}, Env{}); err == nil {
		t.Fatal("bad duration must error")
	}
	if _, err := c.Resolve(Flags{Timeout: "-1s"}, Env{}); err == nil {
		t.Fatal("negative must error")
	}
	rc, err := c.Resolve(Flags{Timeout: "5s"}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.Timeout != 5*time.Second {
		t.Fatal()
	}
}

func TestResolveGRPCDefaults(t *testing.T) {
	c := New()
	rc, err := c.Resolve(Flags{Transport: "grpc"}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.Endpoint != DefaultEndpointGRPC {
		t.Fatalf("gRPC default endpoint wrong: %s", rc.Endpoint)
	}
}

func TestResolveTokenPrecedence(t *testing.T) {
	c := New()
	_ = c.PutContext("dev", ContextEntry{
		Endpoint: "http://localhost:8443",
		Auth:     AuthConfig{TokenEnv: "DASHCTL_TEST_TOKEN_X", Token: "from-yaml-literal"},
	})
	c.CurrentContext = "dev"

	// flag wins
	rc, _ := c.Resolve(Flags{Token: "tok-flag"}, Env{Token: "tok-env"})
	if rc.Token != "tok-flag" {
		t.Fatalf("flag should win: %s", rc.Token)
	}
	// env wins over context
	rc, _ = c.Resolve(Flags{}, Env{Token: "tok-env"})
	if rc.Token != "tok-env" {
		t.Fatal()
	}
	// context TokenEnv wins over literal Token
	t.Setenv("DASHCTL_TEST_TOKEN_X", "tok-from-env-var")
	rc, _ = c.Resolve(Flags{}, Env{})
	if rc.Token != "tok-from-env-var" {
		t.Fatalf("TokenEnv: %s", rc.Token)
	}
	// fallback to literal Token if env var unset
	t.Setenv("DASHCTL_TEST_TOKEN_X", "")
	rc, _ = c.Resolve(Flags{}, Env{})
	if rc.Token != "from-yaml-literal" {
		t.Fatalf("literal token: %s", rc.Token)
	}
}

func TestResolveTokenFile(t *testing.T) {
	dir := t.TempDir()
	tf := filepath.Join(dir, "tok")
	_ = os.WriteFile(tf, []byte("abc123\n"), 0o600)
	c := New()
	_ = c.PutContext("a", ContextEntry{Auth: AuthConfig{TokenFile: tf}})
	c.CurrentContext = "a"
	rc, err := c.Resolve(Flags{}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.Token != "abc123" {
		t.Fatalf("trim wrong: %q", rc.Token)
	}
}

func TestResolveTokenFileMissing(t *testing.T) {
	c := New()
	_ = c.PutContext("a", ContextEntry{Auth: AuthConfig{TokenFile: filepath.Join(t.TempDir(), "missing")}})
	c.CurrentContext = "a"
	if _, err := c.Resolve(Flags{}, Env{}); err == nil {
		t.Fatal("missing token file should error")
	}
}

func TestResolvePlaintextSafety(t *testing.T) {
	c := New()
	// localhost → fine
	rc, err := c.Resolve(Flags{Endpoint: "http://127.0.0.1:8443"}, Env{})
	if err != nil || rc.Endpoint == "" {
		t.Fatal("localhost ok")
	}
	// remote http without --insecure → refused
	if _, err := c.Resolve(Flags{Endpoint: "http://api.example.com:8443"}, Env{}); err == nil {
		t.Fatal("plaintext remote should be refused")
	}
	// remote http WITH --insecure → ok
	if _, err := c.Resolve(Flags{Endpoint: "http://api.example.com:8443", Insecure: true, InsecureSet: true}, Env{}); err != nil {
		t.Fatalf("explicit insecure ok: %v", err)
	}
	// https remote → fine
	if _, err := c.Resolve(Flags{Endpoint: "https://api.example.com:8443"}, Env{}); err != nil {
		t.Fatalf("https ok: %v", err)
	}
	// gRPC localhost → fine
	if _, err := c.Resolve(Flags{Transport: "grpc", Endpoint: "localhost:9443"}, Env{}); err != nil {
		t.Fatalf("local gRPC ok: %v", err)
	}
	// gRPC remote plaintext → refused
	if _, err := c.Resolve(Flags{Transport: "grpc", Endpoint: "api.example.com:9443"}, Env{}); err == nil {
		t.Fatal("remote gRPC plaintext must be refused")
	}
	// gRPC remote with TLS material → ok
	if _, err := c.Resolve(Flags{Transport: "grpc", Endpoint: "api.example.com:9443", CAFile: "/tmp/ca"}, Env{}); err != nil {
		t.Fatalf("gRPC + TLS material ok: %v", err)
	}
}

func TestResolveNoColorEnv(t *testing.T) {
	c := New()
	rc, _ := c.Resolve(Flags{}, Env{NoColor: true})
	if rc.Color != "never" {
		t.Fatal("NO_COLOR overrides")
	}
}

func TestResolveAdminEndpointDerivation(t *testing.T) {
	c := New()
	rc, err := c.Resolve(Flags{Endpoint: "http://127.0.0.1:8443"}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.AdminEndpoint != "http://127.0.0.1:7443" {
		t.Fatalf("admin derivation: %s", rc.AdminEndpoint)
	}
	rc, err = c.Resolve(Flags{Endpoint: "https://host:8443"}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.AdminEndpoint != "https://host:7443" {
		t.Fatalf("https admin derivation: %s", rc.AdminEndpoint)
	}
	// Localhost on a port we don't know how to map: should fall through
	// to DefaultAdminEndpoint.
	rc, err = c.Resolve(Flags{Endpoint: "http://127.0.0.1:9999"}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.AdminEndpoint != DefaultAdminEndpoint {
		t.Fatalf("unmappable port → default, got %s", rc.AdminEndpoint)
	}
	// Explicit admin endpoint takes priority (use --insecure to allow
	// the non-https main endpoint past the safety check).
	rc, err = c.Resolve(Flags{Endpoint: "http://host:8443", AdminEndpoint: "http://other:7777", Insecure: true, InsecureSet: true}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if rc.AdminEndpoint != "http://other:7777" {
		t.Fatal()
	}
}

func TestReadEnv(t *testing.T) {
	t.Setenv("DASHCTL_CONTEXT", "ctx")
	t.Setenv("DASHCTL_TOKEN", "tok")
	t.Setenv("NO_COLOR", "1")
	e := ReadEnv()
	if e.Context != "ctx" || e.Token != "tok" || !e.NoColor {
		t.Fatalf("env not read: %+v", e)
	}
}

func TestContextNamesSorted(t *testing.T) {
	c := New()
	_ = c.PutContext("zeta", ContextEntry{})
	_ = c.PutContext("alpha", ContextEntry{})
	_ = c.PutContext("mu", ContextEntry{})
	names := c.ContextNames()
	if !equalSlice(names, []string{"alpha", "mu", "zeta"}) {
		t.Fatalf("not sorted: %v", names)
	}
}

func TestDeriveAdminEndpointEmpty(t *testing.T) {
	if deriveAdminEndpoint("") != "" {
		t.Fatal("empty → empty")
	}
}

func TestIsLocalEndpoint(t *testing.T) {
	for _, h := range []string{"localhost", "127.0.0.1", "::1", "[::1]", "", "LOCALHOST", "localhost.", "localhost:8443"} {
		if !isLocalEndpoint(h) {
			t.Errorf("expected local: %q", h)
		}
	}
	for _, h := range []string{"api.example.com", "10.0.0.5", "example.org:443"} {
		if isLocalEndpoint(h) {
			t.Errorf("expected remote: %q", h)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "", "x") != "x" {
		t.Fatal()
	}
	if firstNonEmpty() != "" {
		t.Fatal()
	}
}

func TestStripScheme(t *testing.T) {
	cases := map[string]string{
		"http://a":  "a",
		"https://a": "a",
		"a":         "a",
	}
	for in, want := range cases {
		if got := stripScheme(in); got != want {
			t.Errorf("%q → %q (want %q)", in, got, want)
		}
	}
}

func TestSaveCreatesNestedDirs(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	cfg := New()
	if err := Save(cfg, filepath.Join(dir, "config")); err != nil {
		t.Fatal(err)
	}
}

func TestResolvePreferencesPageSizeZero(t *testing.T) {
	c := New()
	c.Preferences.PageSize = -1
	rc, _ := c.Resolve(Flags{}, Env{})
	if rc.PageSize != DefaultPageSize {
		t.Fatalf("page size %d", rc.PageSize)
	}
}

func TestResolveInsecureSkipVerifyFlag(t *testing.T) {
	c := New()
	rc, err := c.Resolve(Flags{SkipTLSVerify: true, SkipTLSSet: true}, Env{})
	if err != nil {
		t.Fatal(err)
	}
	if !rc.TLS.InsecureSkipVerify {
		t.Fatal("flag should set TLS.InsecureSkipVerify")
	}
}

func TestResolveTLSFilesFromFlags(t *testing.T) {
	c := New()
	rc, _ := c.Resolve(Flags{CAFile: "ca", CertFile: "c", KeyFile: "k"}, Env{})
	if rc.TLS.CAFile != "ca" || rc.TLS.CertFile != "c" || rc.TLS.KeyFile != "k" {
		t.Fatal("flags not honoured")
	}
}

// Make sure the YAML round-trip preserves the apiVersion / kind header.
func TestSaveAlwaysStampsHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg")
	cfg := &Config{} // empty (no header)
	if err := Save(cfg, path); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "apiVersion: dashctl/v1") {
		t.Fatal("apiVersion not stamped")
	}
	if !strings.Contains(string(data), "kind: Config") {
		t.Fatal("kind not stamped")
	}
}

func equalSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
