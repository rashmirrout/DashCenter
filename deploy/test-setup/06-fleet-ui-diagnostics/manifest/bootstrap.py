#!/usr/bin/env python3
"""
Bootstrap rich test data into a running DashCenter 06-fleet-ui-diagnostics stack.

Loads the full superset of resources matching the YAML manifests (00-10)
via direct REST API PUT calls — no dashctl dependency required.

Usage:
    python3 bootstrap.py                          # default: http://localhost:38443
    python3 bootstrap.py http://10.0.0.5:38443    # custom endpoint
"""
import json
import sys
import time
import urllib.request
import urllib.error

DASHD = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:38443"

# ─── Counters ──────────────────────────────────────────────────────
_ok = 0
_err = 0


def put(path, body):
    """PUT JSON to dashd REST API."""
    global _ok, _err
    url = f"{DASHD}{path}"
    data = json.dumps(body).encode()
    req = urllib.request.Request(
        url, data=data,
        headers={"Content-Type": "application/json"},
        method="PUT",
    )
    try:
        resp = urllib.request.urlopen(req)
        print(f"  ✓ PUT {path} → {resp.status}")
        _ok += 1
    except urllib.error.HTTPError as e:
        print(f"  ✗ PUT {path} → {e.code} {e.read().decode()[:120]}")
        _err += 1


def banner(title):
    print(f"\n{'═'*60}\n  {title}\n{'═'*60}")


# ─── Wait for dashd readiness ─────────────────────────────────────
print(f"Bootstrapping to {DASHD} …")
for attempt in range(1, 16):
    try:
        urllib.request.urlopen(f"{DASHD}/v1/default/vnets", timeout=3)
        break
    except Exception:
        print(f"  waiting for dashd ({attempt}/15)…")
        time.sleep(2)

# ═══════════════════════════════════════════════════════════════════
#  00 — VNets (14 default + 3 multi-namespace)
# ═══════════════════════════════════════════════════════════════════
banner("00 — VNets")

vnets_default = [
    ("bank-prod-web",     {"vni": 1001}),
    ("bank-prod-db",      {"vni": 1002}),
    ("retail-prod-web",   {"vni": 1101}),
    ("retail-prod-db",    {"vni": 1102}),
    ("media-stream",      {"vni": 1201}),
    ("media-control",     {"vni": 1202}),
    ("iot-edge",          {"vni": 1301}),
    ("iot-core",          {"vni": 1302}),
    ("analytics-spark",   {"vni": 1401}),
    ("analytics-kafka",   {"vni": 1402}),
    ("gaming-lobby",      {"vni": 1501}),
    ("gaming-match",      {"vni": 1502}),
    ("shared-ingress",    {"vni": 1901}),
    ("shared-egress",     {"vni": 1902}),
]
for name, spec in vnets_default:
    put(f"/v1/default/vnets/{name}", {"metadata": {"namespace": "default", "name": name}, **spec})

# Multi-namespace vnets
for ns, name, spec in [
    ("edge",    "cdn-pop",          {"vni": 2001}),
    ("edge",    "cdn-origin",       {"vni": 2002}),
    ("staging", "bank-staging-web", {"vni": 3001}),
]:
    put(f"/v1/{ns}/vnets/{name}", {"metadata": {"namespace": ns, "name": name}, **spec})

# ═══════════════════════════════════════════════════════════════════
#  01 — Service Tunnels (6)
# ═══════════════════════════════════════════════════════════════════
banner("01 — Service Tunnels")

tunnels = [
    ("st-internet-egress",     {"local_underlay_ip": "10.255.0.10", "remote_underlay_ip": "198.51.100.10", "vni": 8001, "params": {"action": "nat", "nat_pool": "203.0.113.0/26"}}),
    ("st-nsg-shared",          {"local_underlay_ip": "10.255.0.20", "remote_underlay_ip": "10.255.1.20",   "vni": 8002, "params": {"action": "inspect", "chain": "shared-nsg-default"}}),
    ("st-privatelink-azuredb", {"local_underlay_ip": "10.255.0.30", "remote_underlay_ip": "10.0.255.30",   "vni": 8003, "params": {"action": "privatelink", "target_fqdn": "db.privatelink.example.com"}}),
    ("st-vpn-corp",            {"local_underlay_ip": "10.255.0.40", "remote_underlay_ip": "192.0.2.40",    "vni": 8004, "params": {"action": "ipsec", "ike_group": "14"}}),
    ("st-ddos-scrub",          {"local_underlay_ip": "10.255.0.50", "remote_underlay_ip": "198.51.100.50", "vni": 8005, "params": {"action": "scrub", "provider": "cloudflare-sim"}}),
    ("st-cross-region",        {"local_underlay_ip": "10.255.0.60", "remote_underlay_ip": "10.128.0.60",   "vni": 8006, "params": {"action": "vxlan_peer", "remote_region": "us-west-2"}}),
]
for name, spec in tunnels:
    put(f"/v1/default/service-tunnels/{name}", {"metadata": {"namespace": "default", "name": name}, **spec})

# ═══════════════════════════════════════════════════════════════════
#  02 — ENIs (40 default + 5 multi-namespace)
# ═══════════════════════════════════════════════════════════════════
banner("02 — ENIs")

