// events.go — helpers that translate internal state changes (registry,
// inventory, elector) into dashcenter.v1.TopologyEvent values for the
// broadcaster.
//
// Keeping these in a dedicated file (rather than inline at the
// callsites in main.go) means the wire shape of every event lives in
// one place, which simplifies review + audit of the kinds main.go is
// allowed to publish.
package cluster

import (
	dashcenterv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashcenter/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// RegistryEvent builds a TopologyEvent for a peer-registry change.
// leaderID is the current cluster.leader_id at the time of the change
// — included so subscribers can re-derive is_leader on the affected
// peer without an extra RPC.
func RegistryEvent(kind ChangeKind, peer PeerInfo, leaderID string) *dashcenterv1.TopologyEvent {
	ev := &dashcenterv1.TopologyEvent{
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.TopologyEvent_Peer{Peer: peerToNodeInfo(peer, leaderID)},
	}
	switch kind {
	case ChangeAdded:
		ev.Kind = dashcenterv1.TopologyEvent_KIND_PEER_ADDED
	case ChangeRemoved:
		ev.Kind = dashcenterv1.TopologyEvent_KIND_PEER_REMOVED
	case ChangeUpdated:
		ev.Kind = dashcenterv1.TopologyEvent_KIND_PEER_UPDATED
	}
	return ev
}

// LeaderChangedEvent builds a TopologyEvent for a leader election
// handoff. Both ids may be empty (e.g. first leader after boot has no
// `from`).
func LeaderChangedEvent(fromID, toID string) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind:        dashcenterv1.TopologyEvent_KIND_LEADER_CHANGED,
		Ts:          timestamppb.Now(),
		OldLeaderId: fromID,
		NewLeaderId: toID,
	}
}

// DpuEvent builds a TopologyEvent for an inventory DPU change. The
// caller picks the kind (added / removed / state-changed) based on
// the inventory.SubscribeFunc semantics.
func DpuEvent(kind dashcenterv1.TopologyEvent_Kind, dpu *dashcenterv1.DpuTopInfo) *dashcenterv1.TopologyEvent {
	return &dashcenterv1.TopologyEvent{
		Kind: kind,
		Ts:   timestamppb.Now(),
		Body: &dashcenterv1.TopologyEvent_Dpu{Dpu: dpu},
	}
}
