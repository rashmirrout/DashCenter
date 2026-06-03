# Multi-stage Dockerfile for dash-redis-adapter.
# Built from the repo root:
#   docker build -f src/impl-go/dash-redis-adapter/Dockerfile -t dashcenter/dash-redis-adapter .

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dash-redis-adapter ./dash-redis-adapter/cmd/dash-redis-adapter

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dash-redis-adapter /usr/local/bin/dash-redis-adapter
EXPOSE 52051
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dash-redis-adapter"]
