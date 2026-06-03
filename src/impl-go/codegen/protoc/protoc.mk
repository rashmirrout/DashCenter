# Fallback proto pipeline using plain `protoc`.
#
# Invoked via top-level Makefile:
#   PROTOGEN=protoc make protos
#
# Requires:
#   * protoc on PATH
#   * protoc-gen-go         (go install google.golang.org/protobuf/cmd/protoc-gen-go@latest)
#   * protoc-gen-go-grpc    (go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest)

PROTO_ROOT := $(CURDIR)/../../../proto
GEN_OUT    := $(CURDIR)/../../gen/go
GO_PKG_PREFIX := github.com/rashmirrout/DashCenter/src/impl-go/gen/go

PROTO_FILES := $(shell find $(PROTO_ROOT) -type f -name '*.proto' -not -path '*/vendor/sonic-dash-api/VERSION*')

.PHONY: gen tools clean

gen:
	@mkdir -p $(GEN_OUT)
	protoc \
		--proto_path=$(PROTO_ROOT) \
		--go_out=$(GEN_OUT) --go_opt=paths=source_relative \
		--go-grpc_out=$(GEN_OUT) --go-grpc_opt=paths=source_relative,require_unimplemented_servers=false \
		$(PROTO_FILES)

tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

clean:
	rm -rf $(GEN_OUT)
