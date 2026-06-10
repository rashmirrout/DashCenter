// Command dashd is the DashCenter daemon — the central control plane for
// DASH DPU fleets. It persists desired state, discovers DPUs, and reconciles
// intent onto each DPU via the southbound dashapi.v1 API.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dispatch"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/ha/leader"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	adminserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/admin"
	grpcserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/grpc"
	restserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/rest"
	filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/subscribe"
)

// probeInterval is the cadence at which the inventory prober checks DPU
// reachability. 5s is the production-grade default: fast enough to detect
// transient DPU loss within 15s (3 missed probes → UNREACHABLE) without
// flooding the southbound.
const probeInterval = 5 * time.Second

// probeViaTCP is the production ProbeFunc: a short-timeout TCP dial to the
// DPU's gRPC endpoint. We deliberately do NOT open a gRPC stream here
// (the dispatch + subscribe paths own that responsibility); a plain TCP
// connect is sufficient evidence the DPU is reachable and is cheap enough
// to run every probeInterval against the whole fleet.
func probeViaTCP(ctx context.Context, endpoint string) error {
d := net.Dialer{Timeout: 3 * time.Second}
conn, err := d.DialContext(ctx, "tcp", endpoint)
if err != nil {
return err
}
_ = conn.Close()
return nil
}

const version = "0.2.0-phase1b"

