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

# Windows portability: GNU make on Windows defaults to looking for `sh`,
# which the Chocolatey/ezwinports make distros do not bundle. Force cmd.exe
# so simple recipes (which only use Go and a few cmds) Just Work.
ifeq ($(OS),Windows_NT)
  SHELL := cmd.exe
  .SHELLFLAGS := /c
  # On Windows the produced binary needs the .exe suffix or `go build -o`
  # writes a sufix-less file that downstream tools won't recognise.
  EXE := .exe
  # Use built-in Windows commands instead of POSIX rm/mkdir.
  MKDIR_P = if not exist "$1" md "$1"
  RM_RF   = if exist "$1" rmdir /s /q "$1"
  GIT_SHA := $(shell git rev-parse --short=12 HEAD 2> NUL)
  ifeq ($(GIT_SHA),)
    GIT_SHA := none
  endif
  BUILD_DATE := $(shell powershell -NoProfile -Command "[DateTime]::UtcNow.ToString('yyyy-MM-ddTHH:mm:ssZ')")
else
  EXE :=
  MKDIR_P = mkdir -p "$1"
  RM_RF   = rm -rf "$1"
  GIT_SHA := $(shell git rev-parse --short=12 HEAD 2>/dev/null || echo none)
  BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
endif

VERSION ?= 0.1.0-dev
COMMIT  ?= $(GIT_SHA)
DATE    ?= $(BUILD_DATE)
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
	@$(call MKDIR_P,$(BIN_DIR))
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/dashctl$(EXE) ./cmd/dashctl
	@echo built $(BIN_DIR)/dashctl$(EXE) ($(VERSION) $(COMMIT))

.PHONY: build-all
# Cross-compile every platform in $(PLATFORMS) into $(DIST). Each line is
# its own recipe so it runs in BOTH cmd.exe (Windows) and sh (POSIX).
build-all: \
  $(DIST)/dashctl-$(VERSION)-linux-amd64 \
  $(DIST)/dashctl-$(VERSION)-linux-arm64 \
  $(DIST)/dashctl-$(VERSION)-darwin-amd64 \
  $(DIST)/dashctl-$(VERSION)-darwin-arm64 \
  $(DIST)/dashctl-$(VERSION)-windows-amd64.exe

$(DIST)/dashctl-$(VERSION)-linux-amd64:
	@$(call MKDIR_P,$(DIST))
	@echo "-> $@"
	go env -w CGO_ENABLED=0 && go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/dashctl
$(DIST)/dashctl-$(VERSION)-linux-arm64:
	@$(call MKDIR_P,$(DIST))
	@echo "-> $@"
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/dashctl
$(DIST)/dashctl-$(VERSION)-darwin-amd64:
	@$(call MKDIR_P,$(DIST))
	@echo "-> $@"
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/dashctl
$(DIST)/dashctl-$(VERSION)-darwin-arm64:
	@$(call MKDIR_P,$(DIST))
	@echo "-> $@"
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/dashctl
$(DIST)/dashctl-$(VERSION)-windows-amd64.exe:
	@$(call MKDIR_P,$(DIST))
	@echo "-> $@"
	go build -trimpath -ldflags "$(LDFLAGS)" -o $@ ./cmd/dashctl

# Per-target GOOS/GOARCH (works in both cmd.exe and sh because make sets
# the env var for the recipe's subprocess directly).
$(DIST)/dashctl-$(VERSION)-linux-amd64:   export GOOS=linux
$(DIST)/dashctl-$(VERSION)-linux-amd64:   export GOARCH=amd64
$(DIST)/dashctl-$(VERSION)-linux-arm64:   export GOOS=linux
$(DIST)/dashctl-$(VERSION)-linux-arm64:   export GOARCH=arm64
$(DIST)/dashctl-$(VERSION)-darwin-amd64:  export GOOS=darwin
$(DIST)/dashctl-$(VERSION)-darwin-amd64:  export GOARCH=amd64
$(DIST)/dashctl-$(VERSION)-darwin-arm64:  export GOOS=darwin
$(DIST)/dashctl-$(VERSION)-darwin-arm64:  export GOARCH=arm64
$(DIST)/dashctl-$(VERSION)-windows-amd64.exe: export GOOS=windows
$(DIST)/dashctl-$(VERSION)-windows-amd64.exe: export GOARCH=amd64
# Static (no libc) for every platform.
build-all: export CGO_ENABLED=0

.PHONY: vet
vet:
	$(GO) vet $(PKGS)

.PHONY: test
test:
	$(GO) test -count=1 -timeout 120s $(PKGS)

.PHONY: test-cover
test-cover:
	$(GO) test -count=1 -timeout 180s -cover $(PKGS)
# End-to-end suite: builds dashctl once, then runs it against a freshly
# spawned dashd + dash-sim per scenario. Needs a Go toolchain on PATH.
# Generous timeout because `go run` cold-compiles dashd and dash-sim on
# the first hit per cache state.
.PHONY: test-integration
test-integration:
	go test -tags=integration -count=1 -timeout 600s ./test/integration/...
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
	@$(call RM_RF,$(BIN_DIR))
	@$(call RM_RF,$(DIST))
