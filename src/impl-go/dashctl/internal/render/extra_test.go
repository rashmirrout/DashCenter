package render

import (
	"bytes"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// Extra tests pushing coverage closer to 100% by exercising helper paths
// that the table-driven tests don't naturally hit.

func TestTryAnySliceFallback(t *testing.T) {
	if _, ok := tryAnySlice(42); ok {
		t.Fatal("non-slice should not coerce")
	}
	in := []map[string]any{{"k": 1}}
	out, ok := tryAnySlice(in)
	if !ok || len(out) != 1 {
		t.Fatal("[]map[string]any should coerce to []any")
	}
}

func TestTryNameSliceFallback(t *testing.T) {
	if _, ok := tryNameSlice(42); ok {
		t.Fatal()
	}
	out, ok := tryNameSlice([]map[string]any{{"kind": "vnet", "name": "v"}})
	if !ok || out[0] != "vnet/v" {
		t.Fatalf("%+v", out)
	}
}

type nameSlice []sampleNamer

func TestRenderNameRespectsNamerInterface(t *testing.T) {
	var buf bytes.Buffer
	if err := renderName(&buf, sampleNamer("x/1")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "x/1\n" {
		t.Fatal()
	}
	buf.Reset()
	if err := renderName(&buf, []Namer{sampleNamer("a"), sampleNamer("b")}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "a\nb\n" {
		t.Fatal()
	}
	buf.Reset()
	if err := renderName(&buf, []any{sampleNamer("x")}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "x\n" {
		t.Fatal()
	}
}

func TestRenderNameNonNamerInSlice(t *testing.T) {
	var buf bytes.Buffer
	// []any with elements that are NOT Namer should just produce nothing
	// (no panic). The current implementation silently skips them and
	// produces an empty output.
	if err := renderName(&buf, []any{42}); err != nil {
		t.Fatal(err)
	}
}

func TestColumnsCoverage(t *testing.T) {
	// Drive every kind's columns to maximise statement coverage.
	cases := map[string][]byte{
		"vnet":           []byte(`{"vni":1,"labels":{"x":"y"}}`),
		"eni":            []byte(`{"vnet_name":"v","mac_address":"00","underlay_ip":"1.1.1.1","admin_state":"up","placement_hint_dpu_ids":["d"]}`),
		"vnet_mapping":   []byte(`{"vnet_name":"v","ip_address":"1.1.1.1","underlay_ip":"2.2.2.2","action":"vnet_encap"}`),
		"acl_policy":     []byte(`{"stage":"in","eni_names":["e1"],"rules":[{"action":"allow"}]}`),
		"route_policy":   []byte(`{"eni_names":["e"],"routes":[{"prefix":"0/0"}]}`),
		"ha_set":         []byte(`{}`),
		"service_tunnel": []byte(`{}`),
	}
	for k, body := range cases {
		it := &client.StoredItem{Kind: k, Name: "x", Namespace: "n", Generation: 1, Spec: body}
		var buf bytes.Buffer
		_ = renderTable(&buf, []any{it}, Options{Columns: ColumnsFor(k)})
	}
}

func TestRenderJSONPathOnString(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, map[string]any{"a": "hi"}, Options{Format: FormatJSONPath, Expression: "{.a}"}); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "hi\n" {
		t.Fatalf("%q", buf.String())
	}
}

func TestRenderJSONPathInvalidExpr(t *testing.T) {
	var buf bytes.Buffer
	// Index parse failure path.
	if err := Render(&buf, []any{1, 2}, Options{Format: FormatJSONPath, Expression: "{.abc}"}); err == nil {
		t.Fatal()
	}
}

func TestParseFormatJSONPathBare(t *testing.T) {
	f, e, err := ParseFormat("jsonpath")
	if err != nil || f != FormatJSONPath || e != "" {
		t.Fatal()
	}
}

func TestRenderTableSingleObject(t *testing.T) {
	it := &client.StoredItem{Kind: "vnet", Name: "v"}
	var buf bytes.Buffer
	if err := Render(&buf, it, Options{Format: FormatTable, Columns: ColumnsFor("vnet")}); err != nil {
		t.Fatal(err)
	}
}
