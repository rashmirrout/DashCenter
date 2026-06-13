// dpu_counters.go renders dashapi.v1.DpuCountersResponse for the
// `dash-sim-client dpu-counters` CLI (PE-3a / PE-G8).
//
// Three text formats supported, each tuned to a different operator
// workflow:
//
//	FormatTable  — single-screen ASCII table, default for interactive use.
//	FormatJSON   — full nested response, for scripting + machine pipelines.
//	FormatYAML   — same content as JSON, friendlier for inline edits + grep.
//	FormatCSV    — flat rows, one per scope; ideal for piping into Excel
//	               or any spreadsheet-shaped sink (per the PE-3a request).
//
// FormatCSV is defined here (rather than in render.go) because it is
// specific to the rollup shape — generic Objects rendering doesn't need
// CSV, and adding it to ParseFormat lets the dpu-counters cmd accept it
// via the standard -o flag.

package render

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"gopkg.in/yaml.v3"
)

// FormatCSV is the comma-separated-value text form. ParseFormat recognises
// "csv" alongside "json"/"yaml"/"table".
const FormatCSV Format = "csv"

// ParseFormatExt is identical to ParseFormat but additionally recognises
// "csv". Existing callers that don't need CSV stay on ParseFormat. The
// dpu-counters CLI uses this so `-o csv` is wired through.
func ParseFormatExt(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "csv":
		return FormatCSV, nil
	}
	return ParseFormat(s)
}

// DpuCounters renders the full GetDpuCounters response. Pretty-prints the
// DPU header (device id, sample time) followed by the DPU-wide bucket,
// then opt-in per-ENI and per-VNET tables when those sections are
// populated.
//
// For JSON/YAML formats we emit a minimal flat envelope (rather than the
// raw protojson) so downstream scripts get a stable, schema-light shape.
func DpuCounters(w io.Writer, format Format, resp *dashapi.DpuCountersResponse) error {
	if resp == nil {
		_, err := fmt.Fprintln(w, "(nil)")
		return err
	}
	env := envelopeDpu(resp)
	switch format {
	case FormatJSON:
		b, err := json.MarshalIndent(env, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	case FormatYAML:
		b, err := yaml.Marshal(env)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, string(b))
		return err
	case FormatCSV:
		return writeDpuCSV(w, resp)
	case FormatTable, "":
		return writeDpuTable(w, resp)
	}
	return fmt.Errorf("unsupported format %q", format)
}

// envelopeDpu is the stable JSON/YAML shape consumed by operator scripts.
// Field names mirror the proto's snake_case to keep the contract obvious.
type dpuEnvelope struct {
	DeviceID    string             `json:"device_id" yaml:"device_id"`
	SampledAt   string             `json:"sampled_at" yaml:"sampled_at"`
	SampledAtNs int64              `json:"sampled_at_ns" yaml:"sampled_at_ns"`
	Dpu         dpuBucketEnvelope  `json:"dpu" yaml:"dpu"`
	Enis        []scopedEnvelope   `json:"enis,omitempty" yaml:"enis,omitempty"`
	Vnets       []scopedEnvelope   `json:"vnets,omitempty" yaml:"vnets,omitempty"`
}

type dpuBucketEnvelope struct {
	PacketsIn  int64 `json:"packets_in" yaml:"packets_in"`
	PacketsOut int64 `json:"packets_out" yaml:"packets_out"`
	BytesIn    int64 `json:"bytes_in" yaml:"bytes_in"`
	BytesOut   int64 `json:"bytes_out" yaml:"bytes_out"`
	Drops      int64 `json:"drops" yaml:"drops"`
}

type scopedEnvelope struct {
	ScopeKey string            `json:"scope_key" yaml:"scope_key"`
	Bucket   dpuBucketEnvelope `json:"bucket" yaml:"bucket"`
}

func envelopeDpu(resp *dashapi.DpuCountersResponse) dpuEnvelope {
	env := dpuEnvelope{
		DeviceID:    resp.GetDeviceId(),
		SampledAtNs: resp.GetSampledAtNs(),
		SampledAt:   time.Unix(0, resp.GetSampledAtNs()).UTC().Format(time.RFC3339Nano),
		Dpu:         envelopeBucket(resp.GetDpu()),
	}
	for _, e := range resp.GetEnis() {
		env.Enis = append(env.Enis, scopedEnvelope{
			ScopeKey: e.GetScopeKey(),
			Bucket:   envelopeBucket(e.GetBucket()),
		})
	}
	for _, v := range resp.GetVnets() {
		env.Vnets = append(env.Vnets, scopedEnvelope{
			ScopeKey: v.GetScopeKey(),
			Bucket:   envelopeBucket(v.GetBucket()),
		})
	}
	return env
}

