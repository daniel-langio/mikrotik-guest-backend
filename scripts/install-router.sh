#!/usr/bin/env bash
# Idempotently installs the GuestPortal hotspot setup on a RouterOS 6.x
# device: new SSID/bridge/subnet, hotspot server, walled garden, API access
# for the backend, scoped so it never touches any pre-existing hotspot.
#
# Usage:
#   ./install-router.sh <host> <admin_user> <admin_pass> <router_name> <backend_host> <api_password>
#
# Example:
#   ./install-router.sh 192.168.1.202 admin adminpass delphi \
#       mikrotik-guest-backend-907906933835.asia-southeast1.run.app S3cret
#
# <router_name> is the id this router will be known as in ROUTERS and in
# each guest's &router=<name> redirect - must be unique across all routers
# sharing one backend. Re-running is safe: every step checks first and
# only creates what's actually missing, logging NEW vs EXISTS for each.

set -uo pipefail

HOST="${1:?host required}"
ADMIN_USER="${2:?admin user required}"
ADMIN_PASS="${3:?admin pass required}"
ROUTER_NAME="${4:?router name required}"
BACKEND_HOST="${5:?backend host required (e.g. mikrotik-guest-backend-xxx.run.app)}"
API_PASSWORD="${6:?api password required}"

SSID="Aphrodite Guest"
NEW_COUNT=0
EXISTS_COUNT=0

ssh_c() {
  sshpass -p "$ADMIN_PASS" ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 \
    "${ADMIN_USER}@${HOST}" "$1" 2>&1 | grep -v "post-quantum\|vulnerable\|may need to be upgraded\|openssh.com/pq"
}

log_new() { NEW_COUNT=$((NEW_COUNT + 1)); echo "[NEW]    $1"; }
log_exists() { EXISTS_COUNT=$((EXISTS_COUNT + 1)); echo "[EXISTS] $1"; }

# ensure_absent CHECK_CMD ADD_CMD DESCRIPTION
# CHECK_CMD should print a non-empty ".id"-bearing line if the resource
# already exists (RouterOS "print" with a filter does this naturally).
ensure() {
  local check="$1" add="$2" desc="$3"
  local out
  out=$(ssh_c "$check")
  if [ -n "$(echo "$out" | tr -d '[:space:]')" ]; then
    log_exists "$desc"
  else
    ssh_c "$add" >/dev/null
    log_new "$desc"
  fi
}

echo "=== Installing GuestPortal on ${HOST} as router '${ROUTER_NAME}' ==="

echo "--- connectivity check ---"
if ! ssh_c "/system identity print" | grep -q name; then
  echo "FATAL: cannot reach ${HOST} as ${ADMIN_USER}" >&2
  exit 1
fi

echo "--- wireless / network ---"
ensure "/interface wireless print terse where name=wlan1-guest" \
       "/interface wireless add master-interface=wlan1 ssid=\"${SSID}\" name=wlan1-guest disabled=no" \
       "virtual AP wlan1-guest"

ensure "/interface wireless print terse where name=wlan2-guest" \
       "/interface wireless add master-interface=wlan2 ssid=\"${SSID}\" name=wlan2-guest disabled=no" \
       "virtual AP wlan2-guest"

ensure "/interface bridge print terse where name=guest-bridge" \
       "/interface bridge add name=guest-bridge" \
       "bridge guest-bridge"

ensure "/interface bridge port print terse where interface=wlan1-guest and bridge=guest-bridge" \
       "/interface bridge port add bridge=guest-bridge interface=wlan1-guest" \
       "bridge port wlan1-guest -> guest-bridge"

ensure "/interface bridge port print terse where interface=wlan2-guest and bridge=guest-bridge" \
       "/interface bridge port add bridge=guest-bridge interface=wlan2-guest" \
       "bridge port wlan2-guest -> guest-bridge"

ensure "/ip address print terse where interface=guest-bridge and address=\"192.168.77.1/24\"" \
       "/ip address add address=192.168.77.1/24 interface=guest-bridge" \
       "IP address 192.168.77.1/24 on guest-bridge"

ensure "/ip pool print terse where name=guest-pool" \
       "/ip pool add name=guest-pool ranges=192.168.77.10-192.168.77.254" \
       "IP pool guest-pool"

ensure "/ip dhcp-server print terse where name=guest-dhcp" \
       "/ip dhcp-server add name=guest-dhcp interface=guest-bridge address-pool=guest-pool disabled=no" \
       "DHCP server guest-dhcp"

