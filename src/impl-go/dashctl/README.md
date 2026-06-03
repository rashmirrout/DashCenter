# `dashctl` — operator CLI

`kubectl`-style command line for DashCenter. Talks to `dashd` over REST/gRPC.

## Build

```powershell
# From src/impl-go/
make all                # builds bin/dashctl
./bin/dashctl --help
```

## Command intents

| Intent     | Commands                              |
|------------|---------------------------------------|
| Discover   | `get`, `list`, `show`                 |
| Explain    | `explain match`, `explain route`      |
| Trace      | `trace flow`, `trace packet`          |
| Validate   | `reconcile`, `verify`, `diff`         |
| Observe    | `health`, `top`, `watch`, `events`    |
| Collect    | `bundle`, `logs`, `export`            |

See [specs/CLI-INTERFACE/dashcenter_diagnostics_cli_guide.md](../../../specs/CLI-INTERFACE/dashcenter_diagnostics_cli_guide.md)
for the full operator-facing manual.

## Layout

```
dashctl/
├── cmd/dashctl/main.go         Entry point
├── pkg/client/                 Reusable Go SDK against dashd's API
└── internal/
    ├── cmd/                    Cobra command definitions
    ├── context/                ~/.dashctl/config (kubectl-style)
    ├── render/                 --json / --yaml / --wide / --tree
    └── version/                Build metadata
```
