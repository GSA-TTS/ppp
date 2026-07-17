#!/usr/bin/env bash
# ppp sandbox provisioning script (embedded via Go //go:embed, copied into the
# guest with `podman machine cp`, and run as `podman machine ssh <name> -- bash
# /tmp/provision.sh`). Runs EVERY boot and MUST be idempotent (spec §5.2).
#
# Inputs (environment, exported by the caller over the ssh command line):
#   PPP_WG_CONF   path to the rewritten wg0.conf inside the guest (default
#                 /tmp/ppp-wg0.conf). It already has Table=off, no DNS= line,
#                 Address=10.0.0.<N>/32 and Endpoint=192.168.127.254:<port>.
#   PPP_AGENT_IMAGE  OCI ref to pull for the agent (optional; skipped if empty).
#
# Fails closed: any error aborts (set -euo pipefail) so a half-provisioned
# sandbox never looks healthy. Every step is safe to re-run.
set -euo pipefail

log() { printf 'ppp-provision: %s\n' "$*" >&2; }

WG_CONF_SRC="${PPP_WG_CONF:-/tmp/ppp-wg0.conf}"
WG_CONF_DST="/etc/wireguard/wg0.conf"
PROVISIONED_MARKER="/var/lib/ppp/.provisioned"
AGENT_IMAGE="${PPP_AGENT_IMAGE:-}"

require_root() {
	if [ "$(id -u)" -ne 0 ]; then
		log "must run as root inside the guest"
		exit 1
	fi
}

# 1. Ensure the WireGuard kernel module is available. wireguard-tools ships in
#    the podman machine-os image (wayfinder #5), so no rpm-ostree install / no
#    reboot on the primary path. --apply-live is only a drift safety-net.
ensure_wireguard() {
	if modprobe wireguard 2>/dev/null; then
		log "wireguard module loaded"
		return 0
	fi
	log "wireguard module not loadable; attempting rpm-ostree --apply-live (drift fallback)"
	if command -v rpm-ostree >/dev/null 2>&1; then
		rpm-ostree install --apply-live --idempotent wireguard-tools || {
			log "wireguard-tools install failed"
			return 1
		}
		modprobe wireguard
	else
		log "no rpm-ostree available and wireguard module missing"
		return 1
	fi
}

# 2. Install the rewritten wg0.conf (0600). The caller has already set Table=off
#    and stripped the DNS= line (ADR-0005).
install_wg_conf() {
	if [ ! -f "$WG_CONF_SRC" ]; then
		log "wg config not found at $WG_CONF_SRC"
		exit 1
	fi
	install -d -m 700 /etc/wireguard
	install -m 600 "$WG_CONF_SRC" "$WG_CONF_DST"
	log "installed $WG_CONF_DST"
}

# Extract the WireGuard endpoint IP from the config's `Endpoint = host:port`.
endpoint_ip() {
	awk -F'[= :]+' '/^[[:space:]]*Endpoint[[:space:]]*=/ {print $2; exit}' "$WG_CONF_DST"
}

# The guest's default gateway (gvproxy on libkrun is 192.168.127.1).
default_gateway() {
	ip route show default 2>/dev/null | awk '/default/ {print $3; exit}'
}

# 3. Routing: pin an off-tunnel /32 route to the WG endpoint BEFORE bringing the
#    tunnel up (so encrypted WG datagrams don't loop back into wg0), then bring
#    wg0 up, then route the default through wg0 (wayfinder #4). Table=off in the
#    config means wg-quick installs no routing of its own.
bring_up_tunnel() {
	local ep gw
	ep="$(endpoint_ip)"
	gw="$(default_gateway)"
	if [ -z "$ep" ]; then
		log "could not parse Endpoint IP from $WG_CONF_DST"
		exit 1
	fi
	if [ -z "$gw" ]; then
		log "no default gateway; falling back to gvproxy 192.168.127.1"
		gw="192.168.127.1"
	fi
	log "pinning off-tunnel route $ep/32 via $gw"
	ip route replace "$ep/32" via "$gw"

	# wg-quick up is idempotent-ish: if wg0 already exists, reload rather than
	# fail. Tear down a stale interface first so re-runs converge cleanly.
	if ip link show wg0 >/dev/null 2>&1; then
		log "wg0 already present; bringing it down before re-up"
		wg-quick down wg0 2>/dev/null || ip link del wg0 2>/dev/null || true
		# re-pin the endpoint route (wg-quick down may have removed it)
		ip route replace "$ep/32" via "$gw"
	fi
	log "wg-quick up wg0"
	wg-quick up wg0
	log "routing default via wg0"
	ip route replace default dev wg0
}

# 4. Wait for the tunnel handshake to complete. Fails CLOSED: if no handshake is
#    confirmed within the window, exit non-zero so a dead tunnel never looks
#    provisioned (the caller sees the failure and can diagnose/retry).
wait_for_tunnel() {
	local i
	for i in $(seq 1 30); do
		: "$i" # counter only; loop body polls the tunnel state
		if wg show wg0 >/dev/null 2>&1; then
			# A completed handshake shows a peer row with a non-zero time.
			if [ -n "$(wg show wg0 latest-handshakes 2>/dev/null | awk '$2 > 0 {print}')" ]; then
				log "tunnel handshake established"
				return 0
			fi
		fi
		sleep 1
	done
	log "error: no WireGuard handshake after 30s; tunnel is not healthy"
	return 1
}

# 5. Trust the mitmproxy CA. Must run AFTER the tunnel is up (mitm.it is a magic
#    host intercepted by the proxy). Belt-and-suspenders atop host-side
#    --import-native-ca (wayfinder #7).
trust_ca() {
	local anchor="/etc/pki/ca-trust/source/anchors/mitmproxy.crt"
	install -d -m 755 "$(dirname "$anchor")"
	if curl -fsS --max-time 20 http://mitm.it/cert/pem -o "$anchor"; then
		update-ca-trust
		log "mitmproxy CA trusted"
	else
		log "warning: could not fetch mitmproxy CA via mitm.it (relying on --import-native-ca)"
	fi
}

# 6. Disable IPv6 (mitmproxy AllowedIPs is IPv4-only). Persisted + applied.
disable_ipv6() {
	local conf="/etc/sysctl.d/99-ppp.conf"
	cat >"$conf" <<'EOF'
net.ipv6.conf.all.disable_ipv6 = 1
net.ipv6.conf.default.disable_ipv6 = 1
EOF
	sysctl -p "$conf" >/dev/null 2>&1 || true
	log "IPv6 disabled"
}

# 7. One-shot: pull the agent image (gated behind the provisioned marker).
pull_agent_image() {
	[ -z "$AGENT_IMAGE" ] && { log "no agent image specified; skipping pull"; return 0; }
	if [ -f "$PROVISIONED_MARKER" ]; then
		log "already provisioned; skipping agent image pull"
		return 0
	fi
	log "pulling agent image $AGENT_IMAGE"
	podman pull "$AGENT_IMAGE"
}

mark_provisioned() {
	install -d -m 755 "$(dirname "$PROVISIONED_MARKER")"
	touch "$PROVISIONED_MARKER"
}

main() {
	require_root
	ensure_wireguard
	install_wg_conf
	bring_up_tunnel
	wait_for_tunnel
	trust_ca
	disable_ipv6
	pull_agent_image
	mark_provisioned
	log "provisioning complete"
}

main "$@"
