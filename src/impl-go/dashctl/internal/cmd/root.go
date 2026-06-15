// Package cmd builds the Cobra command tree for dashctl.
//
// Subcommands are organised into:
//
//	root.go      — global flags, version banner, command registration
//	apply.go     — declarative apply -f
//	get.go       — generic read (single + list)
//	describe.go  — human-readable detail
//	delete.go    — generic delete
//	edit.go      — fetch → $EDITOR → re-apply (CAS)
//	replace.go   — strict-CAS write
//	diff.go      — manifest vs server preview
//	reconcile.go — POST /v1/reconcile
//	dpu.go       — dpu list/status/drift (+ Phase 2 cordon/drain stubs)
//	inventory.go — inventory put/get
//	events.go    — long-poll event stream (Phase 1 best-effort)
//	version.go   — client + server version
//	config.go    — context management
//	completion.go — shell completion
//	explain.go   — offline field reference
//	typed.go     — vnet/eni/... typed convenience subcommands
//	phase2.go    — ha / migration / trace stubs (return Unimplemented)
package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/errors"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/internal/render"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"

	// Force-import REST backend so its init() registers the factory.
	_ "github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client/rest"
)

// BuildInfo is populated by main.go at link time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// globalFlags is mutated by Cobra's persistent flag wiring.
type globalFlags struct {
	configPath    string
	context       string
	endpoint      string
	adminEndpoint string
	transport     string
	namespace     string
	allNamespaces bool
	output        string
	timeout       string
	token         string
	caFile        string
	certFile      string
	keyFile       string
	insecure      bool
	insecureSet   bool
	skipVerify    bool
	skipVerifySet bool
	color         string
	quiet         bool
	verbose       bool
	logLevel      string
	logFile       string
}

// Application bundles the runtime that subcommands need.
type Application struct {
	Build  BuildInfo
	Out    io.Writer
	Err    io.Writer
	In     io.Reader
	Flags  *globalFlags
	Config *config.Config
	Env    config.Env

	// resolveContext is overridable in tests to inject a fake config.
	resolveContext func() (*config.ResolvedConfig, error)

	// dialer is overridable in tests to inject a fake client.
	dialer func(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error)
}

// NewApp builds an Application that reads/writes to the standard streams.
func NewApp(b BuildInfo) *Application {
	return &Application{
		Build:  b,
		Out:    os.Stdout,
		Err:    os.Stderr,
		In:     os.Stdin,
		Flags:  &globalFlags{},
		Env:    config.ReadEnv(),
		dialer: client.Dial,
	}
}

// Execute is the entry point used by cmd/dashctl/main.go.
func Execute(b BuildInfo) int {
	app := NewApp(b)
	return app.Run(os.Args[1:])
}

// Run runs the CLI with the given argv (no program name). Returns the
// dashctl-documented exit code.
func (a *Application) Run(args []string) int {
	root := a.newRootCmd()
	root.SetArgs(args)
	root.SetIn(a.In)
	root.SetOut(a.Out)
	root.SetErr(a.Err)
	err := root.ExecuteContext(rootContext())
	if err == nil {
		return 0
	}
	// Cobra prints usage for "unknown command" and similar — we must not
	// double-print. Cobra's own errors get a generic exit-1.
	if _, ok := err.(*errors.Error); !ok {
		// Cobra-level errors include usage info; surface a short tag.
		fmt.Fprintln(a.Err, "Error:", err)
		return int(errors.CodeUsage)
	}
	fmt.Fprint(a.Err, errors.Format(err))
	return errors.ExitCodeOf(err)
}

// rootContext returns a context cancelled on SIGINT / SIGTERM.
func rootContext() context.Context {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	// Caller does not need to call cancel — the process exits.
	_ = cancel
	return ctx
}

