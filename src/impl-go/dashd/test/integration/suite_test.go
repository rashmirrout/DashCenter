//go:build integration

// Package integration runs end-to-end scenarios against a freshly
// built dashd + dash-sim. Build with:
//
//   go test -tags=integration -timeout 5m ./test/integration/...
//
// Each test brings up its own (dashd, dash-sim) pair on dynamically
// chosen ports and tears them down. The harness is intentionally chatty
// (writes child stdout/stderr to a per-test log file in t.TempDir())
// so developers can copy-paste failing scenarios and re-run them
// manually with the same flags.
//
// Build dependencies: the tests rely on `go run ./...` for both dashd
// and dash-sim, which means a Go toolchain must be on PATH. No Docker.
package integration

import (
"bytes"
"encoding/json"
"errors"
"fmt"
"io"
"net"
"net/http"
"os"
"os/exec"
"path/filepath"
"runtime"
"strings"
"sync"
"testing"
"time"
)

// --- Path resolution: tests live deep in src/impl-go/dashd/test/integration ---

// repoRoot returns the absolute path to the workspace root, derived
// from the current source file location.
func repoRoot(t *testing.T) string {
t.Helper()
_, file, _, ok := callerInfo()
if !ok {
t.Fatal("could not resolve test source file location")
}
// suite_test.go → up six dirs → workspace root.
//   integration → test → dashd → impl-go → src → root
return filepath.Clean(filepath.Join(filepath.Dir(file),
"..", "..", "..", "..", ".."))
}

func callerInfo() (uintptr, string, int, bool) {
return runtimeCaller()
}

func runtimeCaller() (uintptr, string, int, bool) {
return runtime.Caller(2)
}

// --- Free port allocation ---

func freePort(t *testing.T) int {
t.Helper()
lis, err := net.Listen("tcp", "127.0.0.1:0")
if err != nil {
t.Fatalf("freePort: %v", err)
}
defer lis.Close()
return lis.Addr().(*net.TCPAddr).Port
}

// --- Harness ---

type harness struct {
t          *testing.T
root       string
restURL    string
grpcAddr   string
adminURL   string
simAddr    string
simAdmin   string
dpuID      string
stateDir   string
configPath string

dashd     *exec.Cmd
sim       *exec.Cmd
cleanupFn []func()
}

// startHarness builds a dashd + dash-sim pair, waits for both to be
// healthy, registers the DPU into dashd's inventory, and returns the
// fixture. t.Cleanup tears everything down.
func startHarness(t *testing.T) *harness {
t.Helper()
root := repoRoot(t)

restPort := freePort(t)
grpcPort := freePort(t)
adminPort := freePort(t)
simGrpcPort := freePort(t)
simAdminPort := freePort(t)

h := &harness{
t:        t,
root:     root,
restURL:  fmt.Sprintf("http://127.0.0.1:%d", restPort),
grpcAddr: fmt.Sprintf("127.0.0.1:%d", grpcPort),
adminURL: fmt.Sprintf("http://127.0.0.1:%d", adminPort),
simAddr:  fmt.Sprintf("127.0.0.1:%d", simGrpcPort),
simAdmin: fmt.Sprintf("http://127.0.0.1:%d", simAdminPort),
dpuID:    "dpu-int",
stateDir: filepath.Join(t.TempDir(), "state"),
}

if err := os.MkdirAll(h.stateDir, 0o755); err != nil {
t.Fatalf("mkdir state: %v", err)
}

h.writeConfig(t, restPort, grpcPort, adminPort)
h.startSim(t)
h.startDashd(t)

if err := waitHTTP(h.adminURL+"/admin/health", 30*time.Second); err != nil {
t.Fatalf("dashd admin not healthy: %v", err)
}
if err := waitTCP(h.simAddr, 30*time.Second); err != nil {
t.Fatalf("dash-sim grpc not accepting: %v", err)
}

// Register the DPU through dashd's REST API.
h.putInventory(t)

t.Cleanup(h.shutdown)
return h
}

func (h *harness) writeConfig(t *testing.T, rest, grpc, admin int) {
t.Helper()
cfg := fmt.Sprintf(`
listen:
  rest_addr: "127.0.0.1:%d"
  grpc_addr: "127.0.0.1:%d"
  admin_addr: "127.0.0.1:%d"
log:
  level: "info"
  format: "text"
storage:
  file:
    state_dir: "%s"
reconcile:
  tick_interval: 5s
  per_dpu_inbox_size: 1
  apply_rate_limit: 200
  error_budget_per_min: 10
inventory:
  source: "static"
`, rest, grpc, admin, escapeYaml(h.stateDir))

path := filepath.Join(t.TempDir(), "dashd.yaml")
if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
t.Fatalf("write config: %v", err)
}
h.configPath = path
}

