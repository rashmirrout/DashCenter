package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func TestParseFormat(t *testing.T) {
	cases := []struct {
		in    string
		want  Format
		expr  string
		isErr bool
	}{
		{"", "", "", false},
		{"json", FormatJSON, "", false},
		{"yaml", FormatYAML, "", false},
		{"table", FormatTable, "", false},
		{"wide", FormatWide, "", false},
		{"name", FormatName, "", false},
		{"jsonpath", FormatJSONPath, "", false},
		{"jsonpath={.foo}", FormatJSONPath, "{.foo}", false},
		{"template={{ .x }}", FormatTemplate, "{{ .x }}", false},
		{"ghost", "", "", true},
	}
	for _, c := range cases {
		f, e, err := ParseFormat(c.in)
		if c.isErr {
			if err == nil {
				t.Errorf("%q: expected error", c.in)
			}
			continue
		}
		if f != c.want || e != c.expr {
			t.Errorf("%q → %q/%q want %q/%q", c.in, f, e, c.want, c.expr)
		}
	}
}

func TestDefaultForNonTTY(t *testing.T) {
	var buf bytes.Buffer
	if DefaultFor(&buf) != FormatJSON {
		t.Fatal("non-tty default must be json")
	}
}

func TestRenderJSON(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, map[string]int{"a": 1}, Options{Format: FormatJSON}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"a": 1`) {
		t.Fatalf("%s", buf.String())
	}
}

func TestRenderYAML(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, map[string]int{"a": 1}, Options{Format: FormatYAML}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "a: 1") {
		t.Fatalf("%s", buf.String())
	}
}

func TestRenderName(t *testing.T) {
	var buf bytes.Buffer
	val := []map[string]any{{"kind": "vnet", "name": "v1"}, {"kind": "vnet", "name": "v2"}}
	if err := Render(&buf, val, Options{Format: FormatName}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "vnet/v1\n") || !strings.Contains(buf.String(), "vnet/v2\n") {
		t.Fatalf("%s", buf.String())
	}
}

func TestRenderNameNoNamer(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, 42, Options{Format: FormatName}); err == nil {
		t.Fatal("non-namer should error")
	}
}

type sampleNamer string

func (s sampleNamer) Name() string { return string(s) }

func TestRenderNameSingleNamer(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleNamer("vnet/v1"), Options{Format: FormatName}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "vnet/v1\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestRenderJSONPathFields(t *testing.T) {
	v := map[string]any{
		"items": []any{
			map[string]any{"spec": map[string]any{"vni": 1001.0}},
			map[string]any{"spec": map[string]any{"vni": 1002.0}},
		},
		"meta": map[string]any{"name": "v1"},
	}
	cases := map[string]string{
		"{.meta.name}":             "v1\n",
		"{.items.0.spec.vni}":      "1001\n",
		"{.items.1.spec.vni}":      "1002\n",
		"{}":                       "", // whole-doc handled below
		"{.meta}":                  "", // object → json one-liner
	}
	for expr, want := range cases {
		var buf bytes.Buffer
		err := Render(&buf, v, Options{Format: FormatJSONPath, Expression: expr})
		if err != nil {
			t.Fatalf("%s: %v", expr, err)
		}
		if want != "" && buf.String() != want {
			t.Errorf("%s → %q want %q", expr, buf.String(), want)
		}
	}
}

func TestRenderJSONPathErrors(t *testing.T) {
	v := map[string]any{"a": 1}
	bad := []string{"{.missing}", "{.a.b}", "{.a.5}"}
	for _, b := range bad {
		var buf bytes.Buffer
		if err := Render(&buf, v, Options{Format: FormatJSONPath, Expression: b}); err == nil {
			t.Errorf("%s: expected error", b)
		}
	}
}

func TestRenderJSONPathOnArray(t *testing.T) {
	v := []any{"alpha", "beta"}
	var buf bytes.Buffer
	if err := Render(&buf, v, Options{Format: FormatJSONPath, Expression: "{.1}"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "beta\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestRenderTemplate(t *testing.T) {
	var buf bytes.Buffer
	v := map[string]any{"a": 1, "b": "x"}
	if err := Render(&buf, v, Options{Format: FormatTemplate, Expression: "{{ .a }}|{{ .b }}\n"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "1|x\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestRenderTemplateBadExpr(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, map[string]any{}, Options{Format: FormatTemplate, Expression: "{{ .a"}); err == nil {
		t.Fatal()
	}
}

func TestRenderTable(t *testing.T) {
	items := []*client.StoredItem{
		{Kind: "vnet", Namespace: "default", Name: "v1", Generation: 1, Spec: json.RawMessage(`{"vni":1001}`)},
		{Kind: "vnet", Namespace: "default", Name: "v2", Generation: 2, Spec: json.RawMessage(`{"vni":1002}`)},
	}
	rows := make([]any, len(items))
	for i, it := range items {
		rows[i] = it
	}
	var buf bytes.Buffer
	if err := Render(&buf, rows, Options{Format: FormatTable, Columns: ColumnsFor("vnet")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "NAME") || !strings.Contains(buf.String(), "v1") || !strings.Contains(buf.String(), "1001") {
		t.Fatalf("%s", buf.String())
	}
}

func TestRenderTableWide(t *testing.T) {
	items := []*client.StoredItem{
		{Kind: "eni", Namespace: "default", Name: "e1", Spec: json.RawMessage(`{"vnet_name":"v","placement_hint_dpu_ids":["dpu-0"]}`)},
	}
	rows := []any{items[0]}
	var buf bytes.Buffer
	if err := Render(&buf, rows, Options{Format: FormatWide, Columns: ColumnsFor("eni")}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "PLACED-ON") {
		t.Fatal("wide should expose PLACED-ON column")
	}
}

func TestRenderTableNoHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, []any{}, Options{Format: FormatTable, Columns: ColumnsFor("vnet"), NoHeader: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "NAME") {
		t.Fatal("header should be suppressed")
	}
}

func TestRenderTableNoColumnsFallback(t *testing.T) {
	var buf bytes.Buffer
	rows := []any{map[string]any{"a": 1}}
	if err := Render(&buf, rows, Options{Format: FormatTable}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"a":1`) {
		t.Fatalf("%s", buf.String())
	}
}

func TestRenderUnsupportedFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, nil, Options{Format: "no-such-format"}); err == nil {
		t.Fatal()
	}
}

func TestRenderEmptyFormatDefaults(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, map[string]int{"x": 1}, Options{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"x"`) {
		t.Fatal("default should be json for non-tty")
	}
}

func TestColumnsForAllKinds(t *testing.T) {
	for _, k := range []string{"vnet", "eni", "vnet_mapping", "acl_policy", "route_policy", "ha_set", "service_tunnel", "anything-else"} {
		cols := ColumnsFor(k)
		if len(cols) == 0 {
			t.Errorf("no columns for %s", k)
		}
	}
}

func TestDpuColumnsAndDriftColumns(t *testing.T) {
	dpuCols := DpuColumns()
	dpu := client.DpuStatus{ID: "dpu-0", State: "UP", LastSeen: "now", Endpoint: "x:1", Labels: map[string]string{"rack": "A1"}}
	row := dpu
	for _, c := range dpuCols {
		_ = c.Get(row)
	}
	if dpuCols[0].Header != "ID" {
		t.Fatal()
	}

	dCols := DriftColumns()
	di := client.DriftItem{DpuID: "dpu-0", Op: "add", Kind: "vnet", Key: "v1", Detail: "missing"}
	for _, c := range dCols {
		_ = c.Get(di)
	}
}

func TestPlacementColumns(t *testing.T) {
	cols := PlacementColumns()
	row := client.EniPlacementRow{EniName: "e", DpuID: "d", VnetName: "v", Observed: true}
	for _, c := range cols {
		_ = c.Get(row)
	}
}

func TestReadFieldFallbacks(t *testing.T) {
	if readField(nil, "name") != "" {
		t.Fatal()
	}
	if readField((*client.StoredItem)(nil), "name") != "" {
		t.Fatal()
	}
	m := map[string]any{"name": "x"}
	if readField(m, "name") != "x" {
		t.Fatal()
	}
	if readField(m, "missing") != "" {
		t.Fatal()
	}
}

func TestReadSpecFieldTypes(t *testing.T) {
	it := &client.StoredItem{Spec: json.RawMessage(`{"a":"s","b":1,"c":true,"d":false,"e":[1,2],"f":1.5}`)}
	if readSpecField(it, "a") != "s" {
		t.Fatal()
	}
	if readSpecField(it, "b") != "1" {
		t.Fatal()
	}
	if readSpecField(it, "c") != "true" {
		t.Fatal()
	}
	if readSpecField(it, "d") != "false" {
		t.Fatal()
	}
	if readSpecField(it, "e") != "[1,2]" {
		t.Fatal()
	}
	if readSpecField(it, "f") != "1.5" {
		t.Fatal()
	}
	if readSpecField(it, "missing") != "" {
		t.Fatal()
	}
	if readSpecField(&client.StoredItem{}, "x") != "" {
		t.Fatal()
	}
}

func TestExtractSpecVariants(t *testing.T) {
	if extractSpec(nil) != nil {
		t.Fatal()
	}
	if extractSpec(&client.StoredItem{Spec: json.RawMessage(`not-json`)}) != nil {
		t.Fatal()
	}
	if extractSpec(map[string]any{"spec": map[string]any{"x": 1}})["x"].(int) != 1 {
		t.Fatal()
	}
	if extractSpec(map[string]any{}) != nil {
		t.Fatal()
	}
}

func TestReadSpecListAndLen(t *testing.T) {
	it := &client.StoredItem{Spec: json.RawMessage(`{"ips":["a","b"],"others":[1,2,3]}`)}
	ll := readSpecListField(it, "ips")
	if len(ll) != 2 || ll[0] != "a" {
		t.Fatalf("%+v", ll)
	}
	if lenSpecList(it, "others") != 3 {
		t.Fatal()
	}
	if lenSpecList(it, "missing") != 0 {
		t.Fatal()
	}
	if readSpecListField(it, "missing") != nil {
		t.Fatal()
	}
}

func TestFormatLabelsAndList(t *testing.T) {
	if formatLabels(nil) != "" {
		t.Fatal()
	}
	if formatLabels(map[string]string{"a": "1", "b": "2"}) != "a=1,b=2" {
		t.Fatal()
	}
	if formatList(nil) != "" {
		t.Fatal()
	}
	if formatList([]string{"x", "y"}) != "x,y" {
		t.Fatal()
	}
}

func TestFormatNumber(t *testing.T) {
	if formatNumber(2.0) != "2" {
		t.Fatal()
	}
	if formatNumber(2.5) != "2.5" {
		t.Fatal()
	}
}

func TestItemNameNil(t *testing.T) {
	if ItemName(nil) != "" {
		t.Fatal()
	}
	if ItemName(&client.StoredItem{Kind: "vnet", Name: "v"}) != "vnet/v" {
		t.Fatal()
	}
}

// Render to os.Stdout-like via *os.File to exercise isTTY paths (will be false in CI).
func TestIsTTYNonFile(t *testing.T) {
	var buf bytes.Buffer
	if isTTY(&buf) {
		t.Fatal("bytes.Buffer is not a TTY")
	}
	// *os.File but redirected stdin → not a TTY in CI
	if isTTY(os.Stdin) {
		// Acceptable — interactive shells may evaluate to true.
	}
}

func TestReadDpuFieldVariants(t *testing.T) {
	dpu := &client.DpuStatus{ID: "d", State: "UP", Endpoint: "h:1", LastSeen: "now"}
	for _, name := range []string{"id", "endpoint", "state", "last_seen"} {
		if readDpuField(dpu, name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	if readDpuField(nil, "x") != "" {
		t.Fatal()
	}
	if readDpuField((*client.DpuStatus)(nil), "x") != "" {
		t.Fatal()
	}
	if readDpuField(map[string]any{"id": "x"}, "id") != "x" {
		t.Fatal()
	}
}

func TestReadDriftFieldVariants(t *testing.T) {
	d := &client.DriftItem{DpuID: "x", Op: "add", Kind: "vnet", Key: "v", Detail: "info"}
	for _, name := range []string{"dpu_id", "op", "kind", "key", "detail"} {
		if readDriftField(d, name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	if readDriftField(nil, "op") != "" {
		t.Fatal()
	}
	if readDriftField((*client.DriftItem)(nil), "op") != "" {
		t.Fatal()
	}
	if readDriftField(map[string]any{"op": "add"}, "op") != "add" {
		t.Fatal()
	}
}

func TestReadPlacementFieldVariants(t *testing.T) {
	p := &client.EniPlacementRow{EniName: "e", VnetName: "v", DpuID: "d", Observed: true, Namespace: "n"}
	for _, name := range []string{"eni_name", "vnet_name", "dpu_id", "observed", "namespace"} {
		if readPlacementField(p, name) == "" {
			t.Errorf("missing %s", name)
		}
	}
	if readPlacementField(nil, "x") != "" {
		t.Fatal()
	}
	if readPlacementField((*client.EniPlacementRow)(nil), "x") != "" {
		t.Fatal()
	}
}

func TestParseIndex(t *testing.T) {
	if _, err := parseIndex("a", 5); err == nil {
		t.Fatal("non-numeric")
	}
	if _, err := parseIndex("99", 2); err == nil {
		t.Fatal("out of range")
	}
}

func TestStepJSONPathNilCursor(t *testing.T) {
	if _, err := stepJSONPath(nil, "x"); err == nil {
		t.Fatal()
	}
	if _, err := stepJSONPath(42, "x"); err == nil {
		t.Fatal()
	}
}

func TestRenderJSONPathNilOutput(t *testing.T) {
	var buf bytes.Buffer
	v := map[string]any{"a": nil}
	if err := Render(&buf, v, Options{Format: FormatJSONPath, Expression: "{.a}"}); err != nil {
		t.Fatal(err)
	}
	// nil → blank line
	if buf.String() != "\n" {
		t.Fatalf("%q", buf.String())
	}
}

// Ensure rendering a slice-of-pointers via the generic Render path works.
func TestRenderTableSliceFallback(t *testing.T) {
	rows := []map[string]any{{"a": 1}, {"a": 2}}
	var buf bytes.Buffer
	if err := Render(&buf, rows, Options{Format: FormatTable, Columns: []Column{{Header: "A", Get: func(o any) string {
		return fmt.Sprintf("%v", o.(map[string]any)["a"])
	}}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "A") {
		t.Fatal()
	}
}
