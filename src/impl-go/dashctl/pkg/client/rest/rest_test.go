package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	pkgerrors "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// fixture spins up a dashd-shaped REST + admin server and returns a
// configured Client pointing at both.
type fixture struct {
	t        *testing.T
	api      *httptest.Server
	admin    *httptest.Server
	apiMux   *http.ServeMux
	adminMux *http.ServeMux
	c        *Client
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, apiMux: http.NewServeMux(), adminMux: http.NewServeMux()}
	f.api = httptest.NewServer(f.apiMux)
	f.admin = httptest.NewServer(f.adminMux)
	t.Cleanup(func() {
		f.api.Close()
		f.admin.Close()
	})
	rc := &config.ResolvedConfig{
		Endpoint:      f.api.URL,
		AdminEndpoint: f.admin.URL,
		Transport:     config.TransportREST,
		Namespace:     "default",
		Timeout:       2 * time.Second,
	}
	cl, err := New(context.Background(), rc)
	if err != nil {
		t.Fatal(err)
	}
	f.c = cl.(*Client)
	return f
}

// writeJSON helper.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ------- New / construction -------

func TestNewRejectsNilOrEmptyEndpoint(t *testing.T) {
	if _, err := New(context.Background(), nil); err == nil {
		t.Fatal()
	}
	if _, err := New(context.Background(), &config.ResolvedConfig{}); err == nil {
		t.Fatal("empty endpoint must error")
	}
}

func TestNewBuildsTLSOnlyWhenRequested(t *testing.T) {
	rc := &config.ResolvedConfig{Endpoint: "https://x", Transport: config.TransportREST}
	c, err := New(context.Background(), rc)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
}

func TestNewBadCAFile(t *testing.T) {
	dir := t.TempDir()
	rc := &config.ResolvedConfig{
		Endpoint:  "https://x",
		Transport: config.TransportREST,
		TLS:       config.TLSConfig{CAFile: filepath.Join(dir, "nonexistent")},
	}
	if _, err := New(context.Background(), rc); err == nil {
		t.Fatal()
	}
}

func TestNewInvalidCAPem(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "ca")
	_ = os.WriteFile(bad, []byte("not a pem"), 0o600)
	rc := &config.ResolvedConfig{
		Endpoint:  "https://x",
		Transport: config.TransportREST,
		TLS:       config.TLSConfig{CAFile: bad},
	}
	if _, err := New(context.Background(), rc); err == nil {
		t.Fatal()
	}
}

func TestNewMutualTLSPartial(t *testing.T) {
	rc := &config.ResolvedConfig{
		Endpoint:  "https://x",
		Transport: config.TransportREST,
		TLS:       config.TLSConfig{CertFile: "/dev/null"}, // missing KeyFile
	}
	if _, err := New(context.Background(), rc); err == nil {
		t.Fatal()
	}
}

// ------- Health -------

func TestHealth(t *testing.T) {
	f := newFixture(t)
	f.adminMux.HandleFunc("/admin/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"status": "ok", "leader": true,
			"dpus": []map[string]any{{"id": "dpu-0", "state": "DPU_STATE_UP"}},
		})
	})
	h, err := f.c.Health(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if h.Status != "ok" || !h.Leader || len(h.Dpus) != 1 {
		t.Fatalf("unexpected: %+v", h)
	}
}

func TestServerInfoTracksHealth(t *testing.T) {
	f := newFixture(t)
	f.adminMux.HandleFunc("/admin/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "leader": true})
	})
	si, err := f.c.ServerInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !si.OK || !si.Leader {
		t.Fatalf("info: %+v", si)
	}
}

func TestServerInfoUnreachable(t *testing.T) {
	rc := &config.ResolvedConfig{Endpoint: "http://127.0.0.1:1", AdminEndpoint: "http://127.0.0.1:1", Transport: config.TransportREST, Timeout: 200 * time.Millisecond}
	cl, _ := New(context.Background(), rc)
	if _, err := cl.ServerInfo(context.Background()); err == nil {
		t.Fatal("dialing closed port should fail")
	}
}

// ------- Inventory -------

