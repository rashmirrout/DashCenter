// Package grpcserver implements the gRPC server for dashcenter.v1 services.
// It is a thin adapter: all business logic lives in internal/service/.
package grpcserver

import (
"context"
"errors"
"fmt"
"log/slog"
"net"

"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/service"
"github.com/rashmirrout/DashCenter/src/impl-go/dashd/internal/store"
"google.golang.org/grpc"
"google.golang.org/grpc/codes"
"google.golang.org/grpc/reflection"
"google.golang.org/grpc/status"
)

// Server wraps the gRPC server.
type Server struct {
gs   *grpc.Server
cp   service.ControlPlaneService
obs  service.ObservabilityService
ha   service.HaService
mig  service.MigrationService
}

// New creates a gRPC server wired to the shared service layer. ha and
// mig may be nil — in that case those RPCs return codes.Unimplemented
// (legacy / pre-PC test wiring).
func New(cp service.ControlPlaneService, obs service.ObservabilityService, ha service.HaService, mig service.MigrationService) *Server {
gs := grpc.NewServer(
grpc.ChainUnaryInterceptor(recoveryInterceptor, loggingInterceptor),
)

s := &Server{gs: gs, cp: cp, obs: obs, ha: ha, mig: mig}

// Register ControlPlane service.
registerControlPlane(gs, cp)

// Register ObservabilityService.
registerObservability(gs, obs)

// Register HaService (PC-G1..G3).
registerHa(gs, ha)

// Register MigrationService (PC-G4..G6).
registerMigration(gs, mig)

// Enable gRPC server reflection for debugging tools like grpcurl.
reflection.Register(gs)

return s
}

// Serve starts the gRPC server on the given address.
func (s *Server) Serve(addr string) error {
lis, err := net.Listen("tcp", addr)
if err != nil {
return fmt.Errorf("grpc: listen %s: %w", addr, err)
}
slog.Info("grpc: listening", "addr", addr)
return s.gs.Serve(lis)
}

// Stop gracefully stops the gRPC server.
func (s *Server) Stop() {
s.gs.GracefulStop()
}

// --- Interceptors ---

func recoveryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
defer func() {
if r := recover(); r != nil {
slog.Error("grpc: panic recovered", "method", info.FullMethod, "panic", r)
err = status.Errorf(codes.Internal, "internal error")
}
}()
return handler(ctx, req)
}

func loggingInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
resp, err := handler(ctx, req)
if err != nil {
slog.Warn("grpc: rpc failed", "method", info.FullMethod, "error", err)
} else {
slog.Debug("grpc: rpc ok", "method", info.FullMethod)
}
return resp, err
}

// serviceErrToStatus maps internal service/store errors to gRPC status codes.
func serviceErrToStatus(err error) error {
if err == nil {
return nil
}
if errors.Is(err, store.ErrNotFound) {
return status.Errorf(codes.NotFound, "not found")
}
if errors.Is(err, store.ErrGenerationMismatch) {
return status.Errorf(codes.FailedPrecondition, "generation mismatch")
}
if errors.Is(err, service.ErrInvalidArgument) {
return status.Errorf(codes.InvalidArgument, "%v", err)
}
if errors.Is(err, service.ErrResourceExhausted) {
return status.Errorf(codes.ResourceExhausted, "%v", err)
}
if errors.Is(err, service.ErrFailedPrecondition) {
return status.Errorf(codes.FailedPrecondition, "%v", err)
}
return status.Errorf(codes.Internal, "internal error")
}

