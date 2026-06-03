package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/spf13/cobra"
	"google.golang.org/protobuf/encoding/protojson"
)

func newSubscribeCmd() *cobra.Command {
	var (
		kindStrs      []string
		snapshotFirst bool
	)
	c := &cobra.Command{
		Use:   "subscribe",
		Short: "Stream Events (snapshot + live updates)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()

			parsed := make([]dashapi.ObjectKind, 0, len(kindStrs))
			for _, s := range kindStrs {
				k, err := parseKindArg(s)
				if err != nil {
					return err
				}
				parsed = append(parsed, k)
			}

			ctx, cancel := streamContext()
			defer cancel()

			evCh, errCh, err := cl.Subscribe(ctx, parsed, snapshotFirst)
			if err != nil {
				return err
			}

			marshaler := protojson.MarshalOptions{UseProtoNames: true}
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "")

			for {
				select {
				case ev, ok := <-evCh:
					if !ok {
						select {
						case err := <-errCh:
							return err
						default:
							return nil
						}
					}
					out := map[string]interface{}{
						"txn_id":       ev.GetTxnId(),
						"type":         strings.TrimPrefix(ev.GetType().String(), "EVENT_TYPE_"),
						"server_ts_ns": ev.GetServerTsNs(),
					}
					obj := ev.GetObject()
					if obj != nil {
						info, _ := kinds.Lookup(obj.GetKind())
						out["kind"] = info.Name
						out["key"] = obj.GetKey()
						if payload, err := kinds.PayloadOf(obj); err == nil && payload != nil {
							raw, _ := marshaler.Marshal(payload)
							var v interface{}
							_ = json.Unmarshal(raw, &v)
							out["value"] = v
						}
					}
					if err := enc.Encode(out); err != nil {
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
	c.Flags().StringSliceVar(&kindStrs, "kinds", nil, "filter on object kinds, e.g. vnet,eni")
	c.Flags().BoolVar(&snapshotFirst, "snapshot", false, "send a snapshot of current state before live events")
	return c
}

func newKindsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kinds",
		Short: "List supported DASH object kinds and their key field order",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			fmtOut, err := resolveFormat()
			if err != nil {
				return err
			}
			rows := make([]map[string]interface{}, 0, len(kinds.All))
			for _, info := range kinds.All {
				rows = append(rows, map[string]interface{}{
					"name":      info.Name,
					"enum":      dashapi.ObjectKind_name[int32(info.Kind)],
					"key_parts": info.KeyParts,
				})
			}
			switch fmtOut {
			case "yaml":
				for _, r := range rows {
					fmt.Printf("- name: %s\n  enum: %s\n  key_parts: %v\n", r["name"], r["enum"], r["key_parts"])
				}
			case "table":
				fmt.Printf("%-25s %-40s %s\n", "NAME", "ENUM", "KEY_PARTS")
				for _, r := range rows {
					kp := strings.Join(r["key_parts"].([]string), ",")
					fmt.Printf("%-25s %-40s %s\n", r["name"], r["enum"], kp)
				}
			default:
				b, _ := json.MarshalIndent(rows, "", "  ")
				fmt.Println(string(b))
			}
			return nil
		},
	}
}
