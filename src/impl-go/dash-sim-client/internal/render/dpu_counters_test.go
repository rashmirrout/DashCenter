// Tests for DpuCounters rendering across all four output formats
// (table, json, yaml, csv). Synthetic response is used — no network
// dependency — so this file runs in milliseconds and covers the render
// logic exhaustively.

package render

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"io"
	"strings"
	"testing"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"gopkg.in/yaml.v3"
)

func sampleResp() *dashapi.DpuCountersResponse {
	return &dashapi.DpuCountersResponse{
		DeviceId:    "dpu-sim-01",
		SampledAtNs: 1718387200_000000000,
		Dpu: &dashapi.CounterBucket{
			PacketsIn: 100, PacketsOut: 200, BytesIn: 1000, BytesOut: 2000, Drops: 1,
		},
		Enis: []*dashapi.ScopedCounters{
			{ScopeKey: "eni-001", Bucket: &dashapi.CounterBucket{PacketsIn: 10, Drops: 0}},
			{ScopeKey: "eni-002", Bucket: &dashapi.CounterBucket{PacketsIn: 20, Drops: 1}},
		},
		Vnets: []*dashapi.ScopedCounters{
			{ScopeKey: "vnet-prod", Bucket: &dashapi.CounterBucket{PacketsIn: 30}},
		},
	}
}

// ── ParseFormatExt ────────────────────────────────────────────────────────

func TestParseFormatExt_RecognisesCSV(t *testing.T) {
	for _, in := range []string{"csv", "CSV", " csv ", "Csv"} {
		got, err := ParseFormatExt(in)
		if err != nil {
			t.Fatalf("ParseFormatExt(%q): %v", in, err)
		}
		if got != FormatCSV {
			t.Errorf("ParseFormatExt(%q)=%q want csv", in, got)
		}
	}
}

func TestParseFormatExt_DelegatesToParseFormat(t *testing.T) {
	for _, in := range []string{"json", "yaml", "table"} {
		got, err := ParseFormatExt(in)
		if err != nil {
			t.Fatalf("ParseFormatExt(%q): %v", in, err)
		}
		if string(got) != in {
			t.Errorf("ParseFormatExt(%q)=%q want %q", in, got, in)
		}
	}
}

func TestParseFormatExt_UnknownErrors(t *testing.T) {
	if _, err := ParseFormatExt("xml"); err == nil {
		t.Fatal("want error for xml")
	}
}

// ── DpuCounters: nil response ─────────────────────────────────────────────

func TestDpuCounters_NilResponse(t *testing.T) {
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatTable, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(nil)") {
		t.Errorf("want (nil) sentinel: %q", buf.String())
	}
}

// ── Table format ─────────────────────────────────────────────────────────

func TestDpuCounters_TableShowsAllSections(t *testing.T) {
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatTable, sampleResp()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"DEVICE  dpu-sim-01",
		"DPU TOTALS",
		"PER-ENI",
		"eni-001",
		"eni-002",
		"PER-VNET",
		"vnet-prod",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in:\n%s", want, out)
		}
	}
}

func TestDpuCounters_TableSkipsEmptySections(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{
		DeviceId: "d1", SampledAtNs: 1, Dpu: &dashapi.CounterBucket{},
	}
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatTable, resp); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if strings.Contains(out, "PER-ENI") {
		t.Errorf("empty enis section should not appear: %s", out)
	}
	if strings.Contains(out, "PER-VNET") {
		t.Errorf("empty vnets section should not appear: %s", out)
	}
}

func TestDpuCounters_TableEmptyDefaultsToTable(t *testing.T) {
	// Format("") falls through to FormatTable in the switch.
	var buf bytes.Buffer
	if err := DpuCounters(&buf, "", sampleResp()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "DPU TOTALS") {
		t.Errorf("empty format should default to table: %s", buf.String())
	}
}

// ── JSON format ───────────────────────────────────────────────────────────

