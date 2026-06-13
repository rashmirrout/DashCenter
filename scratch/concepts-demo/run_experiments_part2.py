"""
Part 2 driver: continues from where part 1 left off (resources already created).

Runs:
  - C.4..C.7 trace-flow diagnostics
  - D.1 delete-order experiment (VNET first, with referrers)
  - E cleanup (reverse order)

All output is ASCII-safe so Windows cp1252 doesn't choke.
"""

from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request

LEADER_REST = "http://127.0.0.1:28463"
NS = "concepts-demo"

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

def http(method: str, path: str, body: dict | None = None) -> tuple[int, str]:
    url = LEADER_REST + path
    data = json.dumps(body).encode("utf-8") if body is not None else None
    req = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={"Content-Type": "application/json"} if body is not None else {},
    )

    if body is not None:
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

    try:
        parsed = json.loads(text)
        pretty = json.dumps(parsed, indent=2)
    except Exception:
        pretty = text

    print(f"\nHTTP {status}")
    print(pretty)
    return (status, text)

def main() -> int:
    banner("PHASE C - VERIFY FULLY-WIRED ENI (trace-flow diagnostics)")

    section("C.4 - trace-flow: outbound 192.168.250.10 ALLOW (vnet hit)")
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

    section("C.5 - trace-flow: outbound 8.8.8.8 fallback ENCAP via service_tunnel")
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

    section("C.6 - trace-flow: inbound 443 ALLOW")
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

    section("C.7 - trace-flow: inbound 22 DROP by ACL")
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

    section("C.8 - explain-match for the inbound 22 drop (which rule won?)")
    http(
        "POST",
        "/v1/diagnostics/explain-match",
        {
            "dpu_id": "dpu-sim-01",
            "subject": 1,  # ACL
            "flow": {
                "direction": 1,
                "eni_name": ENI,
                "src_ip": "203.0.113.7",
                "dst_ip": "192.168.250.10",
                "src_port": 12345,
                "dst_port": 22,
                "protocol": "tcp",
            },
        },
    )

    section("C.9 - explain-match for the outbound route lookup")
    http(
        "POST",
        "/v1/diagnostics/explain-match",
        {
            "dpu_id": "dpu-sim-01",
            "subject": 2,  # ROUTE
            "flow": {
                "direction": 2,
                "eni_name": ENI,
                "src_ip": "10.0.0.1",
                "dst_ip": "192.168.250.10",
                "src_port": 1024,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    # ------------------------------------------------------------------ #
    banner("PHASE D - DELETE-ORDER EXPERIMENT")

    section("D.1 - Try to delete VNET while ENI still references it")
    http("DELETE", f"/v1/{NS}/vnets/{VNET_WEB}")

    section("D.2 - Try to delete ENI while RoutePolicy + AclPolicy still reference it")
    http("DELETE", f"/v1/{NS}/enis/{ENI}")

    # ------------------------------------------------------------------ #
    banner("PHASE E - CLEANUP (correct reverse order)")
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

    section("E.X - Final verify the namespace is empty")
    for kind in (
        "vnets",
        "service-tunnels",
        "enis",
        "vnet-mappings",
        "route-policies",
        "acl-policies",
    ):
        http("GET", f"/v1/{NS}/{kind}")

    print()
    print(SEP)
    print("  DONE")
    print(SEP)
    return 0

if __name__ == "__main__":
    sys.exit(main())