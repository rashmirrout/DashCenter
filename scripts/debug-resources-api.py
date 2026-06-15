"""Probe the live dashw REST API to verify wire-format round-trips
for every resource kind the /resources page can create.

This script sends the SAME flat wire format the SPA now sends
(via `bodyForPut`) and verifies every spec field round-trips on GET.

Each scenario:
  1. PUT the resource with `{ metadata: {ns, name}, ...spec_fields }`
  2. GET it back via the same BFF
  3. Compare expected spec fields to the dashd envelope `spec`
  4. Clean up

Usage:
  python scripts/debug-resources-api.py [base_url]

Default base_url is http://localhost:3000 (the dashw BFF dev container).
"""

from __future__ import annotations
import json
import sys
import time
import urllib.request
import urllib.error
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
    except Exception as e:  # noqa: BLE001
        return -1, {"_exc": repr(e)}

def diff_keys(want: dict, got: dict, path: str = "") -> list[str]:
    """Return a list of human-readable mismatches: missing keys,
    type drift, or value drift. Recurses into nested dicts."""
    out = []
    for k, want_v in want.items():
        if k not in got:
            out.append(f"  MISSING: {path}{k} (wanted {want_v!r})")
            continue
        got_v = got[k]
        if isinstance(want_v, dict) and isinstance(got_v, dict):
            out.extend(diff_keys(want_v, got_v, path=f"{path}{k}."))
            continue
        if isinstance(want_v, list) and isinstance(got_v, list):
            # For lists of dicts, compare element-wise by index.
            if len(want_v) != len(got_v):
                out.append(f"  DRIFT  : {path}{k}  len wanted={len(want_v)} got={len(got_v)}")
                continue
            if want_v and isinstance(want_v[0], dict):
                for i, (w, g) in enumerate(zip(want_v, got_v)):
                    out.extend(diff_keys(w, g, path=f"{path}{k}.{i}."))
                continue
            if sorted(map(str, want_v)) != sorted(map(str, got_v)):
                out.append(f"  DRIFT  : {path}{k}  wanted={want_v} got={got_v}")
            continue
        if want_v != got_v:
            out.append(f"  DRIFT  : {path}{k}  wanted={want_v!r} got={got_v!r}")
    return out

def to_wire(body: dict) -> dict:
    """Mirror what the SPA's `bodyForPut` does at submit-time:
       lift `metadata.labels` → top-level `labels`. dashd's writer
       only persists labels from the top-level field; nested ones
       are dropped silently. The SPA does this transparently for
       form submissions, but our probe sends raw shapes — so we
       apply the same transform here for an apples-to-apples test."""
    out = dict(body)
    meta = dict(body.get("metadata") or {})
    labels = meta.pop("labels", None)
    if labels is not None:
        out["labels"] = {**(out.get("labels") or {}), **labels}
    out["metadata"] = meta
    return out

def scenario(label: str, kind_slug: str, name: str, spa_body: dict,
             expected_spec: dict, cleanup: bool = True) -> bool:
    """PUT (SPA flat shape) then GET; compare; report; cleanup."""
    print(f"\n── {label} — {kind_slug}/{name} ──")
    status, _ = http("PUT", f"/{NS}/{kind_slug}/{name}", to_wire(spa_body))
    print(f"  PUT  → HTTP {status}")
    if status >= 400:
        print(f"  ✗ PUT failed; body sent was:\n{json.dumps(spa_body, indent=2)}")
        return False

    # Brief settle — dashd writes to etcd then reflects in lists.
    time.sleep(0.3)

    status, got = http("GET", f"/{NS}/{kind_slug}/{name}")
    print(f"  GET  → HTTP {status}")
    if status >= 400:
        print(f"  ✗ GET failed: {got}")
        return False

    # dashd returns wire shape: {kind, name, namespace, generation, spec}
    got_spec = got.get("spec") if isinstance(got, dict) else None
    if got_spec is None:
        print(f"  ✗ GET returned no .spec field. Raw: {json.dumps(got, indent=2)}")
        return False

    diffs = diff_keys(expected_spec, got_spec)
    if not diffs:
        print(f"  ✓ All {len(expected_spec)} spec fields round-tripped.")
        ok = True
    else:
        print(f"  ✗ Spec drift detected:")
        for d in diffs:
            print(d)
        print(f"  · Raw GET spec:\n{json.dumps(got_spec, indent=2)}")
        ok = False

    if cleanup:
        cleanup_status, _ = http("DELETE", f"/{NS}/{kind_slug}/{name}")
        print(f"  DEL  → HTTP {cleanup_status}")

    return ok

