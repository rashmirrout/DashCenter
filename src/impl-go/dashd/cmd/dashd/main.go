// Command dashd is the DashCenter daemon — the central control plane for
// DASH DPU fleets. It persists desired state, discovers DPUs, and reconciles
// intent onto each DPU via the southbound dashapi.v1 API.
package main

import (
"context"
"flag"
"fmt"
"log/slog"
"os"
"os/signal"
"sync"
"syscall"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/config"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/dispatch"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/inventory"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/model"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/reconciler"
adminserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/admin"
restserver "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/server/rest"
filstore "github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store/file"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/subscribe"
)

const version = "0.1.0-phase1"

func main() {
configPath := flag.String("config", "configs/dashd.example.yaml", "path to config YAML")
showVer := flag.Bool("version", false, "print version and exit")
flag.Parse()

if *showVer {
fmt.Println("dashd", version)
return
}

// 1. Load config.
cfg, err := config.Load(*configPath)
if err != nil {
// Fall back to defaults if config file not found.
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

// 6. Create dispatch Manager.
mgr := dispatch.New(obs, &cfg.Reconcile)
mgr.SetStore(st)
mgr.SetInventory(inv)

// 7. Create reconciler.
rec := reconciler.New(st, mgr, cfg.Reconcile.TickInterval)

// 8. Create servers.
restSrv := restserver.New(st, inv, rec)
adminSrv := adminserver.New(inv, st, obs, rec)

// 9. Create subscribe PumpSet.
pumpSet := subscribe.NewSet(obs, mgr.DirtyC())

// 10. Hook inventory subscription for dynamic DPU management.
inv.Subscribe(func(dpuID, endpoint string, removed bool) {
if removed {
pumpSet.Stop(dpuID)
mgr.RemoveWorker(dpuID)
} else {
pumpSet.Start(rootCtx, dpuID, endpoint)
mgr.EnsureWorker(rootCtx, dpuID, endpoint)
}
})

// 11. Create root context with signal handling.
var cancel context.CancelFunc
rootCtx, cancel = signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
defer cancel()

// Re-wire the inventory subscription to use rootCtx (closure captures it).
// Start pumps and workers for existing DPUs.
for _, e := range inv.List() {
pumpSet.Start(rootCtx, e.ID, e.Endpoint)
mgr.EnsureWorker(rootCtx, e.ID, e.Endpoint)
}

// 12. Launch servers and subsystems.
var wg sync.WaitGroup

wg.Add(1)
go func() {
defer wg.Done()
if err := restSrv.Serve(cfg.Listen.RESTAddr); err != nil {
slog.Error("rest server failed", "error", err)
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

slog.Info("dashd ready",
"rest", cfg.Listen.RESTAddr,
"admin", cfg.Listen.AdminAddr,
)

// 13. Wait for shutdown signal.
<-rootCtx.Done()
slog.Info("dashd shutting down...")

// 14. Graceful shutdown in order.
restSrv.Stop()
adminSrv.Stop()
pumpSet.StopAll()
mgr.Stop()
_ = st.Close()

wg.Wait()
slog.Info("dashd stopped")
}

// rootCtx is a package-level variable used by the inventory subscription callback.
// It's set in main() before any DPU registration can happen.
var rootCtx context.Context

func init() {
rootCtx = context.Background() // default; overridden in main()
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