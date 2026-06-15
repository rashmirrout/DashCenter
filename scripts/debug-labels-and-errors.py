"""Drill-down probe to find:
  1. Where dashd accepts `labels` (metadata.labels vs top-level vs spec.labels)
  2. Why AclPolicy / RoutePolicy PUT returns HTTP 400 (verbose error)
  3. Why HaSet PUT returns HTTP 405 (route mismatch)
  4. Why VnetMapping PUT 200 → GET 404 (silent storage failure)

Tests against the BFF on :3000 by default.
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
        err_body = e.read().decode()
        return e.code, None, err_body
    except Exception as e:
        return -1, None, repr(e)

def cleanup(slug, name):
    http("DELETE", f"/{NS}/{slug}/{name}")

def probe_get_spec(slug, name):
    s, got, _ = http("GET", f"/{NS}/{slug}/{name}")
    if s == 404 or not isinstance(got, dict):
        return s, None
    return s, got.get("spec")

# ═══════════════════════════════════════════════════════════════════
print("\n══ TEST 1: where do `labels` need to go for Vnet? ══")
# ═══════════════════════════════════════════════════════════════════

print("\n  variant A — labels inside metadata:")
n = "lb-test-A"
http("PUT", f"/{NS}/vnets/{n}", {
    "metadata": {"namespace": NS, "name": n, "labels": {"src": "A"}},
    "vni": 90001,
})
time.sleep(0.3)
_, spec = probe_get_spec("vnets", n)
print(f"    spec.labels = {spec.get('labels') if spec else 'NONE'}")
cleanup("vnets", n)

print("\n  variant B — labels at TOP level (sibling of metadata):")
n = "lb-test-B"
http("PUT", f"/{NS}/vnets/{n}", {
    "metadata": {"namespace": NS, "name": n},
    "vni": 90002,
    "labels": {"src": "B"},
})
time.sleep(0.3)
_, spec = probe_get_spec("vnets", n)
print(f"    spec.labels = {spec.get('labels') if spec else 'NONE'}")
cleanup("vnets", n)

print("\n  variant C — labels both inside metadata AND top-level:")
n = "lb-test-C"
http("PUT", f"/{NS}/vnets/{n}", {
    "metadata": {"namespace": NS, "name": n, "labels": {"src": "C-meta"}},
    "vni": 90003,
    "labels": {"src": "C-top"},
})
time.sleep(0.3)
_, spec = probe_get_spec("vnets", n)
print(f"    spec.labels = {spec.get('labels') if spec else 'NONE'}")
cleanup("vnets", n)

# Verify bootstrap-style quarantine ENI to see what shape its labels take
print("\n  existing eni-quarantine-01 from bootstrap.py:")
_, spec = probe_get_spec("enis", "eni-quarantine-01")
print(f"    spec.labels = {spec.get('labels') if spec else 'NONE'}")

# ═══════════════════════════════════════════════════════════════════
print("\n══ TEST 2: AclPolicy PUT — verbose error ══")
# ═══════════════════════════════════════════════════════════════════

n = "acl-err-test"
status, _, err = http("PUT", f"/{NS}/acl-policies/{n}", {
    "metadata": {"namespace": NS, "name": n},
    "stage": "inbound",
    "eni_names": ["eni-anything"],
    "rules": [{
        "priority": 100,
        "action": "allow",
        "src_prefixes": ["0.0.0.0/0"],
        "dst_ports": ["443"],
    }],
})
print(f"  PUT → HTTP {status}")
print(f"  error body: {err}")
cleanup("acl-policies", n)

# Look at an existing ACL to find the working shape
print("\n  existing acl-bank-web-inbound full body:")
status, got, _ = http("GET", f"/{NS}/acl-policies/acl-bank-web-inbound")
print(f"  HTTP {status}")
print(json.dumps(got, indent=2)[:600])

# ═══════════════════════════════════════════════════════════════════
print("\n══ TEST 3: RoutePolicy PUT — verbose error ══")
# ═══════════════════════════════════════════════════════════════════

n = "rp-err-test"
status, _, err = http("PUT", f"/{NS}/route-policies/{n}", {
    "metadata": {"namespace": NS, "name": n},
    "eni_names": ["eni-anything"],
    "routes": [{
        "prefix": "0.0.0.0/0",
        "next_hop_type": "vnet",
        "next_hop_target": "analytics-kafka",
        "metric": 100,
    }],
})
print(f"  PUT → HTTP {status}")
print(f"  error body: {err}")
cleanup("route-policies", n)

print("\n  existing rp-bank-web-default full body:")
status, got, _ = http("GET", f"/{NS}/route-policies/rp-bank-web-default")
print(f"  HTTP {status}")
print(json.dumps(got, indent=2)[:600])

# ═══════════════════════════════════════════════════════════════════
print("\n══ TEST 4: HaSet PUT — wrong slug? ══")
# ═══════════════════════════════════════════════════════════════════

# Try a few alternative slug names
for slug in ["ha", "ha-sets", "hasets", "ha-set"]:
    n = f"ha-test-{slug}"
    status, _, err = http("PUT", f"/{NS}/{slug}/{n}", {
        "metadata": {"namespace": NS, "name": n},
        "scope": "appliance",
        "members": [
            {"dpu_id": "dpu-sim-01", "role": "ACTIVE"},
            {"dpu_id": "dpu-sim-02", "role": "STANDBY"},
        ],
        "virtual_ip": "10.0.0.51",
    })
    print(f"  slug='{slug}' → PUT HTTP {status}  err={err[:100] if err else ''}")
    if status < 400:
        cleanup(slug, n)

print("\n  existing ha-bank-prod (the only working HA from bootstrap):")
status, got, _ = http("GET", f"/{NS}/ha/ha-bank-prod")
print(f"  HTTP {status}")
print(json.dumps(got, indent=2)[:600])

# ═══════════════════════════════════════════════════════════════════
print("\n══ TEST 5: VnetMapping PUT 200 → GET 404? ══")
# ═══════════════════════════════════════════════════════════════════

n = "map-test-A"
status, resp, err = http("PUT", f"/{NS}/vnet-mappings/{n}", {
    "metadata": {"namespace": NS, "name": n},
    "vnet_name": "analytics-kafka",
    "ip_address": "10.0.0.99",
    "underlay_ip": "10.99.99.2",
    "mac_address": "aa:bb:cc:00:00:9a",
    "action": "vnet_encap",
})
print(f"  PUT  → HTTP {status} resp={resp} err={err}")
time.sleep(0.5)
status, got, err = http("GET", f"/{NS}/vnet-mappings/{n}")
print(f"  GET  → HTTP {status}  body keys: {list(got.keys()) if isinstance(got, dict) else got}")
if status >= 400:
    print(f"  err body: {err}")
cleanup("vnet-mappings", n)

# Check the LIST endpoint
status, lst, _ = http("GET", f"/{NS}/vnet-mappings")
print(f"\n  LIST count: {len(lst.get('items', [])) if isinstance(lst, dict) else 'N/A'}")
if isinstance(lst, dict) and lst.get("items"):
    first = lst["items"][0]
    print(f"  first item: name='{first.get('name')}' keys={list(first.get('spec',{}).keys())}")

# Look for our map by name via list
items = (lst or {}).get("items", [])
ours = [it for it in items if it.get("name") == n]
print(f"  map-test-A in LIST? {'YES' if ours else 'NO'}")