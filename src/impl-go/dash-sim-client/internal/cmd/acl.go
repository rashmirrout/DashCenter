package cmd

import (
	"fmt"
	"os"
	"strings"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newAclCmd() *cobra.Command {
	root := &cobra.Command{Use: "acl", Short: "Manage ACL groups and rules"}
	g := &cobra.Command{Use: "group", Short: "Manage ACL groups"}
	g.AddCommand(newAclGroupAddCmd())
	g.AddCommand(newAclGroupListCmd())
	g.AddCommand(newAclGroupDeleteCmd())
	root.AddCommand(g)

	r := &cobra.Command{Use: "rule", Short: "Manage ACL rules"}
	r.AddCommand(newAclRuleAddCmd())
	r.AddCommand(newAclRuleListCmd())
	r.AddCommand(newAclRuleDeleteCmd())
	root.AddCommand(r)
	return root
}

// ---- groups ----

func newAclGroupAddCmd() *cobra.Command {
	var stage string
	c := &cobra.Command{
		Use:   "add <group-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Add an ACL group",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.AddAclGroup(ctx, &dashsimv1.AclGroup{
				Id:    args[0],
				Stage: parseAclStage(stage),
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&stage, "stage", "INBOUND", "INBOUND or OUTBOUND")
	return c
}

func newAclGroupListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List ACL groups",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListAclGroups(ctx)
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Many[*dashsimv1.AclGroup](os.Stdout, f, items, aclGroupCols, aclGroupRow)
		},
	}
}

func newAclGroupDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <group-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete an ACL group (cascades to rules)",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteAclGroup(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var aclGroupCols = []string{"ID", "STAGE", "CREATED_NS"}

func aclGroupRow(g *dashsimv1.AclGroup) []string {
	return []string{g.GetId(), strings.TrimPrefix(g.GetStage().String(), "ACL_STAGE_"), fmt.Sprintf("%d", g.GetCreatedTsNs())}
}

// ---- rules ----

func newAclRuleAddCmd() *cobra.Command {
	var (
		groupID, action, src, dst, proto2 string
		num, srcPort, dstPort             uint32
	)
	c := &cobra.Command{
		Use:   "add",
		Args:  cobra.NoArgs,
		Short: "Add an ACL rule",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.AddAclRule(ctx, &dashsimv1.AclRule{
				GroupId:   groupID,
				Num:       num,
				Action:    parseAclAction(action),
				SrcPrefix: src,
				DstPrefix: dst,
				Protocol:  proto2,
				SrcPort:   srcPort,
				DstPort:   dstPort,
			})
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&groupID, "group-id", "", "owning group id (required)")
	c.Flags().Uint32Var(&num, "num", 0, "rule priority number (required, > 0)")
	c.Flags().StringVar(&action, "action", "ALLOW", "ALLOW or DENY")
	c.Flags().StringVar(&src, "src-prefix", "", "source CIDR")
	c.Flags().StringVar(&dst, "dst-prefix", "", "destination CIDR")
	c.Flags().StringVar(&proto2, "protocol", "", "ip protocol (tcp/udp/...)")
	c.Flags().Uint32Var(&srcPort, "src-port", 0, "source port")
	c.Flags().Uint32Var(&dstPort, "dst-port", 0, "destination port")
	_ = c.MarkFlagRequired("group-id")
	_ = c.MarkFlagRequired("num")
	return c
}

func newAclRuleListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Args:  cobra.NoArgs,
		Short: "List ACL rules",
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.ListAclRules(ctx)
			if err != nil {
				return err
			}
			f, _ := resolveFormat()
			return render.Many[*dashsimv1.AclRule](os.Stdout, f, items, aclRuleCols, aclRuleRow)
		},
	}
}

func newAclRuleDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <rule-id>",
		Args:  cobra.ExactArgs(1),
		Short: "Delete an ACL rule by id (group/num)",
		RunE: func(_ *cobra.Command, args []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.DeleteAclRule(ctx, args[0])
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
}

var aclRuleCols = []string{"ID", "GROUP", "NUM", "ACTION", "SRC", "DST", "PROTO", "SPORT", "DPORT"}

func aclRuleRow(r *dashsimv1.AclRule) []string {
	return []string{
		r.GetId(),
		r.GetGroupId(),
		fmt.Sprintf("%d", r.GetNum()),
		strings.TrimPrefix(r.GetAction().String(), "ACL_ACTION_"),
		r.GetSrcPrefix(),
		r.GetDstPrefix(),
		r.GetProtocol(),
		fmt.Sprintf("%d", r.GetSrcPort()),
		fmt.Sprintf("%d", r.GetDstPort()),
	}
}
