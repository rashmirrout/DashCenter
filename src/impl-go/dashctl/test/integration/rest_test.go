//go:build integration

package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ---------- 1. version --client (no server dial) ----------

func TestIntegration_VersionClient(t *testing.T) {
	// Does NOT spin up a harness — just runs the binary.
	bin := buildDashctlBin(t)
	out, err := runBin(t, bin, "version", "--client")
	if err != nil {
		t.Fatalf("dashctl version --client: %v", err)
	}
	if !strings.Contains(out, "Client: dashctl") {
		t.Fatalf("expected 'Client: dashctl' in output, got: %s", out)
	}
}

// ---------- 2. version against unreachable server ----------

func TestIntegration_Version_ServerUnreachable(t *testing.T) {
	bin := buildDashctlBin(t)
	port := freePort(t)
	endpoint := fmt.Sprintf("http://127.0.0.1:%d", port)
	out, err := runBin(t, bin, "--endpoint", endpoint, "--admin-endpoint", endpoint, "version")
	// `version` is documented to ALWAYS return 0, even with unreachable server.
	if err != nil {
		t.Fatalf("version must not error on unreachable server: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Client: dashctl") {
		t.Fatalf("client section missing: %s", out)
	}
	if !strings.Contains(out, "Server: unavailable") {
		t.Fatalf("server section did not report unavailable: %s", out)
	}
}

// ---------- 3. explain (offline, no server) ----------

func TestIntegration_Explain_Offline(t *testing.T) {
	bin := buildDashctlBin(t)
	out, err := runBin(t, bin, "explain", "vnet")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	for _, want := range []string{"KIND:", "Vnet", "FIELDS:", "vni"} {
		if !strings.Contains(out, want) {
			t.Errorf("explain output missing %q\n---\n%s", want, out)
		}
	}
}

// ---------- 4. dpu list — happy path ----------

func TestIntegration_DpuList(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	r := h.runCtl(t, "dpu", "list", "-o", "table")
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, h.dpuID) {
		t.Fatalf("dpu list missing %q: %s", h.dpuID, r.Stdout)
	}
}

// ---------- 5. get vnet — empty start ----------

func TestIntegration_GetVnet_Empty(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	r := h.runCtl(t, "get", "vnet", "-o", "json")
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	// JSON shape may be {} or a list envelope with empty items — either is fine.
	out := strings.TrimSpace(r.Stdout)
	if out == "" {
		t.Fatal("empty stdout for get vnet")
	}
}

// ---------- 6. apply -f manifest round-trip ----------

func TestIntegration_Apply_RoundTrip(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)

	dir := writeManifests(t, vnetManifest, eniManifest)
	r := h.runCtl(t, "apply", "-f", dir)
	if r.Code != 0 {
		t.Fatalf("apply exit=%d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "vnet/v-it apply") {
		t.Fatalf("apply output missing vnet line: %s", r.Stdout)
	}
	if !strings.Contains(r.Stdout, "eni/e-it apply") {
		t.Fatalf("apply output missing eni line: %s", r.Stdout)
	}

	// Round-trip: re-get and verify generation.
	r = h.runCtl(t, "get", "vnet", "v-it", "-o", "yaml")
	if r.Code != 0 {
		t.Fatalf("get vnet: %d %s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "name: v-it") || !strings.Contains(r.Stdout, "generation: 1") {
		t.Fatalf("get yaml missing fields: %s", r.Stdout)
	}
}

// ---------- 7. get -o table / wide ----------

func TestIntegration_Get_OutputFormats(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	dir := writeManifests(t, vnetManifest)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("setup apply: %d %s", r.Code, r.Stderr)
	}

	cases := []struct {
		format string
		want   []string
	}{
		{"table", []string{"NAME", "v-it"}},
		{"wide", []string{"NAME"}},
		{"json", []string{`"name": "v-it"`}},
		{"yaml", []string{"name: v-it"}},
		{"name", []string{"vnet/v-it"}},
	}
	for _, c := range cases {
		t.Run(c.format, func(t *testing.T) {
			r := h.runCtl(t, "get", "vnet", "-o", c.format)
			if r.Code != 0 {
				t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
			}
			for _, w := range c.want {
				if !strings.Contains(r.Stdout, w) {
					t.Errorf("%s: missing %q\n---\n%s", c.format, w, r.Stdout)
				}
			}
		})
	}
}

// ---------- 8. describe ----------

func TestIntegration_Describe(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	dir := writeManifests(t, vnetManifest)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("setup apply: %d %s", r.Code, r.Stderr)
	}
	r := h.runCtl(t, "describe", "vnet", "v-it")
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	for _, w := range []string{"Name:        v-it", "Kind:        Vnet", "Generation:  1"} {
		if !strings.Contains(r.Stdout, w) {
			t.Errorf("describe missing %q\n---\n%s", w, r.Stdout)
		}
	}
}

// ---------- 9. reconcile ----------

func TestIntegration_Reconcile(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	r := h.runCtl(t, "reconcile")
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "Triggered reconcile") {
		t.Fatalf("reconcile output wrong: %s", r.Stdout)
	}
}

// ---------- 10. dpu drift converges to 0 ----------

func TestIntegration_DpuDrift_Converges(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)

	// Push a vnet + eni so something to reconcile exists.
	dir := writeManifests(t, vnetManifest, eniManifest)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("setup apply: %d %s", r.Code, r.Stderr)
	}

	// Wait for the reconciler to converge (admin/drift returns empty).
	h.waitDriftEmptyUntil(t, 30*time.Second)

	r := h.runCtl(t, "dpu", "drift", "--dpu", h.dpuID)
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "0 drift items.") {
		t.Fatalf("drift did not print empty marker: %s", r.Stdout)
	}
}

