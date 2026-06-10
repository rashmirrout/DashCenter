package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	dashErrors "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// fakeClient is a scriptable client.Client for testing every cmd surface.
type fakeClient struct {
	mu             sync.Mutex
	putCalls       []putCall
	deleteCalls    []deleteCall
	reconcileCalls [][]string
	closed         bool

	healthFn         func(context.Context) (client.HealthReport, error)
	serverInfoFn     func(context.Context) (client.ServerInfo, error)
	putFn            func(ctx context.Context, ns, kind, name string, body []byte) (*client.PutResult, error)
	getFn            func(ctx context.Context, ns, kind, name string) (*client.StoredItem, error)
	listFn           func(ctx context.Context, ns, kind string, opts client.ListOptions) ([]*client.StoredItem, error)
	deleteFn         func(ctx context.Context, ns, kind, name string, opts client.DeleteOptions) error
	reconcileFn      func(ctx context.Context, ids []string) error
	putInventoryFn   func(ctx context.Context, dpus []client.DpuInput) error
	getInventoryFn   func(ctx context.Context) ([]client.DpuStatus, error)
	adminDriftFn     func(ctx context.Context, dpu string) ([]client.DriftItem, error)
	adminPlacementFn func(ctx context.Context) ([]client.EniPlacementRow, error)
	simulateFn       func(ctx context.Context, opsJSON []byte) (*client.SimulateResult, error)
}

type putCall struct {
	NS, Kind, Name string
	Body           []byte
}
type deleteCall struct {
	NS, Kind, Name string
	Opts           client.DeleteOptions
}

func (f *fakeClient) Close() error                                { f.closed = true; return nil }
func (f *fakeClient) Health(ctx context.Context) (client.HealthReport, error) {
	if f.healthFn != nil {
		return f.healthFn(ctx)
	}
	return client.HealthReport{Status: "ok", Leader: true, Dpus: []client.DpuStatus{{ID: "dpu-0", State: "UP"}}}, nil
}
func (f *fakeClient) ServerInfo(ctx context.Context) (client.ServerInfo, error) {
	if f.serverInfoFn != nil {
		return f.serverInfoFn(ctx)
	}
	return client.ServerInfo{OK: true, Leader: true, Version: "dashd"}, nil
}
func (f *fakeClient) PutInventory(ctx context.Context, dpus []client.DpuInput) error {
	if f.putInventoryFn != nil {
		return f.putInventoryFn(ctx, dpus)
	}
	return nil
}
func (f *fakeClient) GetInventory(ctx context.Context) ([]client.DpuStatus, error) {
	if f.getInventoryFn != nil {
		return f.getInventoryFn(ctx)
	}
	return []client.DpuStatus{{ID: "dpu-0", State: "UP"}}, nil
}
func (f *fakeClient) Put(ctx context.Context, ns, kind, name string, body []byte) (*client.PutResult, error) {
	f.mu.Lock()
	f.putCalls = append(f.putCalls, putCall{ns, kind, name, append([]byte(nil), body...)})
	f.mu.Unlock()
	if f.putFn != nil {
		return f.putFn(ctx, ns, kind, name, body)
	}
	return &client.PutResult{Accepted: true, Generation: 1}, nil
}
func (f *fakeClient) Get(ctx context.Context, ns, kind, name string) (*client.StoredItem, error) {
	if f.getFn != nil {
		return f.getFn(ctx, ns, kind, name)
	}
	return &client.StoredItem{Kind: kind, Namespace: ns, Name: name, Generation: 1, Spec: json.RawMessage(`{"vni":1001}`)}, nil
}
func (f *fakeClient) List(ctx context.Context, ns, kind string, opts client.ListOptions) ([]*client.StoredItem, error) {
	if f.listFn != nil {
		return f.listFn(ctx, ns, kind, opts)
	}
	return []*client.StoredItem{
		{Kind: kind, Namespace: ns, Name: "a", Generation: 1, Spec: json.RawMessage(`{"vni":1}`)},
		{Kind: kind, Namespace: ns, Name: "b", Generation: 1, Spec: json.RawMessage(`{"vni":2}`)},
	}, nil
}
func (f *fakeClient) Delete(ctx context.Context, ns, kind, name string, opts client.DeleteOptions) error {
	f.mu.Lock()
	f.deleteCalls = append(f.deleteCalls, deleteCall{ns, kind, name, opts})
	f.mu.Unlock()
	if f.deleteFn != nil {
		return f.deleteFn(ctx, ns, kind, name, opts)
	}
	return nil
}
func (f *fakeClient) Reconcile(ctx context.Context, ids []string) error {
	f.mu.Lock()
	f.reconcileCalls = append(f.reconcileCalls, append([]string(nil), ids...))
	f.mu.Unlock()
	if f.reconcileFn != nil {
		return f.reconcileFn(ctx, ids)
	}
	return nil
}
func (f *fakeClient) AdminDrift(ctx context.Context, dpu string) ([]client.DriftItem, error) {
	if f.adminDriftFn != nil {
		return f.adminDriftFn(ctx, dpu)
	}
	return nil, nil
}
func (f *fakeClient) AdminEniPlacement(ctx context.Context) ([]client.EniPlacementRow, error) {
	if f.adminPlacementFn != nil {
		return f.adminPlacementFn(ctx)
	}
	return nil, nil
}
func (f *fakeClient) Simulate(ctx context.Context, opsJSON []byte) (*client.SimulateResult, error) {
	if f.simulateFn != nil {
		return f.simulateFn(ctx, opsJSON)
	}
	return &client.SimulateResult{WouldSucceed: true}, nil
}