// newRootCmd builds the cobra.Command for dashctl with persistent flags.
func (a *Application) newRootCmd() *cobra.Command {
	c := &cobra.Command{
		Use:           "dashctl",
		Short:         "DashCenter operator CLI",
		Long:          longDescription,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	a.bindPersistentFlags(c)

	// Configuration of subcommands is grouped per file for readability.
	c.AddCommand(
		a.newApplyCmd(),
		a.newGetCmd(),
		a.newDescribeCmd(),
		a.newDeleteCmd(),
		a.newReplaceCmd(),
		a.newEditCmd(),
		a.newDiffCmd(),
		a.newReconcileCmd(),
		a.newSimulateCmd(),
		a.newDpuCmd(),
		a.newInventoryCmd(),
		a.newEventsCmd(),
		a.newExplainCmd(),
		a.newVersionCmd(),
		a.newConfigCmd(),
		a.newCompletionCmd(),
		a.newTopologyCmd(),
		a.newCountersCmd(),
		a.newValidateCmd(),
	)
	c.AddCommand(a.newTypedKindGroups()...)
	c.AddCommand(a.newPhase2Stubs()...)
	return c
}

func (a *Application) bindPersistentFlags(c *cobra.Command) {
	f := c.PersistentFlags()
	g := a.Flags
	f.StringVar(&g.configPath, "config", "", "path to dashctl config file")
	f.StringVar(&g.context, "context", "", "named context from the config file")
	f.StringVar(&g.endpoint, "endpoint", "", "dashd endpoint (overrides context)")
	f.StringVar(&g.adminEndpoint, "admin-endpoint", "", "dashd admin endpoint (overrides context)")
	f.StringVar(&g.transport, "transport", "", "rest|grpc (default: rest in Phase 1)")
	f.StringVarP(&g.namespace, "namespace", "n", "", "spec namespace")
	f.BoolVarP(&g.allNamespaces, "all-namespaces", "A", false, "operate across all namespaces (where supported)")
	f.StringVarP(&g.output, "output", "o", "", "output format (json|yaml|table|wide|name|jsonpath=...|template=...)")
	f.StringVar(&g.timeout, "timeout", "", "per-RPC timeout (e.g. 30s)")
	f.StringVar(&g.token, "token", "", "bearer token (overrides DASHCTL_TOKEN)")
	f.StringVar(&g.caFile, "ca", "", "TLS CA cert file")
	f.StringVar(&g.certFile, "cert", "", "TLS client cert file (mTLS)")
	f.StringVar(&g.keyFile, "key", "", "TLS client key file (mTLS)")
	f.BoolVar(&g.insecure, "insecure", false, "allow plaintext HTTP/gRPC to non-localhost endpoints")
	f.BoolVar(&g.skipVerify, "insecure-skip-tls-verify", false, "skip TLS certificate verification")
	f.StringVar(&g.color, "color", "", "color output: auto|always|never")
	f.BoolVarP(&g.quiet, "quiet", "q", false, "suppress non-essential output")
	f.BoolVarP(&g.verbose, "verbose", "v", false, "shorthand for --log-level=debug")
	f.StringVar(&g.logLevel, "log-level", "", "debug|info|warn|error (default: warn)")
	f.StringVar(&g.logFile, "log-file", "", "redirect logs to file (default: stderr)")

	// Track whether boolean flags were explicitly set (for safety logic in config).
	c.PersistentPreRunE = func(cmd *cobra.Command, args []string) error {
		if f := cmd.Flags().Lookup("insecure"); f != nil && f.Changed {
			a.Flags.insecureSet = true
		}
		if f := cmd.Flags().Lookup("insecure-skip-tls-verify"); f != nil && f.Changed {
			a.Flags.skipVerifySet = true
		}
		if a.Flags.verbose && a.Flags.logLevel == "" {
			a.Flags.logLevel = "debug"
		}
		return nil
	}
}

// resolveConfig loads the config file + merges flags/env to produce
// ResolvedConfig.
func (a *Application) resolveConfig() (*config.ResolvedConfig, error) {
	if a.resolveContext != nil {
		return a.resolveContext()
	}
	cfg := a.Config
	if cfg == nil {
		loaded, err := config.Load(a.Flags.configPath)
		if err != nil {
			return nil, errors.Wrap(errors.CodeInvalidArgument, "config", err)
		}
		cfg = loaded
		a.Config = cfg
	}
	rc, err := cfg.Resolve(a.toFlags(), a.Env)
	if err != nil {
		return nil, errors.Wrap(errors.CodeInvalidArgument, "config", err)
	}
	return rc, nil
}

func (a *Application) toFlags() config.Flags {
	g := a.Flags
	return config.Flags{
		Context:       g.context,
		Endpoint:      g.endpoint,
		AdminEndpoint: g.adminEndpoint,
		Transport:     g.transport,
		Namespace:     g.namespace,
		Output:        g.output,
		Color:         g.color,
		Timeout:       g.timeout,
		Token:         g.token,
		CAFile:        g.caFile,
		CertFile:      g.certFile,
		KeyFile:       g.keyFile,
		Insecure:      g.insecure,
		InsecureSet:   g.insecureSet,
		SkipTLSVerify: g.skipVerify,
		SkipTLSSet:    g.skipVerifySet,
	}
}

// dial dials the configured backend, returning a Client and a derived
// per-call context.
func (a *Application) dial(parent context.Context) (client.Client, *config.ResolvedConfig, error) {
	rc, err := a.resolveConfig()
	if err != nil {
		return nil, nil, err
	}
	c, err := a.dialer(parent, rc)
	if err != nil {
		return nil, nil, errors.Classify(err)
	}
	return c, rc, nil
}

// outFormat returns the effective output format for this invocation.
func (a *Application) outFormat() (render.Format, string, error) {
	out := a.Flags.output
	if out == "" {
		// Honour preferences via resolveConfig if we already loaded a config.
		if a.Config != nil && a.Config.Preferences.Output != "" {
			out = a.Config.Preferences.Output
		}
	}
	if out == "" {
		return render.DefaultFor(a.Out), "", nil
	}
	return render.ParseFormat(out)
}

// withTimeout returns ctx with rc.Timeout applied (if positive).
func withTimeout(parent context.Context, rc *config.ResolvedConfig) (context.Context, context.CancelFunc) {
	if rc == nil || rc.Timeout <= 0 {
		return context.WithCancel(parent)
	}
	return context.WithTimeout(parent, rc.Timeout)
}

// helper used by tests to override the dialer.
func (a *Application) setDialer(f func(ctx context.Context, rc *config.ResolvedConfig) (client.Client, error)) {
	a.dialer = f
}

// helper used by tests to override the config resolver.
func (a *Application) setResolver(f func() (*config.ResolvedConfig, error)) {
	a.resolveContext = f
}

// rateOf returns d as a human "Nms" string for log fields.
func rateOf(d time.Duration) string {
	return fmt.Sprintf("%dms", d/time.Millisecond)
}

const longDescription = `dashctl is the operator-facing CLI for DashCenter.

It dials a running dashd over REST (Phase 1) or gRPC (Phase 2) and
exposes a declarative, kubectl-style command surface:

  dashctl apply -f manifest.yaml
  dashctl get vnet
  dashctl get eni eni-app-01 -o yaml
  dashctl describe eni eni-app-01
  dashctl dpu list
  dashctl reconcile

See ` + "`dashctl <command> --help`" + ` for per-command details.`
