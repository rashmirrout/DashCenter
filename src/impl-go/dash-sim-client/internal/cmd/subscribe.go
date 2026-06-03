package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newSubscribeCmd() *cobra.Command {
	var (
		kinds         []string
		snapshotFirst bool
	)
	c := &cobra.Command{
		Use:   "subscribe",
		Short: "Stream Events from the simulator (snapshot + live updates)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()

			parsedKinds := make([]dashsimv1.ObjectKind, 0, len(kinds))
			for _, k := range kinds {
				kind, err := parseObjectKind(k)
				if err != nil {
					return err
				}
				parsedKinds = append(parsedKinds, kind)
			}

			ctx, cancel := streamContext()
			defer cancel()

			evCh, errCh, err := cl.Subscribe(ctx, parsedKinds, snapshotFirst)
			if err != nil {
				return err
			}

			marshaler := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: false}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "")

			for {
				select {
				case ev, ok := <-evCh:
					if !ok {
						// stream ended; check errCh
						select {
						case err := <-errCh:
							return err
						default:
							return nil
						}
					}
					raw, err := marshaler.Marshal(ev)
					if err != nil {
						return err
					}
					// emit JSON line
					var v interface{}
					_ = json.Unmarshal(raw, &v)
					if err := enc.Encode(v); err != nil {
						return err
					}
				case err := <-errCh:
					if err != nil {
						return err
					}
					return nil
				}
			}
		},
	}
	c.Flags().StringSliceVar(&kinds, "kinds", nil, "filter on object kinds: vnet,eni,vnet_mapping,route,acl_group,acl_rule")
	c.Flags().BoolVar(&snapshotFirst, "snapshot", false, "send a full snapshot of current state before live events")
	return c
}

func parseObjectKind(s string) (dashsimv1.ObjectKind, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "vnet":
		return dashsimv1.ObjectKind_OBJECT_KIND_VNET, nil
	case "eni":
		return dashsimv1.ObjectKind_OBJECT_KIND_ENI, nil
	case "vnet_mapping", "mapping":
		return dashsimv1.ObjectKind_OBJECT_KIND_VNET_MAPPING, nil
	case "route":
		return dashsimv1.ObjectKind_OBJECT_KIND_ROUTE, nil
	case "acl_group", "aclgroup":
		return dashsimv1.ObjectKind_OBJECT_KIND_ACL_GROUP, nil
	case "acl_rule", "aclrule":
		return dashsimv1.ObjectKind_OBJECT_KIND_ACL_RULE, nil
	}
	return dashsimv1.ObjectKind_OBJECT_KIND_UNSPECIFIED, fmt.Errorf("unknown kind %q", s)
}
