# Multi-stage Dockerfile for dash-sim.
# Built from the repo root:
#   docker build -f src/impl-go/dash-sim/Dockerfile -t dashcenter/dash-sim .

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-sim ./dash-sim/cmd/dash-sim

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dash-sim /usr/local/bin/dash-sim
EXPOSE 50051 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dash-sim"]