func TestPutGetInventory(t *testing.T) {
	f := newFixture(t)
	var got []client.DpuInput
	f.apiMux.HandleFunc("/v1/inventory", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			body, _ := io.ReadAll(r.Body)
			var p struct {
				Dpus []client.DpuInput `json:"dpus"`
			}
			_ = json.Unmarshal(body, &p)
			got = p.Dpus
			writeJSON(w, 200, map[string]bool{"accepted": true})
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{
				"dpus": []client.DpuStatus{{ID: "dpu-0", State: "DPU_STATE_UP", Endpoint: "x:1"}},
			})
		}
	})
	if err := f.c.PutInventory(context.Background(), []client.DpuInput{{ID: "dpu-0", Endpoint: "x:1"}}); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "dpu-0" {
		t.Fatalf("server received %+v", got)
	}
	inv, err := f.c.GetInventory(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(inv) != 1 {
		t.Fatal()
	}
}

// ------- Put / Get / List / Delete -------

func TestPutHappyPath(t *testing.T) {
	f := newFixture(t)
	var seenBody string
	f.apiMux.HandleFunc("/v1/default/vnets/vnet-1", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seenBody = string(b)
		writeJSON(w, 200, client.PutResult{Accepted: true, Generation: 1})
	})
	res, err := f.c.Put(context.Background(), "", "vnet", "vnet-1", []byte(`{"vni":1001}`))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Accepted || res.Generation != 1 {
		t.Fatalf("%+v", res)
	}
	if !strings.Contains(seenBody, `"vni":1001`) {
		t.Fatalf("server saw %q", seenBody)
	}
}

func TestPutBadKind(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.Put(context.Background(), "", "ghost", "x", []byte(`{}`)); err == nil {
		t.Fatal()
	}
}

func TestPutEmptyName(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.Put(context.Background(), "", "vnet", "", []byte(`{}`)); err == nil {
		t.Fatal()
	}
}

func TestPutNamespaceEscaping(t *testing.T) {
	f := newFixture(t)
	hit := false
	f.apiMux.HandleFunc("/v1/team%20a/vnets/v1", func(w http.ResponseWriter, r *http.Request) {
		hit = true
		writeJSON(w, 200, client.PutResult{Accepted: true, Generation: 1})
	})
	if _, err := f.c.Put(context.Background(), "team a", "vnet", "v1", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if !hit {
		t.Fatal("URL escaping for namespace failed")
	}
}

func TestPutMapsHTTPStatusToError(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets/x", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 409, map[string]string{"error": "generation mismatch", "txn_id": "tx-1"})
	})
	_, err := f.c.Put(context.Background(), "", "vnet", "x", []byte(`{}`))
	if err == nil {
		t.Fatal()
	}
	var ce *pkgerrors.Error
	if !asErr(err, &ce) {
		t.Fatal("not classified")
	}
	if ce.Code != pkgerrors.CodeConflict {
		t.Fatalf("code: %v", ce.Code)
	}
	if ce.TxnID != "tx-1" {
		t.Fatalf("txn passthrough: %q", ce.TxnID)
	}
}

func TestGet(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets/v1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, client.StoredItem{Kind: "vnet", Name: "v1", Namespace: "default", Generation: 5, Spec: json.RawMessage(`{"vni":1}`)})
	})
	it, err := f.c.Get(context.Background(), "", "vnet", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != "vnet" || it.Generation != 5 {
		t.Fatalf("%+v", it)
	}
}

func TestGetFillsMissingFields(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets/v1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"generation": 1, "spec": map[string]int{"vni": 1}})
	})
	it, err := f.c.Get(context.Background(), "", "vnet", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if it.Kind != "vnet" || it.Name != "v1" || it.Namespace != "default" {
		t.Fatalf("%+v", it)
	}
}

func TestGetBadKindOrEmptyName(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.Get(context.Background(), "", "ghost", "x"); err == nil {
		t.Fatal()
	}
	if _, err := f.c.Get(context.Background(), "", "vnet", ""); err == nil {
		t.Fatal()
	}
}

func TestList(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"items": []client.StoredItem{
				{Kind: "vnet", Name: "a", Spec: json.RawMessage(`{"vni":1,"labels":{"tier":"prod"}}`)},
				{Kind: "vnet", Name: "b", Spec: json.RawMessage(`{"vni":2,"labels":{"tier":"dev"}}`)},
				{Kind: "vnet", Name: "c", Spec: json.RawMessage(`{"vni":3,"labels":{"tier":"prod"}}`)},
			},
		})
	})
	items, err := f.c.List(context.Background(), "", "vnet", client.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatal()
	}
	items, err = f.c.List(context.Background(), "", "vnet", client.ListOptions{Selector: "tier=prod"})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Name != "a" || items[1].Name != "c" {
		t.Fatalf("selector filter wrong: %+v", items)
	}
	items, err = f.c.List(context.Background(), "", "vnet", client.ListOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatal()
	}
}

