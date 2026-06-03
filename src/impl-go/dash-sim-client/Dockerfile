# Multi-stage Dockerfile for dash-sim-client.
# Built from the repo root:
#   docker build -f src/impl-go/dash-sim-client/Dockerfile -t dashcenter/dash-sim-client .
#
# Usage:
#   docker run --rm --network host dashcenter/dash-sim-client kinds -o table
#   docker run --rm --network host dashcenter/dash-sim-client --target localhost:50051 ping

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim-client ./dash-sim-client/cmd/dash-sim-client

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dash-sim-client /usr/local/bin/dash-sim-client
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dash-sim-client"]