enis_default = [
    # bank-prod-web (4)
    ("eni-bank-web-01", "bank-prod-web", "aa:bb:cc:01:00:01", "10.0.1.11", "up", ["dpu-sim-01"]),
    ("eni-bank-web-02", "bank-prod-web", "aa:bb:cc:01:00:02", "10.0.1.12", "up", ["dpu-sim-01"]),
    ("eni-bank-web-03", "bank-prod-web", "aa:bb:cc:01:00:03", "10.0.1.13", "up", ["dpu-sim-02"]),
    ("eni-bank-web-04", "bank-prod-web", "aa:bb:cc:01:00:04", "10.0.1.14", "up", ["dpu-sim-02"]),
    # bank-prod-db (2)
    ("eni-bank-db-01", "bank-prod-db", "aa:bb:cc:02:00:01", "10.0.2.11", "up", ["dpu-sim-01"]),
    ("eni-bank-db-02", "bank-prod-db", "aa:bb:cc:02:00:02", "10.0.2.12", "up", ["dpu-sim-02"]),
    # retail-prod-web (4)
    ("eni-retail-web-01", "retail-prod-web", "aa:bb:cc:11:00:01", "10.1.1.11", "up", ["dpu-sim-03"]),
    ("eni-retail-web-02", "retail-prod-web", "aa:bb:cc:11:00:02", "10.1.1.12", "up", ["dpu-sim-03"]),
    ("eni-retail-web-03", "retail-prod-web", "aa:bb:cc:11:00:03", "10.1.1.13", "up", ["dpu-sim-04"]),
    ("eni-retail-web-04", "retail-prod-web", "aa:bb:cc:11:00:04", "10.1.1.14", "up", ["dpu-sim-04"]),
    # retail-prod-db (2)
    ("eni-retail-db-01", "retail-prod-db", "aa:bb:cc:12:00:01", "10.1.2.11", "up", ["dpu-sim-03"]),
    ("eni-retail-db-02", "retail-prod-db", "aa:bb:cc:12:00:02", "10.1.2.12", "up", ["dpu-sim-04"]),
    # media-stream (4)
    ("eni-media-stream-01", "media-stream", "aa:bb:cc:21:00:01", "10.2.1.11", "up", ["dpu-sim-02"]),
    ("eni-media-stream-02", "media-stream", "aa:bb:cc:21:00:02", "10.2.1.12", "up", ["dpu-sim-03"]),
    ("eni-media-stream-03", "media-stream", "aa:bb:cc:21:00:03", "10.2.1.13", "up", ["dpu-sim-04"]),
    ("eni-media-stream-04", "media-stream", "aa:bb:cc:21:00:04", "10.2.1.14", "up", ["dpu-sim-05"]),
    # media-control (1)
    ("eni-media-ctrl-01", "media-control", "aa:bb:cc:22:00:01", "10.2.2.11", "up", ["dpu-sim-05"]),
    # iot-edge (3)
    ("eni-iot-edge-01", "iot-edge", "aa:bb:cc:31:00:01", "10.3.1.11", "up", ["dpu-sim-06"]),
    ("eni-iot-edge-02", "iot-edge", "aa:bb:cc:31:00:02", "10.3.1.12", "up", ["dpu-sim-06"]),
    ("eni-iot-edge-03", "iot-edge", "aa:bb:cc:31:00:03", "10.3.1.13", "up", ["dpu-sim-06"]),
    # iot-core (2)
    ("eni-iot-core-01", "iot-core", "aa:bb:cc:32:00:01", "10.3.2.11", "up", ["dpu-sim-06"]),
    ("eni-iot-core-02", "iot-core", "aa:bb:cc:32:00:02", "10.3.2.12", "up", ["dpu-sim-07"]),
    # analytics-spark (3)
    ("eni-spark-01", "analytics-spark", "aa:bb:cc:41:00:01", "10.4.1.11", "up", ["dpu-sim-07"]),
    ("eni-spark-02", "analytics-spark", "aa:bb:cc:41:00:02", "10.4.1.12", "up", ["dpu-sim-07"]),
    ("eni-spark-03", "analytics-spark", "aa:bb:cc:41:00:03", "10.4.1.13", "up", ["dpu-sim-08"]),
    # analytics-kafka (2)
    ("eni-kafka-01", "analytics-kafka", "aa:bb:cc:42:00:01", "10.4.2.11", "up", ["dpu-sim-07"]),
    ("eni-kafka-02", "analytics-kafka", "aa:bb:cc:42:00:02", "10.4.2.12", "up", ["dpu-sim-08"]),
    # gaming-lobby (4)
    ("eni-gaming-lobby-01", "gaming-lobby", "aa:bb:cc:51:00:01", "10.5.1.11", "up", ["dpu-sim-08"]),
    ("eni-gaming-lobby-02", "gaming-lobby", "aa:bb:cc:51:00:02", "10.5.1.12", "up", ["dpu-sim-09"]),
    ("eni-gaming-lobby-03", "gaming-lobby", "aa:bb:cc:51:00:03", "10.5.1.13", "up", ["dpu-sim-09"]),
    ("eni-gaming-lobby-04", "gaming-lobby", "aa:bb:cc:51:00:04", "10.5.1.14", "up", ["dpu-sim-10"]),
    # gaming-match (4)
    ("eni-gaming-match-01", "gaming-match", "aa:bb:cc:52:00:01", "10.5.2.11", "up", ["dpu-sim-08"]),
    ("eni-gaming-match-02", "gaming-match", "aa:bb:cc:52:00:02", "10.5.2.12", "up", ["dpu-sim-09"]),
    ("eni-gaming-match-03", "gaming-match", "aa:bb:cc:52:00:03", "10.5.2.13", "up", ["dpu-sim-10"]),
    ("eni-gaming-match-04", "gaming-match", "aa:bb:cc:52:00:04", "10.5.2.14", "up", ["dpu-sim-10"]),
    # shared-ingress (2)
    ("eni-shared-ingress-01", "shared-ingress", "aa:bb:cc:91:00:01", "10.9.1.11", "up", ["dpu-sim-05"]),
    ("eni-shared-ingress-02", "shared-ingress", "aa:bb:cc:91:00:02", "10.9.1.12", "up", ["dpu-sim-05"]),
    # shared-egress (3)
    ("eni-shared-egress-01", "shared-egress", "aa:bb:cc:92:00:01", "10.9.2.11", "up", ["dpu-sim-01"]),
    ("eni-shared-egress-02", "shared-egress", "aa:bb:cc:92:00:02", "10.9.2.12", "up", ["dpu-sim-09"]),
    ("eni-shared-egress-03", "shared-egress", "aa:bb:cc:92:00:03", "10.9.2.13", "up", ["dpu-sim-10"]),
]
for name, vnet, mac, ip, state, dpus in enis_default:
    put(f"/v1/default/enis/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "vnet_name": vnet, "mac_address": mac, "underlay_ip": ip,
        "admin_state": state, "placement_hint_dpu_ids": dpus,
    })

