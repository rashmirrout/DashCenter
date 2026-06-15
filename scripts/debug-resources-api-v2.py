"""Compare two wire-format strategies head-to-head:

Strategy A (current SPA via `denormalizeForPut`):
  PUT body: { kind: "Eni", name, namespace, spec: { ...fields... } }

Strategy B (bootstrap.py — known to work):
  PUT body: { metadata: { namespace, name }, ...fields... }

For each strategy, PUT then GET and compare the spec round-trip.
"""

from __future__ import annotations
import json, sys, time, urllib.request, urllib.error
from typing import Any

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:3000"
API = f"{BASE}/api/v1"
NS = "default"

def http(method: str, path: str, body: Any | None = None) -> tuple[int, Any]:
    url = f"{API}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            raw = resp.read().decode() or "null"
            return resp.status, json.loads(raw)
    except urllib.error.HTTPError as e:
        return e.code, {"_error": e.read().decode()}
    except Exception as e:
        return -1, {"_exc": repr(e)}

def cleanup(kind, name):
    http("DELETE", f"/{NS}/{kind}/{name}")

def show_spec(label, kind, name):
    status, got = http("GET", f"/{NS}/{kind}/{name}")
    spec = got.get("spec") if isinstance(got, dict) else None
    print(f"  {label}: status={status} spec_keys={sorted(spec.keys()) if spec else 'NONE'}")
    if spec:
        print(f"    {json.dumps(spec, indent=4)}")

print("\n══════════════════════════════════════════════════════════")
print(" Strategy A — wire envelope { kind, name, namespace, spec }")
print(" (what the SPA currently produces via denormalizeForPut)")
print("══════════════════════════════════════════════════════════")
name_a = "probe-eni-strategyA"
body_a = {
    "kind": "Eni",
    "name": name_a,
    "namespace": NS,
    "spec": {
        "vnet_name": "analytics-kafka",
        "mac_address": "aa:bb:cc:00:00:aa",
        "underlay_ip": "10.99.0.1",
        "admin_state": "up",
        "placement_hint_dpu_ids": ["dpu-sim-01"],
        "labels": {"probe": "A"},
    },
}
status, resp = http("PUT", f"/{NS}/enis/{name_a}", body_a)
print(f"PUT  → status={status}  resp={resp}")
time.sleep(0.5)
show_spec("AFTER STRATEGY A", "enis", name_a)
cleanup("enis", name_a)

print("\n══════════════════════════════════════════════════════════")
print(" Strategy B — flat { metadata, ...spec_fields_inline }")
print(" (what bootstrap.py uses — KNOWN TO WORK)")
print("══════════════════════════════════════════════════════════")
name_b = "probe-eni-strategyB"
body_b = {
    "metadata": {"namespace": NS, "name": name_b},
    "vnet_name": "analytics-kafka",
    "mac_address": "aa:bb:cc:00:00:bb",
    "underlay_ip": "10.99.0.2",
    "admin_state": "up",
    "placement_hint_dpu_ids": ["dpu-sim-01"],
    "labels": {"probe": "B"},
}
status, resp = http("PUT", f"/{NS}/enis/{name_b}", body_b)
print(f"PUT  → status={status}  resp={resp}")
time.sleep(0.5)
show_spec("AFTER STRATEGY B", "enis", name_b)
cleanup("enis", name_b)

print("\n══════════════════════════════════════════════════════════")
print(" Strategy C — flat without metadata wrapper")
print(" { vnet_name, mac_address, ... } at root")
print("══════════════════════════════════════════════════════════")
name_c = "probe-eni-strategyC"
body_c = {
    "namespace": NS,
    "name": name_c,
    "vnet_name": "analytics-kafka",
    "mac_address": "aa:bb:cc:00:00:cc",
    "underlay_ip": "10.99.0.3",
    "admin_state": "up",
    "placement_hint_dpu_ids": ["dpu-sim-01"],
    "labels": {"probe": "C"},
}
status, resp = http("PUT", f"/{NS}/enis/{name_c}", body_c)
print(f"PUT  → status={status}  resp={resp}")
time.sleep(0.5)
show_spec("AFTER STRATEGY C", "enis", name_c)
cleanup("enis", name_c)