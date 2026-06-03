// Command dash-redis-adapter exposes the dashapi.v1.DashApi gRPC service
// backed by a real Redis APP_DB. The wire format matches what SONiC's DASH
// orchagent reads: HASH at "DASH_<KIND>_TABLE:<joined-key>" with field "pb"
// holding the binary protobuf serialization of the upstream message.
//
// Usage:
//
//	dash-redis-adapter --grpc-listen :50061 --redis localhost:6379
//
// The same dash-sim-client binary works against this adapter without any
// changes:
//
//	dash-sim-client --target localhost:50061 apply --kind vnet \
//	    --key vnet-prod --value '{"vni":1001}'
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-redis-adapter/internal/adapter"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

func main() {
	grpcAddr := flag.String("grpc-listen", ":50061", "address for the gRPC server")
	redisAddr := flag.String("redis", "localhost:6379", "Redis APP_DB address (host:port); ignored if --embedded-redis is set")
	redisDB := flag.Int("redis-db", 0, "Redis database number")
	redisPass := flag.String("redis-password", "", "Redis password (optional)")
	embedded := flag.Bool("embedded-redis", false, "start an in-process miniredis instead of dialing a real Redis (great for demos/smoke tests)")
	flag.Parse()

	if *embedded {
		mr, err := miniredis.Run()
		if err != nil {
			log.Fatalf("dash-redis-adapter: start miniredis: %v", err)
		}
		*redisAddr = mr.Addr()
		log.Printf("dash-redis-adapter: started embedded miniredis at %s", *redisAddr)
		defer mr.Close()
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		DB:       *redisDB,
		Password: *redisPass,
	})

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := rdb.Ping(pingCtx).Err(); err != nil {
		log.Fatalf("dash-redis-adapter: redis ping %s: %v", *redisAddr, err)
	}
	log.Printf("dash-redis-adapter: connected to Redis at %s (db=%d)", *redisAddr, *redisDB)

	svc := adapter.New(rdb)
	gsrv := grpc.NewServer()
	dashapi.RegisterDashApiServer(gsrv, svc)
	reflection.Register(gsrv)

	lis, err := net.Listen("tcp", *grpcAddr)
	if err != nil {
		log.Fatalf("dash-redis-adapter: listen %s: %v", *grpcAddr, err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go func() {
		log.Printf("dash-redis-adapter: gRPC listening on %s", *grpcAddr)
		if err := gsrv.Serve(lis); err != nil {
			log.Printf("dash-redis-adapter: gRPC stopped: %v", err)
		}
	}()

	<-ctx.Done()
	log.Print("dash-redis-adapter: shutdown signal received")
	gsrv.GracefulStop()
	_ = rdb.Close()
	fmt.Println("dash-redis-adapter: bye")
}