# Multi-namespace ENIs
for ns, name, vnet, mac, ip, dpus in [
    ("edge", "edge-cdn-01",    "cdn-pop",    "aa:bb:cc:f0:00:01", "10.20.1.11", ["dpu-sim-04"]),
    ("edge", "edge-cdn-02",    "cdn-pop",    "aa:bb:cc:f0:00:02", "10.20.1.12", ["dpu-sim-05"]),
    ("edge", "edge-origin-01", "cdn-origin", "aa:bb:cc:f0:00:03", "10.20.2.11", ["dpu-sim-04"]),
    ("staging", "eni-bank-stg-01", "bank-staging-web", "aa:bb:cc:e0:00:01", "10.30.1.11", ["dpu-sim-06"]),
    ("staging", "eni-bank-stg-02", "bank-staging-web", "aa:bb:cc:e0:00:02", "10.30.1.12", ["dpu-sim-07"]),
]:
    put(f"/v1/{ns}/enis/{name}", {
        "metadata": {"namespace": ns, "name": name},
        "vnet_name": vnet, "mac_address": mac, "underlay_ip": ip,
        "admin_state": "up", "placement_hint_dpu_ids": dpus,
    })

# ═══════════════════════════════════════════════════════════════════
#  03 — VnetMappings (40 default + 5 multi-namespace)
# ═══════════════════════════════════════════════════════════════════
banner("03 — VnetMappings")

