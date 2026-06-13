// Command dashd is the DashCenter daemon — the central control plane for
// DASH DPU fleets. It persists desired state, discovers DPUs, and reconciles
// intent onto each DPU via the southbound dashapi.v1 API.
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc/credentials"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/audit"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/auth"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/cluster"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/capacity"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dispatch"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dpuclient"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/flow"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/ha/leader"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/ha/orchestrator"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/migration"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/operations"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/schema"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
	adminserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/admin"
	grpcserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/grpc"
	restserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/rest"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
	etcdstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/etcd"
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

// 3. Open store. The backend is selected by cfg.Storage.Backend; PA-1b
// adds "etcd" alongside the today-default "file". "raft" is reserved
// for PF (controllerless mode) and rejected at config-validation time.
st, err := openStore(rootStoreCtx(), cfg)
if err != nil {
	slog.Error("store open failed", "backend", cfg.Storage.Backend, "error", err)
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

// 7b. Per-DPU capacity admission tracker (PB-1). nil-tolerant downstream.
// We seed counters from whatever the store already holds so a restart
// does not silently reset the budget. Recount() failure is logged but
// non-fatal: it can only fail due to context cancel or a corrupt store
// envelope, and in both cases the tracker keeps a zero baseline (worst
// case: a few extra writes succeed and then get rejected on the next
// Apply when the in-memory counter catches up via Apply* calls).
capTracker := capacity.NewTracker(inv)
if err := capTracker.Recount(rootStoreCtx(), st); err != nil {
	slog.Warn("capacity: initial Recount failed; starting with zero counters", "error", err)
}

// 7c. Capability + schema gate (PB-3). nil-tolerant downstream.
// Gate consults inventory.Capabilities advertised by each DPU at
// RegisterDpu time. Until a DPU advertises, the gate is permissive
// for that DPU (MC-3 forward-compat). Construction is lock-free —
// the gate carries no internal state; it queries inventory live.
capGate := schema.NewGate(inv)

// 7d. Operations manager (PC-1): cordon admission + (PC-7) drain.
// Mutates inventory.DpuEntry.Cordoned and exposes an in-memory audit
// ring (PD will replace with the persistent audit log).
opsMgr := operations.New(inv)

// 7e. HA orchestrator (PC-G1..G3). Holds the live per-HA-set role
// model. PutHaSet auto-registers sets via SyncFromSpec; switchover
// and failover walk the DASH 10-state machine and emit events on a
// fan-out bus that WatchHaEvents subscribes to. Pusher is the
// southbound abstraction — NoOpPusher until PE wires real DASH HA
// scope dispatch against the sim.
haOrch := orchestrator.New(&orchestrator.NoOpPusher{})

// 7f. Migration coordinator (PC-G4..G6). Persistent 10-phase
// sessions in the desired store (kind=migration_session) so a
// dashd restart resumes in-flight migrations from etcd (PC-G6).
// Effect interface is the southbound seam — LivePutEffect rewrites
// each ENI's placement_hint via the service-layer PutEni at CUTOVER
// so capacity + schema + cordon admission fire on the destination.
// The Rehomer is filled in below (we need the service before we can
// build the LivePutEffect, but the coordinator needs the effect at
// construction; we use a late-binding shim).
migEffect := &migration.LivePutEffect{}
migCoord, err := migration.New(rootStoreCtx(), st, migEffect)
if err != nil {
	slog.Error("migration: coordinator init failed", "error", err)
	os.Exit(1)
}

// 8. Create shared service layer (Phase 1B).
cpService := service.NewControlPlane(st, inv, rec, capTracker, capGate, opsMgr, haOrch)
// Late-bind the migration cutover Rehomer now that the service
// exists — LivePutEffect.Cutover delegates to it.
migEffect.Rehomer = &service.ServiceEniRehomer{Svc: cpService, Store: st}
migService := service.NewMigration(migCoord)
obsService := service.NewObservability(inv, st, obs)

// 8b. Diagnostics engine + service (PE-1). Pure-cache compute over
// the desired-state store; uses NilHitStats today (PD-G5 wires the
// real counter store) and a NopResimulator (PE-3 will swap in a
// dispatch-backed implementation).
diagEngine := flow.New(st, inv, flow.NilHitStats{}, &flow.NopResimulator{})
diagService := service.NewDiagnostics(diagEngine)

// 8c. Cluster service (PE-G2). Build the elector EARLY so the
// aggregator can observe leadership; build the peer registry next
// (etcd-backed when storage.backend=etcd, self-only otherwise).
rootCtx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

elector, err := newElector(rootCtx, cfg)
if err != nil {
	slog.Error("elector open failed", "backend", cfg.HA.Controller.Elector.Backend, "error", err)
	os.Exit(1)
}

clusterReg := openClusterRegistry(rootCtx, cfg)
clusterAgg, err := cluster.NewAggregator(cluster.AggregatorConfig{
	Registry:  clusterReg,
	Inventory: inv,
	Store:     st,
	Elector:   elector,
	Version:   version,
	NodeID:    cfg.NodeID,
	Namespaces: knownNamespaces(cfg),
})
if err != nil {
	slog.Error("cluster aggregator init failed", "error", err)
	os.Exit(1)
}
clusterBcast := cluster.NewBroadcaster(cluster.DefaultBroadcasterConfig())
clusterService := service.NewCluster(clusterAgg, clusterBcast)

// PE-G7: wire the peer registry → broadcaster bridge so peer
// add/remove/update events stream live to WatchTopology subscribers.
// Inventory + leader-change wiring follows the same pattern but
// requires Subscribe APIs on the elector that are not yet exported;
// tracked as a follow-up. For PE-G7 v1 the registry-driven cluster
// pane is the primary live demo.
clusterReg.Subscribe(func(kind cluster.ChangeKind, peer cluster.PeerInfo) {
	cluster.ObserveRegistryChange(kind)
	cluster.ObservePeerCount(clusterReg.PeerCount())
	clusterBcast.Publish(cluster.RegistryEvent(kind, peer, elector.LeaderID()))
})
cluster.ObservePeerCount(clusterReg.PeerCount())

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
//
// PD wiring: build the Authorizer + audit writer + TLS material once
// from cfg.Auth, then pass through to both REST and gRPC via their
// Options structs. cfg.Auth.Mode == "none" leaves everything nil and
// matches the pre-PD plaintext behaviour exactly.
authz, auditWriter, grpcCreds, restListener := buildPDWiring(cfg)
if auditWriter != nil {
	defer auditWriter.Close()
}
restSrv := restserver.NewWithOptions(cpService, obsService, service.NewHa(haOrch), migService, restserver.Options{
	Listener:    restListener,
	Authorizer:  authz,
	AuditWriter: auditWriter,
	Diagnostics: diagService,
	Cluster:     clusterService,
})
grpcSrv := grpcserver.NewWithOptions(cpService, obsService, service.NewHa(haOrch), migService, grpcserver.Options{
	TLSConfig:   grpcCreds,
	Authorizer:  authz,
	AuditWriter: auditWriter,
	Diagnostics: diagService,
	Cluster:     clusterService,
})

// 9b. (rootCtx + elector already built above for cluster wiring.)

// 9c. Build elector early so the admin server can report live leader
// state (admin runs on every node — leader AND follower — so its
// health/leader endpoints MUST observe real leadership instead of the
// PA-0 always-true stub).
// (elector already built in step 8c above.)

adminSrv := adminserver.NewWithElector(inv, st, obs, rec, elector)
adminSrv.SetClusterService(clusterService)

// 9d. Counters pipeline (PE-3b / PE-G9). Store caches the most-recent
// CounterReport per DPU; Poller pulls every cfg.Observability.Counters.PollInterval.
// Both are always-on subsystems (counter visibility is wanted on leader
// AND follower; PD-G5 will stream the store events to operators).
cntStore := counters.NewStore()
var cntPoller *counters.Poller
if cfg.Observability.Counters.Enabled {
	cntPoller = counters.NewPoller(inv, dpuclient.DefaultFactory, cntStore, cfg.Observability.Counters.PollInterval)
} else {
	cntPoller = counters.NewDisabledPoller(inv, dpuclient.DefaultFactory, cntStore, cfg.Observability.Counters.PollInterval)
}
adminSrv.SetCountersWiring(cntStore, cntPoller)

// 10. Create subscribe PumpSet — wired with the production DpuClient
// factory so each Pump can open real Subscribe streams.
pumpSet := subscribe.NewSet(obs, mgr.DirtyC(), dpuclient.DefaultFactory)

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

	// Counter poller goroutine. Always-on — runs on leader AND follower
	// so admin /counters endpoints are populated everywhere. Disabled
	// pollers no-op until /admin/counters/enable flips them on.
	cntPoller.Start(rootCtx)

	// 13. Leader-only subsystems — reconciler + per-DPU dispatch workers +
	// per-DPU subscribe pumps. Backend is selected by
	// cfg.HA.Controller.Elector.Backend ("none" for single-node dev /
	// today's behaviour, "etcd" for multi-node controller-mode clusters).
	// leaderLoop's contract is identical regardless of backend.
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
	cntPoller.Stop()
	pumpSet.StopAll()
	mgr.Stop()
	_ = elector.Close()
	_ = clusterReg.Close()
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

// rootStoreCtx returns a short-lived context for the store-open call.
// We don't want the store dial to inherit the daemon-wide signal-
// notification context because that context is created later, after
// the store is open. A bounded 30s context here is generous for both
// a local file mkdir and an etcd cluster dial.
func rootStoreCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	// Intentionally leak the cancel: openStore returns quickly under any
	// healthy condition; under an unhealthy one the process is about to
	// exit anyway. Avoiding "defer cancel()" here keeps the helper
	// callable from one-shot top-level code.
	_ = cancel
	return ctx
}

