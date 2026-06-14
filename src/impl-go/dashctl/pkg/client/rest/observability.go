// observability.go — PE-3c REST methods on Client: counter snapshot
// + SSE stream. Wire format mirrors the gRPC ObservabilityService
// GetCounters envelope (CounterEvent wrapper).

package rest

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	pkgerrors "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
)

// GetCountersSnapshot fetches the current per-DPU CounterReport list.
// dpuIDs is optional; empty = all DPUs. Returns the unwrapped envelope.
func (c *Client) GetCountersSnapshot(ctx context.Context, dpuIDs []string) (*client.CountersSnapshot, error) {
	q := url.Values{}
	for _, id := range dpuIDs {
		if id == "" {
			continue
		}
		q.Add("dpu", id)
	}
	path := "/v1/observability/counters"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}
	// Raw decode: the snapshot envelope is `{"reports":[<RawJSON>...]}`
	// where each entry is a protojson-encoded CounterReport. Decode the
	// envelope first then unmarshal each entry into a typed report.
	var raw struct {
		Reports []json.RawMessage `json:"reports"`
	}
	if err := c.do(ctx, http.MethodGet, c.api(path), nil, &raw); err != nil {
		return nil, err
	}
	out := &client.CountersSnapshot{Reports: make([]*client.CounterReport, 0, len(raw.Reports))}
	for _, b := range raw.Reports {
		r := &client.CounterReport{}
		if err := json.Unmarshal(b, r); err != nil {
			return nil, pkgerrors.Wrap(pkgerrors.CodeUnavailable, "rest: decode CounterReport", err)
		}
		out.Reports = append(out.Reports, r)
	}
	return out, nil
}

// StreamCounters opens the SSE stream and invokes opts.OnEvent for
// every frame until ctx cancel / server EOF / OnEvent sentinel.
//
// The cursor (opts.LastEventID) is sent both as the Last-Event-ID
// header AND as ?last_event_id= so it survives any reverse proxy that
// strips one. Multiple opts.DpuIDs serialise as repeated ?dpu=
// parameters (matches the dashd handler).
func (c *Client) StreamCounters(ctx context.Context, opts client.CountersWatchOptions) error {
	if opts.OnEvent == nil {
		return pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: StreamCounters requires OnEvent")
	}
	q := url.Values{}
	for _, id := range opts.DpuIDs {
		if id == "" {
			continue
		}
		q.Add("dpu", id)
	}
	if opts.LastEventID > 0 {
		q.Set("last_event_id", strconv.FormatUint(opts.LastEventID, 10))
	}
	path := "/v1/observability/counters/stream"
	if encoded := q.Encode(); encoded != "" {
		path += "?" + encoded
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.api(path), nil)
	if err != nil {
		return pkgerrors.Wrap(pkgerrors.CodeUnavailable, "rest: build counter stream request", err)
	}
	req.Header.Set("Accept", "text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if opts.LastEventID > 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(opts.LastEventID, 10))
	}

	streamHTTP := &http.Client{Transport: c.httpc.Transport}
	resp, err := streamHTTP.Do(req)
	if err != nil {
		return pkgerrors.Wrap(pkgerrors.CodeUnavailable, "rest: open counter stream", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return pkgerrors.New(pkgerrors.CodeUnavailable,
			fmt.Sprintf("rest: counter stream HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body))))
	}

	rd := bufio.NewReaderSize(resp.Body, 64*1024)
	var dataBuf strings.Builder
	for {
		line, err := rd.ReadString('\n')
		if err != nil {
			if err == io.EOF && dataBuf.Len() == 0 {
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return pkgerrors.Wrap(pkgerrors.CodeUnavailable, "rest: counter stream read", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if dataBuf.Len() == 0 {
				continue
			}
			var ev client.CounterEvent
			if jerr := json.Unmarshal([]byte(dataBuf.String()), &ev); jerr == nil {
				if cberr := opts.OnEvent(ev); cberr != nil {
					return cberr
				}
			}
			dataBuf.Reset()
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		const dataPrefix = "data:"
		if strings.HasPrefix(line, dataPrefix) {
			payload := strings.TrimPrefix(line, dataPrefix)
			payload = strings.TrimPrefix(payload, " ")
			if dataBuf.Len() > 0 {
				dataBuf.WriteByte('\n')
			}
			dataBuf.WriteString(payload)
			continue
		}
		// id:, event:, retry: are SSE metadata; the JSON body carries
		// equivalents in the wrapper envelope, so we drop them.
	}
}

// GetCounterDetails fetches per-DPU rollup plus per-ENI / per-VNET
// sub-rollups for dpuID. Returns ErrNotFound when the dpu_id is not
// in the cache (never polled, or just cleared).
func (c *Client) GetCounterDetails(ctx context.Context, dpuID string) (*client.CounterDetails, error) {
	if dpuID == "" {
		return nil, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: GetCounterDetails requires dpuID")
	}
	out := &client.CounterDetails{}
	path := fmt.Sprintf("/v1/observability/counters/%s/details", url.PathEscape(dpuID))
	if err := c.do(ctx, http.MethodGet, c.api(path), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClearCounters wipes every cached entry on dashd. Returns the count
// of entries removed. Idempotent: a second call returns 0.
func (c *Client) ClearCounters(ctx context.Context) (int, error) {
	var reply struct {
		Cleared int `json:"cleared"`
	}
	if err := c.do(ctx, http.MethodDelete, c.api("/v1/observability/counters"), nil, &reply); err != nil {
		return 0, err
	}
	return reply.Cleared, nil
}

// ClearCounter wipes the cached entry for dpuID. Returns true when an
// entry was present (200), false on 404 — the latter is returned
// without a wrapped error so callers can render "nothing to clear"
// cleanly without losing exit-code distinctions on real failures.
func (c *Client) ClearCounter(ctx context.Context, dpuID string) (bool, error) {
	if dpuID == "" {
		return false, pkgerrors.New(pkgerrors.CodeInvalidArgument, "rest: ClearCounter requires dpuID")
	}
	path := fmt.Sprintf("/v1/observability/counters/%s", url.PathEscape(dpuID))
	var reply struct {
		Cleared bool `json:"cleared"`
	}
	err := c.do(ctx, http.MethodDelete, c.api(path), nil, &reply)
	if err != nil {
		var ce *pkgerrors.Error
		if errors.As(err, &ce) && ce.Code == pkgerrors.CodeNotFound {
			return false, nil
		}
		return false, err
	}
	return reply.Cleared, nil
}
