package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

func sampleTopology() *client.TopologySnapshot {
	return &client.TopologySnapshot{
		Cluster: &client.TopologyClusterInfo{
			Healthy:   true,
			LeaderID:  "dashd-2",
			NodeCount: 2,
			Nodes: []client.TopologyClusterNode{
				{NodeID: "dashd-1", RestAddr: ":8443", GrpcAddr: ":9443", Version: "v1"},
				{NodeID: "dashd-2", RestAddr: ":8443", GrpcAddr: ":9443", Version: "v1", IsLeader: true},
			},
		},
		Appliances: []client.TopologyAppliance{
			{ID: "appliance-1", Zone: "us-west-2a", Tier: "gold", Dpus: []client.TopologyDpu{
				{ID: "dpu-sim-01", Slot: 0, State: "DPU_STATE_UP", EniCount: 4},
				{ID: "dpu-sim-02", Slot: 1, State: "DPU_STATE_UP", EniCount: 4, Cordoned: true},
			}},
		},
		Summary: &client.TopologySummary{
			TotalAppliances: 1, TotalDpus: 2, TotalEnis: 8, HealthyDpus: 2, CordonedDpus: 1,
		},
		Objects: map[string]client.TopologyNamespaceObjectCounts{
			"default": {Vnets: 5, Enis: 8, AclPolicies: 3},
		},
	}
}

func TestTopologySnapshotPretty(t *testing.T) {
	fc := &fakeClient{getTopologyFn: func(ctx context.Context, includeEnis bool) (*client.TopologySnapshot, error) {
		if includeEnis {
			t.Errorf("default should NOT include enis")
		}
		return sampleTopology(), nil
	}}
	a, out, _ := testApp(t, fc)
	a.Flags.output = "" // make sure JSON path is not auto-triggered
	if code := runArgs(a, "topology"); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	s := out.String()
	for _, want := range []string{
		"CLUSTER  nodes=2",
		"leader=dashd-2",
		"dashd-1",
		"* dashd-2",
		"SUMMARY",
		"appliances=1",
		"CORDONED",
		"OBJECTS",
		"default",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in output:\n%s", want, s)
		}
	}
}

func TestTopologySnapshotJSON(t *testing.T) {
	fc := &fakeClient{getTopologyFn: func(ctx context.Context, includeEnis bool) (*client.TopologySnapshot, error) {
		return sampleTopology(), nil
	}}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "topology", "--json"); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	s := out.String()
	if !strings.Contains(s, `"leader_id": "dashd-2"`) || !strings.Contains(s, `"total_dpus": 2`) {
		t.Fatalf("json output missing fields: %s", s)
	}
}

func TestTopologySnapshotIncludeEnis(t *testing.T) {
	called := false
	fc := &fakeClient{getTopologyFn: func(ctx context.Context, includeEnis bool) (*client.TopologySnapshot, error) {
		called = true
		if !includeEnis {
			t.Errorf("expected includeEnis=true")
		}
		return sampleTopology(), nil
	}}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "topology", "--include-enis", "--json"); code != 0 {
		t.Fatal()
	}
	if !called {
		t.Fatal("GetTopology not called")
	}
}

func TestTopologyFollowPretty(t *testing.T) {
	fc := &fakeClient{
		streamTopologyFn: func(ctx context.Context, opts client.TopologyWatchOptions) error {
			if opts.LastEventID != 7 {
				t.Errorf("LastEventID=%d, want 7", opts.LastEventID)
			}
			// Drive three frames synchronously, then return cleanly.
			_ = opts.OnEvent(client.TopologyEvent{
				Kind: "KIND_SNAPSHOT", EventID: 1, Ts: "2026-06-13T00:00:00Z",
				Snapshot: sampleTopology(),
			})
			_ = opts.OnEvent(client.TopologyEvent{
				Kind: "KIND_PEER_REMOVED", EventID: 8, Ts: "2026-06-13T00:00:01Z",
				Peer: &client.TopologyClusterNode{NodeID: "dashd-2", RestAddr: ":8443"},
			})
			_ = opts.OnEvent(client.TopologyEvent{
				Kind: "KIND_DROPPED", EventID: 9,
				Notice: &client.TopologyNotice{DroppedCount: 4, Message: "slow subscriber"},
			})
			return nil
		},
	}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "topology", "--follow", "--since-id", "7"); code != 0 {
		t.Fatalf("exit=%d out=%s", code, out)
	}
	s := out.String()
	if !strings.Contains(s, "following dashd topology stream") {
		t.Errorf("missing banner: %s", s)
	}
	for _, want := range []string{"SNAPSHOT", "PEER_REMOVED", "peer=dashd-2", "DROPPED", "dropped=4"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q: %s", want, s)
		}
	}
}

func TestTopologyFollowJSON(t *testing.T) {
	fc := &fakeClient{
		streamTopologyFn: func(ctx context.Context, opts client.TopologyWatchOptions) error {
			_ = opts.OnEvent(client.TopologyEvent{Kind: "KIND_KEEPALIVE", EventID: 42})
			return nil
		},
	}
	a, out, _ := testApp(t, fc)
	if code := runArgs(a, "topology", "--follow", "--json"); code != 0 {
		t.Fatal()
	}
	s := out.String()
	if !strings.Contains(s, `"kind":"KIND_KEEPALIVE"`) || !strings.Contains(s, `"event_id":42`) {
		t.Fatalf("json line missing: %s", s)
	}
}

func TestTopologyFollowSurfacesError(t *testing.T) {
	fc := &fakeClient{
		streamTopologyFn: func(ctx context.Context, opts client.TopologyWatchOptions) error {
			return errors.New("upstream gone")
		},
	}
	a, _, _ := testApp(t, fc)
	if code := runArgs(a, "topology", "--follow"); code == 0 {
		t.Fatal("expected non-zero exit")
	}
}
