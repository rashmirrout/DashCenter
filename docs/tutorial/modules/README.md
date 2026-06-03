# Module deep dives

One markdown per module. Each page is a **self-contained** reference for
working on or extending that module.

| Module | What you'll find |
|---|---|
| [proto-dashapi.md](proto-dashapi.md) | The gRPC service contract: enums, messages, RPCs, key encoding rules. |
| [gen-go.md](gen-go.md) | The generated Go stubs module; how regeneration works; how to consume it. |
| [dashapi-runtime.md](dashapi-runtime.md) | The shared kinds registry; how to add a new object kind. |
| [dash-sim.md](dash-sim.md) | The behavioural simulator; pipeline package internals; admin HTTP. |
| [dash-sim-client.md](dash-sim-client.md) | The transport-only CLI; Cobra subcommand layout; SDK. |
| [dash-redis-adapter.md](dash-redis-adapter.md) | The SONiC-compatible Redis backend; APP_DB key layout; pub/sub. |
| [dashd.md](dashd.md) | (Placeholder) the planned fleet controller daemon. |
| [dashctl.md](dashctl.md) | (Placeholder) the planned controller CLI. |