func TestDpuCounters_JSONRoundtrips(t *testing.T) {
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatJSON, sampleResp()); err != nil {
		t.Fatal(err)
	}
	var env dpuEnvelope
	if err := json.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, buf.String())
	}
	if env.DeviceID != "dpu-sim-01" {
		t.Errorf("device_id wrong: %s", env.DeviceID)
	}
	if env.Dpu.PacketsIn != 100 {
		t.Errorf("dpu.packets_in wrong: %d", env.Dpu.PacketsIn)
	}
	if len(env.Enis) != 2 || env.Enis[1].ScopeKey != "eni-002" {
		t.Errorf("enis envelope wrong: %+v", env.Enis)
	}
	if env.SampledAt == "" {
		t.Errorf("sampled_at should be RFC3339-formatted")
	}
}

// ── YAML format ───────────────────────────────────────────────────────────

func TestDpuCounters_YAMLRoundtrips(t *testing.T) {
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatYAML, sampleResp()); err != nil {
		t.Fatal(err)
	}
	var env dpuEnvelope
	if err := yaml.Unmarshal(buf.Bytes(), &env); err != nil {
		t.Fatalf("output is not valid YAML: %v\n%s", err, buf.String())
	}
	if env.DeviceID != "dpu-sim-01" {
		t.Errorf("device_id wrong: %s", env.DeviceID)
	}
	if len(env.Vnets) != 1 || env.Vnets[0].ScopeKey != "vnet-prod" {
		t.Errorf("vnets envelope wrong: %+v", env.Vnets)
	}
}

// ── CSV format ────────────────────────────────────────────────────────────

func TestDpuCounters_CSVHeaderAndRows(t *testing.T) {
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatCSV, sampleResp()); err != nil {
		t.Fatal(err)
	}
	rd := csv.NewReader(&buf)
	rows, err := rd.ReadAll()
	if err != nil {
		t.Fatalf("CSV invalid: %v", err)
	}
	// header + dpu + 2 enis + 1 vnet = 5 rows
	if len(rows) != 5 {
		t.Fatalf("want 5 rows, got %d: %+v", len(rows), rows)
	}
	want := []string{
		"device_id", "sampled_at_ns", "scope_kind", "scope_key",
		"packets_in", "packets_out", "bytes_in", "bytes_out", "drops",
	}
	if !equalRows(rows[0], want) {
		t.Errorf("header wrong:\n  got: %v\n want: %v", rows[0], want)
	}
	// DPU row sanity
	if rows[1][2] != "dpu" || rows[1][4] != "100" {
		t.Errorf("dpu row wrong: %v", rows[1])
	}
	// ENI rows sorted: eni-001 before eni-002
	if rows[2][2] != "eni" || rows[2][3] != "eni-001" {
		t.Errorf("eni-001 row wrong: %v", rows[2])
	}
	if rows[3][2] != "eni" || rows[3][3] != "eni-002" {
		t.Errorf("eni-002 row wrong: %v", rows[3])
	}
	// VNET row
	if rows[4][2] != "vnet" || rows[4][3] != "vnet-prod" {
		t.Errorf("vnet row wrong: %v", rows[4])
	}
}

func TestDpuCounters_CSVOmitsEmptyEnisAndVnets(t *testing.T) {
	resp := &dashapi.DpuCountersResponse{
		DeviceId: "d1", SampledAtNs: 0, Dpu: &dashapi.CounterBucket{PacketsIn: 1},
	}
	var buf bytes.Buffer
	if err := DpuCounters(&buf, FormatCSV, resp); err != nil {
		t.Fatal(err)
	}
	rd := csv.NewReader(&buf)
	rows, err := rd.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Errorf("want header + 1 dpu row only, got %d", len(rows))
	}
}

// ── Unsupported format ────────────────────────────────────────────────────

func TestDpuCounters_UnsupportedFormatErrors(t *testing.T) {
	var buf bytes.Buffer
	err := DpuCounters(&buf, Format("xml"), sampleResp())
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("want unsupported format error, got %v", err)
	}
}

// ── orDash helper ─────────────────────────────────────────────────────────