// testApp returns an Application with fake client, captured streams,
// and an in-memory config.
func testApp(t *testing.T, fc *fakeClient) (*Application, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	out := &bytes.Buffer{}
	errb := &bytes.Buffer{}
	a := &Application{
		Build:  BuildInfo{Version: "v0", Commit: "x", Date: "now"},
		Out:    out,
		Err:    errb,
		In:     strings.NewReader(""),
		Flags:  &globalFlags{output: "json"},
		Env:    config.Env{},
		Config: config.New(),
	}
	a.setResolver(func() (*config.ResolvedConfig, error) {
		return &config.ResolvedConfig{Endpoint: "http://localhost:8443", AdminEndpoint: "http://localhost:7443", Transport: "rest", Namespace: "default"}, nil
	})
	a.setDialer(func(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error) {
		if fc == nil {
			return &fakeClient{}, nil
		}
		return fc, nil
	})
	return a, out, errb
}

func runArgs(a *Application, args ...string) int {
	return a.Run(args)
}

// ---------- root / generic ----------

func TestRootHelp(t *testing.T) {
	a, out, _ := testApp(t, nil)
	if code := runArgs(a, "--help"); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if !strings.Contains(out.String(), "dashctl") {
		t.Fatalf("missing banner: %s", out.String())
	}
}

func TestVersionClient(t *testing.T) {
	a, out, _ := testApp(t, nil)
	if code := runArgs(a, "version", "--client"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Client: dashctl v0") {
		t.Fatalf("%s", out.String())
	}
}

func TestVersionServer(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "version"); code != 0 {
		t.Fatal()
	}
	s := out.String()
	if !strings.Contains(s, "Client:") || !strings.Contains(s, "Server: dashd  dashd") {
		t.Fatalf("%s", s)
	}
}

func TestVersionServerUnreachable(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{serverInfoFn: func(ctx context.Context) (client.ServerInfo, error) {
		return client.ServerInfo{}, fmt.Errorf("dial refused")
	}})
	if code := runArgs(a, "version"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Server: unavailable") {
		t.Fatalf("%s", out.String())
	}
}

// ---------- apply ----------

func TestApplyMissingFile(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "apply"); code == 0 {
		t.Fatal("apply without -f should fail")
	}
}

func TestApplyBadDryRunValue(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: { vni: 1 }\n"), 0o600)
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "apply", "-f", p, "--dry-run", "ghost"); code == 0 {
		t.Fatal()
	}
}

func TestApplyHappyPath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1, namespace: ns }\nspec: { vni: 1001 }\n"), 0o600)
	fc := &fakeClient{}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	if len(fc.putCalls) != 1 || fc.putCalls[0].Kind != "vnet" || fc.putCalls[0].NS != "ns" {
		t.Fatalf("%+v", fc.putCalls)
	}
	if !strings.Contains(out.String(), "vnet/v1 apply in namespace ns") {
		t.Fatalf("%s", out.String())
	}
}

