// Command dash-sim runs a single simulated DASH-DPU agent: it serves
// dashsim.v1.DashSim over gRPC and exposes a small JSON admin API over HTTP.
//
// Flags:
//
//	--grpc-listen   address for gRPC server     (default ":50051")
//	--admin-listen  address for admin HTTP API  (default ":8080")
//	--device-id     synthetic device identifier (default "dpu-sim-01")
//	--scenario      optional path to a YAML scenario to preload
//	--tick-interval per-object counter tick interval (default 1s)
//
// Example:
//
//	dash-sim --grpc-listen :50051 --admin-listen :8080 --device-id dpu-sim-01 \
//	         --scenario ./testdata/scenarios/small.yaml
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	dashsimv1 "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashsim/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/admin"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/counters"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/events"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/faults"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/scenarios"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	grpcAddr := flag.String("grpc-listen", ":50051", "address for the gRPC server to listen on")
	adminAddr := flag.String("admin-listen", ":8080", "address for the admin HTTP API to listen on")
	deviceID := flag.String("device-id", "dpu-sim-01", "synthetic device identifier")
	scenarioPath := flag.String("scenario", "", "optional path to a YAML scenario file to preload")
	tickInterval := flag.Duration("tick-interval", time.Second, "counter tick interval")
	flag.Parse()

	bus := events.New()
	store := model.New(bus)
	faultInjector := faults.New()
	counterRegistry := counters.New()
	svc := server.New(store, bus, faultInjector, counterRegistry)

	if *scenarioPath != "" {
		if err := scenarios.LoadFile(*scenarioPath, store); err != nil {
			log.Fatalf("dash-sim: load scenario: %v", err)
		}
		log.Printf("dash-sim: loaded scenario %q (sizes=%v)", *scenarioPath, store.Len())
	}

	// gRPC server.
	gsrv := grpc.NewServer()
	dashsimv1.RegisterDashSimServer(gsrv, svc)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("dash-sim: gRPC listen: %v", err)
	}

	// Admin HTTP server.
	adminH := &admin.Handler{
		Store:    store,
		Bus:      bus,
		Faults:   faultInjector,
		Counters: counterRegistry,
		DeviceID: *deviceID,
	}
	httpSrv := &http.Server{
		Addr:              *adminAddr,
		Handler:           adminH.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Counter tick loop.
	go func() {
		t := time.NewTicker(*tickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, e := range store.ListEnis() {
					counterRegistry.Tick(e.Id)
				}
				for _, v := range store.ListVnets() {
					counterRegistry.Tick(v.Id)
				}
			}
		}
	}()

	// Run servers.
	go func() {
		log.Printf("dash-sim: gRPC listening on %s (device=%s)", *grpcAddr, *deviceID)
		if err := gsrv.Serve(lis); err != nil {
			log.Printf("dash-sim: gRPC stopped: %v", err)
		}
	}()
	go func() {
		log.Printf("dash-sim: admin HTTP listening on %s", *adminAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("dash-sim: admin HTTP stopped: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("dash-sim: shutdown signal received")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("dash-sim: admin HTTP shutdown: %v", err)
	}

	stopped := make(chan struct{})
	go func() {
		gsrv.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-shutdownCtx.Done():
		gsrv.Stop()
	}

	fmt.Println("dash-sim: bye")
}
