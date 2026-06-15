package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLookupKindExact(t *testing.T) {
	for _, name := range []string{"Vnet", "Eni", "VnetMapping", "AclPolicy", "RoutePolicy", "HaSet", "ServiceTunnel", "Inventory"} {
		k, ok := LookupKind(name)
		if !ok || k.Kind != name {
			t.Errorf("exact %s failed", name)
		}
	}
}

func TestLookupKindAliasesAndCase(t *testing.T) {
	cases := map[string]string{
		"vnet":           "Vnet",
		"VN":             "Vnet",
		"vnets":          "Vnet",
		"ENI":            "Eni",
		"vnet_mapping":   "VnetMapping",
		"vnet-mapping":   "VnetMapping",
		"vnetmapping":    "VnetMapping",
		"mapping":        "VnetMapping",
		"acl":            "AclPolicy",
		"acl-policy":     "AclPolicy",
		"ROUTE":          "RoutePolicy",
		"haset":          "HaSet",
		"service-tunnel": "ServiceTunnel",
		"st":             "ServiceTunnel",
		"inventory":      "Inventory",
		"inv":            "Inventory",
	}
	for in, want := range cases {
		k, ok := LookupKind(in)
		if !ok || k.Kind != want {
			t.Errorf("%q → %v / %s (want %s)", in, ok, k.Kind, want)
		}
	}
}

func TestLookupKindUnknown(t *testing.T) {
	if _, ok := LookupKind(""); ok {
		t.Fatal()
	}
	if _, ok := LookupKind("Foo"); ok {
		t.Fatal()
	}
}

func TestKindsSorted(t *testing.T) {
	ks := Kinds()
	if len(ks) == 0 {
		t.Fatal("registry empty?")
	}
	for i := 1; i < len(ks); i++ {
		if ks[i-1].Kind > ks[i].Kind {
			t.Fatalf("not sorted: %s > %s", ks[i-1].Kind, ks[i].Kind)
		}
	}
}

func TestParseOK(t *testing.T) {
	doc := []byte(`apiVersion: dashcenter.v1
kind: Vnet
metadata:
  name: v1
  namespace: ns
spec:
  vni: 1001
`)
	env, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != "Vnet" || env.Metadata.Name != "v1" {
		t.Fatal()
	}
}

func TestParseCanonicalisesKind(t *testing.T) {
	doc := []byte(`apiVersion: dashcenter.v1
kind: vnet
metadata: { name: v1 }
spec: { vni: 1 }
`)
	env, err := Parse(doc)
	if err != nil {
		t.Fatal(err)
	}
	if env.Kind != "Vnet" {
		t.Fatalf("kind canonicalised? %q", env.Kind)
	}
}

