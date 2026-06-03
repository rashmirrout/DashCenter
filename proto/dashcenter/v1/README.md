# `proto/dashcenter/v1/` — dashd northbound API

The gRPC + REST contract that `dashctl`, the Web Console, and 3rd-party SDKs
use to talk to `dashd`. Same `.proto` files generate Go server stubs (for
dashd) and Go/Rust client stubs (for dashctl and external tooling).

## Planned files (TODO during implementation)

| File | Service / messages | Purpose |
|---|---|---|
| `inventory.proto` | `Inventory` | List appliances, registration phone-home, labels/selectors. |
| `state.proto`     | `State`     | Raw object reads (ENIs, VNETs, ACLs, routes). |
| `write.proto`     | `Write`     | Validated → staged → committed mutations. |
| `compute.proto`   | `Compute`   | `explain match`, `trace flow`, `acl-hit`. |
| `events.proto`    | `Events`    | Subscribe to state-change stream (WebSocket payloads). |
| `admin.proto`     | `Admin`     | Health, cache flush, reconcile trigger, audit log. |
