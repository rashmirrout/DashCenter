# DashCenter Concepts

This folder holds **conceptual documents** that explain *why* and *how*
the DashCenter platform is wired together. These docs complement (but do
not replace) the canonical references:

- [`docs/dashd-features/features.md`](../dashd-features/features.md) — REST API reference.
- [`docs/CLI_GUIDE.md`](../CLI_GUIDE.md) — `dash-sim-client` CLI guide.
- [`specs/HLD/dashctl-hld.md`](../../specs/HLD/dashctl-hld.md) — `dashctl` design.

## Documents

| Document | Audience | What you'll learn |
|---|---|---|
| [dashd-configuration-concepts.md](dashd-configuration-concepts.md) | Operators, network engineers, integrators | The seven dashd resource kinds (VNET, ENI, VnetMapping, ServiceTunnel, RoutePolicy, AclPolicy, HaSet), their dependency graph, the mandatory phased creation order, what happens when you violate it, and how to verify the wired ENI with `trace-flow` + `explain-match` — all with live captures from a running cluster + complete YAML manifest. |

## Reproduction harnesses

Each narrative document has a sibling folder named after the document
base-name; that folder contains the runnable scripts and payloads used
to generate every live capture in the document.

| Companion folder | Reproduces |
|---|---|
| [`dashd-configuration-concepts/`](./dashd-configuration-concepts/) | All curl + dashd captures embedded in [`dashd-configuration-concepts.md`](./dashd-configuration-concepts.md) |