func TestApplyDryRunClient(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	fc := &fakeClient{}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p, "--dry-run", "client"); code != 0 {
		t.Fatal()
	}
	if len(fc.putCalls) != 0 {
		t.Fatal("client dry-run must not call Put")
	}
	if !strings.Contains(out.String(), "would dry-run") {
		t.Fatalf("%s", out.String())
	}
}

func TestApplyDryRunServer(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	fc := &fakeClient{}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p, "--dry-run", "server"); code != 0 {
		t.Fatal()
	}
	if len(fc.putCalls) != 0 {
		t.Fatal("server dry-run must not Put")
	}
	if !strings.Contains(out.String(), "would apply") {
		t.Fatalf("%s", out.String())
	}
}

func TestApplyFailurePropagates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	fc := &fakeClient{putFn: func(ctx context.Context, ns, kind, name string, body []byte) (*client.PutResult, error) {
		return nil, fmt.Errorf("boom")
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p); code == 0 {
		t.Fatal("expected non-zero")
	}
}

func TestApplyInventoryKindCallsPutInventory(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "i.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Inventory\nspec:\n  dpus:\n    - { id: dpu-0, endpoint: \"x:1\" }\n"), 0o600)
	called := false
	fc := &fakeClient{putInventoryFn: func(ctx context.Context, dpus []client.DpuInput) error {
		called = true
		if len(dpus) != 1 || dpus[0].ID != "dpu-0" {
			t.Fatalf("%+v", dpus)
		}
		return nil
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p); code != 0 {
		t.Fatal()
	}
	if !called {
		t.Fatal("PutInventory not invoked")
	}
}

// ---------- get ----------

func TestGetOneJSON(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "vnet", "v1", "-o", "json"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), `"kind": "Vnet"`) {
		t.Fatalf("%s", out.String())
	}
}

func TestGetListTable(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "vnet", "-o", "table"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "NAME") || !strings.Contains(out.String(), "a") {
		t.Fatalf("%s", out.String())
	}
}

func TestGetUnknownKind(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "get", "ghost"); code == 0 {
		t.Fatal()
	}
}

func TestGetNameMode(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "vnet", "-o", "name"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "vnet/a\nvnet/b\n") {
		t.Fatalf("%q", out.String())
	}
}

func TestGetSelectorPropagates(t *testing.T) {
	var seenSel string
	fc := &fakeClient{listFn: func(ctx context.Context, ns, kind string, opts client.ListOptions) ([]*client.StoredItem, error) {
		seenSel = opts.Selector
		return nil, nil
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "get", "vnet", "-l", "tier=prod"); code != 0 {
		t.Fatal()
	}
	if seenSel != "tier=prod" {
		t.Fatalf("selector: %q", seenSel)
	}
}

func TestGetInventoryKind(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "inventory"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "dpu-0") {
		t.Fatalf("%s", out.String())
	}
}

func TestGetInventoryWithNameStillReturnsAll(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "inventory", "anything"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "dpu-0") {
		t.Fatalf("%s", out.String())
	}
}

// ---------- describe ----------

func TestDescribeOK(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "describe", "vnet", "v1"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Name:        v1") {
		t.Fatalf("%s", out.String())
	}
}

func TestDescribeUnknownKind(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "describe", "ghost", "x"); code == 0 {
		t.Fatal()
	}
}

// ---------- delete ----------

func TestDeleteHappy(t *testing.T) {
	fc := &fakeClient{}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "delete", "vnet", "v1"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "vnet/v1 deleted") || len(fc.deleteCalls) != 1 {
		t.Fatalf("%s\n%+v", out.String(), fc.deleteCalls)
	}
}

func TestDeleteIgnoreNotFound(t *testing.T) {
	fc := &fakeClient{deleteFn: func(ctx context.Context, ns, kind, name string, opts client.DeleteOptions) error {
		if opts.IgnoreNotFound {
			return nil
		}
		return fmt.Errorf("not found")
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "delete", "vnet", "v1", "--ignore-not-found"); code != 0 {
		t.Fatal()
	}
}

func TestDeleteCASFlag(t *testing.T) {
	var gotOpts client.DeleteOptions
	fc := &fakeClient{deleteFn: func(ctx context.Context, ns, kind, name string, opts client.DeleteOptions) error {
		gotOpts = opts
		return nil
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "delete", "vnet", "v1", "--expected-generation", "5"); code != 0 {
		t.Fatal()
	}
	if gotOpts.ExpectedGeneration != 5 {
		t.Fatalf("got %d", gotOpts.ExpectedGeneration)
	}
}

func TestDeleteUnknownKind(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "delete", "ghost", "x"); code == 0 {
		t.Fatal()
	}
}

// ---------- reconcile ----------

func TestReconcile(t *testing.T) {
	fc := &fakeClient{}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "reconcile"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Triggered reconcile on all DPUs") {
		t.Fatalf("%s", out.String())
	}
	if code := runArgs(a, "reconcile", "--dpu", "d1", "--dpu", "d2"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Triggered reconcile on 2 DPU(s)") {
		t.Fatalf("%s", out.String())
	}
}

