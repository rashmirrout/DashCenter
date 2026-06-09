package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/render"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/manifest"
)

func (a *Application) newGetCmd() *cobra.Command {
	var (
		selector string
		limit    int
	)
	c := &cobra.Command{
		Use:   "get <kind> [name]",
		Short: "Read one or many specs",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			kindArg := args[0]
			ki, ok := manifest.LookupKind(kindArg)
			if !ok {
				return errors.Newf(errors.CodeInvalidArgument, "get: unknown kind %q", kindArg)
			}
			cl, rc, err := a.dial(cmd.Context())
			if err != nil {
				return err
			}
			defer cl.Close()
			ctx, cancel := withTimeout(cmd.Context(), rc)
			defer cancel()

			if len(args) >= 2 {
				return a.getOne(ctx, cl, rc.Namespace, ki, args[1])
			}
			return a.getMany(ctx, cl, rc.Namespace, ki, client.ListOptions{Selector: selector, Limit: limit})
		},
	}
	c.Flags().StringVarP(&selector, "selector", "l", "", "label selector (e.g. tier=prod)")
	c.Flags().IntVar(&limit, "limit", 0, "max items to return (0 = unlimited)")
	return c
}

func (a *Application) getOne(ctx context.Context, cl client.Client, ns string, ki manifest.KindInfo, name string) error {
	if ki.Kind == "Inventory" {
		dpus, err := cl.GetInventory(ctx)
		if err != nil {
			return err
		}
		return a.renderInventory(dpus)
	}
	it, err := cl.Get(ctx, ns, ki.StoreKind, name)
	if err != nil {
		return err
	}
	return a.renderItems([]*client.StoredItem{it}, ki)
}

func (a *Application) getMany(ctx context.Context, cl client.Client, ns string, ki manifest.KindInfo, opts client.ListOptions) error {
	if ki.Kind == "Inventory" {
		dpus, err := cl.GetInventory(ctx)
		if err != nil {
			return err
		}
		return a.renderInventory(dpus)
	}
	items, err := cl.List(ctx, ns, ki.StoreKind, opts)
	if err != nil {
		return err
	}
	return a.renderItems(items, ki)
}

func (a *Application) renderItems(items []*client.StoredItem, ki manifest.KindInfo) error {
	format, expr, err := a.outFormat()
	if err != nil {
		return errors.Wrap(errors.CodeInvalidArgument, "get", err)
	}
	if format == "" {
		format = render.DefaultFor(a.Out)
	}

	switch format {
	case render.FormatName:
		for _, it := range items {
			fmt.Fprintln(a.Out, render.ItemName(it))
		}
		return nil
	case render.FormatJSON, render.FormatYAML:
		envelopes, err := itemsToEnvelopes(items, ki)
		if err != nil {
			return err
		}
		// Single-item Get prints just the envelope; List prints a list under "items".
		if len(envelopes) == 1 {
			return render.Render(a.Out, envelopes[0], render.Options{Format: format})
		}
		payload := map[string]any{
			"apiVersion": manifest.APIVersion,
			"kind":       ki.Kind + "List",
			"items":      envelopes,
		}
		return render.Render(a.Out, payload, render.Options{Format: format})
	case render.FormatJSONPath, render.FormatTemplate:
		// Run on the same payload we'd use for json.
		envelopes, err := itemsToEnvelopes(items, ki)
		if err != nil {
			return err
		}
		var payload any
		if len(envelopes) == 1 {
			payload = envelopes[0]
		} else {
			payload = map[string]any{"items": envelopes}
		}
		return render.Render(a.Out, payload, render.Options{Format: format, Expression: expr})
	case render.FormatTable, render.FormatWide:
		rows := make([]any, 0, len(items))
		for _, it := range items {
			rows = append(rows, it)
		}
		opts := render.Options{Format: format, Columns: render.ColumnsFor(ki.StoreKind), Wide: format == render.FormatWide}
		return render.Render(a.Out, rows, opts)
	default:
		return errors.Newf(errors.CodeInvalidArgument, "get: unsupported format %q", format)
	}
}

func itemsToEnvelopes(items []*client.StoredItem, ki manifest.KindInfo) ([]*manifest.Envelope, error) {
	out := make([]*manifest.Envelope, 0, len(items))
	for _, it := range items {
		env, err := manifest.EnvelopeFromStoredItem(ki.StoreKind, it.Namespace, it.Name, it.Generation, it.Spec, nil)
		if err != nil {
			return nil, errors.Wrap(errors.CodeInternal, "get: convert", err)
		}
		out = append(out, env)
	}
	return out, nil
}

func (a *Application) renderInventory(dpus []client.DpuStatus) error {
	format, _, err := a.outFormat()
	if err != nil {
		return err
	}
	if format == "" {
		format = render.DefaultFor(a.Out)
	}
	switch format {
	case render.FormatJSON, render.FormatYAML:
		return render.Render(a.Out, map[string]any{"apiVersion": manifest.APIVersion, "kind": "InventoryList", "dpus": dpus}, render.Options{Format: format})
	case render.FormatName:
		for _, d := range dpus {
			fmt.Fprintln(a.Out, "dpu/"+d.ID)
		}
		return nil
	}
	rows := make([]any, 0, len(dpus))
	for _, d := range dpus {
		rows = append(rows, d)
	}
	return render.Render(a.Out, rows, render.Options{Format: format, Columns: render.DpuColumns()})
}
