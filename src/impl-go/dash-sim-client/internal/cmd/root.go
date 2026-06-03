// Package cmd builds the Cobra command tree for dash-sim-client.
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
		Short: "Operator CLI for the DASH simulator gRPC API",
		Long: `dash-sim-client is the transport-only CLI for dashsim.v1.DashSim.

It dials a running dash-sim instance and exposes every RPC as a subcommand.
Use it for smoke tests, scenario tweaking, and to watch events live.`,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	root.PersistentFlags().StringVar(&flagTarget, "target", "localhost:50051", "dash-sim gRPC endpoint (host:port)")
	root.PersistentFlags().BoolVar(&flagInsecure, "insecure", true, "use plaintext gRPC (default true; set false for TLS — not wired yet)")
	root.PersistentFlags().StringVarP(&flagOutput, "output", "o", "json", "output format: json|yaml|table")
	root.PersistentFlags().DurationVar(&flagTimeout, "timeout", 10*time.Second, "per-RPC timeout")

	root.AddCommand(newVnetCmd())
	root.AddCommand(newEniCmd())
	root.AddCommand(newAclCmd())
	root.AddCommand(newRouteCmd())
	root.AddCommand(newMappingCmd())
	root.AddCommand(newSubscribeCmd())
	root.AddCommand(newCountersCmd())
	root.AddCommand(newHealthCmd())

	return root
}

// Execute runs the root command. Used by cmd/dash-sim-client/main.go.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		// Cobra already printed the error.
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
	// Allow ctrl-c to break long-running streams cleanly.
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