mappings = [
    # bank
    ("map-bank-web-01", "bank-prod-web", "192.168.11.1", "10.0.1.11", "aa:bb:cc:01:00:01", "vnet_encap"),
    ("map-bank-web-02", "bank-prod-web", "192.168.11.2", "10.0.1.12", "aa:bb:cc:01:00:02", "vnet_encap"),
    ("map-bank-web-03", "bank-prod-web", "192.168.11.3", "10.0.1.13", "aa:bb:cc:01:00:03", "vnet_encap"),
    ("map-bank-web-04", "bank-prod-web", "192.168.11.4", "10.0.1.14", "aa:bb:cc:01:00:04", "vnet_encap"),
    ("map-bank-db-01",  "bank-prod-db",  "192.168.12.1", "10.0.2.11", "aa:bb:cc:02:00:01", "vnet_encap"),
    ("map-bank-db-02",  "bank-prod-db",  "192.168.12.2", "10.0.2.12", "aa:bb:cc:02:00:02", "vnet_encap"),
    # retail
    ("map-retail-web-01", "retail-prod-web", "192.168.21.1", "10.1.1.11", "aa:bb:cc:11:00:01", "vnet_encap"),
    ("map-retail-web-02", "retail-prod-web", "192.168.21.2", "10.1.1.12", "aa:bb:cc:11:00:02", "vnet_encap"),
    ("map-retail-web-03", "retail-prod-web", "192.168.21.3", "10.1.1.13", "aa:bb:cc:11:00:03", "vnet_encap"),
    ("map-retail-web-04", "retail-prod-web", "192.168.21.4", "10.1.1.14", "aa:bb:cc:11:00:04", "vnet_encap"),
    ("map-retail-db-01",  "retail-prod-db",  "192.168.22.1", "10.1.2.11", "aa:bb:cc:12:00:01", "vnet_encap"),
    ("map-retail-db-02",  "retail-prod-db",  "192.168.22.2", "10.1.2.12", "aa:bb:cc:12:00:02", "vnet_encap"),
    # media
    ("map-media-stream-01", "media-stream",  "192.168.31.1", "10.2.1.11", "aa:bb:cc:21:00:01", "vnet_encap"),
    ("map-media-stream-02", "media-stream",  "192.168.31.2", "10.2.1.12", "aa:bb:cc:21:00:02", "vnet_encap"),
    ("map-media-stream-03", "media-stream",  "192.168.31.3", "10.2.1.13", "aa:bb:cc:21:00:03", "vnet_encap"),
    ("map-media-stream-04", "media-stream",  "192.168.31.4", "10.2.1.14", "aa:bb:cc:21:00:04", "vnet_encap"),
    ("map-media-ctrl-01",   "media-control", "192.168.32.1", "10.2.2.11", "aa:bb:cc:22:00:01", "vnet_encap"),
    # iot
    ("map-iot-edge-01", "iot-edge", "192.168.41.1", "10.3.1.11", "aa:bb:cc:31:00:01", "vnet_encap"),
    ("map-iot-edge-02", "iot-edge", "192.168.41.2", "10.3.1.12", "aa:bb:cc:31:00:02", "vnet_encap"),
    ("map-iot-edge-03", "iot-edge", "192.168.41.3", "10.3.1.13", "aa:bb:cc:31:00:03", "vnet_encap"),
    ("map-iot-core-01", "iot-core", "192.168.42.1", "10.3.2.11", "aa:bb:cc:32:00:01", "vnet_encap"),
    ("map-iot-core-02", "iot-core", "192.168.42.2", "10.3.2.12", "aa:bb:cc:32:00:02", "vnet_encap"),
    # analytics
    ("map-spark-01", "analytics-spark", "192.168.51.1", "10.4.1.11", "aa:bb:cc:41:00:01", "vnet_encap"),
    ("map-spark-02", "analytics-spark", "192.168.51.2", "10.4.1.12", "aa:bb:cc:41:00:02", "vnet_encap"),
    ("map-spark-03", "analytics-spark", "192.168.51.3", "10.4.1.13", "aa:bb:cc:41:00:03", "vnet_encap"),
    ("map-kafka-01", "analytics-kafka", "192.168.52.1", "10.4.2.11", "aa:bb:cc:42:00:01", "vnet_encap"),
    ("map-kafka-02", "analytics-kafka", "192.168.52.2", "10.4.2.12", "aa:bb:cc:42:00:02", "vnet_encap"),
    # gaming
    ("map-gaming-lobby-01", "gaming-lobby", "192.168.61.1", "10.5.1.11", "aa:bb:cc:51:00:01", "vnet_encap"),
    ("map-gaming-lobby-02", "gaming-lobby", "192.168.61.2", "10.5.1.12", "aa:bb:cc:51:00:02", "vnet_encap"),
    ("map-gaming-lobby-03", "gaming-lobby", "192.168.61.3", "10.5.1.13", "aa:bb:cc:51:00:03", "vnet_encap"),
    ("map-gaming-lobby-04", "gaming-lobby", "192.168.61.4", "10.5.1.14", "aa:bb:cc:51:00:04", "vnet_encap"),
    ("map-gaming-match-01", "gaming-match", "192.168.62.1", "10.5.2.11", "aa:bb:cc:52:00:01", "vnet_encap"),
    ("map-gaming-match-02", "gaming-match", "192.168.62.2", "10.5.2.12", "aa:bb:cc:52:00:02", "vnet_encap"),
    ("map-gaming-match-03", "gaming-match", "192.168.62.3", "10.5.2.13", "aa:bb:cc:52:00:03", "vnet_encap"),
    ("map-gaming-match-04", "gaming-match", "192.168.62.4", "10.5.2.14", "aa:bb:cc:52:00:04", "vnet_encap"),
    # shared (some with service_tunnel action)
    ("map-shared-ingress-01", "shared-ingress", "192.168.91.1", "10.9.1.11", "aa:bb:cc:91:00:01", "service_tunnel"),
    ("map-shared-ingress-02", "shared-ingress", "192.168.91.2", "10.9.1.12", "aa:bb:cc:91:00:02", "vnet_encap"),
    ("map-shared-egress-01",  "shared-egress",  "192.168.92.1", "10.9.2.11", "aa:bb:cc:92:00:01", "service_tunnel"),
    ("map-shared-egress-02",  "shared-egress",  "192.168.92.2", "10.9.2.12", "aa:bb:cc:92:00:02", "service_tunnel"),
    ("map-shared-egress-03",  "shared-egress",  "192.168.92.3", "10.9.2.13", "aa:bb:cc:92:00:03", "vnet_encap"),
]
for name, vnet, ip, underlay, mac, action in mappings:
    put(f"/v1/default/vnet-mappings/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "vnet_name": vnet, "ip_address": ip, "underlay_ip": underlay,
        "mac_address": mac, "action": action,
    })

# Multi-namespace mappings
for ns, name, vnet, ip, underlay, mac in [
    ("edge", "map-edge-pop-11",    "cdn-pop",    "192.168.201.11", "10.20.1.11", "aa:bb:cc:f0:00:01"),
    ("edge", "map-edge-pop-12",    "cdn-pop",    "192.168.201.12", "10.20.1.12", "aa:bb:cc:f0:00:02"),
    ("edge", "map-edge-origin-11", "cdn-origin", "192.168.202.11", "10.20.2.11", "aa:bb:cc:f0:00:03"),
    ("staging", "map-bank-stg-01", "bank-staging-web", "192.168.131.1", "10.30.1.11", "aa:bb:cc:e0:00:01"),
    ("staging", "map-bank-stg-02", "bank-staging-web", "192.168.131.2", "10.30.1.12", "aa:bb:cc:e0:00:02"),
]:
    put(f"/v1/{ns}/vnet-mappings/{name}", {
        "metadata": {"namespace": ns, "name": name},
        "vnet_name": vnet, "ip_address": ip, "underlay_ip": underlay,
        "mac_address": mac, "action": "vnet_encap",
    })

# ═══════════════════════════════════════════════════════════════════
#  04 — Route Policies (15 default + 2 multi-namespace)
# ═══════════════════════════════════════════════════════════════════
banner("04 — Route Policies")

