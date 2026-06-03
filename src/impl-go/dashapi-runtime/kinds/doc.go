// Package kinds is the single source of truth for the DASH object kinds
// exposed by the dashapi.v1.DashApi service. It maps an ObjectKind to its
// upstream proto.Message zero value, the upstream key-message field names,
// and pack/unpack helpers for the `Object.payload` oneof.
//
// Every per-kind switch in the codebase lives HERE so adding a new kind is
// a one-place change.
package kinds
