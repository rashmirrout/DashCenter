package model

import (
	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"google.golang.org/protobuf/proto"
)

// Deep copies via proto.Clone keep the in-memory map and the returned
// snapshots/events strictly disjoint, so a caller can't mutate the store by
// hanging on to a returned pointer.

func cloneVnet(in *dashsimv1.Vnet) *dashsimv1.Vnet {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.Vnet)
}

func cloneEni(in *dashsimv1.Eni) *dashsimv1.Eni {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.Eni)
}

func cloneVnetMapping(in *dashsimv1.VnetMapping) *dashsimv1.VnetMapping {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.VnetMapping)
}

func cloneRoute(in *dashsimv1.Route) *dashsimv1.Route {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.Route)
}

func cloneAclGroup(in *dashsimv1.AclGroup) *dashsimv1.AclGroup {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.AclGroup)
}

func cloneAclRule(in *dashsimv1.AclRule) *dashsimv1.AclRule {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*dashsimv1.AclRule)
}
