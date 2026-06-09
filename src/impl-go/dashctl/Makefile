# dashctl — operator CLI for DashCenter.
#
# Targets:
#   make build            local dev build  → bin/dashctl
#   make build-all        cross-compile linux/darwin/windows × amd64/arm64
#   make test             unit tests
#   make test-cover       per-package coverage report
#   make vet              go vet
#   make tidy             go mod tidy
#   make image            build Docker image
#   make clean            remove bin/ + dist/
#
# Override version metadata:
#   make build VERSION=v0.2.0 COMMIT=$(git rev-parse --short HEAD)

VERSION ?= 0.1.0-dev
COMMIT  ?= $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo none)
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w \
  -X main.version=$(VERSION) \
  -X main.commit=$(COMMIT) \
  -X main.buildDate=$(DATE)
GO      := go
BIN_DIR := bin
DIST    := dist
PKGS    := ./...

# Cross-compile matrix. Each target is "GOOS/GOARCH".
PLATFORMS := \
  linux/amd64 \
  linux/arm64 \
  darwin/amd64 \
  darwin/arm64 \
  windows/amd64

.PHONY: all
all: vet test build

.PHONY: build
build:
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/dashctl ./cmd/dashctl
	@echo "built $(BIN_DIR)/dashctl ($(VERSION) $(COMMIT))"

.PHONY: build-all
build-all:
	@mkdir -p $(DIST)
	@for plat in $(PLATFORMS); do \
	  os=$${plat%/*}; arch=$${plat#*/}; \
	  ext=""; [ $$os = "windows" ] && ext=".exe"; \
	  out=$(DIST)/dashctl-$(VERSION)-$$os-$$arch$$ext; \
	  echo "→ $$out"; \
	  GOOS=$$os GOARCH=$$arch CGO_ENABLED=0 $(GO) build -trimpath -ldflags '$(LDFLAGS)' -o $$out ./cmd/dashctl || exit 1; \
	done

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: test
test:
	$(GO) test -count=1 -timeout 120s $(PKGS)

.PHONY: test-cover
test-cover:
	$(GO) test -count=1 -timeout 180s -cover $(PKGS)

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: image
image:
	docker build \
	  -f Dockerfile \
	  --build-arg VERSION=$(VERSION) \
	  --build-arg COMMIT=$(COMMIT) \
	  --build-arg BUILD_DATE=$(DATE) \
	  -t dashcenter/dashctl:$(VERSION) \
	  -t dashcenter/dashctl:dev \
	  ../../..

.PHONY: clean
clean:
	rm -rf $(BIN_DIR) $(DIST)