// openStore picks the configured backend and returns a ready-to-use
// store. PA-1b adds the "etcd" branch alongside the today-default
// "file". "raft" is rejected at config validation, never reaches here.
func openStore(ctx context.Context, cfg *config.Config) (store.DesiredStore, error) {
	switch cfg.Storage.Backend {
	case "file":
		return filstore.Open(cfg.Storage.File.StateDir)
	case "etcd":
		return etcdstore.Open(ctx, etcdstore.Config{
			Endpoints:   cfg.Storage.Etcd.Endpoints,
			KeyPrefix:   cfg.Storage.Etcd.KeyPrefix,
			DialTimeout: cfg.Storage.Etcd.DialTimeout,
			CertFile:    cfg.Storage.Etcd.TLS.CertFile,
			KeyFile:     cfg.Storage.Etcd.TLS.KeyFile,
			CAFile:      cfg.Storage.Etcd.TLS.CAFile,
		})
	default:
		return nil, fmt.Errorf("openStore: unsupported backend %q (config validator should have caught this)", cfg.Storage.Backend)
	}
}

// newElector picks the configured leader-election backend.
//
// PA-3 adds the "etcd" branch alongside the today-default "none"
// (single-node dev / unit tests / dashctl-fleet smoke). Backend is
// driven entirely by cfg.HA.Controller.Elector.Backend; the leaderLoop
// downstream is identical for both implementations.
//
// Mode is implicit: when cfg.Mode == "controllerless" the config
// validator rejects startup before we ever reach here.
func newElector(ctx context.Context, cfg *config.Config) (leader.Elector, error) {
	backend := cfg.HA.Controller.Elector.Backend
	switch backend {
	case "", config.ElectorBackendNone:
		// NoneElector preserves pre-PA-0 single-node behaviour exactly.
		// NodeID falls back to "dashd-local" only when cfg.NodeID is
		// somehow empty (the config validator already rejects that, but
		// we keep the fallback so a defaults-only Config still works in
		// unit tests).
		nodeID := cfg.NodeID
		if nodeID == "" {
			nodeID = "dashd-local"
		}
		return &leader.NoneElector{NodeID: nodeID}, nil

	case config.ElectorBackendEtcd:
		// Etcd-backed concurrency-session election. Multi-node controller
		// mode: each dashd instance campaigns under the same LeaderKey;
		// only the elected leader runs the reconciler + dispatch +
		// subscribe goroutines.
		return leader.NewEtcdElector(ctx, leader.EtcdConfig{
			Endpoints:   cfg.HA.Controller.Elector.Endpoints,
			NodeID:      cfg.NodeID,
			LeaseTTL:    cfg.HA.Controller.Elector.LeaseTTL,
			LeaderKey:   cfg.HA.Controller.Elector.LeaderKey,
			DialTimeout: cfg.HA.Controller.Elector.DialTimeout,
			CertFile:    cfg.HA.Controller.Elector.TLS.CertFile,
			KeyFile:     cfg.HA.Controller.Elector.TLS.KeyFile,
			CAFile:      cfg.HA.Controller.Elector.TLS.CAFile,
		})

	default:
		return nil, fmt.Errorf("newElector: unsupported backend %q (config validator should have caught this)", backend)
	}
}

