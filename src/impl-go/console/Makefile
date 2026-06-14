# dashw — DashCenter Web Console
# Build, test, lint, and Docker targets.
#
# Usage:
#   make build          Build the dashw binary
#   make test           Run all Go tests with coverage
#   make lint           Run golangci-lint
#   make docker         Build Docker image
#   make clean          Remove build artifacts

VERSION  ?= $(shell date +%Y%m%d)-dev
BINARY   := dashw
GOFLAGS  := -trimpath -ldflags="-s -w -X main.version=$(VERSION)"
DOCKER_TAG ?= dashcenter/dashw:dev

.PHONY: all build test test-cover lint vet fmt clean docker web-build bff-build help

all: build

# ── Build ────────────────────────────────────────────────────────

## build: Build the dashw binary (requires SPA in web/dist/)
build: bff-build

## bff-build: Build Go BFF binary
bff-build:
	CGO_ENABLED=0 go build $(GOFLAGS) -o ../../bin/$(BINARY) ./cmd/dashw

## web-build: Build the SPA and copy to web/dist/
web-build:
	@if [ -d "../../impl-web/console" ] && [ -f "../../impl-web/console/package.json" ]; then \
		cd ../../impl-web/console && npm ci && npm run build; \
		rm -rf web/dist; \
		cp -r ../../impl-web/console/dist web/dist; \
		echo "SPA build copied to web/dist/"; \
	else \
		echo "SPA source not found at ../../impl-web/console — using placeholder"; \
		mkdir -p web/dist; \
	fi

## full-build: Build SPA + Go binary (production)
full-build: web-build bff-build

# ── Test ─────────────────────────────────────────────────────────

## test: Run all tests
test:
	go test ./... -v -count=1

## test-cover: Run tests with coverage report
test-cover:
	go test ./... -v -count=1 -coverprofile=coverage.out -covermode=atomic
	go tool cover -func=coverage.out
	@echo ""
	@echo "HTML report: go tool cover -html=coverage.out -o coverage.html"

## test-race: Run tests with race detector
test-race:
	go test ./... -v -count=1 -race

# ── Code quality ─────────────────────────────────────────────────

## lint: Run golangci-lint
lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run ./...; \
	else \
		echo "golangci-lint not installed — skipping"; \
	fi

## vet: Run go vet
vet:
	go vet ./...

## fmt: Check formatting
fmt:
	@test -z "$$(gofmt -l .)" || (echo "Unformatted files:" && gofmt -l . && exit 1)

# ── Docker ───────────────────────────────────────────────────────

## docker: Build Docker image (multi-stage: node → go → distroless)
docker:
	docker build -t $(DOCKER_TAG) -f Dockerfile ../../..

## docker-console: Alias for docker
docker-console: docker

# ── Clean ────────────────────────────────────────────────────────

## clean: Remove build artifacts
clean:
	rm -f ../../bin/$(BINARY)
	rm -f coverage.out coverage.html
	rm -rf web/dist
	mkdir -p web/dist
	touch web/dist/.gitkeep

# ── Help ─────────────────────────────────────────────────────────

## help: Show this help
help:
	@echo "dashw — DashCenter Web Console"
	@echo ""
	@echo "Targets:"
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /' | sort