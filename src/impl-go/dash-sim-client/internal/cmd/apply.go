package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var (
		file  string
		kind  string
		key   string
		value string
	)
	c := &cobra.Command{
		Use:   "apply",
		Short: "Create or replace one or more DASH objects",
		Long: `Apply creates or replaces objects on the target.

Two input modes:

  -f, --file <path>   read one or more {kind, key, value} docs from JSON or
                      a YAML stream (multi-doc supported via '---').
                      Each doc's "value" follows upstream protojson naming.

  --kind <name> --key k1[:k2[:...]] --value <json>
                      single-shot inline form.

Examples:

  dash-sim-client apply -f scenario.yaml
  dash-sim-client apply --kind vnet --key vnet-prod --value '{"vni":1001}'
  dash-sim-client apply --kind vnet_mapping --key vnet-prod:10.0.0.10 \
      --value '{"underlay_ip":{"ipv4":1681915680},"routing_type":"ROUTING_TYPE_VNET"}'`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()

			var docs []docEntry
			switch {
			case file != "":
				docs, err = readDocuments(file)
				if err != nil {
					return err
				}
			case kind != "":
				var v interface{}
				if value != "" {
					if err := jsonOrYAMLUnmarshal(value, &v); err != nil {
						return err
					}
				}
				docs = []docEntry{{Kind: kind, Key: parseKeyArg(key), Value: v}}
			default:
				return fmt.Errorf("apply: provide either -f <file> or --kind/--key/--value")
			}

			ctx, cancel := rpcContext()
			defer cancel()

			for i, d := range docs {
				obj, err := docToObject(d)
				if err != nil {
					return fmt.Errorf("apply[%d]: %w", i, err)
				}
				ack, err := cl.Apply(ctx, obj)
				if err != nil {
					return err
				}
				if err := printAck(ack); err != nil {
					return err
				}
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "path to JSON/YAML scenario file")
	c.Flags().StringVar(&kind, "kind", "", "object kind (short name or enum)")
	c.Flags().StringVar(&key, "key", "", "joined key, e.g. vnet-prod or vnet-prod:10.0.0.10")
	c.Flags().StringVar(&value, "value", "", "inline JSON or YAML value")
	return c
}

func jsonOrYAMLUnmarshal(s string, v *interface{}) error {
	trimmed := trimSpace(s)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return jsonUnmarshal([]byte(s), v)
	}
	return yamlUnmarshal([]byte(s), v)
}
