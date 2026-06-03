package cmd

import (
	"fmt"
	"strings"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
)

func parseAclStage(s string) dashsimv1.AclStage {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "INBOUND":
		return dashsimv1.AclStage_ACL_STAGE_INBOUND
	case "OUTBOUND":
		return dashsimv1.AclStage_ACL_STAGE_OUTBOUND
	}
	return dashsimv1.AclStage_ACL_STAGE_UNSPECIFIED
}

func parseAclAction(s string) dashsimv1.AclAction {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ALLOW", "PERMIT":
		return dashsimv1.AclAction_ACL_ACTION_ALLOW
	case "DENY", "DROP":
		return dashsimv1.AclAction_ACL_ACTION_DENY
	}
	return dashsimv1.AclAction_ACL_ACTION_UNSPECIFIED
}

func parseRouteAction(s string) dashsimv1.RouteAction {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "FORWARD":
		return dashsimv1.RouteAction_ROUTE_ACTION_FORWARD
	case "DROP":
		return dashsimv1.RouteAction_ROUTE_ACTION_DROP
	case "ENCAP":
		return dashsimv1.RouteAction_ROUTE_ACTION_ENCAP
	}
	return dashsimv1.RouteAction_ROUTE_ACTION_UNSPECIFIED
}

func parseLabels(in []string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for _, kv := range in {
		i := strings.IndexByte(kv, '=')
		if i <= 0 {
			continue
		}
		out[kv[:i]] = kv[i+1:]
	}
	return out
}

func fmtLabels(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(m))
	for k, v := range m {
		parts = append(parts, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(parts, ",")
}

func printAck(ack *dashsimv1.Ack) error {
	if ack == nil {
		return fmt.Errorf("nil ack")
	}
	status := "OK"
	if !ack.GetAccepted() {
		status = "REJECTED"
	}
	if ack.GetError() != "" {
		fmt.Printf("%s txn=%s error=%s ts=%d\n", status, ack.GetTxnId(), ack.GetError(), ack.GetServerTsNs())
		if !ack.GetAccepted() {
			return fmt.Errorf("server rejected: %s", ack.GetError())
		}
	} else {
		fmt.Printf("%s txn=%s ts=%d\n", status, ack.GetTxnId(), ack.GetServerTsNs())
	}
	return nil
}
