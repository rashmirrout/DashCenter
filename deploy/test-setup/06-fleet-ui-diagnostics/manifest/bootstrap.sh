#!/bin/sh
# Bootstrap test data into the DashCenter stack
# Usage: ./bootstrap.sh [DASHD_URL]
# Default: http://localhost:38443 (dashd-1 exposed port)

DASHD="${1:-http://localhost:38443}"
NS="default"
echo "Bootstrapping test data to $DASHD ..."

# ── Vnets ──
for f in vnet-prod vnet-staging vnet-dev; do
  VNI=$((100 + $(echo $f | grep -c staging)*100 + $(echo $f | grep -c dev)*200))
  case $f in
    vnet-prod)    VNI=100; CIDR="10.0.0.0/16"; MAC="00:00:00:00:01:00" ;;
    vnet-staging) VNI=200; CIDR="10.1.0.0/16"; MAC="00:00:00:00:02:00" ;;
    vnet-dev)     VNI=300; CIDR="10.2.0.0/16"; MAC="00:00:00:00:03:00" ;;
  esac
  echo "  PUT vnet/$f (vni=$VNI)"
  curl -s -X PUT "$DASHD/v1/$NS/vnets/$f" \
    -H "Content-Type: application/json" \
    -d "{\"metadata\":{\"namespace\":\"$NS\",\"name\":\"$f\"},\"vni\":$VNI,\"address_space\":[\"$CIDR\"],\"gw_mac\":\"$MAC\"}" \
    -o /dev/null -w "  -> %{http_code}\n"
done

# ── ENIs ──
create_eni() {
  NAME=$1; VNET=$2; MAC=$3; UIP=$4; PIP=$5
  echo "  PUT eni/$NAME"
  curl -s -X PUT "$DASHD/v1/$NS/enis/$NAME" \
    -H "Content-Type: application/json" \
    -d "{\"metadata\":{\"namespace\":\"$NS\",\"name\":\"$NAME\"},\"vnet_name\":\"$VNET\",\"mac_address\":\"$MAC\",\"underlay_ip\":\"$UIP\",\"admin_state\":\"ENABLED\",\"primary_ip\":\"$PIP\"}" \
    -o /dev/null -w "  -> %{http_code}\n"
}
create_eni eni-prod-01 vnet-prod "00:11:22:33:44:01" "192.168.1.1" "10.0.1.10"
create_eni eni-prod-02 vnet-prod "00:11:22:33:44:02" "192.168.1.2" "10.0.1.20"
create_eni eni-prod-03 vnet-prod "00:11:22:33:44:03" "192.168.1.3" "10.0.1.30"
create_eni eni-staging-01 vnet-staging "00:11:22:33:55:01" "192.168.2.1" "10.1.1.10"
create_eni eni-staging-02 vnet-staging "00:11:22:33:55:02" "192.168.2.2" "10.1.1.20"
create_eni eni-dev-01 vnet-dev "00:11:22:33:66:01" "192.168.3.1" "10.2.1.10"

# ── ACL Policies ──
echo "  PUT acl-policy/acl-prod-web"
curl -s -X PUT "$DASHD/v1/$NS/acl-policies/acl-prod-web" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"namespace":"default","name":"acl-prod-web"},"eni_names":["eni-prod-01","eni-prod-02"],"default_action":"DENY","rules":[{"priority":100,"action":"ALLOW","direction":"IN","protocol":6,"dst_port_range":{"start":443,"end":443},"src_prefixes":["0.0.0.0/0"]},{"priority":200,"action":"ALLOW","direction":"OUT","protocol":6,"src_prefixes":["10.0.0.0/16"]}]}' \
  -o /dev/null -w "  -> %{http_code}\n"

echo "  PUT acl-policy/acl-staging-all"
curl -s -X PUT "$DASHD/v1/$NS/acl-policies/acl-staging-all" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"namespace":"default","name":"acl-staging-all"},"eni_names":["eni-staging-01","eni-staging-02"],"default_action":"ALLOW","rules":[{"priority":100,"action":"DENY","direction":"IN","protocol":6,"dst_port_range":{"start":22,"end":22},"src_prefixes":["0.0.0.0/0"]}]}' \
  -o /dev/null -w "  -> %{http_code}\n"

# ── Route Policies ──
echo "  PUT route-policy/rp-prod-default"
curl -s -X PUT "$DASHD/v1/$NS/route-policies/rp-prod-default" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"namespace":"default","name":"rp-prod-default"},"eni_names":["eni-prod-01","eni-prod-02","eni-prod-03"],"direction":"OUT","rules":[{"priority":100,"action":"PERMIT","prefixes":["10.0.0.0/8"]},{"priority":200,"action":"DENY","prefixes":["172.16.0.0/12"]}]}' \
  -o /dev/null -w "  -> %{http_code}\n"

# ── Service Tunnels ──
echo "  PUT service-tunnel/tunnel-prod-staging"
curl -s -X PUT "$DASHD/v1/$NS/service-tunnels/tunnel-prod-staging" \
  -H "Content-Type: application/json" \
  -d '{"metadata":{"namespace":"default","name":"tunnel-prod-staging"},"source_vnet":"vnet-prod","destination_vnet":"vnet-staging","tunnel_type":"VXLAN","bidirectional":true}' \
  -o /dev/null -w "  -> %{http_code}\n"

echo ""
echo "Bootstrap complete! Refresh http://localhost:3001 to see data."
echo ""
echo "Summary:"
curl -s "$DASHD/v1/$NS/vnets" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  Vnets: {len(d.get(\"items\",d.get(\"vnets\",[])))}') " 2>/dev/null || echo "  (could not query vnets)"
curl -s "$DASHD/v1/$NS/enis" | python3 -c "import sys,json; d=json.load(sys.stdin); print(f'  ENIs: {len(d.get(\"items\",d.get(\"enis\",[])))}') " 2>/dev/null || echo "  (could not query enis)"