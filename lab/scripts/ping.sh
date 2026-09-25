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
# -M do sets the DF bit (Linux iputils-ping). -s 1400 exceeds the 1280 hop MTU.
# ping exits non-zero when frag-needed is returned; that is expected here.
docker exec "$CONTAINER" ping -M "do" -s 1400 -c 3 -W 2 "$BLACKHOLE_IP" || true
