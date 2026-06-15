package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteThrottleMiddleware_GETPassesThrough(t *testing.T) {
	handler := writeThrottleMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// GETs should never be throttled, even beyond the rate limit.
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/v1/default/vnets", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("GET request %d: got %d, want 200", i, rr.Code)
		}
	}
}

func TestWriteThrottleMiddleware_PUTAllowedWithinLimit(t *testing.T) {
	// Rate limit of 100 RPS — a single PUT should always pass.
	handler := writeThrottleMiddleware(100)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPut, "/v1/default/vnets/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("first PUT: got %d, want 200", rr.Code)
	}
}

func TestWriteThrottleMiddleware_PUTThrottledBeyondLimit(t *testing.T) {
	// Rate limit of 1 RPS with burst=1. First PUT passes, second is throttled.
	handler := writeThrottleMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First PUT — consumes the token.
	req1 := httptest.NewRequest(http.MethodPut, "/v1/default/vnets/a", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("first PUT: got %d, want 200", rr1.Code)
	}

	// Second PUT — no tokens left, should be throttled.
	req2 := httptest.NewRequest(http.MethodPut, "/v1/default/vnets/b", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second PUT: got %d, want 429", rr2.Code)
	}
	if rr2.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After header = %q, want %q", rr2.Header().Get("Retry-After"), "1")
	}
}

func TestWriteThrottleMiddleware_AllMutatingMethods(t *testing.T) {
	// Rate limit of 1 — each method should consume the token.
	methods := []string{http.MethodPut, http.MethodPost, http.MethodDelete, http.MethodPatch}

	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			handler := writeThrottleMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			// First: passes.
			req1 := httptest.NewRequest(method, "/v1/test", nil)
			rr1 := httptest.NewRecorder()
			handler.ServeHTTP(rr1, req1)
			if rr1.Code != http.StatusOK {
				t.Errorf("first %s: got %d, want 200", method, rr1.Code)
			}

			// Second: throttled.
			req2 := httptest.NewRequest(method, "/v1/test", nil)
			rr2 := httptest.NewRecorder()
			handler.ServeHTTP(rr2, req2)
			if rr2.Code != http.StatusTooManyRequests {
				t.Errorf("second %s: got %d, want 429", method, rr2.Code)
			}
		})
	}
}

func TestWriteThrottleMiddleware_GETNotAffectedByWriteThrottle(t *testing.T) {
	// Exhaust the write token, then verify GET still passes.
	handler := writeThrottleMiddleware(1)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Exhaust the token with a PUT.
	put := httptest.NewRequest(http.MethodPut, "/v1/test", nil)
	handler.ServeHTTP(httptest.NewRecorder(), put)

	// GET should still pass.
	get := httptest.NewRequest(http.MethodGet, "/v1/test", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, get)
	if rr.Code != http.StatusOK {
		t.Errorf("GET after PUT exhaustion: got %d, want 200", rr.Code)
	}
}

func TestDefaultWriteRateLimit(t *testing.T) {
	if defaultWriteRateLimit != 200 {
		t.Errorf("defaultWriteRateLimit = %v, want 200", defaultWriteRateLimit)
	}
}
