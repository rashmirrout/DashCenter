# Multi-stage Dockerfile for dash-sim.
# Built from the repo root:
#   docker build -f src/impl-go/dash-sim/Dockerfile -t dashcenter/dash-sim .
#
# Ships BOTH dash-sim (the gRPC server) and dash-sim-client (the
# operator CLI) so operators can `docker exec -it <sim> sh` and run
# diagnostics directly against localhost:50051.

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim        ./dash-sim/cmd/dash-sim
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim-client ./dash-sim-client/cmd/dash-sim-client

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/dash-sim        /usr/local/bin/dash-sim
COPY --from=build /out/dash-sim-client /usr/local/bin/dash-sim-client
EXPOSE 50051 8080
USER nobody:nobody
ENTRYPOINT ["/usr/local/bin/dash-sim"]