func TestListBadSelector(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []any{}})
	})
	if _, err := f.c.List(context.Background(), "", "vnet", client.ListOptions{Selector: "!"}); err == nil {
		t.Fatal()
	}
}

func TestListBadKind(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.List(context.Background(), "", "ghost", client.ListOptions{}); err == nil {
		t.Fatal()
	}
}

func TestDelete(t *testing.T) {
	f := newFixture(t)
	deleted := false
	f.apiMux.HandleFunc("/v1/default/vnets/v1", func(w http.ResponseWriter, r *http.Request) {
		deleted = true
		w.WriteHeader(204)
	})
	if err := f.c.Delete(context.Background(), "", "vnet", "v1", client.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal()
	}
}

func TestDeleteIgnoreNotFound(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/default/vnets/v1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 404, map[string]string{"error": "not found"})
	})
	if err := f.c.Delete(context.Background(), "", "vnet", "v1", client.DeleteOptions{IgnoreNotFound: true}); err != nil {
		t.Fatalf("ignore-not-found: %v", err)
	}
	if err := f.c.Delete(context.Background(), "", "vnet", "v1", client.DeleteOptions{}); err == nil {
		t.Fatal("without flag → must error")
	}
}

func TestDeleteBadKindOrName(t *testing.T) {
	f := newFixture(t)
	if err := f.c.Delete(context.Background(), "", "ghost", "x", client.DeleteOptions{}); err == nil {
		t.Fatal()
	}
	if err := f.c.Delete(context.Background(), "", "vnet", "", client.DeleteOptions{}); err == nil {
		t.Fatal()
	}
}

// ------- Reconcile -------

func TestReconcileAll(t *testing.T) {
	f := newFixture(t)
	called := false
	f.apiMux.HandleFunc("/v1/reconcile", func(w http.ResponseWriter, r *http.Request) {
		called = true
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	if err := f.c.Reconcile(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal()
	}
}

func TestReconcilePerDpu(t *testing.T) {
	f := newFixture(t)
	var body []byte
	f.apiMux.HandleFunc("/v1/reconcile", func(w http.ResponseWriter, r *http.Request) {
		body, _ = io.ReadAll(r.Body)
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	if err := f.c.Reconcile(context.Background(), []string{"dpu-1", "dpu-2"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "dpu-1") {
		t.Fatalf("body missing dpu ids: %s", body)
	}
}

// ------- Admin drift / placement -------

func TestAdminDrift(t *testing.T) {
	f := newFixture(t)
	f.adminMux.HandleFunc("/admin/drift", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dpu") != "dpu-0" {
			t.Fatalf("dpu param missing: %v", r.URL.Query())
		}
		writeJSON(w, 200, map[string]any{"items": []client.DriftItem{{DpuID: "dpu-0", Op: "add", Kind: "vnet", Key: "v1"}}})
	})
	items, err := f.c.AdminDrift(context.Background(), "dpu-0")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatal()
	}
}

func TestAdminDriftEmptyDpu(t *testing.T) {
	f := newFixture(t)
	if _, err := f.c.AdminDrift(context.Background(), ""); err == nil {
		t.Fatal()
	}
}

func TestAdminEniPlacement(t *testing.T) {
	f := newFixture(t)
	f.adminMux.HandleFunc("/admin/eni-placement", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"items": []client.EniPlacementRow{{EniName: "e1", DpuID: "dpu-0", Observed: true}}})
	})
	items, err := f.c.AdminEniPlacement(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].EniName != "e1" {
		t.Fatal()
	}
}

// ------- Error classification corner cases -------

func TestClassifyHTTPErrorWithRawBody(t *testing.T) {
	err := classifyHTTPError(500, []byte("kaboom"))
	var ce *pkgerrors.Error
	if !asErr(err, &ce) {
		t.Fatal()
	}
	if ce.Reason != "kaboom" {
		t.Fatalf("reason: %q", ce.Reason)
	}
}

