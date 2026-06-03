package cmd

import (
	"fmt"
	"os"

	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/spf13/cobra"
)

func newGetCmd() *cobra.Command {
	var (
		kind string
		key  string
	)
	c := &cobra.Command{
		Use:   "get",
		Short: "Read one object",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			k, err := parseKindArg(kind)
			if err != nil {
				return err
			}
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			obj, err := cl.Get(ctx, k, parseKeyArg(key))
			if err != nil {
				return err
			}
			fmtOut, err := resolveFormat()
			if err != nil {
				return err
			}
			return render.Object(os.Stdout, fmtOut, obj)
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "object kind (required)")
	c.Flags().StringVar(&key, "key", "", "joined key, e.g. vnet-prod or acl-in:1 (required)")
	_ = c.MarkFlagRequired("kind")
	_ = c.MarkFlagRequired("key")
	return c
}

func newDeleteCmd() *cobra.Command {
	var (
		kind string
		key  string
	)
	c := &cobra.Command{
		Use:   "delete",
		Short: "Remove one object (cascade is per-server semantics)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			k, err := parseKindArg(kind)
			if err != nil {
				return err
			}
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			ack, err := cl.Delete(ctx, k, parseKeyArg(key))
			if err != nil {
				return err
			}
			return printAck(ack)
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "object kind (required)")
	c.Flags().StringVar(&key, "key", "", "joined key (required)")
	_ = c.MarkFlagRequired("kind")
	_ = c.MarkFlagRequired("key")
	return c
}

func newListCmd() *cobra.Command {
	var (
		kind   string
		prefix string
		limit  int32
	)
	c := &cobra.Command{
		Use:   "list",
		Short: "Stream every object of a kind (optionally filtered by --prefix)",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			k, err := parseKindArg(kind)
			if err != nil {
				return err
			}
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			items, err := cl.List(ctx, k, prefix)
			if err != nil {
				return err
			}
			if limit > 0 && int32(len(items)) > limit {
				items = items[:limit]
			}
			fmtOut, err := resolveFormat()
			if err != nil {
				return err
			}
			return render.Objects(os.Stdout, fmtOut, items)
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "object kind (required)")
	c.Flags().StringVar(&prefix, "prefix", "", "joined-key prefix filter")
	c.Flags().Int32Var(&limit, "limit", 0, "max items to return (0 = no limit)")
	_ = c.MarkFlagRequired("kind")
	return c
}

func newCountersCmd() *cobra.Command {
	var (
		kind string
		key  string
	)
	c := &cobra.Command{
		Use:   "counters",
		Short: "Read synthetic counters for an object",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			k, err := parseKindArg(kind)
			if err != nil {
				return err
			}
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			m, err := cl.GetCounters(ctx, k, parseKeyArg(key))
			if err != nil {
				return err
			}
			fmtOut, err := resolveFormat()
			if err != nil {
				return err
			}
			return render.CountersMap(os.Stdout, fmtOut, m)
		},
	}
	c.Flags().StringVar(&kind, "kind", "", "object kind (required)")
	c.Flags().StringVar(&key, "key", "", "joined key (required)")
	_ = c.MarkFlagRequired("kind")
	_ = c.MarkFlagRequired("key")
	return c
}

func newPingCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ping",
		Short: "Connectivity check (dial + list vnets)",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			cl, err := dial()
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := rpcContext()
			defer cancel()
			k, _ := parseKindArg("vnet")
			vs, err := cl.List(ctx, k, "")
			if err != nil {
				return err
			}
			fmt.Printf("ok: target=%s vnets=%d\n", flagTarget, len(vs))
			return nil
		},
	}
}
