"""
Part 3 driver: capture successful trace-flow against the default namespace
data that's already programmed to DPUs.
"""

from __future__ import annotations

import json
import sys
import urllib.error
import urllib.request

LEADER_REST = "http://127.0.0.1:28463"
NS = "default"

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
    banner("Inspect default namespace (already programmed to DPUs)")

    section("List the ENIs we'll trace against")
    http("GET", "/v1/default/enis")

    section("List the VnetMappings (overlay->underlay)")
    http("GET", "/v1/default/vnet-mappings")

    section("List the RoutePolicies")
    http("GET", "/v1/default/route-policies")

    banner("trace-flow against pre-programmed ENIs (default namespace)")

    section("OUTBOUND from eni-bank-web-04 to bank-prod-db (192.168.12.1)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-02",
            "flow": {
                "direction": 2,
                "eni_name": "eni-bank-web-04",
                "src_ip": "10.0.0.1",
                "dst_ip": "192.168.12.1",
                "src_port": 1024,
                "dst_port": 3306,
                "protocol": "tcp",
            },
        },
    )

    section("OUTBOUND from eni-bank-web-04 to public IP 8.8.8.8 (default route via service_tunnel)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-02",
            "flow": {
                "direction": 2,
                "eni_name": "eni-bank-web-04",
                "src_ip": "10.0.0.1",
                "dst_ip": "8.8.8.8",
                "src_port": 1024,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    section("INBOUND to eni-bank-web-04 on 443 (should ALLOW)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-02",
            "flow": {
                "direction": 1,
                "eni_name": "eni-bank-web-04",
                "src_ip": "203.0.113.10",
                "dst_ip": "192.168.11.4",
                "src_port": 12345,
                "dst_port": 443,
                "protocol": "tcp",
            },
        },
    )

    section("INBOUND to eni-bank-web-04 on 22 (should DROP by ACL)")
    http(
        "POST",
        "/v1/diagnostics/trace-flow",
        {
            "dpu_id": "dpu-sim-02",
            "flow": {
                "direction": 1,
                "eni_name": "eni-bank-web-04",
                "src_ip": "203.0.113.10",
                "dst_ip": "192.168.11.4",
                "src_port": 12345,
                "dst_port": 22,
                "protocol": "tcp",
            },
        },
    )

    section("explain-match ACL for the dropped 22")
    http(
        "POST",
        "/v1/diagnostics/explain-match",
        {
            "dpu_id": "dpu-sim-02",
            "subject": 1,
            "flow": {
                "direction": 1,
                "eni_name": "eni-bank-web-04",
                "src_ip": "203.0.113.10",
                "dst_ip": "192.168.11.4",
                "src_port": 12345,
                "dst_port": 22,
                "protocol": "tcp",
            },
        },
    )

    section("explain-match ROUTE for outbound 192.168.12.1")
    http(
        "POST",
        "/v1/diagnostics/explain-match",
        {
            "dpu_id": "dpu-sim-02",
            "subject": 2,
            "flow": {
                "direction": 2,
                "eni_name": "eni-bank-web-04",
                "src_ip": "10.0.0.1",
                "dst_ip": "192.168.12.1",
                "src_port": 1024,
                "dst_port": 3306,
                "protocol": "tcp",
            },
        },
    )

    print()
    print(SEP)
    print("  DONE")
    print(SEP)
    return 0

if __name__ == "__main__":
    sys.exit(main())