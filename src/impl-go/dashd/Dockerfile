# Multi-stage Dockerfile for dashd.
# Built from the repo root:
#   docker build -f src/impl-go/dashd/Dockerfile -t dashcenter/dashd .

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dashd ./dashd/cmd/dashd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashd /usr/local/bin/dashd
# 8443 REST, 9443 gRPC, 7443 admin HTTP.
EXPOSE 8443 9443 7443
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dashd"]
# Default config path; override with `command:` in compose if needed.
CMD ["--config", "/etc/dashd/dashd.yaml"]
