package cache

import (
"net/http"
"net/http/httptest"
"testing"
"time"
)

func TestDefaultInvalidationRules_HasExpectedRules(t *testing.T) {
rules := DefaultInvalidationRules()
if len(rules) < 10 {
t.Errorf("expected at least 10 rules, got %d", len(rules))
}
}

func TestInvalidateForPath_Vnets(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

// Populate cache entries
c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)
c.Set("topology", []byte("topo"), 10*time.Second, 30*time.Second)
c.Set("vnet/detail:prod", []byte("vd"), 10*time.Second, 30*time.Second)
c.Set("capacity", []byte("cap"), 10*time.Second, 30*time.Second)
c.Set("dpu/detail:dpu-1", []byte("dd"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
inv.InvalidateForPath("/api/v1/default/vnets/my-vnet")

// These should be invalidated
if _, status, _ := c.Get("fleet/summary"); status != Miss {
t.Error("fleet/summary should be invalidated after vnet mutation")
}
if _, status, _ := c.Get("topology"); status != Miss {
t.Error("topology should be invalidated after vnet mutation")
}
if _, status, _ := c.Get("vnet/detail:prod"); status != Miss {
t.Error("vnet/detail:prod should be invalidated after vnet mutation")
}
if _, status, _ := c.Get("capacity"); status != Miss {
t.Error("capacity should be invalidated after vnet mutation")
}

// DPU should NOT be invalidated by vnet mutation
if _, status, _ := c.Get("dpu/detail:dpu-1"); status != Hit {
t.Error("dpu/detail:dpu-1 should NOT be invalidated by vnet mutation")
}
}

func TestInvalidateForPath_Enis(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)
c.Set("dpu/detail:dpu-1", []byte("dd"), 10*time.Second, 30*time.Second)
c.Set("vnet/detail:prod", []byte("vd"), 10*time.Second, 30*time.Second)
c.Set("capacity", []byte("cap"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
inv.InvalidateForPath("/api/v1/default/enis/eni-01")

if _, status, _ := c.Get("fleet/summary"); status != Miss {
t.Error("fleet/summary should be invalidated after ENI mutation")
}
if _, status, _ := c.Get("dpu/detail:dpu-1"); status != Miss {
t.Error("dpu/detail should be invalidated after ENI mutation")
}
if _, status, _ := c.Get("capacity"); status != Miss {
t.Error("capacity should be invalidated after ENI mutation")
}
}

func TestInvalidateForPath_Reconcile(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)
c.Set("topology", []byte("topo"), 10*time.Second, 30*time.Second)
c.Set("dpu/detail:dpu-1", []byte("dd"), 10*time.Second, 30*time.Second)
c.Set("vnet/detail:prod", []byte("vd"), 10*time.Second, 30*time.Second)
c.Set("capacity", []byte("cap"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
inv.InvalidateForPath("/api/v1/reconcile")

// Reconcile flushes everything
if _, status, _ := c.Get("fleet/summary"); status != Miss {
t.Error("fleet/summary should be invalidated after reconcile")
}
if _, status, _ := c.Get("topology"); status != Miss {
t.Error("topology should be invalidated after reconcile")
}
if _, status, _ := c.Get("dpu/detail:dpu-1"); status != Miss {
t.Error("dpu/detail should be invalidated after reconcile")
}
if _, status, _ := c.Get("capacity"); status != Miss {
t.Error("capacity should be invalidated after reconcile")
}
}

func TestInvalidateForPath_NoMatchDoesNothing(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
inv.InvalidateForPath("/api/admin/health")

if _, status, _ := c.Get("fleet/summary"); status != Hit {
t.Error("fleet/summary should NOT be invalidated by a health check")
}
}

func TestMiddleware_SkipsGET(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
})

handler := inv.Middleware(backend)

req := httptest.NewRequest(http.MethodGet, "/api/v1/default/vnets", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

// GET should not invalidate
if _, status, _ := c.Get("fleet/summary"); status != Hit {
t.Error("GET should not trigger cache invalidation")
}
}

func TestMiddleware_InvalidatesOnPUT200(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)
c.Set("vnet/detail:x", []byte("vd"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
})

handler := inv.Middleware(backend)

req := httptest.NewRequest(http.MethodPut, "/api/v1/default/vnets/my-vnet", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if _, status, _ := c.Get("fleet/summary"); status != Miss {
t.Error("PUT 200 should invalidate fleet/summary")
}
if _, status, _ := c.Get("vnet/detail:x"); status != Miss {
t.Error("PUT 200 should invalidate vnet/detail:x")
}
}

func TestMiddleware_SkipsOn500(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusInternalServerError)
})

handler := inv.Middleware(backend)

req := httptest.NewRequest(http.MethodPut, "/api/v1/default/vnets/my-vnet", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

// 500 should not invalidate
if _, status, _ := c.Get("fleet/summary"); status != Hit {
t.Error("PUT 500 should not trigger cache invalidation")
}
}

func TestMiddleware_InvalidatesOnDELETE(t *testing.T) {
c := New(time.Hour)
defer c.Stop()

c.Set("fleet/summary", []byte("fs"), 10*time.Second, 30*time.Second)

inv := NewInvalidator(c)
backend := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
w.WriteHeader(http.StatusOK)
})

handler := inv.Middleware(backend)

req := httptest.NewRequest(http.MethodDelete, "/api/v1/default/enis/eni-01", nil)
rec := httptest.NewRecorder()
handler.ServeHTTP(rec, req)

if _, status, _ := c.Get("fleet/summary"); status != Miss {
t.Error("DELETE 200 should invalidate fleet/summary")
}
}