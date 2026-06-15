# dashw — DashCenter Web Console
# Multi-stage build: Node (SPA) → Go (BFF) → distroless (runtime)
#
# Build:  docker build -t dashcenter/dashw:dev -f src/impl-go/console/Dockerfile .
# Run:    docker run -p 3000:3000 -e DASHD_REST_ADDR=http://dashd:8443 dashcenter/dashw:dev

# ── Stage 1: Build SPA ──────────────────────────────────────────
FROM node:20-alpine AS web-builder

WORKDIR /app/web
COPY src/impl-web/console/package*.json ./
RUN npm ci 2>/dev/null || echo "No SPA package.json yet — skipping npm install"
COPY src/impl-web/console/ ./
RUN if [ -f package.json ] && grep -q '"build"' package.json; then \
npx vite build; \
else \
mkdir -p dist && echo '<!DOCTYPE html><html><head><title>dashw</title></head><body><h1>dashw</h1><p>SPA not built</p></body></html>' > dist/index.html; \
fi

# ── Stage 2: Build BFF ──────────────────────────────────────────
FROM golang:1.22-alpine AS go-builder

RUN apk add --no-cache git

WORKDIR /app

# Copy module files (console is stdlib-only, no external deps)
COPY src/impl-go/console/go.mod ./src/impl-go/console/

# Copy source code
COPY src/impl-go/gen/ ./src/impl-go/gen/
COPY src/impl-go/console/ ./src/impl-go/console/

# Copy SPA build output into the Go embed directory
COPY --from=web-builder /app/web/dist ./src/impl-go/console/web/dist/

# Build the binary (GOWORK=off bypasses workspace — console has no external deps)
ENV GOWORK=off
# -mod=mod lets the build auto-resolve missing go.sum entries for
# indirect deps (the module pulls prometheus/client_golang + grpc as
# part of the PE-G7 topology hub; transitive entries are populated
# during the first download).
ENV GOFLAGS=-mod=mod
RUN cd src/impl-go/console && \
CGO_ENABLED=0 GOOS=linux go build \
-trimpath \
-ldflags="-s -w -X main.version=$(date +%Y%m%d)" \
-o /dashw \
./cmd/dashw

# ── Stage 3: Runtime ────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=go-builder /dashw /dashw

EXPOSE 3000 8080

ENTRYPOINT ["/dashw"]
CMD ["--listen", ":3000"]