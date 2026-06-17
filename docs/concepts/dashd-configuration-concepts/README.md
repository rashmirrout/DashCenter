# dashd-configuration-concepts — runnable evidence

This folder is the **reproduction harness** for the narrative document
[`../dashd-configuration-concepts.md`](../dashd-configuration-concepts.md). Every
command and every dashd response embedded in that document was captured
by the scripts in this folder against a live 3-node dashd fleet.

## Files

| File | Purpose |
|---|---|
| `run_experiments.py`        | Part 1: pre-flight, wrong-order PUTs (§4), correct-order phased creation (§5), reconcile. |
| `run_experiments_part2.py`  | Part 2: trace-flow against the `concepts-demo` namespace, delete-order experiment (§8), cleanup. |
| `run_experiments_part3.py`  | Part 3: trace-flow + explain-match against the pre-programmed `default` namespace ENIs (the captures used in §7). |
| `payloads/*.json`           | Example JSON payloads used while iterating. |
| `run*.log`                  | Captured transcripts (regenerated each run). **Not committed** — see [`.gitignore`](../../../.gitignore). |

## Reproduce

You need a running dashd fleet. The defaults assume the lab fleet from
[`deploy/test-setup/05-full-console/`](../../../deploy/test-setup/05-full-console/) (leader on REST `:28463`).

```powershell
cd c:\WorkSpace\PS\PublicRepo\DashCenter
python docs/concepts/dashd-configuration-concepts/run_experiments.py        > docs/concepts/dashd-configuration-concepts/run.log       2>&1
python docs/concepts/dashd-configuration-concepts/run_experiments_part2.py  > docs/concepts/dashd-configuration-concepts/run-part2.log 2>&1
python docs/concepts/dashd-configuration-concepts/run_experiments_part3.py  > docs/concepts/dashd-configuration-concepts/run-part3.log 2>&1
```

> If the leader has moved, update `LEADER_REST` in each script. Find the
> current leader with:
> `curl http://127.0.0.1:27443/admin/leader ; curl http://127.0.0.1:27453/admin/leader ; curl http://127.0.0.1:27463/admin/leader`

## What each phase proves

- **Pre-flight**: namespace `concepts-demo` is empty before the run.
- **PHASE A — wrong-order PUTs**: every kind that references another
  fails admission with `HTTP 400 invalid argument: ... cross-namespace
  reference rejected`.
- **PHASE B — correct-order PUTs**: every kind succeeds with
  `HTTP 200 {accepted: true, generation: 1}`.
- **PHASE C — trace-flow against `default`**: the engine returns
  full `verdict + trace[] + matched_*` triples showing the pipeline
  walked end-to-end.
- **PHASE D — delete-order**: current build is permissive; design
  intent (412 on referenced object) is not yet enforced.
- **PHASE E — cleanup**: shows the auto-rename of VnetMapping
  (server uses `{vnet_name}-{ip_address}`, not the URL name).