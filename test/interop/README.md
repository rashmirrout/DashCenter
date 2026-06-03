# Cross-implementation interop tests

Proves implementations under `src/impl-XX/` are wire-compatible.

Example matrix:

| dashd | dash-sim | Expected |
|---|---|---|
| impl-go  | impl-go  | pass (baseline) |
| impl-go  | impl-rust| pass |
| impl-rust| impl-go  | pass |
| impl-rust| impl-rust| pass |

Each combination is exercised by spinning up the relevant containers via
`deploy/compose/docker-compose.yml` profiles and running the conformance
suite from `test/conformance/`.