// openClusterRegistry returns the cluster.Registry for the PE-G2
// ClusterService. When storage backend is etcd, the registry publishes
// this node's listen addresses under its OWN etcd lease (decoupled
// from the elector's lease so leadership loss does NOT depublish us).
// Otherwise a self-only registry is returned — the aggregator still
// works but only sees this node.
func openClusterRegistry(ctx context.Context, cfg *config.Config) *cluster.Registry {
	self := cluster.PeerInfo{
		NodeID:    cfg.NodeID,
		RESTAddr:  cfg.Listen.RESTAddr,
		GRPCAddr:  cfg.Listen.GRPCAddr,
		AdminAddr: cfg.Listen.AdminAddr,
		Version:   version,
		StartedAt: time.Now().UTC(),
	}

	if cfg.Storage.Backend != "etcd" || len(cfg.Storage.Etcd.Endpoints) == 0 {
		slog.Info("cluster.registry: self-only mode (no etcd endpoints)",
			"node_id", self.NodeID)
		return cluster.OpenSelfOnly(self)
	}

	// Derive the peers key prefix from the storage prefix so all etcd
	// keys cluster under the same operator-visible root. The store
	// uses "<KeyPrefix><namespace>/<kind>/..."; we anchor peers at
	// "<KeyPrefix>peers/".
	keyPrefix := cfg.Storage.Etcd.KeyPrefix
	if keyPrefix == "" {
		keyPrefix = "/dashd/state/"
	}
	if keyPrefix[len(keyPrefix)-1] != '/' {
		keyPrefix += "/"
	}
	peerPrefix := keyPrefix + "peers/"

	var tls *cluster.TLSConfig
	if cfg.Storage.Etcd.TLS.CertFile != "" || cfg.Storage.Etcd.TLS.CAFile != "" {
		tls = &cluster.TLSConfig{
			CertFile: cfg.Storage.Etcd.TLS.CertFile,
			KeyFile:  cfg.Storage.Etcd.TLS.KeyFile,
			CAFile:   cfg.Storage.Etcd.TLS.CAFile,
		}
	}

	reg, err := cluster.Open(ctx, cluster.Config{
		Endpoints:   cfg.Storage.Etcd.Endpoints,
		KeyPrefix:   peerPrefix,
		DialTimeout: cfg.Storage.Etcd.DialTimeout,
		LeaseTTL:    8 * time.Second,
		TLS:         tls,
	}, self)
	if err != nil {
		// Production fallback: dashd stays up with a self-only view
		// rather than failing closed. Operators see the warning in
		// logs and know peer membership isn't published.
		slog.Warn("cluster.registry: etcd open failed, falling back to self-only",
			"error", err, "endpoints", cfg.Storage.Etcd.Endpoints)
		return cluster.OpenSelfOnly(self)
	}
	return reg
}