// ---------- dpu ----------

func TestDpuList(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "dpu", "list", "-o", "table"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "ID") || !strings.Contains(out.String(), "dpu-0") {
		t.Fatalf("%s", out.String())
	}
}

func TestDpuStatus(t *testing.T) {
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "dpu", "status"); code != 0 {
		t.Fatal()
	}
}

func TestDpuDriftRequiresDpu(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "dpu", "drift"); code == 0 {
		t.Fatal()
	}
}

func TestDpuDriftEmpty(t *testing.T) {
	fc := &fakeClient{adminDriftFn: func(ctx context.Context, dpu string) ([]client.DriftItem, error) {
		return nil, nil
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "dpu", "drift", "--dpu", "dpu-0"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "0 drift items.") {
		t.Fatalf("%s", out.String())
	}
}

func TestDpuDriftNonEmpty(t *testing.T) {
	fc := &fakeClient{adminDriftFn: func(ctx context.Context, dpu string) ([]client.DriftItem, error) {
		return []client.DriftItem{{DpuID: dpu, Op: "add", Kind: "vnet", Key: "v1"}}, nil
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "dpu", "drift", "--dpu", "dpu-0"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "add") || !strings.Contains(out.String(), "vnet") {
		t.Fatalf("%s", out.String())
	}
}

func TestDpuDescribeOK(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "dpu", "describe", "dpu-0"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "Name:        dpu-0") {
		t.Fatalf("%s", out.String())
	}
}

func TestDpuDescribeNotFound(t *testing.T) {
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "dpu", "describe", "ghost"); code == 0 {
		t.Fatal()
	}
}

func TestDpuCordonStub(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "dpu", "cordon"); code == 0 {
		t.Fatal()
	}
}

// ---------- inventory ----------

func TestInventoryPut(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "i.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Inventory\nspec:\n  dpus:\n    - {id: dpu-0, endpoint: 'x:1'}\n"), 0o600)
	fc := &fakeClient{}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "inventory", "put", "-f", p); code != 0 {
		t.Fatal()
	}
}

func TestInventoryPutWrongKind(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "i.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "inventory", "put", "-f", p); code == 0 {
		t.Fatal()
	}
}

func TestInventoryGet(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "inventory", "get"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "dpu-0") {
		t.Fatalf("%s", out.String())
	}
}

// ---------- diff ----------

func TestDiffCreate(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: { vni: 1001 }\n"), 0o600)
	fc := &fakeClient{getFn: func(ctx context.Context, ns, kind, name string) (*client.StoredItem, error) {
		return nil, dashErrors.New(dashErrors.CodeNotFound, "not found")
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "diff", "-f", p); code != 0 {
		t.Fatalf("exit=%d %s", code, out.String())
	}
	if !strings.Contains(out.String(), "would CREATE") {
		t.Fatalf("%s", out.String())
	}
}

// Adapt to typed *errors.Error via errors.New embedding in diff command.
// Simplest: use the public typed error.
func TestDiffChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: { vni: 1002 }\n"), 0o600)
	fc := &fakeClient{getFn: func(ctx context.Context, ns, kind, name string) (*client.StoredItem, error) {
		return &client.StoredItem{Kind: "vnet", Name: name, Spec: json.RawMessage(`{"vni":1001}`)}, nil
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "diff", "-f", p); code != 0 {
		t.Fatalf("%d", code)
	}
	if !strings.Contains(out.String(), "vni: 1001 → 1002") {
		t.Fatalf("%s", out.String())
	}
}

func TestDiffNoChange(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: { vni: 1001 }\n"), 0o600)
	fc := &fakeClient{getFn: func(ctx context.Context, ns, kind, name string) (*client.StoredItem, error) {
		return &client.StoredItem{Kind: "vnet", Name: name, Spec: json.RawMessage(`{"vni":1001}`)}, nil
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "diff", "-f", p); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "no changes") {
		t.Fatalf("%s", out.String())
	}
}

func TestDiffMissingFlag(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "diff"); code == 0 {
		t.Fatal()
	}
}

