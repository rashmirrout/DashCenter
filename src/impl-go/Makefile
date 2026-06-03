# Top-level Makefile for the Go implementation.
#
#   make protos          # generate gen/go/* (PROTOGEN=buf default; or PROTOGEN=protoc)
#   make all             # build dashd, dashctl, dash-sim, dash-sim-client
#   make test            # go test ./... in every module
#   make lint            # golangci-lint
#   make clean           # remove ./bin and generated stubs

PROTOGEN ?= buf
BIN_DIR  ?= bin
MODULES  := dashd dashctl dash-sim dash-sim-client

.PHONY: all protos protos-buf protos-protoc build test lint clean tidy

all: protos build

protos:
ifeq ($(PROTOGEN),buf)
	$(MAKE) -C codegen/buf gen
else ifeq ($(PROTOGEN),protoc)
	$(MAKE) -f codegen/protoc/protoc.mk gen
else
	$(error PROTOGEN must be 'buf' or 'protoc', got '$(PROTOGEN)')
endif

build:
	@mkdir -p $(BIN_DIR)
	go build -o $(BIN_DIR)/dashd            ./dashd/cmd/dashd
	go build -o $(BIN_DIR)/dashctl          ./dashctl/cmd/dashctl
	go build -o $(BIN_DIR)/dash-sim         ./dash-sim/cmd/dash-sim
	go build -o $(BIN_DIR)/dash-sim-client  ./dash-sim-client/cmd/dash-sim-client

test:
	@for m in $(MODULES); do (cd $$m && go test ./...) || exit 1; done

lint:
	golangci-lint run ./...

tidy:
	@for m in $(MODULES); do (cd $$m && go mod tidy) || exit 1; done

clean:
	rm -rf $(BIN_DIR) gen/go
