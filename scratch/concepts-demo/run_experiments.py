"""
Comprehensive experiment driver for dashd configuration concepts.

Runs:
  - Wrong-order attempts (capture exact error responses)
  - Correct-order phased creation (capture every success response)
  - Verification (list, get, trace-flow)
  - Cleanup (delete in reverse dependency order)

Every command + response is printed in a clear, copy-pasteable block.

Usage:
  python scratch/concepts-demo/run_experiments.py
"""

from __future__ import annotations

import json
import sys
import time
import urllib.error
import urllib.request

# dashd-3 is the current leader (REST :28463)
LEADER_REST = "http://127.0.0.1:28463"
NS = "concepts-demo"

# Use unique names so we don't clash with any pre-existing data
VNET_WEB = "demo-web-vnet"
VNET_DB = "demo-db-vnet"
TUNNEL = "demo-nat-tunnel"
ENI = "demo-eni-01"
VNET_MAPPING = "demo-web-mapping-01"
ROUTE_POLICY = "demo-web-routes"
ACL_IN = "demo-web-acl-inbound"
ACL_OUT = "demo-web-acl-outbound"

SEP = "=" * 78


def banner(title: str) -> None:
    print()
    print(SEP)
    print(f"  {title}")
    print(SEP)


def section(title: str) -> None:
    print()
    print(f"--- {title} ---")


def http(
    method: str, path: str, body: dict | None = None, expect_status: int | None = None
) -> tuple[int, str]:
    """Execute HTTP request, print command and response, return (status, body)."""
    url = LEADER_REST + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={"Content-Type": "application/json"} if body is not None else {},
    )

    # Echo the equivalent curl
    if body is not None:
        body_str = json.dumps(body, indent=2)
        print(f"\n$ curl -s -X {method} \\")
        print(f"    {url} \\")
        print(f"    -H 'Content-Type: application/json' \\")
        print(f"    -d '{json.dumps(body)}'")
    else:
        print(f"\n$ curl -s -X {method} {url}")

    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            status = resp.getcode()
            text = resp.read().decode("utf-8")
    except urllib.error.HTTPError as e:
        status = e.code
        text = e.read().decode("utf-8")
    except urllib.error.URLError as e:
        print(f"\n!! URLError: {e}")
        return (0, str(e))

    # Pretty-print response if JSON
    try:
        parsed = json.loads(text)
        pretty = json.dumps(parsed, indent=2)
    except Exception:
        pretty = text

    print(f"\nHTTP {status}")
    print(pretty)

    if expect_status is not None and status != expect_status:
        print(f"!! EXPECTED HTTP {expect_status}, got {status}")

    return (status, text)


