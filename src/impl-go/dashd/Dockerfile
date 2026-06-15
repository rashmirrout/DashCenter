# Multi-stage Dockerfile for dashd.
# Built from the repo root:
#   docker build -f src/impl-go/dashd/Dockerfile -t dashcenter/dashd .
#
# Ships BOTH dashd (the controller) and dashctl (the operator CLI)
# so operators can `docker exec -it <dashd> sh` and run diagnostics
# directly against localhost:8443 / localhost:9443.

FROM golang:1.22-alpine AS build
WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN go build -trimpath -ldflags="-s -w" -o /out/dashd   ./dashd/cmd/dashd
RUN go build -trimpath -ldflags="-s -w" -o /out/dashctl ./dashctl/cmd/dashctl

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=build /out/dashd   /usr/local/bin/dashd
COPY --from=build /out/dashctl /usr/local/bin/dashctl
# 8443 REST, 9443 gRPC, 7443 admin HTTP.
EXPOSE 8443 9443 7443
USER nobody:nobody
ENTRYPOINT ["/usr/local/bin/dashd"]
# Default config path; override with `command:` in compose if needed.
CMD ["--config", "/etc/dashd/dashd.yaml"]