// escapeYaml turns backslashes into forward slashes for YAML compat
// (Windows paths contain backslashes that YAML otherwise treats as
// escape sequences).
func escapeYaml(s string) string {
return strings.ReplaceAll(s, `\`, `/`)
}

func (h *harness) startSim(t *testing.T) {
t.Helper()
logF := openLog(t, "dash-sim.log")
h.cleanupFn = append(h.cleanupFn, func() { _ = logF.Close() })

cmd := exec.Command("go", "run", "./cmd/dash-sim",
"--grpc-listen", h.simAddr,
"--admin-listen", strings.TrimPrefix(h.simAdmin, "http://"),
"--device-id", h.dpuID,
)
cmd.Dir = filepath.Join(h.root, "src", "impl-go", "dash-sim")
cmd.Stdout = logF
cmd.Stderr = logF

if err := cmd.Start(); err != nil {
t.Fatalf("start dash-sim: %v", err)
}
h.sim = cmd
}

func (h *harness) startDashd(t *testing.T) {
t.Helper()
logF := openLog(t, "dashd.log")
h.cleanupFn = append(h.cleanupFn, func() { _ = logF.Close() })

cmd := exec.Command("go", "run", "./cmd/dashd",
"--config", h.configPath,
)
cmd.Dir = filepath.Join(h.root, "src", "impl-go", "dashd")
cmd.Stdout = logF
cmd.Stderr = logF

if err := cmd.Start(); err != nil {
t.Fatalf("start dashd: %v", err)
}
h.dashd = cmd
}

func (h *harness) shutdown() {
killProc(h.dashd)
killProc(h.sim)
for _, fn := range h.cleanupFn {
fn()
}
}

func killProc(c *exec.Cmd) {
if c == nil || c.Process == nil {
return
}
_ = c.Process.Kill()
// Reap; ignore errors (exit status from kill is expected).
_ = c.Wait()
}

func openLog(t *testing.T, name string) *os.File {
t.Helper()
p := filepath.Join(t.TempDir(), name)
f, err := os.Create(p)
if err != nil {
t.Fatalf("open log %s: %v", p, err)
}
t.Logf("integration log: %s", p)
return f
}

// --- HTTP helpers ---

func httpDo(t *testing.T, method, url, body string) (int, []byte) {
t.Helper()
var rdr io.Reader
if body != "" {
rdr = bytes.NewBufferString(body)
}
req, err := http.NewRequest(method, url, rdr)
if err != nil {
t.Fatalf("%s %s: build req: %v", method, url, err)
}
req.Header.Set("Content-Type", "application/json")

client := &http.Client{Timeout: 10 * time.Second}
resp, err := client.Do(req)
if err != nil {
t.Fatalf("%s %s: %v", method, url, err)
}
defer resp.Body.Close()
out, _ := io.ReadAll(resp.Body)
return resp.StatusCode, out
}

func (h *harness) putInventory(t *testing.T) {
t.Helper()
body := fmt.Sprintf(`{"dpus":[{"id":%q,"endpoint":%q}]}`, h.dpuID, h.simAddr)
code, body2 := httpDo(t, "PUT", h.restURL+"/v1/inventory", body)
if code >= 400 {
t.Fatalf("PUT /v1/inventory failed: %d %s", code, body2)
}
}

// --- Convergence helpers ---

// waitConverged polls /admin/drift?dpu= until items is empty, returns
// the elapsed duration or fails.
func (h *harness) waitConverged(t *testing.T, timeout time.Duration) {
t.Helper()
deadline := time.Now().Add(timeout)
for time.Now().Before(deadline) {
code, body := httpDo(t, "GET", h.adminURL+"/admin/drift?dpu="+h.dpuID, "")
if code != 200 {
time.Sleep(100 * time.Millisecond)
continue
}
var out struct {
Items []any `json:"items"`
}
if err := json.Unmarshal(body, &out); err == nil && len(out.Items) == 0 {
return
}
time.Sleep(100 * time.Millisecond)
}
// One more time: dump the current drift for debugging.
_, body := httpDo(t, "GET", h.adminURL+"/admin/drift?dpu="+h.dpuID, "")
t.Fatalf("did not converge within %v; drift=%s", timeout, body)
}

// --- Wait helpers ---

// waitHTTP polls url with GET until it returns 2xx or 5xx (anything
// that isn't a network error → server is up). 1s tick.
func waitHTTP(url string, timeout time.Duration) error {
deadline := time.Now().Add(timeout)
for time.Now().Before(deadline) {
resp, err := http.Get(url)
if err == nil {
_ = resp.Body.Close()
return nil
}
time.Sleep(200 * time.Millisecond)
}
return fmt.Errorf("waitHTTP %s: timeout after %v", url, timeout)
}

// waitTCP polls a tcp dial until it connects.
func waitTCP(addr string, timeout time.Duration) error {
deadline := time.Now().Add(timeout)
for time.Now().Before(deadline) {
c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
if err == nil {
_ = c.Close()
return nil
}
time.Sleep(200 * time.Millisecond)
}
return errors.New("waitTCP: timeout")
}

// --- toolchain probe ---

// TestMain ensures the Go toolchain is reachable. Without it every
// scenario would fail on `go run`, which is hard to diagnose.
var _ = sync.Once{} // reserved for future shared setup

func skipIfNoToolchain(t *testing.T) {
t.Helper()
if _, err := exec.LookPath("go"); err != nil {
t.Skip("go toolchain not on PATH; skipping integration suite")
}
}