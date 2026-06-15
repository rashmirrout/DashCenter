"""Probe the SPA-shape PUT path more precisely:
  1. labels at TOP LEVEL (what bodyForPut produces) → does it round-trip?
  2. HaSet — what shape does dashd accept for scope+members?
  3. VnetMapping — confirm name is auto-derived from vnet_name+ip_address
"""

from __future__ import annotations
import json, sys, time, urllib.request, urllib.error
from typing import Any

BASE = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:3000"
API = f"{BASE}/api/v1"
NS = "default"

def http(method, path, body=None) -> tuple[int, Any, str]:
    url = f"{API}{path}"
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    if body is not None:
        req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            raw = resp.read().decode() or "null"
            return resp.status, json.loads(raw), ""
    except urllib.error.HTTPError as e:
        return e.code, None, e.read().decode()
    except Exception as e:
        return -1, None, repr(e)

def cleanup(slug, name):
    http("DELETE", f"/{NS}/{slug}/{name}")

# ═══════════════════════════════════════════════════════════════
print("\n══ TEST 1: SPA labels at TOP LEVEL (bodyForPut output) ══")
# ═══════════════════════════════════════════════════════════════
n = "lb-spa-shape"
status, _, err = http("PUT", f"/{NS}/vnets/{n}", {
    "metadata": {"namespace": NS, "name": n},
    "vni": 90099,
    "labels": {"src": "top-level"},  # ← what bodyForPut now produces
})
print(f"  PUT  → HTTP {status} err={err}")
time.sleep(0.4)
status, got, _ = http("GET", f"/{NS}/vnets/{n}")
spec = got.get("spec") if isinstance(got, dict) else None
print(f"  GET  → spec.labels = {spec.get('labels') if spec else 'NONE'}")
cleanup("vnets", n)

# ═══════════════════════════════════════════════════════════════
print("\n══ TEST 2: HaSet — discover the real write shape ══")
# ═══════════════════════════════════════════════════════════════
print("\n  Existing HaSets after bootstrap — what shape do they have?")
status, lst, _ = http("GET", f"/{NS}/ha-sets")
items = (lst or {}).get("items", [])
print(f"  count: {len(items)}")
for it in items[:2]:
    print(json.dumps(it, indent=2)[:500])

# Try the bootstrap-style shape with `mode` + `member_dpu_ids`
print("\n  variant A — bootstrap shape (mode + member_dpu_ids):")
n = "ha-test-A"
body_a = {
    "metadata": {"namespace": NS, "name": n},
    "mode": "active_standby",
    "member_dpu_ids": ["dpu-sim-01", "dpu-sim-02"],
    "virtual_ip": "10.0.0.99",
    "flow_sync_endpoints": ["udp://dpu-sim-01:4789", "udp://dpu-sim-02:4789"],
}
status, _, err = http("PUT", f"/{NS}/ha-sets/{n}", body_a)
print(f"  PUT  → HTTP {status} err={err}")
time.sleep(0.4)
status, got, _ = http("GET", f"/{NS}/ha-sets/{n}")
spec = got.get("spec") if isinstance(got, dict) else None
print(f"  GET  → spec keys: {sorted(spec.keys()) if spec else 'NONE'}")
print(f"  full spec: {json.dumps(spec, indent=2) if spec else 'NONE'}")
cleanup("ha-sets", n)

# Try with scope+members (current SPA schema)
print("\n  variant B — SPA shape (scope + members[]):")
n = "ha-test-B"
body_b = {
    "metadata": {"namespace": NS, "name": n},
    "scope": "appliance",
    "members": [
        {"dpu_id": "dpu-sim-01", "role": "ACTIVE"},
        {"dpu_id": "dpu-sim-02", "role": "STANDBY"},
    ],
    "virtual_ip": "10.0.0.98",
}
status, _, err = http("PUT", f"/{NS}/ha-sets/{n}", body_b)
print(f"  PUT  → HTTP {status} err={err}")
time.sleep(0.4)
status, got, _ = http("GET", f"/{NS}/ha-sets/{n}")
spec = got.get("spec") if isinstance(got, dict) else None
print(f"  GET  → spec keys: {sorted(spec.keys()) if spec else 'NONE'}")
print(f"  full spec: {json.dumps(spec, indent=2) if spec else 'NONE'}")
cleanup("ha-sets", n)

# ═══════════════════════════════════════════════════════════════
print("\n══ TEST 3: VnetMapping — confirm name auto-derivation ══")
# ═══════════════════════════════════════════════════════════════

# PUT with our chosen name, then GET by the synthetic key
n_chosen = "my-chosen-name"
synthetic = "analytics-kafka-10.0.0.77"
body = {
    "metadata": {"namespace": NS, "name": n_chosen},
    "vnet_name": "analytics-kafka",
    "ip_address": "10.0.0.77",
    "underlay_ip": "10.99.0.77",
    "mac_address": "aa:bb:cc:00:00:77",
    "action": "vnet_encap",
}
status, resp, err = http("PUT", f"/{NS}/vnet-mappings/{n_chosen}", body)
print(f"  PUT  → HTTP {status} resp={resp}")

time.sleep(0.5)
print(f"\n  GET via user-chosen '{n_chosen}':")
status, got, err = http("GET", f"/{NS}/vnet-mappings/{n_chosen}")
print(f"    → HTTP {status}  err={err[:60] if err else ''}")

print(f"\n  GET via synthetic '{synthetic}':")
status, got, err = http("GET", f"/{NS}/vnet-mappings/{synthetic}")
print(f"    → HTTP {status}")
if isinstance(got, dict) and got.get("spec"):
    print(f"    spec.name='{got['spec'].get('name')}'")
    print(f"    spec keys: {sorted(got['spec'].keys())}")

cleanup("vnet-mappings", synthetic)
cleanup("vnet-mappings", n_chosen)