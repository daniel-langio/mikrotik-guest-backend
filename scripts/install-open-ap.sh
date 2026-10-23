#!/usr/bin/env bash
# Open (no-password) AP on a RouterOS 6.x router; every client MAC that gets a
# DHCP lease is cached as a static lease (survives reboot) commented "open-ap".
# Usage: ./install-open-ap.sh <host> <admin_user> <admin_pass> [ssid]
# Example: ./install-open-ap.sh 192.168.1.201 admin pass "Free WiFi"
# Read the cache: /ip dhcp-server lease print where comment~"open-ap"
set -uo pipefail
HOST="${1:?host}"; U="${2:?user}"; P="${3:?pass}"; SSID="${4:-Free WiFi}"

r() { sshpass -p "$P" ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 "$U@$HOST" "$1" 2>&1 \
  | grep -v "post-quantum\|vulnerable\|may need to be upgraded\|openssh.com/pq"; }
# ensure CHECK ADD: add only if CHECK prints nothing
ensure() { [ -n "$(r "$1" | tr -d '[:space:]')" ] && echo "[EXISTS] $3" || { r "$2" >/dev/null; echo "[NEW]    $3"; }; }

ensure "/interface wireless print terse where name=wlan1-open" \
  "/interface wireless add master-interface=wlan1 ssid=\"$SSID\" name=wlan1-open security-profile=default disabled=yes" "AP wlan1-open"
ensure "/interface bridge print terse where name=open-bridge" "/interface bridge add name=open-bridge" "bridge open-bridge"
ensure "/interface bridge port print terse where interface=wlan1-open" \
  "/interface bridge port add bridge=open-bridge interface=wlan1-open" "bridge port"
ensure "/ip address print terse where interface=open-bridge" \
  "/ip address add address=192.168.78.1/24 interface=open-bridge" "ip 192.168.78.1/24"
ensure "/ip pool print terse where name=open-pool" \
  "/ip pool add name=open-pool ranges=192.168.78.10-192.168.78.254" "pool"
# lease-script: freeze each bound lease into a static one = persistent MAC cache
ensure "/ip dhcp-server print terse where name=open-dhcp" \
  "/ip dhcp-server add name=open-dhcp interface=open-bridge address-pool=open-pool lease-time=1d disabled=no lease-script=\":if (\\\$leaseBound=1) do={ :local id [/ip dhcp-server lease find where active-mac-address=\\\$leaseActMAC and dynamic=yes and server=open-dhcp]; :if (\\\$id!=\\\"\\\") do={ /ip dhcp-server lease make-static \\\$id; /ip dhcp-server lease set \\\$id comment=\\\"open-ap \\\$leaseActMAC\\\" } }\"" "DHCP open-dhcp"
ensure "/ip dhcp-server network print terse where address=192.168.78.0/24" \
  "/ip dhcp-server network add address=192.168.78.0/24 gateway=192.168.78.1 dns-server=192.168.78.1" "dhcp network"
ensure "/ip firewall nat print terse where comment=open-ap" \
  "/ip firewall nat add chain=srcnat src-address=192.168.78.0/24 action=masquerade comment=open-ap" "NAT"
