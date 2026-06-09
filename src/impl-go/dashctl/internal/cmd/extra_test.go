package cmd

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	dashErrors "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// Extra coverage for the cmd package: typed kind groups, edit failure
// modes, get with output formats, helper functions.

func TestFirstNonEmptyHelper(t *testing.T) {
	if firstNonEmpty("", "x") != "x" {
		t.Fatal()
	}
	if firstNonEmpty() != "" {
		t.Fatal()
	}
}

func TestRateOf(t *testing.T) {
	if !strings.HasSuffix(rateOf(150_000_000), "ms") {
		t.Fatal()
	}
}

func TestAsErrChain(t *testing.T) {
	root := dashErrors.New(dashErrors.CodeConflict, "x")
	wrap := fmt.Errorf("outer: %w", root)
	var got *dashErrors.Error
	if !asErr(wrap, &got) || got.Code != dashErrors.CodeConflict {
		t.Fatal()
	}
	if asErr(fmt.Errorf("plain"), &got) {
		t.Fatal("plain must not match")
	}
}

func TestGetAllOutputFormats(t *testing.T) {
	formats := []string{"json", "yaml", "table", "wide", "name", "jsonpath={.spec.vni}", "template={{ .spec.vni }}\n"}
	for _, f := range formats {
		a, out, _ := testApp(t, &fakeClient{})
		if code := runArgs(a, "get", "vnet", "v1", "-o", f); code != 0 {
			t.Errorf("%s: exit %d", f, code)
		}
		if out.Len() == 0 {
			t.Errorf("%s: empty output", f)
		}
	}
}

func TestGetListJSONShape(t *testing.T) {
	a, out, _ := testApp(t, &fakeClient{})
	if code := runArgs(a, "get", "vnet", "-o", "json"); code != 0 {
		t.Fatal()
	}
	if !strings.Contains(out.String(), `"VnetList"`) {
		t.Fatalf("%s", out.String())
	}
}

func TestInventoryDpusFromSpecMalformed(t *testing.T) {
	if dpus := inventoryDpusFromSpec(map[string]any{}); dpus != nil {
		t.Fatal("no dpus key → nil")
	}
	if dpus := inventoryDpusFromSpec(map[string]any{"dpus": "not-a-list"}); dpus != nil {
		t.Fatal("wrong type → nil")
	}
	dpus := inventoryDpusFromSpec(map[string]any{"dpus": []any{
		map[string]any{"id": "a", "endpoint": "x:1", "labels": map[string]any{"r": "1", "bad": 42}},
		"not-a-map",
	}})
	if len(dpus) != 1 || dpus[0].ID != "a" || dpus[0].Labels["r"] != "1" {
		t.Fatalf("%+v", dpus)
	}
}

func TestApplyMultiDoc(t *testing.T) {
	a, _, _ := testApp(t, &fakeClient{})
	a.In = strings.NewReader(`apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v1 }
spec: { vni: 1 }
---
apiVersion: dashcenter.v1
kind: Vnet
metadata: { name: v2 }
spec: { vni: 2 }
`)
	if code := runArgs(a, "apply", "-f", "-"); code != 0 {
		t.Fatal()
	}
}

func TestGetBadOutputFormat(t *testing.T) {
	a, _, _ := testApp(t, &fakeClient{})
	// Persistent-flag bind resets a.Flags.output, so pass via argv.
	if code := runArgs(a, "get", "vnet", "v1", "-o", "garbage"); code == 0 {
		t.Fatal()
	}
}

// Exercise the explicit dialer override + the resolver guard branches in
// dial() — they're easier to hit directly than via a Cobra command.
func TestDialResolverErrorPath(t *testing.T) {
	a := &Application{
		Out:    discardWriter{},
		Err:    discardWriter{},
		Flags:  &globalFlags{},
		Config: config.New(),
	}
	a.setResolver(func() (*config.ResolvedConfig, error) {
		return nil, fmt.Errorf("nope")
	})
	if _, _, err := a.dial(context.Background()); err == nil {
		t.Fatal()
	}
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// Force the dialer error path.
func TestDialDialErrorPath(t *testing.T) {
	a := &Application{
		Out:    discardWriter{},
		Err:    discardWriter{},
		Flags:  &globalFlags{},
		Config: config.New(),
	}
	a.setResolver(func() (*config.ResolvedConfig, error) {
		return &config.ResolvedConfig{Transport: "rest", Endpoint: "http://localhost"}, nil
	})
	a.setDialer(func(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error) {
		return nil, fmt.Errorf("boom")
	})
	if _, _, err := a.dial(context.Background()); err == nil {
		t.Fatal()
	}
}

func TestRootContextNotNil(t *testing.T) {
	if rootContext() == nil {
		t.Fatal()
	}
}
