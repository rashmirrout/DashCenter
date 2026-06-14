// Mutation-aware cache invalidation.
//
// When the BFF proxy handles a successful PUT/POST/DELETE to dashd,
// the InvalidationMiddleware intercepts the response and flushes
// related cache entries so subsequent reads reflect the mutation.
//
// See dashw-web-scale-design-req-analysis.md §3.4 for the full map.
package cache

import (
	"net/http"
	"strings"
)

// InvalidationRule maps a URL path pattern to cache key prefixes that
// should be flushed when a mutation to that path succeeds.
type InvalidationRule struct {
	// PathContains is a substring match against the request URL path.
	PathContains string
	// Prefixes lists cache key prefixes to invalidate.
	// A prefix of "*" triggers a full cache flush.
	Prefixes []string
}

// DefaultInvalidationRules returns the standard mutation → cache
// invalidation map for the dashw BFF.
//
// Mapping logic (from scale analysis §3.4):
//   - PUT/DELETE vnets   → fleet/summary, topology, vnet/, capacity
//   - PUT/DELETE enis    → fleet/summary, topology, dpu/, vnet/, capacity
//   - PUT/DELETE acl-policies   → fleet/summary, vnet/
//   - PUT/DELETE route-policies → fleet/summary, vnet/
//   - PUT/DELETE service-tunnels → vnet/
//   - PUT/DELETE ha      → fleet/summary
//   - POST reconcile     → full flush
//   - POST drain/cordon  → fleet/summary, dpu/, topology
func DefaultInvalidationRules() []InvalidationRule {
	return []InvalidationRule{
		// Vnet mutations
		{PathContains: "/vnets", Prefixes: []string{
			"fleet/summary", "topology", "vnet/", "capacity",
		}},
		// ENI mutations
		{PathContains: "/enis", Prefixes: []string{
			"fleet/summary", "topology", "dpu/", "vnet/", "capacity",
		}},
		// ACL policy mutations
		{PathContains: "/acl-policies", Prefixes: []string{
			"fleet/summary", "vnet/",
		}},
		// Route policy mutations
		{PathContains: "/route-policies", Prefixes: []string{
			"fleet/summary", "vnet/",
		}},
		// Service tunnel mutations
		{PathContains: "/service-tunnels", Prefixes: []string{
			"vnet/",
		}},
		// Vnet mapping mutations
		{PathContains: "/vnet-mappings", Prefixes: []string{
			"vnet/",
		}},
		// HA set mutations
		{PathContains: "/ha", Prefixes: []string{
			"fleet/summary",
		}},
		// Reconcile → flush everything
		{PathContains: "/reconcile", Prefixes: []string{"*"}},
		// Drain / cordon / uncordon
		{PathContains: "/drain", Prefixes: []string{
			"fleet/summary", "dpu/", "topology",
		}},
		{PathContains: "/cordon", Prefixes: []string{
			"fleet/summary", "dpu/", "topology",
		}},
	}
}

// Invalidator holds the cache reference and invalidation rules.
type Invalidator struct {
	cache *Cache
	rules []InvalidationRule
}

// NewInvalidator creates an Invalidator with the default rules.
func NewInvalidator(c *Cache) *Invalidator {
	return &Invalidator{
		cache: c,
		rules: DefaultInvalidationRules(),
	}
}

// InvalidateForPath flushes cache entries that match the given
// request path based on the configured invalidation rules.
func (inv *Invalidator) InvalidateForPath(path string) {
	for _, rule := range inv.rules {
		if strings.Contains(path, rule.PathContains) {
			for _, prefix := range rule.Prefixes {
				inv.cache.InvalidatePattern(prefix)
			}
		}
	}
}

// responseRecorder captures the status code from the upstream response
// so we can decide whether to invalidate after proxying.
type responseRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *responseRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// Middleware returns an http.Handler middleware that intercepts
// successful (2xx) mutating requests (PUT, POST, DELETE) and
// invalidates related cache entries.
//
// Usage:
//
//	handler = invalidator.Middleware(proxyHandler)
func (inv *Invalidator) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only intercept mutating methods
		if r.Method != http.MethodPut && r.Method != http.MethodPost && r.Method != http.MethodDelete {
			next.ServeHTTP(w, r)
			return
		}

		// Wrap the ResponseWriter to capture status code
		rec := &responseRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Only invalidate on success (2xx)
		if rec.statusCode >= 200 && rec.statusCode < 300 {
			inv.InvalidateForPath(r.URL.Path)
		}
	})
}