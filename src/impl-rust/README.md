# Rust implementation of DashCenter (stub)

Mirror of `src/impl-go/` in Rust. **Not yet implemented** — only the directory
shape and Cargo workspace exist so future-you can `cargo new` into each crate
without re-arranging anything.

## Planned crates

| Crate                        | Binary(s)          | Equivalent Go module             |
|------------------------------|--------------------|----------------------------------|
| `crates/dashd/`              | `dashd`            | `src/impl-go/dashd`              |
| `crates/dashctl/`            | `dashctl`          | `src/impl-go/dashctl`            |
| `crates/dash-sim/`           | `dash-sim`         | `src/impl-go/dash-sim`           |
| `crates/dash-sim-client/`    | `dash-sim-client`  | `src/impl-go/dash-sim-client`    |
| `crates/dashsim-client-sdk/` | (library)          | `dash-sim-client/pkg/client`     |

## Planned stack

| Concern                | Crate |
|------------------------|-------|
| gRPC                   | `tonic` |
| Async runtime          | `tokio` |
| Proto codegen          | `tonic-build` (`build.rs` per crate) |
| HTTP                   | `axum` |
| Redis                  | `fred` or `redis-rs` |
| Raft                   | `openraft` |
| SWIM gossip            | `foca` or `chitchat` |
| CLI                    | `clap` v4 |

## When to flesh this out

After the Go impl reaches v0.2 (sim + dashd talking over real protos +
conformance suite passing). Re-running the conformance suite against the Rust
build is how we'll certify interop.