def main() -> int:
    banner("PRE-FLIGHT — confirm leader + clean namespace")

    section("Confirm leader")
    http("GET", "/admin/leader")

    section("Check namespace is clean (no leftover demo objects)")
    for kind in (
        "vnets",
        "service-tunnels",
        "enis",
        "vnet-mappings",
        "route-policies",
        "acl-policies",
    ):
        print(f"\n# list {kind} in ns='{NS}'")
        http("GET", f"/v1/{NS}/{kind}")

    # ---------------------------------------------------------------------- #
    # PHASE A — WRONG-ORDER ATTEMPTS                                          #
    # ---------------------------------------------------------------------- #
    banner("PHASE A — WRONG-ORDER ATTEMPTS (expected to fail or accept-but-dangle)")

    section("A.1 — Create ENI referencing a VNET that does NOT exist")
    http(
        "PUT",
        f"/v1/{NS}/enis/{ENI}",
        {
            "vnet_name": "vnet-does-not-exist",
            "mac_address": "aa:bb:cc:dd:ee:01",
            "underlay_ip": "10.99.0.11",
            "admin_state": "up",
        },
    )

    section("A.2 — Create VnetMapping referencing a VNET that does NOT exist")
    http(
        "PUT",
        f"/v1/{NS}/vnet-mappings/{VNET_MAPPING}",
        {
            "vnet_name": "vnet-does-not-exist",
            "ip_address": "192.168.250.1",
            "underlay_ip": "10.99.0.11",
            "mac_address": "aa:bb:cc:dd:ee:01",
            "action": "vnet_encap",
        },
    )

    section("A.3 — Create RoutePolicy referencing ENIs that do NOT exist")
    http(
        "PUT",
        f"/v1/{NS}/route-policies/{ROUTE_POLICY}",
        {
            "eni_names": ["eni-does-not-exist"],
            "routes": [
                {
                    "prefix": "192.168.250.0/24",
                    "next_hop_type": "vnet",
                    "next_hop_target": "vnet-does-not-exist",
                    "metric": 10,
                }
            ],
        },
    )

    section("A.4 — Create AclPolicy referencing ENIs that do NOT exist")
    http(
        "PUT",
        f"/v1/{NS}/acl-policies/{ACL_IN}",
        {
            "stage": "inbound",
            "eni_names": ["eni-does-not-exist"],
            "rules": [
                {
                    "priority": 100,
                    "action": "allow",
                    "src_prefixes": ["0.0.0.0/0"],
                    "dst_ports": ["443"],
                    "protocols": ["tcp"],
                    "description": "https in",
                }
            ],
        },
    )

    section("A.5 — Try to use the (still-non-existent) ENI in trace-flow diagnostics")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-01",
            "flow": {
                "direction": 1,
                "eni_name": ENI,
                "src_ip": "10.0.0.1",
                "dst_ip": "192.168.250.1",
                "src_port": 0,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    # Clean up anything that may have been accepted
    section("A.6 — Clean up any objects that were accepted in PHASE A")
    for kind, name in (
        ("acl-policies", ACL_IN),
        ("route-policies", ROUTE_POLICY),
        ("vnet-mappings", VNET_MAPPING),
        ("enis", ENI),
    ):
        http("DELETE", f"/v1/{NS}/{kind}/{name}")

    # ---------------------------------------------------------------------- #
    # PHASE B — CORRECT ORDER                                                 #
    # ---------------------------------------------------------------------- #
    banner("PHASE B — CORRECT-ORDER PHASED CREATION")

    section("B.1 — Create VNETs (no dependencies)")
    http(
        "PUT",
        f"/v1/{NS}/vnets/{VNET_WEB}",
        {
            "vni": 9001,
            "address_space": ["192.168.250.0/24"],
            "gw_mac": "00:00:00:00:99:01",
        },
        expect_status=200,
    )
    http(
        "PUT",
        f"/v1/{NS}/vnets/{VNET_DB}",
        {
            "vni": 9002,
            "address_space": ["192.168.251.0/24"],
            "gw_mac": "00:00:00:00:99:02",
        },
        expect_status=200,
    )

    section("B.2 — Create ServiceTunnel (no dependencies)")
    http(
        "PUT",
        f"/v1/{NS}/service-tunnels/{TUNNEL}",
        {
            "local_underlay_ip": "10.255.99.10",
            "remote_underlay_ip": "198.51.100.99",
            "vni": 9101,
            "params": {
                "action": "nat",
                "nat_pool": "203.0.113.128/26",
                "snat_persist_seconds": "300",
            },
        },
        expect_status=200,
    )

    section("B.3 — Create ENI (references VNET demo-web-vnet)")
    http(
        "PUT",
        f"/v1/{NS}/enis/{ENI}",
        {
            "vnet_name": VNET_WEB,
            "mac_address": "aa:bb:cc:99:00:01",
            "underlay_ip": "10.99.0.11",
            "admin_state": "up",
            "placement_hint_dpu_ids": ["dpu-sim-01"],
        },
        expect_status=200,
    )

    section("B.4 — Create VnetMapping (references VNET demo-web-vnet)")
    http(
        "PUT",
        f"/v1/{NS}/vnet-mappings/{VNET_MAPPING}",
        {
            "vnet_name": VNET_WEB,
            "ip_address": "192.168.250.10",
            "underlay_ip": "10.99.0.11",
            "mac_address": "aa:bb:cc:99:00:01",
            "action": "vnet_encap",
        },
        expect_status=200,
    )

    section("B.5 — Create RoutePolicy (references ENI + VNETs + ServiceTunnel)")
    http(
        "PUT",
        f"/v1/{NS}/route-policies/{ROUTE_POLICY}",
        {
            "eni_names": [ENI],
            "routes": [
                {
                    "prefix": "192.168.250.0/24",
                    "next_hop_type": "vnet",
                    "next_hop_target": VNET_WEB,
                    "metric": 10,
                },
                {
                    "prefix": "192.168.251.0/24",
                    "next_hop_type": "vnet",
                    "next_hop_target": VNET_DB,
                    "metric": 20,
                },
                {
                    "prefix": "0.0.0.0/0",
                    "next_hop_type": "service_tunnel",
                    "next_hop_target": TUNNEL,
                    "metric": 100,
                },
            ],
        },
        expect_status=200,
    )

    section("B.6 — Create AclPolicy inbound (references ENI)")
    http(
        "PUT",
        f"/v1/{NS}/acl-policies/{ACL_IN}",
        {
            "stage": "inbound",
            "eni_names": [ENI],
            "rules": [
                {
                    "priority": 100,
                    "action": "allow",
                    "src_prefixes": ["0.0.0.0/0"],
                    "dst_ports": ["443"],
                    "protocols": ["tcp"],
                    "description": "https from anywhere",
                },
                {
                    "priority": 200,
                    "action": "deny",
                    "src_prefixes": ["0.0.0.0/0"],
                    "dst_ports": ["22"],
                    "protocols": ["tcp"],
                    "description": "no ssh from anywhere",
                },
                {
                    "priority": 1000,
                    "action": "deny",
                    "src_prefixes": ["0.0.0.0/0"],
                    "description": "catch-all deny",
                },
            ],
        },
        expect_status=200,
    )

    section("B.7 — Create AclPolicy outbound (references ENI)")
    http(
        "PUT",
        f"/v1/{NS}/acl-policies/{ACL_OUT}",
        {
            "stage": "outbound",
            "eni_names": [ENI],
            "rules": [
                {
                    "priority": 100,
                    "action": "allow",
                    "dst_prefixes": ["192.168.251.0/24"],
                    "dst_ports": ["3306", "5432"],
                    "protocols": ["tcp"],
                    "description": "to db tier",
                },
                {
                    "priority": 110,
                    "action": "allow",
                    "dst_prefixes": ["0.0.0.0/0"],
                    "dst_ports": ["443"],
                    "protocols": ["tcp"],
                    "description": "outbound https",
                },
                {
                    "priority": 1000,
                    "action": "deny",
                    "dst_prefixes": ["0.0.0.0/0"],
                    "description": "catch-all egress deny",
                },
            ],
        },
        expect_status=200,
    )

    # ---------------------------------------------------------------------- #
    # PHASE C — VERIFY                                                        #
    # ---------------------------------------------------------------------- #
    banner("PHASE C — VERIFY FULLY-WIRED ENI")

    section("C.1 — List every kind in the namespace (sanity)")
    for kind in (
        "vnets",
        "service-tunnels",
        "enis",
        "vnet-mappings",
        "route-policies",
        "acl-policies",
    ):
        print(f"\n# list {kind}")
        http("GET", f"/v1/{NS}/{kind}")

    section("C.2 — Get the ENI back")
    http("GET", f"/v1/{NS}/enis/{ENI}")

    section("C.3 — Force a reconcile so the DPU programs the policy")
    http("POST", "/v1/reconcile", {"dpu_ids": ["dpu-sim-01"], "namespaces": [NS]})

    # Give the reconciler a moment
    time.sleep(2)

    section("C.4 — trace-flow: outbound 192.168.250.10 → 192.168.250.10 (vnet hit)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-01",
            "flow": {
                "direction": 2,  # OUTBOUND
                "eni_name": ENI,
                "src_ip": "10.0.0.1",
                "dst_ip": "192.168.250.10",
                "src_port": 1024,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    section("C.5 — trace-flow: outbound 0.0.0.0 fallback → service_tunnel (NAT)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-01",
            "flow": {
                "direction": 2,  # OUTBOUND
                "eni_name": ENI,
                "src_ip": "10.0.0.1",
                "dst_ip": "8.8.8.8",
                "src_port": 1024,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    section("C.6 — trace-flow: inbound 443 should be ALLOWED")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-01",
            "flow": {
                "direction": 1,  # INBOUND
                "eni_name": ENI,
                "src_ip": "203.0.113.7",
                "dst_ip": "192.168.250.10",
                "src_port": 12345,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    section("C.7 — trace-flow: inbound 22 should be DROPPED by ACL")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-01",
            "flow": {
                "direction": 1,  # INBOUND
                "eni_name": ENI,
                "src_ip": "203.0.113.7",
                "dst_ip": "192.168.250.10",
                "src_port": 12345,
                "dst_port": 22,
                "protocol": "tcp",
            },
        },
    )

    # ---------------------------------------------------------------------- #
    # PHASE D — DELETE-ORDER EXPERIMENT                                       #
    # ---------------------------------------------------------------------- #
    banner("PHASE D — DELETE-ORDER (delete VNET first, expect FailedPrecondition)")

    section("D.1 — Try to delete VNET while ENI still references it")
    http("DELETE", f"/v1/{NS}/vnets/{VNET_WEB}")

    # ---------------------------------------------------------------------- #
    # PHASE E — CLEANUP                                                       #
    # ---------------------------------------------------------------------- #
    banner("PHASE E — CLEANUP (reverse dependency order)")
    for kind, name in (
        ("acl-policies", ACL_OUT),
        ("acl-policies", ACL_IN),
        ("route-policies", ROUTE_POLICY),
        ("vnet-mappings", VNET_MAPPING),
        ("enis", ENI),
        ("service-tunnels", TUNNEL),
        ("vnets", VNET_DB),
        ("vnets", VNET_WEB),
    ):
        http("DELETE", f"/v1/{NS}/{kind}/{name}")

    print()
    print(SEP)
    print("  DONE — see scratch/concepts-demo/run.log for the full transcript")
    print(SEP)
    return 0


if __name__ == "__main__":
    sys.exit(main())