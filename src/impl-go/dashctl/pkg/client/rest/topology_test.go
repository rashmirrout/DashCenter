// Topology snapshot + SSE-stream wire tests for the REST backend.
//
// Covers:
//   * GetTopology decodes the protojson snapshot into TopologySnapshot.
//   * StreamTopology parses multi-line `data:`, dispatches per event,
//     skips `:keepalive` comments + `id:` metadata, honours Last-Event-ID.
//   * OnEvent's sentinel error stops the stream cleanly.

package rest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func TestGetTopologyDecodesSnapshot(t *testing.T) {
	f := newFixture(t)
	f.apiMux.HandleFunc("/v1/cluster/topology", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("include_enis") != "true" {
			t.Errorf("want include_enis=true, got %q", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"computed_at":"2026-06-13T00:00:00Z",
			"cluster":{"healthy":true,"leader_id":"dashd-1","node_count":1,
				"nodes":[{"node_id":"dashd-1","rest_addr":":8443","is_leader":true}]},
			"summary":{"total_appliances":0,"total_dpus":0,"total_enis":0,"healthy_dpus":0}
		}`))
	})

	snap, err := f.c.GetTopology(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if snap.Cluster == nil || snap.Cluster.LeaderID != "dashd-1" || !snap.Cluster.Nodes[0].IsLeader {
		t.Fatalf("decoded snapshot wrong: %+v", snap)
	}
}

func TestStreamTopologyParsesSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Last-Event-ID"); got != "5" {
			t.Errorf("want Last-Event-ID=5, got %q", got)
		}
		if got := r.URL.Query().Get("last_event_id"); got != "5" {
			t.Errorf("want last_event_id=5 query, got %q", got)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// snapshot event
		_, _ = fmt.Fprint(w, "event: snapshot\nid: 6\ndata: {\"kind\":\"KIND_SNAPSHOT\",\"event_id\":6}\n\n")
		// keepalive comment (must be skipped)
		_, _ = fmt.Fprint(w, ":keepalive\n\n")
		// peer_added with multi-line data
		_, _ = fmt.Fprint(w, "event: peer_added\nid: 7\ndata: {\"kind\":\"KIND_PEER_ADDED\",\"event_id\":7,\n")
		_, _ = fmt.Fprint(w, "data: \"peer\":{\"node_id\":\"dashd-9\"}}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()

	// Point a client at the SSE server.
	c := &Client{baseURL: srv.URL, httpc: &http.Client{Transport: srv.Client().Transport}}

	var seen []client.TopologyEvent
	err := c.StreamTopology(context.Background(), client.TopologyWatchOptions{
		LastEventID: 5,
		OnEvent: func(ev client.TopologyEvent) error {
			seen = append(seen, ev)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(seen), seen)
	}
	if seen[0].Kind != "KIND_SNAPSHOT" || seen[0].EventID != 6 {
		t.Errorf("event[0] = %+v", seen[0])
	}
	if seen[1].Kind != "KIND_PEER_ADDED" || seen[1].Peer == nil || seen[1].Peer.NodeID != "dashd-9" {
		t.Errorf("event[1] = %+v", seen[1])
	}
}

func TestStreamTopologyStopsOnCallbackError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Push three frames; we expect only the first one to be observed.
		for i := 1; i <= 3; i++ {
			_, _ = fmt.Fprintf(w, "data: {\"kind\":\"KIND_KEEPALIVE\",\"event_id\":%d}\n\n", i)
		}
	}))
	defer srv.Close()

	c := &Client{baseURL: srv.URL, httpc: &http.Client{Transport: srv.Client().Transport}}

	stopErr := fmt.Errorf("stop now")
	got := 0
	err := c.StreamTopology(context.Background(), client.TopologyWatchOptions{
		OnEvent: func(ev client.TopologyEvent) error {
			got++
			return stopErr
		},
	})
	if err != stopErr {
		t.Fatalf("want sentinel error, got %v", err)
	}
	if got != 1 {
		t.Fatalf("want 1 callback before stop, got %d", got)
	}
}

func TestStreamTopologyRejectsNilCallback(t *testing.T) {
	c := &Client{baseURL: "http://x"}
	err := c.StreamTopology(context.Background(), client.TopologyWatchOptions{})
	if err == nil || !strings.Contains(err.Error(), "OnEvent") {
		t.Fatalf("want OnEvent-required error, got %v", err)
	}
}