func TestOrDashHelper(t *testing.T) {
	if orDash("") != "-" {
		t.Errorf("orDash empty = %q want -", orDash(""))
	}
	if orDash("x") != "x" {
		t.Errorf("orDash non-empty = %q want x", orDash("x"))
	}
}

// ── error paths: failing writer ───────────────────────────────────────────

// failingWriter returns ErrShortWrite after `okBytes` bytes are written.
// Used to exercise the encoder error returns in DpuCounters (CSV / table /
// json / yaml) — paths a real *os.File would never trigger.
type failingWriter struct{ okBytes, n int }

func (f *failingWriter) Write(p []byte) (int, error) {
	if f.n >= f.okBytes {
		return 0, io.ErrShortWrite
	}
	rem := f.okBytes - f.n
	if len(p) <= rem {
		f.n += len(p)
		return len(p), nil
	}
	f.n = f.okBytes
	return rem, io.ErrShortWrite
}

func TestDpuCounters_CSVPropagatesWriterError(t *testing.T) {
	// header is the first ~80 bytes; failing at byte 0 means the header
	// write itself fails.
	w := &failingWriter{okBytes: 0}
	err := DpuCounters(w, FormatCSV, sampleResp())
	if err == nil {
		t.Fatal("want writer error")
	}
}

func TestDpuCounters_TablePropagatesWriterError(t *testing.T) {
	w := &failingWriter{okBytes: 0}
	err := DpuCounters(w, FormatTable, sampleResp())
	if err == nil {
		t.Fatal("want writer error")
	}
}

func TestDpuCounters_JSONPropagatesWriterError(t *testing.T) {
	w := &failingWriter{okBytes: 0}
	err := DpuCounters(w, FormatJSON, sampleResp())
	if err == nil {
		t.Fatal("want writer error")
	}
}

func TestDpuCounters_YAMLPropagatesWriterError(t *testing.T) {
	w := &failingWriter{okBytes: 0}
	err := DpuCounters(w, FormatYAML, sampleResp())
	if err == nil {
		t.Fatal("want writer error")
	}
}

// Trigger the per-ENI table writer-error branch specifically: enough bytes
// for the device header + DPU TOTALS, then fail when writing the PER-ENI
// section. okBytes tuned by experimentation against sampleResp().
func TestDpuCounters_TableFailsMidWayThroughEniSection(t *testing.T) {
	// We want enough bytes to clear DPU TOTALS but not finish PER-ENI.
	w := &failingWriter{okBytes: 200}
	err := DpuCounters(w, FormatTable, sampleResp())
	if err == nil {
		t.Fatal("want writer error mid-stream")
	}
}

// Trigger the per-VNET table writer-error branch: enough bytes for
// DPU TOTALS + PER-ENI but fail in PER-VNET.
func TestDpuCounters_TableFailsMidWayThroughVnetSection(t *testing.T) {
	// First determine the byte size of the table output up to but NOT
	// including the per-VNET section by rendering a response with no
	// VNETs into a counting buffer. Then set okBytes to that count + 1
	// so the next byte written (which will be inside PER-VNET) errors.
	respNoVnet := sampleResp()
	respNoVnet.Vnets = nil
	var counter bytes.Buffer
	if err := DpuCounters(&counter, FormatTable, respNoVnet); err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	okBytes := counter.Len() + 1 // pass DPU TOTALS + PER-ENI, fail in PER-VNET
	w := &failingWriter{okBytes: okBytes}
	err := DpuCounters(w, FormatTable, sampleResp())
	if err == nil {
		t.Fatalf("want writer error in PER-VNET section (okBytes=%d)", okBytes)
	}
}

// CSV mid-stream failure: failingWriter triggers cw.Error() after Flush.
func TestDpuCounters_CSVFailsAfterHeader(t *testing.T) {
	w := &failingWriter{okBytes: 50}
	err := DpuCounters(w, FormatCSV, sampleResp())
	if err == nil {
		t.Fatal("want CSV writer error")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────

func equalRows(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
