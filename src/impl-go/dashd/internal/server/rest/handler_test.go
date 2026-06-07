package rest

import (
"encoding/json"
"net/http"
"net/http/httptest"
"strings"
"testing"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
)

func setupTestServer(t *testing.T) *httptest.Server {
t.Helper()
dir := t.TempDir()
fs, err := filstore.Open(dir)
if err != nil {
t.Fatalf("Open: %v", err)
}
t.Cleanup(func() { fs.Close() })

inv := inventory.New()
srv := New(fs, inv, nil)
return httptest.NewServer(srv.srv.Handler)
}

// 1. PUT /v1/vnets/v1 → 200; GET returns same
func TestPutGetVnet(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

// PUT
resp := doReq(t, ts, "PUT", "/v1/vnets/v1", `{"vni":100}`)
if resp.StatusCode != 200 {
t.Fatalf("PUT expected 200, got %d", resp.StatusCode)
}
var ack map[string]any
json.NewDecoder(resp.Body).Decode(&ack)
if ack["accepted"] != true {
t.Error("expected accepted=true")
}

// GET
resp2 := doReq(t, ts, "GET", "/v1/vnets/v1", "")
if resp2.StatusCode != 200 {
t.Fatalf("GET expected 200, got %d", resp2.StatusCode)
}
}

// 2. PUT with malformed JSON → 400
func TestPutMalformedJSON(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

resp := doReq(t, ts, "PUT", "/v1/vnets/v1", `{broken`)
if resp.StatusCode != 400 {
t.Errorf("expected 400, got %d", resp.StatusCode)
}
}

// 3. GET non-existent → 404
func TestGetNotFound(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

resp := doReq(t, ts, "GET", "/v1/vnets/nope", "")
if resp.StatusCode != 404 {
t.Errorf("expected 404, got %d", resp.StatusCode)
}
}

// 4. DELETE → 204
func TestDelete(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

doReq(t, ts, "PUT", "/v1/vnets/v1", `{"vni":100}`)
resp := doReq(t, ts, "DELETE", "/v1/vnets/v1", "")
if resp.StatusCode != 204 {
t.Errorf("expected 204, got %d", resp.StatusCode)
}
}

// 5. POST /v1/reconcile → 200
func TestReconcile(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

resp := doReq(t, ts, "POST", "/v1/reconcile", "")
if resp.StatusCode != 200 {
t.Errorf("expected 200, got %d", resp.StatusCode)
}
}

// 6. PUT /v1/inventory + GET
func TestPutGetInventory(t *testing.T) {
ts := setupTestServer(t)
defer ts.Close()

resp := doReq(t, ts, "PUT", "/v1/inventory",
`{"dpus":[{"id":"dpu-0","endpoint":"localhost:50051"}]}`)
if resp.StatusCode != 200 {
t.Fatalf("PUT inventory expected 200, got %d", resp.StatusCode)
}

resp2 := doReq(t, ts, "GET", "/v1/inventory", "")
if resp2.StatusCode != 200 {
t.Fatalf("GET inventory expected 200, got %d", resp2.StatusCode)
}
}

func doReq(t *testing.T, ts *httptest.Server, method, path, body string) *http.Response {
t.Helper()
var reader *strings.Reader
if body != "" {
reader = strings.NewReader(body)
} else {
reader = strings.NewReader("")
}
req, err := http.NewRequest(method, ts.URL+path, reader)
if err != nil {
t.Fatalf("NewRequest: %v", err)
}
req.Header.Set("Content-Type", "application/json")
resp, err := http.DefaultClient.Do(req)
if err != nil {
t.Fatalf("Do: %v", err)
}
return resp
}