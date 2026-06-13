// Unit tests for the source/via JSON annotation added to fan-out
// frames so browsers can identify which dashd produced the event and
// which dashw replica relayed it.

package cluster

import (
	"strings"
	"testing"

	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
)

func TestInjectSourceVia_BothLabels(t *testing.T) {
	got := string(injectSourceVia([]byte(`{"kind":"KIND_KEEPALIVE","event_id":4}`), "dashd:9443", "dashw-1"))
	if !strings.Contains(got, `"source":"dashd:9443"`) || !strings.Contains(got, `"via":"dashw-1"`) {
		t.Fatalf("missing source/via: %q", got)
	}
	if !strings.HasSuffix(got, "}") || !strings.HasPrefix(got, "{") {
		t.Fatalf("not a JSON object: %q", got)
	}
	if !strings.Contains(got, `"kind":"KIND_KEEPALIVE"`) || !strings.Contains(got, `"event_id":4`) {
		t.Fatalf("original keys lost: %q", got)
	}
}

func TestInjectSourceVia_EmptyLabelsPassthrough(t *testing.T) {
	in := []byte(`{"kind":"KIND_KEEPALIVE"}`)
	got := injectSourceVia(in, "", "")
	if string(got) != string(in) {
		t.Fatalf("expected passthrough, got %q", got)
	}
}

func TestInjectSourceVia_OnlySource(t *testing.T) {
	got := string(injectSourceVia([]byte(`{"kind":"KIND_KEEPALIVE"}`), "dashd-1", ""))
	if !strings.Contains(got, `"source":"dashd-1"`) || strings.Contains(got, `"via":`) {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestInjectSourceVia_QuotesEscaped(t *testing.T) {
	// strconv.AppendQuote handles escaping; sanity check.
	got := string(injectSourceVia([]byte(`{"kind":"X"}`), `da"sh"d`, "dashw"))
	if !strings.Contains(got, `"source":"da\"sh\"d"`) {
		t.Fatalf("escape missing: %q", got)
	}
}

func TestInjectSourceVia_NonObjectPassthrough(t *testing.T) {
	cases := [][]byte{nil, []byte(""), []byte("null"), []byte(`["array"]`)}
	for _, c := range cases {
		got := injectSourceVia(c, "x", "y")
		if string(got) != string(c) {
			t.Fatalf("expected passthrough for %q, got %q", c, got)
		}
	}
}

func TestHubBuildFrame_StampsLabels(t *testing.T) {
	h := &Hub{cfg: HubConfig{UpstreamLabel: "dashd-1:9443", SelfLabel: "dashw-7"}}
	ev := &dashcenterv1.TopologyEvent{
		Kind:    dashcenterv1.TopologyEvent_KIND_KEEPALIVE,
		EventId: 42,
	}
	f, err := h.buildFrame(ev)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}
	js := string(f.JSON)
	if !strings.Contains(js, `"source":"dashd-1:9443"`) {
		t.Fatalf("source missing: %q", js)
	}
	if !strings.Contains(js, `"via":"dashw-7"`) {
		t.Fatalf("via missing: %q", js)
	}
	if !strings.Contains(js, `"kind":"KIND_KEEPALIVE"`) {
		t.Fatalf("kind lost: %q", js)
	}
}
