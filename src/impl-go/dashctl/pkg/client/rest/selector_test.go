package rest

import (
	"encoding/json"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func TestParseSelectorEmpty(t *testing.T) {
	s, err := ParseSelector("")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Matches(map[string]string{"any": "thing"}) {
		t.Fatal("empty selector should match anything")
	}
	if !s.Matches(nil) {
		t.Fatal()
	}
}

func TestParseSelectorTerms(t *testing.T) {
	cases := []struct {
		expr   string
		labels map[string]string
		match  bool
	}{
		{"tier=prod", map[string]string{"tier": "prod"}, true},
		{"tier=prod", map[string]string{"tier": "dev"}, false},
		{"tier=prod", map[string]string{}, false},
		{"tier==prod", map[string]string{"tier": "prod"}, true},
		{"tier!=prod", map[string]string{"tier": "prod"}, false},
		{"tier!=prod", map[string]string{"tier": "dev"}, true},
		{"tier!=prod", map[string]string{}, true}, // absence == not equal
		{"tier", map[string]string{"tier": "anything"}, true},
		{"tier", map[string]string{}, false},
		{"!tier", map[string]string{}, true},
		{"!tier", map[string]string{"tier": "x"}, false},
		{"tier=prod,team=alpha", map[string]string{"tier": "prod", "team": "alpha"}, true},
		{"tier=prod,team=alpha", map[string]string{"tier": "prod", "team": "bravo"}, false},
		{"  tier =  prod  ", map[string]string{"tier": "prod"}, true},
	}
	for _, c := range cases {
		s, err := ParseSelector(c.expr)
		if err != nil {
			t.Errorf("%q: parse: %v", c.expr, err)
			continue
		}
		if got := s.Matches(c.labels); got != c.match {
			t.Errorf("%q vs %v → %v, want %v", c.expr, c.labels, got, c.match)
		}
	}
}

func TestParseSelectorErrors(t *testing.T) {
	for _, bad := range []string{"!", "=v", "==v", "!=v"} {
		if _, err := ParseSelector(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestParseSelectorEmptyTermsSkipped(t *testing.T) {
	s, err := ParseSelector("tier=prod,,team=alpha")
	if err != nil {
		t.Fatal(err)
	}
	if !s.Matches(map[string]string{"tier": "prod", "team": "alpha"}) {
		t.Fatal()
	}
}

func TestFilterBySelector(t *testing.T) {
	items := []*client.StoredItem{
		{Name: "a", Spec: json.RawMessage(`{"labels":{"tier":"prod"}}`)},
		{Name: "b", Spec: json.RawMessage(`{"labels":{"tier":"dev"}}`)},
		{Name: "c", Spec: json.RawMessage(`{"labels":{"tier":"prod"}}`)},
	}
	sel, _ := ParseSelector("tier=prod")
	out := filterBySelector(items, sel)
	if len(out) != 2 || out[0].Name != "a" || out[1].Name != "c" {
		t.Fatalf("%+v", out)
	}
	// nil selector → pass-through
	if got := filterBySelector(items, nil); len(got) != 3 {
		t.Fatal()
	}
}

func TestExtractLabelsBadJSON(t *testing.T) {
	it := &client.StoredItem{Spec: json.RawMessage(`not-json`)}
	if extractLabels(it) != nil {
		t.Fatal("bad json → nil")
	}
	if extractLabels(nil) != nil {
		t.Fatal()
	}
	it = &client.StoredItem{Spec: nil}
	if extractLabels(it) != nil {
		t.Fatal()
	}
}

func TestExtractLabelsHappy(t *testing.T) {
	it := &client.StoredItem{Spec: json.RawMessage(`{"labels":{"a":"b"}}`)}
	if extractLabels(it)["a"] != "b" {
		t.Fatal()
	}
}
