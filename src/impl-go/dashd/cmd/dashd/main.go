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
slog.Info("dashd starting", "version", version)

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

// 12. Hook inventory subscription for dynamic DPU management.
inv.Subscribe(func(dpuID, endpoint string, removed bool) {
if removed {
pumpSet.Stop(dpuID)
mgr.RemoveWorker(dpuID)
} else {
pumpSet.Start(rootCtx, dpuID, endpoint)
mgr.EnsureWorker(rootCtx, dpuID, endpoint)
}
})

// Start pumps and workers for existing DPUs.
for _, e := range inv.List() {
pumpSet.Start(rootCtx, e.ID, e.Endpoint)
mgr.EnsureWorker(rootCtx, e.ID, e.Endpoint)
}

// 13. Launch servers and subsystems.
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

wg.Add(1)
go func() {
defer wg.Done()
if err := rec.Run(rootCtx); err != nil {
slog.Error("reconciler failed", "error", err)
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

slog.Info("dashd ready",
"rest", cfg.Listen.RESTAddr,
"grpc", grpcAddr,
"admin", cfg.Listen.AdminAddr,
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
_ = st.Close()

wg.Wait()
slog.Info("dashd stopped")
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