// Package cmd builds the Cobra command tree for dash-sim-client.
//
// Subcommands:
//
//	apply       — create or replace an Object (from --file or --kind/--key/--value)
//	get         — read an Object by --kind --key
//	delete      — remove an Object by --kind --key
//	list        — list every Object of --kind [--prefix]
//	subscribe   — stream Events (snapshot + live)
//	counters    — read synthetic counters for --kind --key
//	kinds       — list supported ObjectKind names
//	ping        — connectivity check
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/internal/render"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim-client/pkg/client"
	"github.com/spf13/cobra"
)

var (
	flagTarget   string
	flagInsecure bool
	flagOutput   string
	flagTimeout  time.Duration
)

// NewRootCmd returns the top-level cobra command for `dash-sim-client`.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "dash-sim-client",
		Short: "Operator CLI for the dashapi.v1.DashApi gRPC service",
		Long: `dash-sim-client is the transport-only CLI for dashapi.v1.DashApi.

It dials any server that implements that API: the dash-sim behavioural
simulator, or — in phase 3 — a vendor adapter on real DASH-compliant
hardware. All payload types come straight from the upstream sonic-dash-api
schema, so the same command line works against both.`,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringVar(&flagTarget, "target", "localhost:50051", "DashApi gRPC endpoint (host:port)")
	root.PersistentFlags().BoolVar(&flagInsecure, "insecure", true, "use plaintext gRPC (TLS not wired yet)")
	root.PersistentFlags().StringVarP(&flagOutput, "output", "o", "json", "output format: json|yaml|table")
	root.PersistentFlags().DurationVar(&flagTimeout, "timeout", 10*time.Second, "per-RPC timeout")

	root.AddCommand(newApplyCmd())
	root.AddCommand(newGetCmd())
	root.AddCommand(newDeleteCmd())
	root.AddCommand(newListCmd())
	root.AddCommand(newSubscribeCmd())
	root.AddCommand(newCountersCmd())
	root.AddCommand(newKindsCmd())
	root.AddCommand(newPingCmd())

	return root
}

// Execute runs the root command. Used by cmd/dash-sim-client/main.go.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}

// -----------------------------------------------------------------------------
// Helpers shared across subcommands
// -----------------------------------------------------------------------------

func dial() (*client.Client, error) {
	if !flagInsecure {
		return nil, fmt.Errorf("TLS not yet supported; pass --insecure")
	}
	return client.Dial(flagTarget)
}

func rpcContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), flagTimeout)
}

func streamContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()
	return ctx, cancel
}

func resolveFormat() (render.Format, error) {
	return render.ParseFormat(flagOutput)
}