func TestClassifyHTTPErrorWithEmptyBody(t *testing.T) {
	err := classifyHTTPError(404, nil)
	var ce *pkgerrors.Error
	if !asErr(err, &ce) || ce.Code != pkgerrors.CodeNotFound {
		t.Fatal()
	}
}

func TestRequestSetsAuthHeader(t *testing.T) {
	gotAuth := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		writeJSON(w, 200, map[string]bool{"ok": true})
	}))
	defer srv.Close()
	rc := &config.ResolvedConfig{Endpoint: srv.URL, AdminEndpoint: srv.URL, Transport: config.TransportREST, Token: "tkn", Timeout: time.Second}
	cl, _ := New(context.Background(), rc)
	defer cl.Close()
	_ = cl.Reconcile(context.Background(), nil)
	if gotAuth != "Bearer tkn" {
		t.Fatalf("auth header: %q", gotAuth)
	}
}

func TestRequestSetsUserAgent(t *testing.T) {
	gotUA := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		writeJSON(w, 200, map[string]any{})
	}))
	defer srv.Close()
	rc := &config.ResolvedConfig{Endpoint: srv.URL, AdminEndpoint: srv.URL, Transport: config.TransportREST, Timeout: time.Second}
	cl, _ := New(context.Background(), rc)
	defer cl.Close()
	_ = cl.Reconcile(context.Background(), nil)
	if gotUA != UserAgent {
		t.Fatalf("user agent: %q", gotUA)
	}
}

func TestRequestRespectsCtxTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, 200, map[string]any{})
	}))
	defer srv.Close()
	rc := &config.ResolvedConfig{Endpoint: srv.URL, AdminEndpoint: srv.URL, Transport: config.TransportREST, Timeout: 30 * time.Millisecond}
	cl, _ := New(context.Background(), rc)
	defer cl.Close()
	if err := cl.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestRequestPropagatesContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(150 * time.Millisecond)
		writeJSON(w, 200, map[string]any{})
	}))
	defer srv.Close()
	rc := &config.ResolvedConfig{Endpoint: srv.URL, AdminEndpoint: srv.URL, Transport: config.TransportREST, Timeout: 0}
	cl, _ := New(context.Background(), rc)
	defer cl.Close()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	err := cl.Reconcile(ctx, nil)
	if err == nil {
		t.Fatal("expected cancellation error")
	}
}

func TestPathHelpers(t *testing.T) {
	if pathForKind("vnets", "", "v1") != "/v1/default/vnets/v1" {
		t.Fatal()
	}
	if pathForKind("vnets", "ns", "v1") != "/v1/ns/vnets/v1" {
		t.Fatal()
	}
	if pathForKindList("vnets", "") != "/v1/default/vnets" {
		t.Fatal()
	}
}

func TestNsOrDefault(t *testing.T) {
	if nsOrDefault("") != "default" || nsOrDefault("x") != "x" {
		t.Fatal()
	}
}

// Ensure factory registration happened (init() side effect).
func TestRESTFactoryRegistered(t *testing.T) {
	rc := &config.ResolvedConfig{Endpoint: "http://localhost", Transport: config.TransportREST}
	cl, err := client.Dial(context.Background(), rc)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
}

// asErr should also work when the *pkgerrors.Error is nested.
func TestAsErrNested(t *testing.T) {
	root := pkgerrors.New(pkgerrors.CodeConflict, "x")
	wrapped := fmt.Errorf("outer: %w", root)
	var got *pkgerrors.Error
	if !asErr(wrapped, &got) {
		t.Fatal()
	}
}

// asErr on plain non-Error returns false.
func TestAsErrNonError(t *testing.T) {
	var got *pkgerrors.Error
	if asErr(stderrors.New("x"), &got) {
		t.Fatal("non-Error misclassified")
	}
}

// Make sure DecodeResponse error path is exercised.
func TestDecodeResponseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("not-json"))
	}))
	defer srv.Close()
	rc := &config.ResolvedConfig{Endpoint: srv.URL, AdminEndpoint: srv.URL, Transport: config.TransportREST, Timeout: time.Second}
	cl, _ := New(context.Background(), rc)
	defer cl.Close()
	_, err := cl.GetInventory(context.Background())
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClose(t *testing.T) {
	f := newFixture(t)
	if err := f.c.Close(); err != nil {
		t.Fatal()
	}
}