func TestDiffInventoryFullReplace(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "i.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Inventory\nspec: { dpus: [] }\n"), 0o600)
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "diff", "-f", p); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "full-replace") {
		t.Fatalf("%s", out.String())
	}
}

// ---------- replace ----------

func TestReplaceRequiresGeneration(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "replace", "-f", p); code == 0 {
		t.Fatal("missing generation must fail")
	}
}

func TestReplaceWithGeneration(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1, generation: 5 }\nspec: {vni:1}\n"), 0o600)
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "replace", "-f", p); code != 0 {
		t.Fatal()
	}
}

func TestReplaceMissingFile(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "replace"); code == 0 {
		t.Fatal()
	}
}

// ---------- edit ----------

func TestEditNoChanges(t *testing.T) {
	// editorFunc returns input unchanged → "no changes"
	old := editorFunc
	editorFunc = func(data []byte) ([]byte, error) { return data, nil }
	defer func() { editorFunc = old }()
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "edit", "vnet", "v1"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "no changes") {
		t.Fatalf("%s", out.String())
	}
}

func TestEditAppliesEdit(t *testing.T) {
	old := editorFunc
	editorFunc = func(data []byte) ([]byte, error) {
		// Swap vni
		s := strings.Replace(string(data), "vni: 1001", "vni: 1010", 1)
		return []byte(s), nil
	}
	defer func() { editorFunc = old }()
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "edit", "vnet", "v1"); code != 0 {
		t.Fatalf("%s", out.String())
	}
	if !strings.Contains(out.String(), "updated (generation 1)") {
		t.Fatalf("%s", out.String())
	}
}

func TestEditUnknownKind(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "edit", "ghost", "x"); code == 0 {
		t.Fatal()
	}
}

func TestEditEditorFails(t *testing.T) {
	old := editorFunc
	editorFunc = func(data []byte) ([]byte, error) { return nil, fmt.Errorf("editor crashed") }
	defer func() { editorFunc = old }()
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "edit", "vnet", "v1"); code == 0 {
		t.Fatal()
	}
}

// ---------- events / phase2 stubs ----------

func TestEventsUnimplemented(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "events"); code == 0 {
		t.Fatal()
	}
}

func TestPhase2StubsExitUnimplemented(t *testing.T) {
	cases := [][]string{
		{"ha", "switchover"},
		{"ha", "failover"},
		{"ha", "events"},
		{"migration", "plan"},
		{"migration", "start"},
		{"trace", "flow"},
		{"trace", "explain"},
	}
	for _, c := range cases {
		a, _, _ := testApp(t, nil)
		if code := runArgs(a, c...); code == 0 {
			t.Errorf("%v: expected non-zero", c)
		}
	}
}

// ---------- explain ----------

func TestExplain(t *testing.T) {
	a, out, _ := testApp(t, nil)
	if code := runArgs(a, "explain", "vnet"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), "FIELDS:") || !strings.Contains(out.String(), "vni") {
		t.Fatalf("%s", out.String())
	}
}

func TestExplainUnknown(t *testing.T) {
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "explain", "ghost"); code == 0 {
		t.Fatal()
	}
}

// ---------- completion ----------

func TestCompletion(t *testing.T) {
	for _, sh := range []string{"bash", "zsh", "fish", "powershell"} {
		a, out, _ := testApp(t, nil)
		if code := runArgs(a, "completion", sh); code != 0 {
			t.Errorf("%s: exit %d", sh, code)
		}
		if out.Len() == 0 {
			t.Errorf("%s: empty completion", sh)
		}
	}
	a, _, _ := testApp(t, nil)
	if code := runArgs(a, "completion", "tcsh"); code == 0 {
		t.Fatal("invalid shell must error")
	}
}

// ---------- config subcommands ----------

