package cmd

import (
	"fmt"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/spf13/cobra"
)

func newApplyCmd() *cobra.Command {
	var (
		file  string
		kind  string
		key   string
		value string
		force bool
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

Create vs. modify detection:
  - New objects are created normally.
  - Existing objects trigger a WARNING.
  - Without --force, modifications are BLOCKED.
  - With --force, existing objects are overwritten.

Examples:

  dash-sim-client apply -f scenario.yaml
  dash-sim-client apply -f scenario.yaml --force
  dash-sim-client apply --kind vnet --key vnet-prod --value '{"vni":1001}'`,
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

			created, modified, blocked, failed := 0, 0, 0, 0
			for _, d := range docs {
				obj, err := docToObject(d)
				if err != nil {
					fmt.Printf("%s/%s FAILED: %v\n", d.Kind, strings.Join(d.Key, ":"), err)
					failed++
					continue
				}

				// Resolve kind for existence check.
				kindEnum := obj.GetKind()
				keyParts := obj.GetKey()
				joinedKey := strings.Join(keyParts, ":")

				// Check if object already exists.
				_, getErr := cl.Get(ctx, kindEnum, keyParts)
				exists := getErr == nil

				if exists && !force {
					fmt.Printf("%s/%s BLOCKED — already exists; use --force to overwrite\n",
						kindNameForDisplay(kindEnum), joinedKey)
					blocked++
					continue
				}

				ack, err := cl.Apply(ctx, obj)
				if err != nil {
					fmt.Printf("%s/%s FAILED: %v\n", kindNameForDisplay(kindEnum), joinedKey, err)
					failed++
					continue
				}
				if !ack.GetAccepted() {
					fmt.Printf("%s/%s FAILED: %s\n", kindNameForDisplay(kindEnum), joinedKey, ack.GetError())
					failed++
					continue
				}

				op := "CREATE"
				if exists {
					op = "MODIFY"
					modified++
				} else {
					created++
				}
				fmt.Printf("%s/%s %s txn=%s\n", kindNameForDisplay(kindEnum), joinedKey, op, ack.GetTxnId())
			}

			total := created + modified + blocked + failed
			fmt.Printf("\nApplied %d object(s): %d created, %d modified, %d blocked, %d failed\n",
				total, created, modified, blocked, failed)

			if blocked > 0 {
				fmt.Printf("\nWARNING: %d object(s) already exist and were NOT modified.\n", blocked)
				fmt.Printf("  To overwrite: dash-sim-client apply -f <file> --force\n")
				return fmt.Errorf("%d object(s) blocked — use --force to overwrite", blocked)
			}
			if failed > 0 {
				return fmt.Errorf("%d object(s) failed", failed)
			}
			return nil
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "path to JSON/YAML scenario file")
	c.Flags().StringVar(&kind, "kind", "", "object kind (short name or enum)")
	c.Flags().StringVar(&key, "key", "", "joined key, e.g. vnet-prod or vnet-prod:10.0.0.10")
	c.Flags().StringVar(&value, "value", "", "inline JSON or YAML value")
	c.Flags().BoolVar(&force, "force", false, "allow overwriting existing objects")
	return c
}

func kindNameForDisplay(k dashapi.ObjectKind) string {
	info, err := kinds.Lookup(k)
	if err != nil {
		return k.String()
	}
	return info.Name
}

func jsonOrYAMLUnmarshal(s string, v *interface{}) error {
	trimmed := trimSpace(s)
	if len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[') {
		return jsonUnmarshal([]byte(s), v)
	}
	return yamlUnmarshal([]byte(s), v)
}