route_policies = [
    ("rp-bank-web-default",  ["eni-bank-web-01","eni-bank-web-02","eni-bank-web-03","eni-bank-web-04"],
     [{"prefix":"192.168.11.0/24","next_hop_type":"vnet","next_hop_target":"bank-prod-web","metric":10},
      {"prefix":"192.168.12.0/24","next_hop_type":"vnet","next_hop_target":"bank-prod-db","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","metric":100}]),
    ("rp-bank-db-default",   ["eni-bank-db-01","eni-bank-db-02"],
     [{"prefix":"192.168.12.0/24","next_hop_type":"vnet","next_hop_target":"bank-prod-db","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-retail-web-default", ["eni-retail-web-01","eni-retail-web-02","eni-retail-web-03","eni-retail-web-04"],
     [{"prefix":"192.168.21.0/24","next_hop_type":"vnet","next_hop_target":"retail-prod-web","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","metric":100}]),
    ("rp-retail-db-default",  ["eni-retail-db-01","eni-retail-db-02"],
     [{"prefix":"192.168.22.0/24","next_hop_type":"vnet","next_hop_target":"retail-prod-db","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-media-stream-ecmp",  ["eni-media-stream-01","eni-media-stream-02","eni-media-stream-03","eni-media-stream-04"],
     [{"prefix":"192.168.31.0/24","next_hop_type":"vnet","next_hop_target":"media-stream","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","metric":100}]),
    ("rp-media-control",      ["eni-media-ctrl-01"],
     [{"prefix":"192.168.32.0/24","next_hop_type":"vnet","next_hop_target":"media-control","metric":10}]),
    ("rp-iot-edge-uplink",    ["eni-iot-edge-01","eni-iot-edge-02","eni-iot-edge-03"],
     [{"prefix":"192.168.41.0/24","next_hop_type":"vnet","next_hop_target":"iot-edge","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","metric":100}]),
    ("rp-iot-core-default",   ["eni-iot-core-01","eni-iot-core-02"],
     [{"prefix":"192.168.42.0/24","next_hop_type":"vnet","next_hop_target":"iot-core","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-spark-compute",      ["eni-spark-01","eni-spark-02","eni-spark-03"],
     [{"prefix":"192.168.51.0/24","next_hop_type":"vnet","next_hop_target":"analytics-spark","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-kafka-bus",          ["eni-kafka-01","eni-kafka-02"],
     [{"prefix":"192.168.52.0/24","next_hop_type":"vnet","next_hop_target":"analytics-kafka","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-shared-ingress",     ["eni-shared-ingress-01","eni-shared-ingress-02"],
     [{"prefix":"192.168.0.0/16","next_hop_type":"vnet","next_hop_target":"shared-ingress","metric":10}]),
    ("rp-shared-egress",      ["eni-shared-egress-01","eni-shared-egress-02","eni-shared-egress-03"],
     [{"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","metric":10}]),
    ("rp-gaming-lobby",       ["eni-gaming-lobby-01","eni-gaming-lobby-02","eni-gaming-lobby-03","eni-gaming-lobby-04"],
     [{"prefix":"192.168.61.0/24","next_hop_type":"vnet","next_hop_target":"gaming-lobby","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel","next_hop_target":"st-ddos-scrub","metric":100}]),
    ("rp-gaming-match",       ["eni-gaming-match-01","eni-gaming-match-02","eni-gaming-match-03","eni-gaming-match-04"],
     [{"prefix":"192.168.62.0/24","next_hop_type":"vnet","next_hop_target":"gaming-match","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("rp-gaming-geo-lb",      ["eni-gaming-lobby-01","eni-gaming-lobby-02"],
     [{"prefix":"10.200.0.0/16","next_hop_type":"vnet","next_hop_target":"gaming-lobby","metric":15}]),
]
for name, enis, routes in route_policies:
    put(f"/v1/default/route-policies/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "eni_names": enis, "routes": routes,
    })

# Multi-namespace route policies
for ns, name, enis, routes in [
    ("edge", "rp-edge-cdn-default", ["edge-cdn-01","edge-cdn-02"],
     [{"prefix":"192.168.201.0/24","next_hop_type":"vnet","next_hop_target":"cdn-pop","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
    ("staging", "rp-bank-stg-default", ["eni-bank-stg-01","eni-bank-stg-02"],
     [{"prefix":"192.168.131.0/24","next_hop_type":"vnet","next_hop_target":"bank-staging-web","metric":10},
      {"prefix":"0.0.0.0/0","next_hop_type":"drop","metric":1000}]),
]:
    put(f"/v1/{ns}/route-policies/{name}", {
        "metadata": {"namespace": ns, "name": name},
        "eni_names": enis, "routes": routes,
    })

# ═══════════════════════════════════════════════════════════════════
#  05 — ACL Policies (15 default + 2 multi-namespace — representative)
# ═══════════════════════════════════════════════════════════════════
banner("05 — ACL Policies")

acl_policies = [
    ("acl-bank-web-inbound",      "inbound",  ["eni-bank-web-01","eni-bank-web-02","eni-bank-web-03","eni-bank-web-04"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-bank-web-outbound",     "outbound", ["eni-bank-web-01","eni-bank-web-02","eni-bank-web-03","eni-bank-web-04"],
     [{"priority":100,"action":"allow","dst_prefixes":["192.168.12.0/24"],"dst_ports":["3306","5432"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","dst_prefixes":["0.0.0.0/0"]}]),
    ("acl-bank-db-inbound",       "inbound",  ["eni-bank-db-01","eni-bank-db-02"],
     [{"priority":100,"action":"allow","src_prefixes":["192.168.11.0/24"],"dst_ports":["3306"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-retail-web-inbound",    "inbound",  ["eni-retail-web-01","eni-retail-web-02","eni-retail-web-03","eni-retail-web-04"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-retail-web-outbound",   "outbound", ["eni-retail-web-01","eni-retail-web-02","eni-retail-web-03","eni-retail-web-04"],
     [{"priority":100,"action":"allow","dst_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","dst_prefixes":["0.0.0.0/0"]}]),
    ("acl-retail-db-inbound",     "inbound",  ["eni-retail-db-01","eni-retail-db-02"],
     [{"priority":100,"action":"allow","src_prefixes":["192.168.21.0/24"],"dst_ports":["1521"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-media-stream-inbound",  "inbound",  ["eni-media-stream-01","eni-media-stream-02","eni-media-stream-03","eni-media-stream-04"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["1935"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-media-stream-outbound", "outbound", ["eni-media-stream-01","eni-media-stream-02","eni-media-stream-03","eni-media-stream-04"],
     [{"priority":100,"action":"allow","dst_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","dst_prefixes":["0.0.0.0/0"]}]),
    ("acl-iot-edge-inbound",      "inbound",  ["eni-iot-edge-01","eni-iot-edge-02","eni-iot-edge-03"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["8883"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-iot-core-inbound",      "inbound",  ["eni-iot-core-01","eni-iot-core-02"],
     [{"priority":100,"action":"allow","src_prefixes":["192.168.41.0/24"],"dst_ports":["8883"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-spark-inbound",         "inbound",  ["eni-spark-01","eni-spark-02","eni-spark-03"],
     [{"priority":100,"action":"allow","src_prefixes":["192.168.52.0/24"],"dst_ports":["9092"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-shared-ingress-inbound","inbound",  ["eni-shared-ingress-01","eni-shared-ingress-02"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["80","443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-gaming-lobby-inbound",  "inbound",  ["eni-gaming-lobby-01","eni-gaming-lobby-02","eni-gaming-lobby-03","eni-gaming-lobby-04"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-gaming-match-inbound",  "inbound",  ["eni-gaming-match-01","eni-gaming-match-02","eni-gaming-match-03","eni-gaming-match-04"],
     [{"priority":100,"action":"allow","src_prefixes":["192.168.61.0/24"],"dst_ports":["7777-7800"],"protocols":["udp"]},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"]}]),
    ("acl-gaming-outbound",       "outbound", ["eni-gaming-lobby-01","eni-gaming-lobby-02","eni-gaming-match-01","eni-gaming-match-02"],
     [{"priority":100,"action":"allow","dst_prefixes":["192.168.61.0/24"]},
      {"priority":110,"action":"allow","dst_prefixes":["192.168.62.0/24"]},
      {"priority":1000,"action":"deny","dst_prefixes":["0.0.0.0/0"]}]),
]
for name, stage, enis, rules in acl_policies:
    put(f"/v1/default/acl-policies/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "stage": stage, "eni_names": enis, "rules": rules,
    })

# Multi-namespace ACL policies (edge + staging)
for ns, name, stage, enis, rules in [
    ("edge", "acl-edge-cdn-inbound", "inbound", ["edge-cdn-01","edge-cdn-02"],
     [{"priority":100,"action":"allow","src_prefixes":["0.0.0.0/0"],"dst_ports":["443"],"protocols":["tcp"],"description":"public https"},
      {"priority":110,"action":"allow_and_continue","src_prefixes":["10.255.0.0/16"],"protocols":["icmp"],"description":"ops icmp continues"},
      {"priority":120,"action":"allow","src_prefixes":["10.20.2.0/24"],"dst_ports":["6379"],"protocols":["tcp"],"description":"redis cache fetch"},
      {"priority":130,"action":"deny","src_prefixes":["0.0.0.0/0"],"dst_ports":["22","23","3389"],"protocols":["tcp"],"description":"no remote shell"},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"],"description":"catch-all"}]),
    ("staging", "acl-bank-stg-inbound", "inbound", ["eni-bank-stg-01","eni-bank-stg-02"],
     [{"priority":100,"action":"allow","src_prefixes":["10.0.0.0/8"],"dst_ports":["443","80"],"protocols":["tcp"],"description":"internal only"},
      {"priority":110,"action":"allow","src_prefixes":["10.255.0.0/16"],"protocols":["icmp"],"description":"ops icmp"},
      {"priority":120,"action":"deny","src_prefixes":["0.0.0.0/0"],"dst_ports":["22","23","3389"],"protocols":["tcp"],"description":"no remote shell"},
      {"priority":1000,"action":"deny","src_prefixes":["0.0.0.0/0"],"description":"catch-all"}]),
]:
    put(f"/v1/{ns}/acl-policies/{name}", {
        "metadata": {"namespace": ns, "name": name},
        "stage": stage, "eni_names": enis, "rules": rules,
    })

# ═══════════════════════════════════════════════════════════════════
#  06 — HA Sets (4)
# ═══════════════════════════════════════════════════════════════════
banner("06 — HA Sets")

ha_sets = [
    ("ha-bank-prod",       "active_standby", ["dpu-sim-01","dpu-sim-02"], "10.0.0.100"),
    ("ha-retail-prod",     "active_standby", ["dpu-sim-03","dpu-sim-04"], "10.1.0.100"),
    ("ha-shared-services", "active_active",  ["dpu-sim-05","dpu-sim-01"], "10.9.0.100"),
    ("ha-gaming-crossrack","active_standby", ["dpu-sim-09","dpu-sim-10"], "10.5.0.100"),
]
for name, mode, dpus, vip in ha_sets:
    put(f"/v1/default/ha/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "mode": mode, "member_dpu_ids": dpus, "virtual_ip": vip,
        "flow_sync_endpoints": [f"udp://{d}:4789" for d in dpus],
    })

# ═══════════════════════════════════════════════════════════════════
#  07 — Disabled + Resimulate ENIs (quarantine + flow re-derive)
# ═══════════════════════════════════════════════════════════════════
banner("07 — Disabled / Resimulate ENIs")

# Quarantine ENI: admin_state=down, isolated for incident response
put("/v1/default/enis/eni-quarantine-01", {
    "metadata": {"namespace": "default", "name": "eni-quarantine-01",
                 "labels": {"tenant": "analytics", "tier": "compute", "quarantine": "true"}},
    "vnet_name": "analytics-spark",
    "mac_address": "aa:bb:cc:41:0f:01",
    "underlay_ip": "10.4.1.99",
    "admin_state": "down",
    "placement_hint_dpu_ids": ["dpu-sim-08"],
    "resimulate_flows": False,
})

# Re-PUT eni-bank-web-04 with resimulate_flows=true → bumps generation, forces flow rebuild
put("/v1/default/enis/eni-bank-web-04", {
    "metadata": {"namespace": "default", "name": "eni-bank-web-04",
                 "labels": {"tenant": "bank", "tier": "web", "resimulated": "true"}},
    "vnet_name": "bank-prod-web",
    "mac_address": "aa:bb:cc:01:00:04",
    "underlay_ip": "10.0.1.14",
    "admin_state": "up",
    "placement_hint_dpu_ids": ["dpu-sim-02"],
    "resimulate_flows": True,
})

# ═══════════════════════════════════════════════════════════════════
#  08 — Advanced ACL chains (numeric protos, port ranges, src_port)
# ═══════════════════════════════════════════════════════════════════
banner("08 — Advanced ACL chains")

advanced_acls = [
    # Platform prom allow chain — runs before tenant ACLs
    ("acl-platform-prom-allow", "inbound",
     ["eni-bank-web-01","eni-bank-web-02","eni-retail-web-01","eni-retail-web-02",
      "eni-media-stream-01","eni-iot-edge-01","eni-gaming-lobby-01"],
     [{"priority":1,"action":"allow_and_continue","src_prefixes":["10.255.0.0/16"],
       "dst_ports":["9100","9200","9090"],"protocols":["6"],
       "description":"prom scrape (numeric proto)"},
      {"priority":2,"action":"allow_and_continue","src_prefixes":["10.255.0.0/16"],
       "protocols":["1","58"],"description":"icmp + icmpv6 from ops"},
      {"priority":10,"action":"deny","src_prefixes":["0.0.0.0/0"],
       "dst_ports":["111","2049"],"protocols":["tcp","udp"],
       "description":"block NFS at platform"}]),
    # IoT rate-limit hint chain
    ("acl-iot-edge-rate-limit", "inbound",
     ["eni-iot-edge-01","eni-iot-edge-02","eni-iot-edge-03"],
     [{"priority":3,"action":"allow_and_continue","src_prefixes":["0.0.0.0/0"],
       "dst_ports":["1024-65535"],"protocols":["tcp","udp"],
       "description":"ephemeral ports — log + continue"},
      {"priority":4,"action":"deny","src_prefixes":["0.0.0.0/0"],
       "dst_ports":["1900","5353","11211"],"protocols":["udp"],
       "description":"amplification ports — platform deny"}]),
    # Egress platform policy with src_port matching
    ("acl-platform-egress-tag", "outbound",
     ["eni-bank-web-01","eni-retail-web-01","eni-media-stream-01","eni-gaming-lobby-01"],
     [{"priority":1,"action":"allow_and_continue","dst_prefixes":["10.255.0.0/16"],
       "src_ports":["32768-60999"],"protocols":["tcp"],
       "description":"egress to ops (ephemeral src range)"},
      {"priority":2,"action":"allow_and_continue","dst_prefixes":["0.0.0.0/0"],
       "dst_ports":["53"],"protocols":["17"],
       "description":"DNS (numeric proto)"}]),
]
for name, stage, enis, rules in advanced_acls:
    put(f"/v1/default/acl-policies/{name}", {
        "metadata": {"namespace": "default", "name": name},
        "stage": stage, "eni_names": enis, "rules": rules,
    })

# ═══════════════════════════════════════════════════════════════════
#  09 — Advanced Routes (3-way ECMP + gaming blackhole)
# ═══════════════════════════════════════════════════════════════════
banner("09 — Advanced Routes")

# 3-way ECMP across NAT / cross-region / DDoS scrub
put("/v1/default/route-policies/rp-shared-egress-ecmp", {
    "metadata": {"namespace": "default", "name": "rp-shared-egress-ecmp",
                 "labels": {"tenant": "shared", "class": "ecmp"}},
    "eni_names": ["eni-shared-egress-01","eni-shared-egress-02","eni-shared-egress-03"],
    "routes": [{
        "prefix": "198.51.100.0/24",
        "ecmp_members": [
            {"next_hop_type":"service_tunnel","next_hop_target":"st-internet-egress","weight":50},
            {"next_hop_type":"service_tunnel","next_hop_target":"st-cross-region","weight":30},
            {"next_hop_type":"service_tunnel","next_hop_target":"st-ddos-scrub","weight":20},
        ],
        "metric": 15,
    }],
})

# Gaming blackhole + fallback via scrub
put("/v1/default/route-policies/rp-gaming-blackhole-fallback", {
    "metadata": {"namespace": "default", "name": "rp-gaming-blackhole-fallback",
                 "labels": {"tenant": "gaming", "class": "failsafe"}},
    "eni_names": ["eni-gaming-lobby-03","eni-gaming-lobby-04"],
    "routes": [
        {"prefix":"198.18.0.0/15","next_hop_type":"drop","metric":5,
         "description":"benchmark range — always drop"},
        {"prefix":"203.0.113.0/24","next_hop_type":"drop","metric":5,
         "description":"doc range — always drop"},
        {"prefix":"0.0.0.0/0","next_hop_type":"service_tunnel",
         "next_hop_target":"st-ddos-scrub","metric":200,
         "description":"fallback via scrub"},
    ],
})

# ═══════════════════════════════════════════════════════════════════
#  11 — Diagnostics fixtures (06 scenario only)
#  Layered on top of 00–10 for DiagnosticsService Lab 13. See
#  manifest/11-diagnostics-fixtures.yaml for the dashctl-style envelope.
# ═══════════════════════════════════════════════════════════════════
banner("11 — Diagnostics fixtures")

# Multi-stage ACL chain on eni-bank-web-01: drives explain-match SUBJECT_ACL.
put("/v1/default/acl-policies/acl-diag-chain", {
    "metadata": {"namespace": "default", "name": "acl-diag-chain",
                 "labels": {"tenant": "diag", "purpose": "explain-match-demo"}},
    "stage": "inbound", "eni_names": ["eni-bank-web-01"],
    "rules": [
        {"priority":  50, "action": "allow_and_continue", "src_prefixes": ["10.255.0.0/16"],
         "description": "step 1: monitoring path opens, then chain continues"},
        {"priority": 100, "action": "allow", "src_prefixes": ["0.0.0.0/0"],
         "dst_ports": ["443"], "protocols": ["tcp"],
         "description": "step 2: standard https admit"},
        {"priority": 150, "action": "deny", "src_prefixes": ["0.0.0.0/0"],
         "dst_ports": ["22","23"], "protocols": ["tcp"],
         "description": "step 3: hard block remote shell"},
        {"priority": 200, "action": "allow", "src_prefixes": ["198.51.100.0/24"],
         "protocols": ["icmp"],
         "description": "step 4: scoped ICMP from probe network"},
        {"priority": 250, "action": "deny", "src_prefixes": ["0.0.0.0/0"],
         "protocols": ["icmp"], "description": "step 5: no other ICMP"},
        {"priority": 999, "action": "deny", "src_prefixes": ["0.0.0.0/0"],
         "description": "step 6: catch-all"},
    ],
})

# Dead-rule ACL on eni-bank-web-02: every rule covers TEST-NET ranges
# the lab will never traverse, so acl-hit-stats {"zero_hits_only":true}
# returns them all.
put("/v1/default/acl-policies/acl-diag-dead-rules", {
    "metadata": {"namespace": "default", "name": "acl-diag-dead-rules",
                 "labels": {"tenant": "diag", "purpose": "dead-rule-demo"}},
    "stage": "outbound", "eni_names": ["eni-bank-web-02"],
    "rules": [
        {"priority": 100, "action": "allow", "dst_prefixes": ["192.0.2.0/24"],
         "dst_ports": ["8080"], "protocols": ["tcp"],
         "description": "dead: TEST-NET-1 range, never reached"},
        {"priority": 110, "action": "allow", "dst_prefixes": ["198.51.100.0/24"],
         "dst_ports": ["8443"], "protocols": ["tcp"],
         "description": "dead: TEST-NET-2 range, never reached"},
        {"priority": 120, "action": "allow", "dst_prefixes": ["203.0.113.0/24"],
         "dst_ports": ["9001"], "protocols": ["tcp"],
         "description": "dead: TEST-NET-3 range, never reached"},
        {"priority": 130, "action": "deny", "dst_prefixes": ["240.0.0.0/4"],
         "protocols": ["tcp"],
         "description": "dead: reserved class-E, no traffic possible"},
        {"priority": 999, "action": "deny", "dst_prefixes": ["0.0.0.0/0"],
         "description": "catch-all (will also be zero-hit in lab)"},
    ],
})

# Overlap-route policy on eni-bank-web-01: drives explain-match SUBJECT_ROUTE.
# Four prefixes (0/0, /8, /24, /32) across all four next-hop types so a
# single trace can showcase longest-prefix + metric tie-break.
put("/v1/default/route-policies/rp-diag-overlap", {
    "metadata": {"namespace": "default", "name": "rp-diag-overlap",
                 "labels": {"tenant": "diag", "purpose": "longest-prefix-demo"}},
    "eni_names": ["eni-bank-web-01"],
    "routes": [
        {"prefix": "0.0.0.0/0",    "next_hop_type": "drop", "metric": 1000,
         "description": "default deny — last resort"},
        {"prefix": "10.0.0.0/8",   "next_hop_type": "vnet",
         "next_hop_target": "bank-prod-web", "metric": 100,
         "description": "broad: any rfc1918 10/8 → bank-web"},
        {"prefix": "10.0.1.0/24",  "next_hop_type": "vnet",
         "next_hop_target": "bank-prod-db",  "metric": 50,
         "description": "narrower: db-subnet → bank-db"},
        {"prefix": "10.0.1.10/32", "next_hop_type": "service_tunnel",
         "next_hop_target": "st-internet-egress", "metric": 10,
         "description": "most specific: scrubbed via internet-egress tunnel"},
    ],
})

# ═══════════════════════════════════════════════════════════════════
#  Done
# ═══════════════════════════════════════════════════════════════════
print(f"\n{'═'*60}")
print(f"  Bootstrap complete!  ✓ {_ok} succeeded   ✗ {_err} failed")
print(f"  Resources: 17 vnets, 46 ENIs, 45 mappings,")
print(f"             19 route policies, 22 ACL policies,")
print(f"             6 tunnels, 4 HA sets")
print(f"  Namespaces: default, edge, staging")
print(f"  Special:   1 quarantined ENI (admin_state=down),")
print(f"             1 resimulate-bumped ENI (gen=2),")
print(f"             3 diagnostics-lab fixtures (acl-diag-chain,")
print(f"               acl-diag-dead-rules, rp-diag-overlap)")
print(f"{'═'*60}")
print(f"\n  Open http://localhost:3001 to explore the console.")
print(f"  See manual-handson.md Lab 13 for the DiagnosticsService deep dive.")
