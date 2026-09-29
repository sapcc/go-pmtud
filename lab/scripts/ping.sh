#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Blackhole IP — sits behind the low-MTU (1280) control-plane hop.
BLACKHOLE_IP="10.99.0.2"

CONTAINER=$(docker ps \
  --filter "label=io.x-k8s.kind.cluster=pmtud" \
  --filter "label=io.x-k8s.kind.role=worker" \
  --format '{{.Names}}' | head -1)

if [ -z "$CONTAINER" ]; then
  echo "error: no worker node found — is the lab running?" >&2
  exit 1
fi

echo "Sending DF-set ping from $CONTAINER -> $BLACKHOLE_IP (payload 1400 > hop MTU 1280)"
echo "Expect: ICMP frag-needed (Frag needed and DF set)"
echo "---"

# Run tcpdump and ping in a single docker exec so they share the same network
# namespace without requiring a second terminal or timing coordination.
# $BLACKHOLE_IP is injected via -e; all other variables are container-local.
docker exec -i -e BLACKHOLE_IP="$BLACKHOLE_IP" "$CONTAINER" bash << 'SCRIPT'
which tcpdump >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq tcpdump >/dev/null 2>&1)

# Clear any cached PMTU for the blackhole IP left over from a previous run;
# without this the kernel rejects sendmsg() immediately and no packet is sent.
ip route flush cache 2>/dev/null || true

CAPFILE=$(mktemp /tmp/cap.XXXXXX.pcap)

# Resolve the egress interface for the blackhole IP so tcpdump binds to a real
# device (DLT_EN10MB) rather than 'any' (DLT_LINUX_SLL2).  The icmp[N] byte
# accessor in BPF does not compile reliably for DLT_LINUX_SLL2 inside Docker.
IFACE=$(ip route get "$BLACKHOLE_IP" 2>/dev/null \
  | awk 'NR==1 { for(i=1;i<=NF;i++) if($i=="dev") { print $(i+1); exit } }')
IFACE=${IFACE:-eth1}

tcpdump -ni "$IFACE" 'icmp and icmp[0] == 3 and icmp[1] == 4' -w "$CAPFILE" -q 2>/dev/null &
TDPID=$!
sleep 0.5  # let tcpdump attach before sending the first packet

# -M do sets the DF bit; -s 1400 exceeds the 1280 hop MTU. Non-zero exit expected.
ping -M do -s 1400 -c 3 -W 2 "$BLACKHOLE_IP" || true
sleep 0.3  # drain any late-arriving packets

kill "$TDPID" 2>/dev/null
wait "$TDPID" 2>/dev/null || true

printf '\n--- tcpdump: ICMP frag-needed ---\n'
tcpdump -r "$CAPFILE" -nvvv 2>/dev/null || true
rm -f "$CAPFILE"
SCRIPT
