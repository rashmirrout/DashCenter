//go:build tools
// +build tools

// Package tools pins versions of dev-only Go tools so they're tracked in go.sum.
//
// Install with:
//
//	go install google.golang.org/protobuf/cmd/protoc-gen-go
//	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc
//	go install github.com/bufbuild/buf/cmd/buf
//	go install github.com/fullstorydev/grpcurl/cmd/grpcurl
package tools

import (
	_ "github.com/bufbuild/buf/cmd/buf"
	_ "github.com/fullstorydev/grpcurl/cmd/grpcurl"
	_ "google.golang.org/grpc/cmd/protoc-gen-go-grpc"
	_ "google.golang.org/protobuf/cmd/protoc-gen-go"
)
