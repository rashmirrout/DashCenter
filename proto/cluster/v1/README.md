# `proto/cluster/v1/` — dashd internal cluster RPCs (Model 2 only)

These services are spoken **only between dashd peers** in the controllerless
deployment model. Not exposed to operators.

## Planned files (TODO during implementation)

| File | Service | Purpose |
|---|---|---|
| `cluster.proto` | `Cluster` | Raft AppendEntries / RequestVote / InstallSnapshot wrappers; SWIM ping/ack tunneled through gRPC where firewalls require it. |

In Model 1 (centralized controller) the entire `cluster/` package is unused;
the Go build excludes it via the `cluster` build tag.
