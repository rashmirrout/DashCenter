// Package rest implements the dashctl Client interface against dashd's
// HTTP REST surface (:8443) and admin surface (:7443). All RPC routes
// are derived from src/impl-go/dashd/internal/server/rest/server.go and
// admin/server.go.
package rest

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	pkgerrors "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

const (
	// UserAgent is sent on every outbound request.
	UserAgent = "dashctl/0.1.0"
)

// init registers the REST factory with the Client.Dial dispatcher.
func init() {
	client.Register(config.TransportREST, New)
}

// New is the factory function for the REST backend.
func New(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error) {
	if rc == nil {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: nil config")
	}
	if rc.Endpoint == "" {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: endpoint required")
	}
	tlsCfg, err := buildTLS(rc.TLS)
	if err != nil {
		return nil, err
	}
	tr := &http.Transport{
		TLSClientConfig:       tlsCfg,
		MaxIdleConns:          16,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	// Per-RPC timeout is enforced via context.WithTimeout in each call,
	// but we set a generous http.Client.Timeout as a hard ceiling.
	timeout := rc.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	httpc := &http.Client{Transport: tr, Timeout: timeout * 2}
	return &Client{
		baseURL:  strings.TrimRight(rc.Endpoint, "/"),
		adminURL: strings.TrimRight(rc.AdminEndpoint, "/"),
		token:    rc.Token,
		httpc:    httpc,
		timeout:  timeout,
	}, nil
}

// Client is the REST backend.
type Client struct {
	baseURL  string
	adminURL string
	token    string
	httpc    *http.Client
	timeout  time.Duration
}

// Compile-time assertion that Client implements client.Client.
var _ client.Client = (*Client)(nil)

// Close releases idle HTTP connections.
func (c *Client) Close() error {
	c.httpc.CloseIdleConnections()
	return nil
}

// --- Health & inventory ---

// Health calls dashd's admin /health endpoint.
func (c *Client) Health(ctx context.Context) (client.HealthReport, error) {
	var out client.HealthReport
	if err := c.do(ctx, http.MethodGet, c.admin("/admin/health"), nil, &out); err != nil {
		return out, err
	}
	return out, nil
}

// ServerInfo returns version/leader using /admin/health as the source.
// dashd does not yet expose a dedicated version RPC; this is best-effort.
func (c *Client) ServerInfo(ctx context.Context) (client.ServerInfo, error) {
	h, err := c.Health(ctx)
	if err != nil {
		return client.ServerInfo{OK: false}, err
	}
	return client.ServerInfo{OK: h.Status != "", Leader: h.Leader, Version: "dashd"}, nil
}

// PutInventory sends a full replace of the DPU inventory.
func (c *Client) PutInventory(ctx context.Context, dpus []client.DpuInput) error {
	body := map[string]any{"dpus": dpus}
	return c.do(ctx, http.MethodPut, c.api("/v1/inventory"), body, nil)
}

// GetInventory fetches the dashd-known inventory.
func (c *Client) GetInventory(ctx context.Context) ([]client.DpuStatus, error) {
	var resp struct {
		Dpus []client.DpuStatus `json:"dpus"`
	}
	if err := c.do(ctx, http.MethodGet, c.api("/v1/inventory"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Dpus, nil
}

// --- Specs (Put/Get/List/Delete) ---

// Put writes a spec. ns may be empty (server-side defaults to "default").
// kind is a registry key (e.g. "vnet"); name is the URL key.
func (c *Client) Put(ctx context.Context, ns, kind, name string, specJSON []byte) (*client.PutResult, error) {
	ki, ok := manifest.LookupKind(kind)
	if !ok {
		return nil, pkgerrors.Newf(pkgerrors.CodeInvalidArgument, "rest: unknown kind %q", kind)
	}
	if name == "" {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: name required")
	}
	path := pathForKind(ki.URLPlural, ns, name)
	var out client.PutResult
	if err := c.doRaw(ctx, http.MethodPut, c.api(path), specJSON, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Get fetches a single spec.
func (c *Client) Get(ctx context.Context, ns, kind, name string) (*client.StoredItem, error) {
	ki, ok := manifest.LookupKind(kind)
	if !ok {
		return nil, pkgerrors.Newf(pkgerrors.CodeInvalidArgument, "rest: unknown kind %q", kind)
	}
	if name == "" {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: name required")
	}
	path := pathForKind(ki.URLPlural, ns, name)
	var out client.StoredItem
	if err := c.do(ctx, http.MethodGet, c.api(path), nil, &out); err != nil {
		return nil, err
	}
	if out.Kind == "" {
		out.Kind = ki.StoreKind
	}
	if out.Name == "" {
		out.Name = name
	}
	if out.Namespace == "" {
		out.Namespace = nsOrDefault(ns)
	}
	return &out, nil
}

// List fetches every spec of one kind in one namespace. Selector is
// applied client-side in Phase 1 (dashd does not yet support server-side
// selectors).
func (c *Client) List(ctx context.Context, ns, kind string, opts client.ListOptions) ([]*client.StoredItem, error) {
	ki, ok := manifest.LookupKind(kind)
	if !ok {
		return nil, pkgerrors.Newf(pkgerrors.CodeInvalidArgument, "rest: unknown kind %q", kind)
	}
	path := pathForKindList(ki.URLPlural, ns)
	var raw struct {
		Items []*client.StoredItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.api(path), nil, &raw); err != nil {
		return nil, err
	}
	out := raw.Items
	if opts.Selector != "" {
		sel, err := ParseSelector(opts.Selector)
		if err != nil {
			return nil, pkgerrors.Wrap(pkgerrors.CodeInvalidArgument, "rest: invalid selector", err)
		}
		out = filterBySelector(out, sel)
	}
	if opts.Limit > 0 && len(out) > opts.Limit {
		out = out[:opts.Limit]
	}
	return out, nil
}

// Delete removes a spec. IgnoreNotFound swallows 404.
func (c *Client) Delete(ctx context.Context, ns, kind, name string, opts client.DeleteOptions) error {
	ki, ok := manifest.LookupKind(kind)
	if !ok {
		return pkgerrors.Newf(pkgerrors.CodeInvalidArgument, "rest: unknown kind %q", kind)
	}
	if name == "" {
		return pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: name required")
	}
	path := pathForKind(ki.URLPlural, ns, name)
	err := c.do(ctx, http.MethodDelete, c.api(path), nil, nil)
	if err == nil {
		return nil
	}
	if opts.IgnoreNotFound {
		var ce *pkgerrors.Error
		if asErr(err, &ce) && ce.Code == pkgerrors.CodeNotFound {
			return nil
		}
	}
	return err
}

// Reconcile triggers a fleet (or per-DPU) reconcile. Phase 1 supports
// only fleet-wide reconcile via dashd's REST or admin endpoint; per-DPU
// is silently ignored (the next sweep covers it). dashd Phase 2 will add
// a dpu_ids request body.
func (c *Client) Reconcile(ctx context.Context, dpuIDs []string) error {
	// dashd REST: POST /v1/reconcile  (always all DPUs in Phase 1).
	// We send {} so future per-DPU bodies are forward-compatible.
	body := map[string]any{}
	if len(dpuIDs) > 0 {
		body["dpu_ids"] = dpuIDs
	}
	return c.do(ctx, http.MethodPost, c.api("/v1/reconcile"), body, nil)
}

// --- Admin views ---

// AdminDrift returns live add/update/remove items for one DPU.
func (c *Client) AdminDrift(ctx context.Context, dpuID string) ([]client.DriftItem, error) {
	if dpuID == "" {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: dpu id required")
	}
	q := url.Values{"dpu": []string{dpuID}}
	var raw struct {
		Items []client.DriftItem `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.admin("/admin/drift?"+q.Encode()), nil, &raw); err != nil {
		return nil, err
	}
	return raw.Items, nil
}

// AdminEniPlacement returns the ENI→DPU placement view.
func (c *Client) AdminEniPlacement(ctx context.Context) ([]client.EniPlacementRow, error) {
	var raw struct {
		Items []client.EniPlacementRow `json:"items"`
	}
	if err := c.do(ctx, http.MethodGet, c.admin("/admin/eni-placement"), nil, &raw); err != nil {
		return nil, err
	}
	return raw.Items, nil
}

// --- HTTP plumbing ---

func (c *Client) api(p string) string   { return c.baseURL + p }
func (c *Client) admin(p string) string { return c.adminURL + p }

// do is the universal request helper for JSON bodies.
func (c *Client) do(ctx context.Context, method, fullURL string, in, out any) error {
	var body []byte
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return pkgerrors.Wrap(pkgerrors.CodeInvalidArgument, "rest: marshal", err)
		}
		body = b
	}
	return c.doRaw(ctx, method, fullURL, body, out)
}

// doRaw is identical to do but accepts a pre-encoded JSON body. Used by
// Put because the spec body is already JSON.
func (c *Client) doRaw(ctx context.Context, method, fullURL string, body []byte, out any) error {
	if c.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.timeout)
		defer cancel()
	}
	var rdr io.Reader
	if len(body) > 0 {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, rdr)
	if err != nil {
		return pkgerrors.Wrap(pkgerrors.CodeInvalidArgument, "rest: build request", err)
	}
	req.Header.Set("Accept", "application/json")
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("User-Agent", UserAgent)
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.httpc.Do(req)
	if err != nil {
		return pkgerrors.Classify(err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20)) // 16 MiB cap
	if resp.StatusCode >= 400 {
		return classifyHTTPError(resp.StatusCode, respBody)
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return pkgerrors.Wrap(pkgerrors.CodeInternal, "rest: decode response", err)
	}
	return nil
}

// classifyHTTPError converts a non-2xx response body into a typed *Error.
// Body shape from dashd: {"error":"...","txn_id":"..."} (txn_id may be absent).
func classifyHTTPError(status int, body []byte) error {
	var parsed struct {
		Error string `json:"error"`
		TxnID string `json:"txn_id"`
	}
	_ = json.Unmarshal(body, &parsed)
	reason := strings.TrimSpace(parsed.Error)
	if reason == "" {
		reason = strings.TrimSpace(string(body))
	}
	err := pkgerrors.FromHTTPStatus(status, reason)
	if parsed.TxnID != "" {
		err = err.WithTxnID(parsed.TxnID)
	}
	return err
}

// --- TLS ---

func buildTLS(t config.TLSConfig) (*tls.Config, error) {
	if t.CAFile == "" && t.CertFile == "" && t.KeyFile == "" && !t.InsecureSkipVerify {
		return nil, nil
	}
	cfg := &tls.Config{InsecureSkipVerify: t.InsecureSkipVerify} //nolint:gosec — explicit user opt-in
	if t.CAFile != "" {
		pem, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, pkgerrors.Wrap(pkgerrors.CodeInvalidArgument, "rest: read CA", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(pem) {
			return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: invalid CA pem")
		}
		cfg.RootCAs = pool
	}
	if t.CertFile != "" || t.KeyFile != "" {
		if t.CertFile == "" || t.KeyFile == "" {
			return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: both cert-file and key-file required for mTLS")
		}
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, pkgerrors.Wrap(pkgerrors.CodeInvalidArgument, "rest: load client cert", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

// --- Path helpers ---

func nsOrDefault(ns string) string {
	if ns == "" {
		return config.DefaultNamespace
	}
	return ns
}

func pathForKind(plural, ns, name string) string {
	ns = nsOrDefault(ns)
	return fmt.Sprintf("/v1/%s/%s/%s", url.PathEscape(ns), plural, url.PathEscape(name))
}

func pathForKindList(plural, ns string) string {
	ns = nsOrDefault(ns)
	return fmt.Sprintf("/v1/%s/%s", url.PathEscape(ns), plural)
}

// asErr is errors.As with a single allocation site (so the public API does
// not need to import "errors" in five places).
func asErr(err error, target **pkgerrors.Error) bool {
	for cur := err; cur != nil; {
		if e, ok := cur.(*pkgerrors.Error); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := cur.(unwrapper)
		if !ok {
			break
		}
		cur = u.Unwrap()
	}
	return false
}