// knownNamespaces is the list of namespaces the ClusterService
// aggregator enumerates for per-namespace object counts. For PE-G2 v1
// the store has no namespace-enumeration API, so we hardcode the lab
// namespaces ("default", "edge", "staging"). A follow-up PR can wire
// this from a `cluster.namespaces:` knob in dashd.yaml.
func knownNamespaces(_ *config.Config) []string {
	return []string{"default", "edge", "staging"}
}

// buildPDWiring derives the runtime auth + audit + TLS handles from
// cfg.Auth + cfg.Audit. Returns nil/empty values when auth is disabled
// (cfg.Auth.Mode == "" or "none"), which keeps NewWithOptions on the
// today-equivalent plaintext + AllowAll path.
//
// The function is intentionally non-fatal — TLS material missing or
// unreadable is logged as a warning + downgrades to plaintext. PD-day
// operators get the same "config validator caught it" guarantee from
// config.validateAuth without needing to plumb a fatal error path here.
func buildPDWiring(cfg *config.Config) (auth.Authorizer, *audit.Writer, credentials.TransportCredentials, auth.ListenerConfig) {
mode := cfg.Auth.Mode
if mode == "" {
mode = "none"
}

listener := auth.ListenerConfig{
Mode:              mode,
CertFile:          cfg.Auth.TLS.CertFile,
KeyFile:           cfg.Auth.TLS.KeyFile,
CAFile:            cfg.Auth.TLS.CAFile,
RequireClientCert: cfg.Auth.TLS.RequireClientCert,
}

// Build the gRPC TransportCredentials when TLS material is present.
var grpcCreds credentials.TransportCredentials
if mode == "token" || mode == "mtls" {
if listener.CertFile != "" && listener.KeyFile != "" {
cert, err := tls.LoadX509KeyPair(listener.CertFile, listener.KeyFile)
if err != nil {
slog.Error("pd: load tls keypair failed; running plaintext", "error", err)
} else {
tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
if mode == "mtls" {
caPEM, err := os.ReadFile(listener.CAFile)
if err != nil {
slog.Error("pd: read ca failed; mTLS disabled", "error", err)
} else {
pool := x509.NewCertPool()
if pool.AppendCertsFromPEM(caPEM) {
tlsCfg.ClientCAs = pool
tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
} else {
slog.Error("pd: ca pem contained no certs; mTLS disabled")
}
}
}
grpcCreds = credentials.NewTLS(tlsCfg)
}
}
}

// Build the Authorizer.
var authz auth.Authorizer
switch mode {
case "none":
// nil -> NewWithOptions falls back to AllowAllAuthorizer.
case "token":
tokens := map[string]auth.Subject{}
for _, t := range cfg.Auth.Tokens {
tokens[t.Token] = auth.Subject{Name: t.Name, Role: t.Role}
}
authz = &auth.TokenAuthorizer{Tokens: tokens, Roles: auth.DefaultRoleMap}
case "mtls":
cnRoles := map[string]auth.Subject{}
// Reuse cfg.Auth.Tokens: under mtls mode, TokenEntry.Token is
// the client cert CN string. Locked in CONTRIBUTING.md.
for _, t := range cfg.Auth.Tokens {
cnRoles[t.Token] = auth.Subject{Name: t.Name, Role: t.Role}
}
authz = &auth.MTLSAuthorizer{CNRoles: cnRoles, Roles: auth.DefaultRoleMap}
}

// Build the audit writer. Reuses the store_dir for the audit log
// so a single state dir holds everything dashd needs.
var aw *audit.Writer
auditDir := cfg.Storage.File.StateDir
if auditDir != "" {
w, err := audit.Open(audit.Config{
Dir:            auditDir,
SyncEveryWrite: true,
})
if err != nil {
slog.Warn("pd: audit writer disabled", "error", err)
} else {
aw = w
slog.Info("pd: audit writer enabled", "dir", auditDir)
}
}

return authz, aw, grpcCreds, listener
}
