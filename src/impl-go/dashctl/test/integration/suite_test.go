//go:build integration

// Package integration runs end-to-end scenarios that exercise the
// compiled dashctl binary against a freshly built dashd + dash-sim.
//
// Build & run:
//
//	cd src/impl-go/dashctl
//	make test-integration                            (preferred)
//	# or:
//	go test -tags=integration -timeout 5m ./test/integration/...
//
// Each test brings up its own (dashd, dash-sim) pair on dynamically
// chosen ports so the suite is parallel-safe across packages. dashctl
// itself is built ONCE per `go test` invocation via TestMain.
//
// The 13 scenarios mirror deploy/dashctl-fleet/dashctl-e2e.sh:
//
//	1. dashctl version --client                  (Test_VersionClient)
//	2. dashd /admin/health                        (covered by harness boot)
//	3. inventory: DPU UP                          (Test_DpuList)
//	4. dashctl get vnet (empty)                   (Test_GetVnet_Empty)
//	5. dashctl apply -f manifests/                (Test_Apply_RoundTrip)
//	6. dashctl get vnet -o table                  (Test_Get_OutputFormats)
//	7. dashctl get eni -o wide                    (Test_Get_OutputFormats)
//	8. dashctl describe eni                       (Test_Describe)
//	9. dashctl reconcile                          (Test_Reconcile)
//	10. dashctl dpu list                          (Test_DpuList)
//	11. dashctl dpu drift                         (Test_DpuDrift_Converges)
//	12. dashctl delete eni                        (Test_Delete_IdempotentAfter)
//	13. dashctl explain vnet                      (Test_Explain_Offline)
//
// Plus protocol-level scenarios:
//
//	14. dashctl edit (CAS via fake editor)       (covered by unit tests)
//	15. label selector on get                     (Test_Get_LabelSelector)
//	16. dashctl version --client when server unreachable (Test_Version_ServerUnreachable)
package integration

import (
	"bytes"
	"encoding/json"
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

// --- Package-level state ---

var (
	dashctlBin     string
	dashctlBinOnce sync.Once
	dashctlBinErr  error
)

// TestMain builds dashctl once, then runs the suite.
func TestMain(m *testing.M) {
	// Don't actually build here — wait until the first test asks for it
	// (so `go test -list .` doesn't trigger a compile). buildDashctlBin
	// is idempotent via sync.Once.
	os.Exit(m.Run())
}

// --- Path / build helpers ---

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot resolve test source location")
	}
	// suite_test.go → up 5 dirs → repo root.
	//   integration → test → dashctl → impl-go → src → root
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", "..", "..", ".."))
}