def main():
    print(f"Probing dashw at {BASE}")
    print(f"Namespace: {NS}\n")

    # Discover seed data we can reference safely.
    _, vnets = http("GET", f"/{NS}/vnets")
    vnet_names = [
        v.get("name") or v.get("metadata", {}).get("name")
        for v in (vnets or {}).get("items", [])
    ]
    vnet_names = [v for v in vnet_names if v]
    print(f"Discovered {len(vnet_names)} vnets; first 3: {vnet_names[:3]}")

    _, inv = http("GET", "/inventory")
    dpu_ids = [d.get("id") for d in (inv or {}).get("dpus", [])]
    dpu_ids = [d for d in dpu_ids if d]
    print(f"Discovered {len(dpu_ids)} DPUs; first 3: {dpu_ids[:3]}")

    _, sts = http("GET", f"/{NS}/service-tunnels")
    st_names = [
        s.get("name") or s.get("metadata", {}).get("name")
        for s in (sts or {}).get("items", [])
    ]
    st_names = [s for s in st_names if s]
    print(f"Discovered {len(st_names)} service-tunnels; first 3: {st_names[:3]}")

    if not vnet_names or not dpu_ids:
        print("Not enough seed data to run scenarios. Exiting.")
        sys.exit(1)

    parent_vnet = vnet_names[0]
    dpu1 = dpu_ids[0]
    dpu2 = dpu_ids[1] if len(dpu_ids) > 1 else dpu_ids[0]

    # Discover real ENI names so AclPolicy / RoutePolicy don't fail
    # dashd's referential-integrity check (eni_names must reference
    # existing ENIs in the same namespace).
    _, enis = http("GET", f"/{NS}/enis")
    eni_names = [
        e.get("name") or e.get("metadata", {}).get("name")
        for e in (enis or {}).get("items", [])
    ]
    eni_names = [e for e in eni_names if e]
    print(f"Discovered {len(eni_names)} existing ENIs; first 3: {eni_names[:3]}")
    real_eni = eni_names[0] if eni_names else "eni-anything"

    results = []

    # ── 1. Vnet ──
    name = "probe-vnet-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "vni": 99001,
    }
    expected = {"name": name, "vni": 99001, "labels": {"probe": "true"}}
    results.append(("Vnet", scenario(
        "Vnet create + readback", "vnets", name, spa_body, expected,
    )))

    # ── 2. ENI (the user-reported scenario) ──
    name = "probe-eni-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "vnet_name": parent_vnet,
        "mac_address": "aa:bb:cc:00:00:99",
        "underlay_ip": "10.99.99.1",
        "admin_state": "up",
        "placement_hint_dpu_ids": [dpu1],
    }
    expected = {
        "name": name,
        "vnet_name": parent_vnet,
        "mac_address": "aa:bb:cc:00:00:99",
        "underlay_ip": "10.99.99.1",
        "admin_state": "up",
        "placement_hint_dpu_ids": [dpu1],
        "labels": {"probe": "true"},
    }
    results.append(("ENI (user scenario)", scenario(
        "ENI create + readback (user-reported scenario)",
        "enis", name, spa_body, expected,
    )))

    # ── 3. Vnet Mapping ──
    # dashd auto-derives the mapping name as `{vnet_name}-{ip_address}`
    # — we must PUT to and GET from that synthetic key, not the
    # user-chosen one. (The SPA's form now mirrors this.)
    overlay_ip = "10.0.0.99"
    name = f"{parent_vnet}-{overlay_ip}"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "vnet_name": parent_vnet,
        "ip_address": overlay_ip,
        "underlay_ip": "10.99.99.2",
        "mac_address": "aa:bb:cc:00:00:9a",
        "action": "vnet_encap",
    }
    expected = {
        "vnet_name": parent_vnet,
        "ip_address": overlay_ip,
        "underlay_ip": "10.99.99.2",
        "mac_address": "aa:bb:cc:00:00:9a",
        "action": "vnet_encap",
        "labels": {"probe": "true"},
    }
    results.append(("VnetMapping", scenario(
        "VnetMapping create + readback (synthetic name)",
        "vnet-mappings", name, spa_body, expected,
    )))

    # ── 4. Service Tunnel ──
    name = "probe-st-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "local_underlay_ip": "10.0.0.10",
        "remote_underlay_ip": "10.0.0.20",
        "vni": 99002,
        "params": {"action": "nat"},
    }
    expected = {
        "name": name,
        "local_underlay_ip": "10.0.0.10",
        "remote_underlay_ip": "10.0.0.20",
        "vni": 99002,
        "params": {"action": "nat"},
        "labels": {"probe": "true"},
    }
    results.append(("ServiceTunnel", scenario(
        "ServiceTunnel create + readback",
        "service-tunnels", name, spa_body, expected,
    )))

    # ── 5. ACL Policy ──
    name = "probe-acl-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "stage": "inbound",
        "eni_names": [real_eni],
        "rules": [{
            "priority": 100,
            "action": "allow",
            "src_prefixes": ["0.0.0.0/0"],
            "dst_ports": ["443"],
        }],
    }
    expected = {
        "name": name,
        "stage": "inbound",
        "eni_names": [real_eni],
        "rules": [{
            "priority": 100,
            "action": "allow",
            "src_prefixes": ["0.0.0.0/0"],
            "dst_ports": ["443"],
        }],
        "labels": {"probe": "true"},
    }
    results.append(("AclPolicy", scenario(
        "AclPolicy create + readback",
        "acl-policies", name, spa_body, expected,
    )))

    # ── 6. Route Policy ──
    name = "probe-route-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name, "labels": {"probe": "true"}},
        "eni_names": [real_eni],
        "routes": [{
            "prefix": "0.0.0.0/0",
            "next_hop_type": "vnet",
            "next_hop_target": parent_vnet,
            "metric": 100,
        }],
    }
    expected = {
        "name": name,
        "eni_names": [real_eni],
        "routes": [{
            "prefix": "0.0.0.0/0",
            "next_hop_type": "vnet",
            "next_hop_target": parent_vnet,
            "metric": 100,
        }],
        "labels": {"probe": "true"},
    }
    results.append(("RoutePolicy", scenario(
        "RoutePolicy create + readback",
        "route-policies", name, spa_body, expected,
    )))

    # ── 7. HA Set ──
    # dashd routes HaSet under `/v1/{ns}/ha-sets/{name}` and uses
    # the `mode` + `member_dpu_ids[]` shape, NOT scope+members[].role.
    name = "probe-ha-001"
    spa_body = {
        "metadata": {"namespace": NS, "name": name},
        "mode": "active_standby",
        "member_dpu_ids": [dpu1, dpu2],
        "virtual_ip": "10.0.0.50",
        "flow_sync_endpoints": [
            f"udp://{dpu1}:4789",
            f"udp://{dpu2}:4789",
        ],
    }
    expected = {
        "name": name,
        "mode": "active_standby",
        "member_dpu_ids": [dpu1, dpu2],
        "virtual_ip": "10.0.0.50",
        "flow_sync_endpoints": [
            f"udp://{dpu1}:4789",
            f"udp://{dpu2}:4789",
        ],
    }
    results.append(("HaSet", scenario(
        "HaSet create + readback (new shape)",
        "ha-sets", name, spa_body, expected,
    )))

    # ── Summary ──
    print("\n══ SUMMARY ══")
    for label, ok in results:
        print(f"  {'✓' if ok else '✗'}  {label}")
    failed = [l for l, ok in results if not ok]
    if failed:
        print(f"\n{len(failed)} failure(s): {', '.join(failed)}")
        sys.exit(1)
    print(f"\nAll {len(results)} scenarios round-tripped cleanly.")

if __name__ == "__main__":
    main()