func envelopeBucket(b *dashapi.CounterBucket) dpuBucketEnvelope {
	return dpuBucketEnvelope{
		PacketsIn:  b.GetPacketsIn(),
		PacketsOut: b.GetPacketsOut(),
		BytesIn:    b.GetBytesIn(),
		BytesOut:   b.GetBytesOut(),
		Drops:      b.GetDrops(),
	}
}

// writeDpuTable emits the human-friendly ASCII table layout. Three
// optional sub-tables — DPU-wide always shown; per-ENI + per-VNET shown
// only when non-empty.
func writeDpuTable(w io.Writer, resp *dashapi.DpuCountersResponse) error {
	_, _ = fmt.Fprintf(w, "DEVICE  %s\n", orDash(resp.GetDeviceId()))
	_, _ = fmt.Fprintf(w, "TIME    %s (ns=%d)\n",
		time.Unix(0, resp.GetSampledAtNs()).UTC().Format(time.RFC3339Nano),
		resp.GetSampledAtNs())

	_, _ = fmt.Fprintln(w, "\nDPU TOTALS")
	if err := writeTable(w, bucketCols(), [][]string{bucketRow("dpu", resp.GetDpu())}); err != nil {
		return err
	}
	if len(resp.GetEnis()) > 0 {
		_, _ = fmt.Fprintln(w, "\nPER-ENI")
		rows := make([][]string, 0, len(resp.GetEnis()))
		for _, e := range resp.GetEnis() {
			rows = append(rows, bucketRow(e.GetScopeKey(), e.GetBucket()))
		}
		if err := writeTable(w, bucketCols(), rows); err != nil {
			return err
		}
	}
	if len(resp.GetVnets()) > 0 {
		_, _ = fmt.Fprintln(w, "\nPER-VNET")
		rows := make([][]string, 0, len(resp.GetVnets()))
		for _, v := range resp.GetVnets() {
			rows = append(rows, bucketRow(v.GetScopeKey(), v.GetBucket()))
		}
		if err := writeTable(w, bucketCols(), rows); err != nil {
			return err
		}
	}
	return nil
}

// writeDpuCSV emits one row per scope (dpu, then each ENI, then each
// VNET) with a `scope_kind` discriminator so the consumer can route by
// dimension. CSV header is stable across releases.
//
// Note: csv.Writer buffers internally; Write() only returns an error
// when its internal flush is forced to call the underlying io.Writer
// (very large buffers). For typical payloads errors surface from
// Error() after Flush(), so we ignore Write's return and check Error()
// once at the end.
func writeDpuCSV(w io.Writer, resp *dashapi.DpuCountersResponse) error {
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{
		"device_id", "sampled_at_ns", "scope_kind", "scope_key",
		"packets_in", "packets_out", "bytes_in", "bytes_out", "drops",
	})
	dev := resp.GetDeviceId()
	ts := strconv.FormatInt(resp.GetSampledAtNs(), 10)
	_ = cw.Write(csvBucketRow(dev, ts, "dpu", "", resp.GetDpu()))
	for _, e := range resp.GetEnis() {
		_ = cw.Write(csvBucketRow(dev, ts, "eni", e.GetScopeKey(), e.GetBucket()))
	}
	for _, v := range resp.GetVnets() {
		_ = cw.Write(csvBucketRow(dev, ts, "vnet", v.GetScopeKey(), v.GetBucket()))
	}
	cw.Flush()
	return cw.Error()
}

// ── small helpers ────────────────────────────────────────────────────────

func bucketCols() []string {
	return []string{"SCOPE", "PACKETS_IN", "PACKETS_OUT", "BYTES_IN", "BYTES_OUT", "DROPS"}
}

func bucketRow(scope string, b *dashapi.CounterBucket) []string {
	return []string{
		scope,
		strconv.FormatInt(b.GetPacketsIn(), 10),
		strconv.FormatInt(b.GetPacketsOut(), 10),
		strconv.FormatInt(b.GetBytesIn(), 10),
		strconv.FormatInt(b.GetBytesOut(), 10),
		strconv.FormatInt(b.GetDrops(), 10),
	}
}

func csvBucketRow(deviceID, ts, scopeKind, scopeKey string, b *dashapi.CounterBucket) []string {
	return []string{
		deviceID, ts, scopeKind, scopeKey,
		strconv.FormatInt(b.GetPacketsIn(), 10),
		strconv.FormatInt(b.GetPacketsOut(), 10),
		strconv.FormatInt(b.GetBytesIn(), 10),
		strconv.FormatInt(b.GetBytesOut(), 10),
		strconv.FormatInt(b.GetDrops(), 10),
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
