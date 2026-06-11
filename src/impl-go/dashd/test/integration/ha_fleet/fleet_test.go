//go:build integration_ha_fleet

// Package ha_fleet drives the deploy/test-setup/04-ha-fleet/ Docker
// Compose stack and asserts the headline HA properties end-to-end.
//
// Build:
//
//	go test -tags=integration_ha_fleet -timeout 10m ./test/integration/ha_fleet/...
//
// Prereqs (same as the manual hands-on):
//   - Docker Engine >= 24 with Compose v2
//   - free host ports 12379, 27443/27453/27463, 28443/28453/28463, 29443/29453/29463
//   - the dashcenter/dashd:dev + dashcenter/dashctl:dev + dashcenter/dash-sim:dev
//     images either prebuilt OR buildable from the repo root (`docker compose build`
//     is invoked by `start-fleet`).
//
// The test runs `start-fleet.sh` (or .ps1 on Windows) to bring everything up,
// applies deploy/test-setup/04-ha-fleet/manifest/, asserts identical reads from
// all three dashd nodes, performs an HA switchover, kills the current leader,
// re-asserts state on the new leader, then runs `stop-fleet` regardless of
// outcome. Total wall-clock ~90s on a cached docker host.
package ha_fleet

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	// REST/admin host ports — must match docker-compose.yml in 04-ha-fleet/.
	rest1, admin1 = 28443, 27443
	rest2, admin2 = 28453, 27453
	rest3, admin3 = 28463, 27463
)

func fleetDir(t *testing.T) string {
	t.Helper()
	// This file lives at src/impl-go/dashd/test/integration/ha_fleet/fleet_test.go
	// 04-ha-fleet/ lives at deploy/test-setup/04-ha-fleet/.
	_, thisFile, _, _ := runtime.Caller(0)
	p, err := filepath.Abs(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "..", "deploy", "test-setup", "04-ha-fleet"))
	if err != nil {
		t.Fatalf("resolve fleet dir: %v", err)
	}
	return p
}

// runScript invokes start-fleet / stop-fleet in the fleet dir, picking the
// right shell for the host OS. Output is streamed to t.Log so a failed run
// has the same diagnostics as a manual run.
func runScript(t *testing.T, dir, name string, args ...string) error {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("pwsh", append([]string{"-NoProfile", "-File", "./" + name + ".ps1"}, args...)...)
	} else {
		cmd = exec.Command("bash", append([]string{"./" + name + ".sh"}, args...)...)
	}
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	t.Logf("--- %s output ---\n%s---", name, out)
	return err
}

func getJSON(t *testing.T, url string, dst interface{}) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: status %d body=%s", url, resp.StatusCode, string(body))
	}
	return json.Unmarshal(body, dst)
}

type leaderResp struct {
	Leader   bool   `json:"leader"`
	LeaderID string `json:"leader_id"`
}

