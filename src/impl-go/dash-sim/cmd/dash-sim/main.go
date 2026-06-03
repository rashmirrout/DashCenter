// Command dash-sim runs a single simulated DASH-DPU agent: it serves
// dashapi.v1.DashApi over gRPC and exposes a JSON admin API over HTTP.
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

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
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
	grpcAddr := flag.String("grpc-listen", ":50051", "address for the gRPC server")
	adminAddr := flag.String("admin-listen", ":8080", "address for the admin HTTP API")
	deviceID := flag.String("device-id", "dpu-sim-01", "synthetic device identifier")
	scenarioPath := flag.String("scenario", "", "optional path to a YAML scenario to preload")
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

	gsrv := grpc.NewServer()
	dashapi.RegisterDashApiServer(gsrv, svc)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("dash-sim: gRPC listen: %v", err)
	}

	adminH := &admin.Handler{
		Store: store, Bus: bus, Faults: faultInjector,
		Counters: counterRegistry, DeviceID: *deviceID,
	}
	httpSrv := &http.Server{
		Addr:              *adminAddr,
		Handler:           adminH.Mux(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Counter tick loop — every interval, tick every known (kind, key).
	go func() {
		t := time.NewTicker(*tickInterval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				for _, keys := range store.AllKeys() {
					for _, k := range keys {
						counterRegistry.Tick(k)
					}
				}
			}
		}
	}()

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
	_ = httpSrv.Shutdown(shutdownCtx)

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