func TestParseRequiredFields(t *testing.T) {
	cases := map[string][]byte{
		"missing apiVersion": []byte(`kind: Vnet
metadata: { name: x }`),
		"wrong apiVersion": []byte(`apiVersion: other
kind: Vnet
metadata: { name: x }`),
		"missing kind": []byte(`apiVersion: dashcenter.v1
metadata: { name: x }`),
		"unknown kind": []byte(`apiVersion: dashcenter.v1
kind: Foo
metadata: { name: x }`),
		"missing name (non-inventory)": []byte(`apiVersion: dashcenter.v1
kind: Vnet`),
		"unparsable yaml": []byte(`apiVersion: dashcenter.v1
kind: : bad`),
	}
	for name, doc := range cases {
		if _, err := Parse(doc); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestParseInventoryAllowsNoName(t *testing.T) {
	doc := []byte(`apiVersion: dashcenter.v1
kind: Inventory
spec:
  dpus:
    - { id: dpu-0, endpoint: "x:1" }
`)
	if _, err := Parse(doc); err != nil {
		t.Fatal(err)
	}
}

func TestValidateNil(t *testing.T) {
	if _, err := Validate(nil); err == nil {
		t.Fatal()
	}
}

func TestValidateInjectsEmptySpec(t *testing.T) {
	env, err := Validate(&Envelope{APIVersion: APIVersion, Kind: "Vnet", Metadata: Metadata{Name: "x"}})
	if err != nil {
		t.Fatal(err)
	}
	if env.Spec == nil {
		t.Fatal("Spec should be non-nil after Validate")
	}
}

func TestParseMulti(t *testing.T) {
	doc := []byte(`apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: a }
spec: { vni: 1 }
---
# this is a comment-only doc, skipped
---
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: b }
spec: { vni: 2 }
`)
	env, err := ParseMulti(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(env) != 2 || env[0].Metadata.Name != "a" || env[1].Metadata.Name != "b" {
		t.Fatalf("got %d docs: %+v", len(env), env)
	}
}

func TestParseMultiErrorIncludesIndex(t *testing.T) {
	doc := []byte(`apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: a }
spec: { vni: 1 }
---
apiVersion: dashcenter.v1
kind: NotAKind
metadata: { name: b }
`)
	_, err := ParseMulti(doc)
	if err == nil || !strings.Contains(err.Error(), "doc 2") {
		t.Fatalf("expected error with doc index, got %v", err)
	}
}

func TestSpecJSONPrunesAndProjects(t *testing.T) {
	env := &Envelope{
		APIVersion: APIVersion, Kind: "Vnet",
		Metadata: Metadata{Namespace: "ns", Name: "v1", Generation: 7, Labels: map[string]string{"a": "b"}},
		Spec:     map[string]any{"vni": 1001},
	}
	out, err := env.SpecJSON()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["name"] != "v1" || m["namespace"] != "ns" {
		t.Fatalf("name/namespace not projected: %v", m)
	}
	if v, ok := m["expected_generation"]; !ok || int(v.(float64)) != 7 {
		t.Fatalf("expected_generation: %v", m)
	}
	if l, ok := m["labels"].(map[string]any); !ok || l["a"] != "b" {
		t.Fatalf("labels: %v", m)
	}
	if int(m["vni"].(float64)) != 1001 {
		t.Fatalf("vni: %v", m)
	}
}

func TestSpecJSONSpecOverridesMetadata(t *testing.T) {
	env := &Envelope{
		APIVersion: APIVersion, Kind: "Vnet",
		Metadata: Metadata{Name: "from-meta", Generation: 99},
		Spec:     map[string]any{"name": "from-spec", "expected_generation": 7, "vni": 1},
	}
	out, _ := env.SpecJSON()
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["name"] != "from-spec" {
		t.Fatal("spec.name should win")
	}
	if int(m["expected_generation"].(float64)) != 7 {
		t.Fatal("spec.expected_generation should win")
	}
}

func TestSpecJSONNilSafe(t *testing.T) {
	var e *Envelope
	if _, err := e.SpecJSON(); err == nil {
		t.Fatal("nil envelope must error")
	}
}

func TestEnvelopeFromStoredItem(t *testing.T) {
	specJSON := []byte(`{"name":"v1","namespace":"ns","vni":1001,"expected_generation":7,"labels":{"a":"b"}}`)
	env, err := EnvelopeFromStoredItem("vnet", "ns", "v1", 7, specJSON, nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.Metadata.Name != "v1" || env.Metadata.Generation != 7 {
		t.Fatal()
	}
	if env.Metadata.Labels["a"] != "b" {
		t.Fatalf("labels: %+v", env.Metadata.Labels)
	}
	if _, ok := env.Spec["name"]; ok {
		t.Fatal("projected name should be stripped")
	}
	if _, ok := env.Spec["expected_generation"]; ok {
		t.Fatal("projected gen should be stripped")
	}
}

func TestEnvelopeFromStoredItemUnknownKind(t *testing.T) {
	if _, err := EnvelopeFromStoredItem("unknown", "", "x", 0, nil, nil); err == nil {
		t.Fatal()
	}
}

func TestEnvelopeFromStoredItemBadSpec(t *testing.T) {
	if _, err := EnvelopeFromStoredItem("vnet", "", "x", 0, []byte("not-json"), nil); err == nil {
		t.Fatal()
	}
}

func TestEnvelopeFromStoredItemEmptySpec(t *testing.T) {
	env, err := EnvelopeFromStoredItem("vnet", "", "x", 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if env.Spec == nil {
		t.Fatal()
	}
}

func TestMarshalAndJSON(t *testing.T) {
	env := &Envelope{
		APIVersion: APIVersion, Kind: "Vnet",
		Metadata: Metadata{Name: "v"},
		Spec:     map[string]any{"vni": 1},
	}
	if y, err := env.Marshal(); err != nil || !strings.Contains(string(y), "kind: Vnet") {
		t.Fatalf("yaml: err=%v body=%s", err, y)
	}
	if j, err := env.MarshalJSON(); err != nil || !strings.Contains(string(j), `"kind": "Vnet"`) {
		t.Fatalf("json: err=%v body=%s", err, j)
	}
}