func main() {
configPath := flag.String("config", "configs/dashd.example.yaml", "path to config YAML")
showVer := flag.Bool("version", false, "print version and exit")
dryRun := flag.Bool("dry-run", false, "load config and validate, print placement counts, exit 0")
flag.Parse()

if *showVer {
fmt.Println("dashd", version)
return
}

// 1. Load config.
cfg, err := config.Load(*configPath)
if err != nil {
slog.Warn("config load failed, using defaults", "path", *configPath, "error", err)
cfg = config.Default()
}

// 2. Initialize logging.
initLogging(cfg.Log.Level, cfg.Log.Format)
slog.Info("dashd starting",
	"version", version,
	"node_id", cfg.NodeID,
	"mode", cfg.Mode,
	"auth_mode", cfg.Auth.Mode,
	"storage_backend", cfg.Storage.Backend,
)

// 2a. One-time startup banner when auth is disabled (D15 locked).
// Logged at WARN so it shows up under any reasonable log filter, but
// only once per process so it doesn't clutter steady-state logs.
if cfg.Auth.Mode == "" || cfg.Auth.Mode == "none" {
	slog.Warn("auth disabled — DO NOT use in production",
		"hint", "set auth.mode to token or mtls before exposing dashd outside a trusted network",
	)
}

// 3. Open store.
st, err := filstore.Open(cfg.Storage.File.StateDir)
if err != nil {
slog.Error("store open failed", "error", err)
os.Exit(1)
}

// 4. Build inventory.
inv := inventory.New()
if cfg.Inventory.Source == "file" && cfg.Inventory.File != "" {
if err := inventory.LoadFromFile(cfg.Inventory.File, inv); err != nil {
slog.Warn("inventory load failed", "path", cfg.Inventory.File, "error", err)
}
}

// 5. Create ObsCache.
obs := model.NewObsCache()

// 6. Create dispatch Manager — wire the production DpuClient factory
// so each worker can Apply/Delete via the southbound dashapi.v1 RPCs.
mgr := dispatch.New(obs, &cfg.Reconcile)
mgr.SetStore(st)
mgr.SetInventory(inv)
mgr.SetClientFactory(dpuclient.DefaultFactory)

// 7. Create reconciler.
rec := reconciler.New(st, mgr, cfg.Reconcile.TickInterval)

// 7a. Create prober — watches DPU reachability and advances the inventory
// state machine (REGISTERING → UP, UP → UNREACHABLE after 3 misses).
// Without this wired the inventory state never advances past REGISTERING
// and the GetDpuStatus / GetHealth observability surfaces are dishonest.
prober := inventory.NewProber(inv, probeInterval, probeViaTCP)

// 8. Create shared service layer (Phase 1B).
cpService := service.NewControlPlane(st, inv, rec)
obsService := service.NewObservability(inv, st, obs)

// --- Dry-run mode ---
if *dryRun {
slog.Info("dry-run: config loaded successfully")
slog.Info("dry-run: store opened", "state_dir", cfg.Storage.File.StateDir)
dpus := inv.List()
slog.Info("dry-run: inventory", "dpu_count", len(dpus))
for _, d := range dpus {
slog.Info("dry-run: dpu", "id", d.ID, "endpoint", d.Endpoint, "state", d.State.String())
}
// List all desired specs.
kinds := []string{"vnet", "eni", "vnet_mapping", "acl_policy", "route_policy", "ha_set", "service_tunnel"}
totalSpecs := 0
for _, kind := range kinds {
items, err := cpService.List(context.Background(), "", kind)
if err != nil {
continue
}
if len(items) > 0 {
slog.Info("dry-run: desired specs", "kind", kind, "count", len(items))
}
totalSpecs += len(items)
}
slog.Info("dry-run: total desired specs", "count", totalSpecs)
slog.Info("dry-run: validation passed, exiting")
_ = st.Close()
os.Exit(0)
}

// 9. Create servers.
restSrv := restserver.New(cpService, obsService)
grpcSrv := grpcserver.New(cpService, obsService)
adminSrv := adminserver.New(inv, st, obs, rec)

// 10. Create subscribe PumpSet — wired with the production DpuClient
// factory so each Pump can open real Subscribe streams.
pumpSet := subscribe.NewSet(obs, mgr.DirtyC(), dpuclient.DefaultFactory)

// 11. Create root context with signal handling.
rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

	// 12. Always-on subsystems (run on leader AND follower alike).
	//
	// These goroutines own resources that must be available regardless of
	// leadership: REST/gRPC/admin servers serve read-only RPCs from etcd
	// (Phase 2 PA-3), and the prober keeps inventory state honest so the
	// admin health endpoint is meaningful on followers.
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := restSrv.Serve(cfg.Listen.RESTAddr); err != nil {
			slog.Error("rest server failed", "error", err)
			cancel()
		}
	}()

	// Resolve gRPC listen address.
	grpcAddr := cfg.Listen.GRPCAddr
	if grpcAddr == "" {
		grpcAddr = ":9443"
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := grpcSrv.Serve(grpcAddr); err != nil {
			slog.Error("grpc server failed", "error", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := adminSrv.Serve(cfg.Listen.AdminAddr); err != nil {
			slog.Error("admin server failed", "error", err)
			cancel()
		}
	}()

	// Prober goroutine. Run blocks until rootCtx is cancelled; it manages its
	// own per-DPU probe goroutines internally.
	wg.Add(1)
	go func() {
		defer wg.Done()
		prober.Run(rootCtx)
	}()

	// 13. Leader-only subsystems — reconciler + per-DPU dispatch workers +
	// per-DPU subscribe pumps. Phase 1 / single-node uses NoneElector which
	// is permanent-leader, so this collapses to "run once and stay running
	// until shutdown" — identical behaviour to the pre-PA-0 baseline. Phase 2
	// PA-3 will swap in EtcdElector with zero changes to leaderLoop.
	elector := &leader.NoneElector{NodeID: "dashd-local"}
	wg.Add(1)
	go func() {
		defer wg.Done()
		leaderLoop(rootCtx, elector, inv, pumpSet, mgr, rec)
	}()

	slog.Info("dashd ready",
		"rest", cfg.Listen.RESTAddr,
		"grpc", grpcAddr,
		"admin", cfg.Listen.AdminAddr,
		"leader_id", elector.LeaderID(),
	)

	// 14. Wait for shutdown signal.
	<-rootCtx.Done()
	slog.Info("dashd shutting down...")

	// 15. Graceful shutdown in order.
	restSrv.Stop()
	grpcSrv.Stop()
	adminSrv.Stop()
	pumpSet.StopAll()
	mgr.Stop()
	_ = elector.Close()
	_ = st.Close()

	wg.Wait()
	slog.Info("dashd stopped")
}

// leaderLoop runs the leader-only subsystems for as long as this process
// holds leadership. It is structured as an outer loop so that PA-3's
// EtcdElector can lose and re-acquire leadership without leaking
// goroutines or losing wiring.
//
// For NoneElector (Phase 1 / single-node), AwaitLeadership returns
// immediately, LostLeadership never fires, and the function runs exactly
// once until rootCtx is cancelled by the signal handler. That matches the
// pre-PA-0 behaviour exactly.
//
// For EtcdElector (Phase 2 PA-3+), losing the lease will close the
// LostLeadership channel, leaderLoop cancels leaderCtx (tearing down all
// per-DPU workers and pumps), then loops back to re-campaign.
func leaderLoop(
	rootCtx context.Context,
	elector leader.Elector,
	inv *inventory.Inventory,
	pumpSet *subscribe.PumpSet,
	mgr *dispatch.Manager,
	rec *reconciler.Reconciler,
) {
	for {
		// Campaign for leadership. Returns nil immediately for NoneElector;
		// blocks until elected (or shutdown) for EtcdElector.
		if err := elector.AwaitLeadership(rootCtx); err != nil {
			// Cancelled — shutting down. Don't log at error since rootCtx
			// cancel is the expected exit path.
			slog.Info("leaderLoop: campaign ended", "reason", err)
			return
		}

		slog.Info("leaderLoop: assumed leadership, starting leader-only subsystems",
			"leader_id", elector.LeaderID())

		// leaderCtx scopes every leader-only goroutine so we can tear them
		// all down deterministically on lost leadership without affecting
		// always-on subsystems (REST/gRPC/admin/prober).
		leaderCtx, leaderCancel := context.WithCancel(rootCtx)

		runLeaderTasks(leaderCtx, inv, pumpSet, mgr, rec)

		// Wait for either shutdown or leadership loss.
		select {
		case <-rootCtx.Done():
			slog.Info("leaderLoop: shutdown signal — tearing down leader tasks")
			leaderCancel()
			return
		case <-elector.LostLeadership():
			slog.Warn("leaderLoop: lost leadership — tearing down and re-campaigning")
			leaderCancel()
			// Loop back. NoneElector never reaches this branch (its
			// LostLeadership only closes on Close, which fires during
			// shutdown — and shutdown wins the race via rootCtx.Done above).
		}
	}
}

// runLeaderTasks starts every goroutine that should only run on the leader:
// per-DPU subscribe pumps, per-DPU dispatch workers, the inventory
// subscription that creates/destroys those when DPUs come and go, and the
// reconciler tick loop.
//
// All goroutines started here observe leaderCtx and exit when it is
// cancelled. The function returns as soon as wiring is in place; the
// caller (leaderLoop) blocks on its own select.
func runLeaderTasks(
	leaderCtx context.Context,
	inv *inventory.Inventory,
	pumpSet *subscribe.PumpSet,
	mgr *dispatch.Manager,
	rec *reconciler.Reconciler,
) {
	// Hook inventory subscription for dynamic DPU management.
	// NOTE: the inventory.Registry holds the callback for the lifetime of
	// the process; on leader-loss we rely on pumpSet.StopAll + mgr.Stop
	// (called from the deferred shutdown in main) to drain workers and
	// pumps. Until then, while leaderCtx is alive, this callback drives
	// per-DPU pump/worker creation as DPUs are added/removed.
	inv.Subscribe(func(dpuID, endpoint string, removed bool) {
		if removed {
			pumpSet.Stop(dpuID)
			mgr.RemoveWorker(dpuID)
		} else {
			pumpSet.Start(leaderCtx, dpuID, endpoint)
			mgr.EnsureWorker(leaderCtx, dpuID, endpoint)
		}
	})

	// Start pumps and workers for existing DPUs.
	for _, e := range inv.List() {
		pumpSet.Start(leaderCtx, e.ID, e.Endpoint)
		mgr.EnsureWorker(leaderCtx, e.ID, e.Endpoint)
	}

	// Reconciler tick loop.
	go func() {
		if err := rec.Run(leaderCtx); err != nil && leaderCtx.Err() == nil {
			// Reconciler returned an error before shutdown was requested —
			// log it. We do not cancel rootCtx here because losing the
			// reconciler is not by itself fatal: leader re-election can
			// restart it. Phase 1 / NoneElector won't reach this path in
			// practice because rec.Run only returns on ctx cancel.
			slog.Error("leaderLoop: reconciler exited unexpectedly", "error", err)
		}
	}()
}

func initLogging(level, format string) {
	lvl := parseLogLevel(level)
	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	} else {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lvl})
	}
	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}