// buildDashctlBin compiles `cmd/dashctl` to a temp path (once per test
// run) and returns the absolute path to the binary.
func buildDashctlBin(t *testing.T) string {
	t.Helper()
	dashctlBinOnce.Do(func() {
		root := repoRoot(t)
		out, err := os.MkdirTemp("", "dashctl-it-")
		if err != nil {
			dashctlBinErr = fmt.Errorf("mktemp: %w", err)
			return
		}
		bin := filepath.Join(out, "dashctl")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		cmd := exec.Command("go", "build", "-trimpath", "-o", bin, "./cmd/dashctl")
		cmd.Dir = filepath.Join(root, "src", "impl-go", "dashctl")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		stdout := &bytes.Buffer{}
		cmd.Stdout = stdout
		cmd.Stderr = stdout
		if err := cmd.Run(); err != nil {
			dashctlBinErr = fmt.Errorf("go build: %v\n%s", err, stdout.String())
			return
		}
		dashctlBin = bin
		t.Logf("dashctl binary built at %s", bin)
	})
	if dashctlBinErr != nil {
		t.Fatal(dashctlBinErr)
	}
	return dashctlBin
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
	bin        string // dashctl binary path
	restURL    string
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
// healthy, registers the DPU in dashd's inventory, and returns the
// fixture (with t.Cleanup wired).
func startHarness(t *testing.T) *harness {
	t.Helper()
	root := repoRoot(t)
	bin := buildDashctlBin(t)

	restPort := freePort(t)
	grpcPort := freePort(t)
	adminPort := freePort(t)
	simGrpcPort := freePort(t)
	simAdminPort := freePort(t)

	h := &harness{
		t:        t,
		root:     root,
		bin:      bin,
		restURL:  fmt.Sprintf("http://127.0.0.1:%d", restPort),
		adminURL: fmt.Sprintf("http://127.0.0.1:%d", adminPort),
		simAddr:  fmt.Sprintf("127.0.0.1:%d", simGrpcPort),
		simAdmin: fmt.Sprintf("http://127.0.0.1:%d", simAdminPort),
		dpuID:    "dpu-int",
		stateDir: filepath.Join(t.TempDir(), "state"),
	}
	_ = grpcPort // dashctl Phase 1 does not dial gRPC

	if err := os.MkdirAll(h.stateDir, 0o755); err != nil {
		t.Fatalf("mkdir state: %v", err)
	}
	h.writeConfig(t, restPort, grpcPort, adminPort)
	h.startSim(t)
	h.startDashd(t)

	// Be generous: `go run` cold-compiles on first hit.
	if err := waitHTTP(h.adminURL+"/admin/health", 60*time.Second); err != nil {
		t.Fatalf("dashd admin not healthy: %v", err)
	}
	if err := waitTCP(h.simAddr, 60*time.Second); err != nil {
		t.Fatalf("dash-sim grpc not accepting: %v", err)
	}
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
  source: "api"
`, rest, grpc, admin, escapeYaml(h.stateDir))

	path := filepath.Join(t.TempDir(), "dashd.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	h.configPath = path
}

func escapeYaml(s string) string { return strings.ReplaceAll(s, `\`, `/`) }

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

	cmd := exec.Command("go", "run", "./cmd/dashd", "--config", h.configPath)
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
	// Windows: brief grace period so child handles are released before
	// t.TempDir() cleanup races them.
	time.Sleep(200 * time.Millisecond)
}

func killProc(c *exec.Cmd) {
	if c == nil || c.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", fmt.Sprintf("%d", c.Process.Pid)).Run()
	} else {
		_ = c.Process.Kill()
	}
	_ = c.Wait()
}

func openLog(t *testing.T, name string) *os.File {
	t.Helper()
	// Honour DASHCTL_IT_LOG_DIR for debugging — otherwise use t.TempDir()
	// (which is auto-cleaned even on failure).
	dir := os.Getenv("DASHCTL_IT_LOG_DIR")
	if dir == "" {
		dir = t.TempDir()
	} else {
		dir = filepath.Join(dir, t.Name())
		_ = os.MkdirAll(dir, 0o755)
	}
	p := filepath.Join(dir, name)
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("open log %s: %v", p, err)
	}
	t.Logf("integration log: %s", p)
	return f
}

// --- dashctl runner ---

type cliResult struct {
	Code   int
	Stdout string
	Stderr string
}

// runCtl invokes the compiled dashctl binary with --endpoint /
// --admin-endpoint pointing at this harness's dashd.
func (h *harness) runCtl(t *testing.T, args ...string) cliResult {
	t.Helper()
	full := append([]string{
		"--endpoint", h.restURL,
		"--admin-endpoint", h.adminURL,
	}, args...)
	cmd := exec.Command(h.bin, full...)
	cmd.Env = append(os.Environ(),
		"DASHCTL_OUTPUT=json", // deterministic by default; tests override per-call
	)
	out := &bytes.Buffer{}
	errb := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = errb
	err := cmd.Run()
	code := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			t.Fatalf("dashctl %v: %v", args, err)
		}
	}
	t.Logf("dashctl %v → exit=%d stdout=%q stderr=%q",
		args, code, truncate(out.String(), 400), truncate(errb.String(), 400))
	return cliResult{Code: code, Stdout: out.String(), Stderr: errb.String()}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// captureCommand runs `bin args...` and returns combined stdout+stderr.
// Used by tests that don't go through the harness.
func captureCommand(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = os.Environ()
	out := &bytes.Buffer{}
	cmd.Stdout = out
	cmd.Stderr = out
	err := cmd.Run()
	return out.String(), err
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
		t.Fatalf("%s %s: %v", method, url, err)
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
		t.Fatalf("PUT /v1/inventory: %d %s", code, body2)
	}
}

// --- Wait helpers ---

func waitHTTP(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("waitHTTP %s: %w", url, lastErr)
}

func waitTCP(addr string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 1*time.Second)
		if err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("waitTCP %s: not reachable", addr)
}

// waitUpUntil polls /admin/health until status="ok" (every DPU UP) or
// returns the inventory raw body for diagnostics on timeout.
func (h *harness) waitUpUntil(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var lastBody []byte
	for time.Now().Before(deadline) {
		_, body := httpDo(t, "GET", h.adminURL+"/admin/health", "")
		lastBody = body
		var parsed struct {
			Status string `json:"status"`
			Dpus   []struct {
				State string `json:"state"`
			} `json:"dpus"`
		}
		if json.Unmarshal(body, &parsed) == nil &&
			parsed.Status == "ok" &&
			len(parsed.Dpus) > 0 {
			allUp := true
			for _, d := range parsed.Dpus {
				if d.State != "DPU_STATE_UP" {
					allUp = false
					break
				}
			}
			if allUp {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("DPU did not transition to UP within %s; last /admin/health=%s", timeout, lastBody)
}

// waitDriftEmptyUntil polls /admin/drift?dpu=… until the items array
// is empty.
func (h *harness) waitDriftEmptyUntil(t *testing.T, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		_, body := httpDo(t, "GET", h.adminURL+"/admin/drift?dpu="+h.dpuID, "")
		var parsed struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(body, &parsed)
		if len(parsed.Items) == 0 {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("drift did not clear within %s", timeout)
}