ensure "/ip dhcp-server network print terse where address=\"192.168.77.0/24\"" \
       "/ip dhcp-server network add address=192.168.77.0/24 gateway=192.168.77.1 dns-server=192.168.77.1" \
       "DHCP network 192.168.77.0/24"

ensure "/interface list member print terse where list=LAN interface=guest-bridge" \
       "/interface list member add list=LAN interface=guest-bridge" \
       "guest-bridge added to LAN interface list"

echo "--- hotspot ---"
ensure "/ip hotspot profile print terse where name=guest-portal" \
       "/ip hotspot profile add name=guest-portal hotspot-address=192.168.77.1 dns-name=\"guest.aphrodite\" html-directory=flash/guest-hotspot login-by=http-chap" \
       "hotspot profile guest-portal"

ensure "/ip hotspot print terse where name=GuestPortal" \
       "/ip hotspot add name=GuestPortal interface=guest-bridge address-pool=guest-pool profile=guest-portal disabled=no" \
       "hotspot server GuestPortal"

ensure "/ip hotspot walled-garden print terse where dst-host=\"${BACKEND_HOST}\"" \
       "/ip hotspot walled-garden add dst-host=${BACKEND_HOST} action=allow" \
       "walled-garden entry for ${BACKEND_HOST}"

echo "--- API access for the backend ---"
if ssh_c "/ip service print terse where name=api" | grep -q "disabled=yes"; then
  ssh_c "/ip service enable api" >/dev/null
  log_new "api service enabled"
else
  log_exists "api service already enabled"
fi

ensure "/ip firewall filter print terse where comment=\"aphrodite-guestportal: api open\"" \
       "/ip firewall filter add chain=input protocol=tcp dst-port=8728 action=accept place-before=0 comment=\"aphrodite-guestportal: api open\"" \
       "firewall rule: allow API (8728)"

ensure "/user group print terse where name=backend-api" \
       "/user group add name=backend-api policy=local,api,read,write" \
       "user group backend-api"

if ssh_c "/user print terse where name=backend-api" | grep -q name; then
  log_exists "user backend-api (password left untouched)"
else
  ssh_c "/user add name=backend-api group=backend-api password=${API_PASSWORD}" >/dev/null
  log_new "user backend-api created"
fi

echo "--- login page ---"
LOGIN_HTML=$(mktemp)
cat > "$LOGIN_HTML" <<EOF
<!doctype html>
<html lang="en">
<head>
    <meta charset="utf-8">
    <meta http-equiv="pragma" content="no-cache" />
    <meta http-equiv="expires" content="-1" />
    <meta http-equiv="refresh" content="0; url=https://${BACKEND_HOST}/welcome?mac=\$(mac-esc)&ip=\$(ip)&dst=\$(link-orig-esc)&router=${ROUTER_NAME}" />
    <title>Aphrodite Guest Portal</title>
</head>
<body>
    <p>Redirecting to the guest portal&hellip;</p>
    <p><a href="https://${BACKEND_HOST}/welcome?mac=\$(mac-esc)&ip=\$(ip)&dst=\$(link-orig-esc)&router=${ROUTER_NAME}">Click here if you are not redirected automatically.</a></p>
</body>
</html>
EOF
# Always re-uploaded (cheap, and keeps it in sync if BACKEND_HOST/ROUTER_NAME changed).
sshpass -p "$ADMIN_PASS" scp -o StrictHostKeyChecking=accept-new "$LOGIN_HTML" \
  "${ADMIN_USER}@${HOST}:flash/guest-hotspot/login.html" >/dev/null 2>&1
rm -f "$LOGIN_HTML"
echo "[NEW/REFRESHED] login.html (always re-uploaded to stay in sync)"

echo ""
echo "=== Done: ${NEW_COUNT} created, ${EXISTS_COUNT} already present ==="
echo ""
echo "Add this router to the backend's ROUTERS env var/secret:"
echo "  ${ROUTER_NAME}|<PUBLIC_IP_OR_HOST>:<FORWARDED_PORT>|backend-api|${API_PASSWORD}"
echo ""
echo "Still needed (outside this script, cannot be automated):"
echo "  - Port-forward on the internet gateway: <ext-port> TCP -> ${HOST}:8728"
echo "  - Add the line above to the ROUTERS secret and redeploy"