func TestConfigCRUD(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	withConfig := func() *Application {
		a, _, _ := testApp(t, &fakeClient{})
		return a
	}
	runCfg := func(a *Application, args ...string) int {
		// Inject --config every time to defeat the persistent-flag
		// default reset.
		return runArgs(a, append([]string{"--config", path}, args...)...)
	}
	// view (file missing → empty config)
	{
		a := withConfig()
		_ = runCfg(a, "config", "view")
	}
	// set-context creates and saves
	{
		a := withConfig()
		if code := runCfg(a, "config", "set-context", "dev", "--endpoint", "http://localhost:8443", "--namespace", "ns-a"); code != 0 {
			t.Fatalf("set-context: %d", code)
		}
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "dev") || !strings.Contains(string(data), "ns-a") {
			t.Fatalf("saved file missing values: %s", data)
		}
	}
	// get-contexts
	{
		a := withConfig()
		buf := &bytes.Buffer{}
		a.Out = buf
		_ = runCfg(a, "config", "get-contexts")
		if !strings.Contains(buf.String(), "* dev") {
			t.Fatalf("%s", buf.String())
		}
	}
	// rename
	{
		a := withConfig()
		if code := runCfg(a, "config", "rename-context", "dev", "prod"); code != 0 {
			t.Fatalf("rename: %d", code)
		}
	}
	// use-context invalid
	{
		a := withConfig()
		if code := runCfg(a, "config", "use-context", "ghost"); code == 0 {
			t.Fatal("expected error on unknown context")
		}
	}
	// current-context
	{
		a := withConfig()
		_ = runCfg(a, "config", "current-context")
	}
	// delete
	{
		a := withConfig()
		if code := runCfg(a, "config", "delete-context", "prod"); code != 0 {
			t.Fatalf("delete: %d", code)
		}
	}
}

// ---------- typed kind groups ----------

func TestTypedKindPutGetDeleteDescribe(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)

	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "vnet", "put", "-f", p); code != 0 {
		t.Fatal()
	}
	if code := runArgs(a, "vnet", "get", "v1"); code != 0 {
		t.Fatal()
	}
	if code := runArgs(a, "vnet", "list"); code != 0 {
		t.Fatal()
	}
	if code := runArgs(a, "vnet", "delete", "v1"); code != 0 {
		t.Fatal()
	}
	if code := runArgs(a, "vnet", "describe", "v1"); code != 0 {
		t.Fatal()
	}
}

func TestTypedPutWrongKind(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Eni\nmetadata: { name: e1 }\nspec: { vnet_name: v }\n"), 0o600)
	a, _, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "vnet", "put", "-f", p); code == 0 {
		t.Fatal("kind mismatch must error")
	}
}

// ---------- error formatting ----------

func TestRunPrintsErrorAndExitCode(t *testing.T) {
	fc := &fakeClient{putFn: func(ctx context.Context, ns, kind, name string, body []byte) (*client.PutResult, error) {
		return nil, &cliErr{code: 4, msg: "gen mismatch"}
	}}
	dir := t.TempDir()
	p := filepath.Join(dir, "v.yaml")
	_ = os.WriteFile(p, []byte("apiVersion: dashcenter.v1\nkind: Vnet\nmetadata: { name: v1 }\nspec: {vni:1}\n"), 0o600)
	a, _, errb := testApp(t, fc)
	if code := runArgs(a, "apply", "-f", p); code == 0 {
		t.Fatal("expected non-zero")
	}
	if !strings.Contains(errb.String(), "Error:") {
		t.Fatalf("%s", errb.String())
	}
}

// Synthetic typed *errors.Error for tests.
type cliErr struct {
	code int
	msg  string
}

func (e *cliErr) Error() string { return e.msg }

// Ensure DialFailureIsClassified does not panic.
func TestDialError(t *testing.T) {
	a, _, errb := testApp(t, nil)
	a.setDialer(func(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error) {
		return nil, io.ErrUnexpectedEOF
	})
	if code := runArgs(a, "dpu", "list"); code == 0 {
		t.Fatal("dial failure must non-zero")
	}
	if !strings.Contains(errb.String(), "Error:") {
		t.Fatalf("%s", errb.String())
	}
}

// Resolve config error surfaces gracefully.
func TestResolveError(t *testing.T) {
	a, _, _ := testApp(t, nil)
	a.setResolver(func() (*config.ResolvedConfig, error) {
		return nil, fmt.Errorf("config: nope")
	})
	if code := runArgs(a, "dpu", "list"); code == 0 {
		t.Fatal()
	}
}