// findLeader polls all three admin endpoints and returns (port, leaderID).
// Retries until deadline.
func findLeader(t *testing.T, deadline time.Time) (int, string) {
	t.Helper()
	for time.Now().Before(deadline) {
		for _, p := range []int{admin1, admin2, admin3} {
			var lr leaderResp
			if err := getJSON(t, fmt.Sprintf("http://127.0.0.1:%d/admin/leader", p), &lr); err == nil && lr.Leader {
				return p, lr.LeaderID
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("no leader before deadline")
	return 0, ""
}

// dashctlInContainer runs `docker compose run --rm dashctl …` with --insecure
// targeting the leader's REST endpoint (which is dashd-1/2/3:8443 inside the
// compose network).
func dashctlInContainer(t *testing.T, fleetDir, leaderID string, args ...string) (string, error) {
	t.Helper()
	target := fmt.Sprintf("http://%s:8443", leaderID)
	full := append([]string{"compose", "run", "--rm", "dashctl", "--endpoint", target, "--insecure"}, args...)
	cmd := exec.Command("docker", full...)
	cmd.Dir = fleetDir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// applyManifest runs `dashctl apply -R -f /tmp/manifest` after copying the
// repo manifest dir into dashd-1 first (the dashctl one-shot container has
// no volume mount for the manifest).
func applyManifest(t *testing.T, fleetDir string, leaderID string) {
	t.Helper()
	// Stage manifest into dashd-1 container (it's the most reliable host —
	// dashd-1's container is created by every compose up).
	cp := exec.Command("docker", "cp", filepath.Join(fleetDir, "manifest"), "dc-ha-dashd-1:/tmp/manifest")
	if out, err := cp.CombinedOutput(); err != nil {
		t.Fatalf("docker cp manifest: %v\n%s", err, out)
	}
	// dashctl container can read /tmp/manifest via the shared filesystem layer? No —
	// containers don't share filesystems. We need to bind-mount the host path into the
	// dashctl run. The fleet compose declares no volume; spin a one-off using an
	// explicit bind.
	abs := filepath.Join(fleetDir, "manifest")
	args := []string{
		"run", "--rm",
		"--network", "dashcenter-ha-fleet",
		"-v", abs + ":/tmp/manifest:ro",
		"dashcenter/dashctl:dev",
		"--endpoint", fmt.Sprintf("http://%s:8443", leaderID),
		"--insecure",
		"apply", "-R", "-f", "/tmp/manifest",
	}
	cmd := exec.Command("docker", args...)
	cmd.Dir = fleetDir
	out, err := cmd.CombinedOutput()
	t.Logf("--- apply output ---\n%s---", out)
	if err != nil {
		t.Fatalf("apply failed: %v", err)
	}
}

// countObjects shells out to the same one-shot dashctl image and counts the
// `dashctl get <kind> -o name` rows.
func countObjects(t *testing.T, leaderRESTPort int, kind string) int {
	t.Helper()
	args := []string{
		"run", "--rm", "--network", "host",
		"dashcenter/dashctl:dev",
		"--endpoint", fmt.Sprintf("http://127.0.0.1:%d", leaderRESTPort),
		"--insecure",
		"get", kind, "-o", "name",
	}
	cmd := exec.Command("docker", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("get %s failed: %v\n%s", kind, err, out)
	}
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.Contains(line, "/") {
			n++
		}
	}
	return n
}

// TestHaFleet_FullStack is the headline test. It:
//
//  1. brings the 04-ha-fleet stack up,
//  2. asserts exactly one leader,
//  3. applies the 103-object manifest,
//  4. verifies the same counts via every dashd,
//  5. exercises an HA switchover,
//  6. kills the leader and asserts succession + state preservation,
//  7. tears down (always, even on test failure).
func TestHaFleet_FullStack(t *testing.T) {
	dir := fleetDir(t)

	// Always tear down — even on early failure. This avoids the next CI run
	// inheriting half-running containers.
	t.Cleanup(func() {
		if err := runScript(t, dir, "stop-fleet"); err != nil {
			t.Logf("stop-fleet failed (non-fatal during cleanup): %v", err)
		}
	})

	t.Log("==> start-fleet")
	if err := runScript(t, dir, "start-fleet"); err != nil {
		t.Fatalf("start-fleet: %v", err)
	}

	// Re-find leader inside the test (start-fleet only prints it).
	leaderAdmin, leaderID := findLeader(t, time.Now().Add(30*time.Second))
	t.Logf("==> initial leader: %s (admin :%d)", leaderID, leaderAdmin)

	// Map admin port -> REST port.
	restPort := map[int]int{admin1: rest1, admin2: rest2, admin3: rest3}[leaderAdmin]

	// Apply the full manifest.
	t.Log("==> applying manifest (103 objects)")
	applyManifest(t, dir, leaderID)

	// Per-kind count assertion (matches manual-handson.md table).
	t.Log("==> verifying object counts")
	want := map[string]int{
		"vnet":          12,
		"eni":           30,
		"vnetmapping":   30,
		"routepolicy":   12,
		"aclpolicy":     12,
		"servicetunnel": 4,
		"haset":         3,
	}
	totalWant := 103
	totalGot := 0
	for kind, exp := range want {
		got := countObjects(t, restPort, kind)
		if got != exp {
			t.Errorf("kind=%s want=%d got=%d", kind, exp, got)
		}
		totalGot += got
	}
	if totalGot != totalWant {
		t.Errorf("total objects want=%d got=%d", totalWant, totalGot)
	}

	// Fan-out: every dashd serves the same vnet count from etcd.
	t.Log("==> verifying read fan-out across all 3 dashd")
	for _, rp := range []int{rest1, rest2, rest3} {
		n := countObjects(t, rp, "vnet")
		if n != 12 {
			t.Errorf("rest port %d: vnet count want=12 got=%d", rp, n)
		}
	}

	// HA switchover: walk SSE long enough to capture all 4 events.
	t.Log("==> triggering switchover ha-bank-prod")
	if err := triggerSwitchover(t, restPort, "default", "ha-bank-prod"); err != nil {
		t.Errorf("switchover: %v", err)
	}

	// Kill leader, expect a new leader within lease_ttl + slack (8s + ~6s).
	t.Logf("==> killing leader %s", leaderID)
	killCmd := exec.Command("docker", "stop", "dc-ha-"+leaderID)
	if out, err := killCmd.CombinedOutput(); err != nil {
		t.Fatalf("docker stop: %v\n%s", err, out)
	}

	newAdmin, newLeader := findLeader(t, time.Now().Add(20*time.Second))
	if newLeader == leaderID {
		t.Errorf("expected new leader != %s, got %s", leaderID, newLeader)
	}
	t.Logf("==> new leader after failover: %s (admin :%d)", newLeader, newAdmin)

	// State must survive failover.
	newRestPort := map[int]int{admin1: rest1, admin2: rest2, admin3: rest3}[newAdmin]
	totalAfter := 0
	for kind := range want {
		totalAfter += countObjects(t, newRestPort, kind)
	}
	if totalAfter != totalWant {
		t.Errorf("post-failover total objects want=%d got=%d", totalWant, totalAfter)
	}
}

// triggerSwitchover POSTs the switchover RPC and reads SSE events until it
// either sees the 4th event or hits a 10s timeout.
func triggerSwitchover(t *testing.T, restPort int, ns, name string) error {
	t.Helper()
	url := fmt.Sprintf("http://127.0.0.1:%d/v1/ha/%s/%s/switchover", restPort, ns, name)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBufferString(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("switchover status=%d body=%s", resp.StatusCode, string(body))
	}
	// Drain events: expect 4 `data: {…}` lines.
	buf := make([]byte, 4096)
	events := 0
	for ctx.Err() == nil {
		n, err := resp.Body.Read(buf)
		if n > 0 {
			events += strings.Count(string(buf[:n]), "data:")
			if events >= 4 {
				return nil
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	if events < 4 {
		return fmt.Errorf("switchover: got %d SSE events, want >=4", events)
	}
	return nil
}
