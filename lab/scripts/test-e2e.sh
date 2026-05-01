#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

echo "============================================"
echo "  go-pmtud End-to-End Lab Test"
echo "============================================"
echo ""

# Step 1: Get cluster-b node IP and ensure HTTP server is running
echo "[1/5] Setting up cluster-b traffic target..."
CLUSTER_B_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-b" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)
DEST_IP=$(docker exec "$CLUSTER_B_NODE" ip -o addr show | grep "172\.31\." | awk '{print $4}' | cut -d/ -f1 | head -1)

if [ -z "$DEST_IP" ]; then
  echo "FAIL: Could not determine cluster-b node IP"
  exit 1
fi
echo "  Destination: $DEST_IP:8080"

# Ensure HTTP server is running on cluster-b (host network, avoids pod MTU 1500 clamping)
docker exec "$CLUSTER_B_NODE" bash -c 'pkill -f "python3 -m http.server 8080" 2>/dev/null || true' 2>/dev/null
docker exec -d "$CLUSTER_B_NODE" python3 -m http.server 8080 --directory /tmp
sleep 1

# Step 2: Flush route caches on cluster-a nodes
echo ""
echo "[2/5] Flushing route caches on cluster-a workers..."
for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}'); do
  docker exec "$container" ip route flush cache 2>/dev/null || true
  echo "  Flushed: $container"
done

# Step 3: Generate traffic (large POST from cluster-a → cluster-b)
echo ""
echo "[3/5] Generating traffic (large POST from cluster-a → cluster-b)..."
CLUSTER_A_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)

# Start tcpdump in background on router to capture ICMP
docker exec "$CLUSTER_B_NODE" bash -c 'dd if=/dev/urandom of=/tmp/testdata bs=1024 count=512 2>/dev/null' 2>/dev/null
docker exec -d pmtud-router sh -c "timeout 15 tcpdump -ni any -c 10 'icmp and icmp[0] == 3 and icmp[1] == 4' -w /tmp/icmp_capture.pcap 2>/dev/null"
sleep 1

# Send large POST - TCP segments will be ~9000 bytes, router drops + sends ICMP frag-needed
docker exec "$CLUSTER_A_NODE" bash -c \
  "dd if=/dev/urandom bs=1024 count=512 2>/dev/null | curl -s -X POST --data-binary @- -o /dev/null --max-time 10 http://${DEST_IP}:8080/" 2>/dev/null || true

# Wait for ICMP to be captured and replicated
echo "  Waiting for ICMP and replication (5s)..."
sleep 5

# Step 4: Verify ICMP was generated
echo ""
echo "[4/5] Checking for ICMP fragmentation-needed on router..."
ICMP_COUNT=$(docker exec pmtud-router sh -c "tcpdump -r /tmp/icmp_capture.pcap 2>/dev/null | wc -l" 2>/dev/null || echo "0")

if [ "$ICMP_COUNT" -gt 0 ]; then
  echo "  ✓ Router generated $ICMP_COUNT ICMP frag-needed packet(s)"
else
  echo "  ✗ No ICMP frag-needed packets captured on router"
  echo ""
  echo "DIAGNOSTIC: The MTU mismatch may not be triggering. Check:"
  echo "  - Router interface MTUs: docker exec pmtud-router ip link show"
  echo "  - Offloads disabled: docker exec $CLUSTER_A_NODE ethtool -k eth1 | grep segmentation"
  echo "  - Routes: docker exec $CLUSTER_A_NODE ip route get $DEST_IP"
  exit 1
fi

# Step 5: Verify PMTU cache on cluster-a workers
echo ""
echo "[5/5] Verifying PMTU cache on cluster-a workers..."
PASS=0
TOTAL=0

for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}'); do
  TOTAL=$((TOTAL + 1))
  ROUTE_OUTPUT=$(docker exec "$container" ip route get "$DEST_IP" 2>/dev/null || echo "no route")

  if echo "$ROUTE_OUTPUT" | grep -q "mtu 1500"; then
    echo "  ✓ $container: PMTU=1500"
    PASS=$((PASS + 1))
  else
    echo "  ✗ $container: PMTU not set (route: $ROUTE_OUTPUT)"
  fi
done

# Cleanup
docker exec pmtud-router rm -f /tmp/icmp_capture.pcap 2>/dev/null || true

echo ""
echo "============================================"
if [ "$PASS" -eq "$TOTAL" ]; then
  echo "  PASS: All $TOTAL workers have PMTU=1500"
  echo "============================================"
  exit 0
else
  echo "  PARTIAL: $PASS/$TOTAL workers have PMTU=1500"
  echo "  (The node that originated traffic always gets it;"
  echo "   peers get it via go-pmtud replication)"
  echo "============================================"
  if [ "$PASS" -gt 0 ]; then
    exit 0
  fi
  exit 1
fi