// ---------- 11. delete + idempotent re-delete + 404 on get ----------

func TestIntegration_Delete_IdempotentAfter(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)
	dir := writeManifests(t, vnetManifest)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("setup: %d %s", r.Code, r.Stderr)
	}

	r := h.runCtl(t, "delete", "vnet", "v-it")
	if r.Code != 0 {
		t.Fatalf("delete: %d %s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "vnet/v-it deleted") {
		t.Fatalf("delete output: %s", r.Stdout)
	}

	// Plain re-delete should fail with NOT_FOUND (exit 3).
	r = h.runCtl(t, "delete", "vnet", "v-it")
	if r.Code != 3 {
		t.Fatalf("re-delete: expected exit 3 (NotFound), got %d stdout=%s stderr=%s", r.Code, r.Stdout, r.Stderr)
	}

	// --ignore-not-found should be exit 0.
	r = h.runCtl(t, "delete", "vnet", "v-it", "--ignore-not-found")
	if r.Code != 0 {
		t.Fatalf("--ignore-not-found: %d %s", r.Code, r.Stderr)
	}

	// get is 404.
	r = h.runCtl(t, "get", "vnet", "v-it")
	if r.Code != 3 {
		t.Fatalf("get after delete: expected exit 3, got %d stdout=%s", r.Code, r.Stdout)
	}
}

// ---------- 12. label selector ----------

func TestIntegration_Get_LabelSelector(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)

	dir := writeManifests(t,
		// Same kind, different labels.
		`apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v-prod, labels: { tier: prod } }
spec: { vni: 7001 }`,
		`apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v-dev, labels: { tier: dev } }
spec: { vni: 7002 }`)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("setup: %d %s", r.Code, r.Stderr)
	}

	r := h.runCtl(t, "get", "vnet", "-l", "tier=prod", "-o", "name")
	if r.Code != 0 {
		t.Fatalf("exit=%d stderr=%s", r.Code, r.Stderr)
	}
	if !strings.Contains(r.Stdout, "vnet/v-prod") {
		t.Fatalf("v-prod missing: %s", r.Stdout)
	}
	if strings.Contains(r.Stdout, "vnet/v-dev") {
		t.Fatalf("v-dev should have been filtered out: %s", r.Stdout)
	}
}

// ---------- 13. CAS on PUT via metadata.generation ----------
//
// dashd Phase 1B supports expected_generation only on Put*, not on
// Delete. This test confirms that:
//
//   - First apply (no generation) → succeeds, dashd returns gen=1.
//   - Second apply with metadata.generation=1 → succeeds, gen=2.
//   - Third apply with metadata.generation=1 again → exit 4
//     (FAILED_PRECONDITION because current gen is 2, not 1).
//
// dashctl's `replace` command requires metadata.generation and is the
// canonical CAS verb.
func TestIntegration_Replace_CAS_Mismatch(t *testing.T) {
	h := startHarness(t)
	h.waitUpUntil(t, 30*time.Second)

	const v0 = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v-cas }
spec: { vni: 8001 }`
	dir := writeManifests(t, v0)
	if r := h.runCtl(t, "apply", "-f", dir); r.Code != 0 {
		t.Fatalf("seed apply: %d %s", r.Code, r.Stderr)
	}

	// Replace with generation=1 → expected to succeed (current gen IS 1).
	const v1 = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v-cas, generation: 1 }
spec: { vni: 8002 }`
	dir = writeManifests(t, v1)
	if r := h.runCtl(t, "replace", "-f", dir); r.Code != 0 {
		t.Fatalf("first replace with correct gen: %d %s", r.Code, r.Stderr)
	}

	// Replace again with the same (now stale) generation=1 → must error.
	if r := h.runCtl(t, "replace", "-f", dir); r.Code != 4 {
		t.Fatalf("stale-gen replace: expected exit 4, got %d stdout=%s stderr=%s",
			r.Code, r.Stdout, r.Stderr)
	}
}

// --- Helpers ---

const vnetManifest = `apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v-it, labels: { tier: it } }
spec: { vni: 9001 }`

const eniManifest = `apiVersion: dashcenter.v1
kind: Eni
metadata: { name: e-it, labels: { tier: it } }
spec:
  vnet_name: v-it
  mac_address: "00:11:22:33:44:55"
  underlay_ip: "10.0.5.99"
  admin_state: "up"`

// writeManifests creates a temp directory with each YAML body in its
// own file (named in lexicographic apply order), and returns the dir.
func writeManifests(t *testing.T, bodies ...string) string {
	t.Helper()
	dir := t.TempDir()
	for i, body := range bodies {
		name := filepath.Join(dir, fmt.Sprintf("%02d.yaml", i))
		if err := os.WriteFile(name, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// runBin runs a one-shot dashctl WITHOUT going through harness — used
// by offline / unreachable-server tests.
func runBin(t *testing.T, bin string, args ...string) (string, error) {
	t.Helper()
	type result struct {
		out string
		err error
	}
	c := make(chan result, 1)
	go func() {
		out, err := captureCommand(bin, args...)
		c <- result{out, err}
	}()
	select {
	case r := <-c:
		t.Logf("dashctl %v → %q", args, truncate(r.out, 400))
		return r.out, r.err
	case <-time.After(30 * time.Second):
		t.Fatal("dashctl invocation hung")
		return "", nil
	}
}
