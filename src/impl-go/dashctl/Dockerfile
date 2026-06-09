# Multi-stage Dockerfile for dashctl.
#
# Built from the repo root (same convention as dashd / dash-sim):
#   docker build -f src/impl-go/dashctl/Dockerfile -t dashcenter/dashctl:dev .
#
# The build stage compiles a CGO-free static binary so the runtime stage
# can use distroless/static. Linker flags strip debug info and stamp
# version metadata (override at build time with --build-arg).

FROM golang:1.22-alpine AS build
ARG VERSION=0.1.0-dev
ARG COMMIT=none
ARG BUILD_DATE=unknown

WORKDIR /workspace
COPY . .
WORKDIR /workspace/src/impl-go
RUN CGO_ENABLED=0 go build \
      -trimpath \
      -ldflags "-s -w \
        -X main.version=${VERSION} \
        -X main.commit=${COMMIT} \
        -X main.buildDate=${BUILD_DATE}" \
      -o /out/dashctl \
      ./dashctl/cmd/dashctl

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/dashctl /usr/local/bin/dashctl
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/dashctl"]
