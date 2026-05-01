#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Get a cluster-b node IP on pmtud-net-b (reachable from cluster-a via router)
echo "Finding cluster-b node IP..."
CLUSTER_B_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-b" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)
CLUSTER_B_IP=$(docker exec "$CLUSTER_B_NODE" ip -o addr show | grep "172\.31\." | awk '{print $4}' | cut -d/ -f1 | head -1)

if [ -z "$CLUSTER_B_IP" ]; then
  echo "ERROR: Could not determine cluster-b node IP on pmtud-net-b"
  exit 1
fi

echo "Cluster-b node IP: $CLUSTER_B_IP (NodePort 30080)"

# Ensure a simple HTTP server is running on cluster-b worker (host network, MTU 9000)
# podinfo doesn't work because pod veth MTU is 1500, clamping TCP MSS
echo "Starting HTTP server on cluster-b worker (host network)..."
docker exec "$CLUSTER_B_NODE" bash -c '
  pkill -f "python3 -m http.server 8080" 2>/dev/null || true
  dd if=/dev/urandom of=/tmp/testdata bs=1024 count=2048 2>/dev/null
' 2>/dev/null
docker exec -d "$CLUSTER_B_NODE" python3 -m http.server 8080 --directory /tmp
sleep 1

# Run large POST from a cluster-a worker node to trigger ICMP frag-needed
# The cluster-a worker sends 9000-byte TCP segments (eth1 MTU 9000, offloads disabled)
# The router's net-b interface has MTU 1500, causing ICMP type 3 code 4
echo "Generating traffic from cluster-a to cluster-b..."
CLUSTER_A_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)

echo "  Source: $CLUSTER_A_NODE"
echo "  Destination: http://$CLUSTER_B_IP:8080"
echo ""

# Flush route cache to ensure fresh PMTU discovery
docker exec "$CLUSTER_A_NODE" ip route flush cache 2>/dev/null || true

# Send large POST — TCP segments will be ~9000 bytes (matching eth1 MTU)
# Router can't forward out eth2 (MTU 1500) → sends ICMP frag-needed back
echo "Sending large POST (512KB, triggers PMTU discovery)..."
docker exec "$CLUSTER_A_NODE" bash -c \
  "dd if=/dev/urandom bs=1024 count=512 2>/dev/null | curl -s -X POST --data-binary @- -o /dev/null -w 'HTTP %{http_code} - Sent %{size_upload} bytes in %{time_total}s\n' --max-time 15 http://${CLUSTER_B_IP}:8080/" || true

echo ""
echo "Check PMTU with: docker exec $CLUSTER_A_NODE ip route get $CLUSTER_B_IP"
docker exec "$CLUSTER_A_NODE" ip route get "$CLUSTER_B_IP" 2>/dev/null || true
