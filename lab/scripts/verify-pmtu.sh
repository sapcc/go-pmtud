#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Verify PMTU cache on cluster-a worker nodes reflects MTU 1500 for cluster-b destinations

CLUSTER="${CLUSTER:-a}"
CLUSTER_NAME="pmtud-cluster-${CLUSTER}"

# Get a cluster-b node IP (the destination whose PMTU should be cached)
CLUSTER_B_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-b" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)
DEST_IP=$(docker exec "$CLUSTER_B_NODE" ip -o addr show | grep "172\.31\." | awk '{print $4}' | cut -d/ -f1 | head -1)

if [ -z "$DEST_IP" ]; then
  echo "ERROR: Could not determine cluster-b destination IP"
  exit 1
fi

echo "Checking PMTU cache for destination $DEST_IP on cluster-${CLUSTER} workers..."
echo ""

PASS=0
FAIL=0

for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=${CLUSTER_NAME}" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}'); do
  echo "--- $container ---"
  ROUTE_OUTPUT=$(docker exec "$container" ip route get "$DEST_IP" 2>/dev/null || echo "no route")
  echo "  $ROUTE_OUTPUT"

  if echo "$ROUTE_OUTPUT" | grep -q "mtu 1500"; then
    echo "  ✓ PMTU cache shows MTU 1500"
    PASS=$((PASS + 1))
  else
    echo "  ✗ PMTU cache does NOT show MTU 1500"
    FAIL=$((FAIL + 1))
  fi
  echo ""
done

echo "Results: $PASS passed, $FAIL failed"

if [ "$FAIL" -gt 0 ]; then
  echo ""
  echo "NOTE: PMTU cache may not be populated yet. Try:"
  echo "  1. Generate traffic: make generate-traffic"
  echo "  2. Wait a few seconds for go-pmtud replication"
  echo "  3. Re-run this check"
  exit 1
fi

exit 0
