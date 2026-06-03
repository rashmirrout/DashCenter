# `src/` — implementations

Every directory under `src/impl-XX/` is a **complete, self-contained build of
the four DashCenter modules** (`dashd`, `dashctl`, `dash-sim`, `dash-sim-client`)
in one language. All implementations consume the same shared assets at the
repo root:

| Shared asset                | Owner            | Consumers              |
|-----------------------------|------------------|------------------------|
| `proto/`                    | architecture team| every `src/impl-XX/`   |
| `deploy/compose/scenarios/` | architecture team| every `dash-sim` build |
| `test/conformance/`         | architecture team| every implementation   |

## Current implementations

| Folder              | Language | Status              |
|---------------------|----------|---------------------|
| [`impl-go/`](impl-go)     | Go 1.22+ | **Primary** — scaffolded. |
| [`impl-rust/`](impl-rust) | Rust 1.78+ | Stub — directory shape only. |

## Adding a new implementation

1. Copy `impl-rust/` as a template and rename to `impl-<lang>/`.
2. Implement the three binaries using the protos under `../../proto/`.
3. Add a build profile to `deploy/compose/docker-compose.yml`.
4. Make sure `test/conformance/` passes against your `dash-sim` binary.
