#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

echo "============================================"
echo "  go-pmtud Lab Status"
echo "============================================"
echo ""

# Kind cluster
echo "## Kind Cluster"
if kind get clusters 2>/dev/null | grep -q "^pmtud$"; then
  NODE_COUNT=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud" --format '{{.Names}}' | wc -l | tr -d ' ')
  echo "  ✓ pmtud ($NODE_COUNT nodes)"
  docker ps --filter "label=io.x-k8s.kind.cluster=pmtud" --format '    {{.Names}} ({{.Status}})'
else
  echo "  ✗ pmtud (not found)"
fi
echo ""

# Transit network
echo "## Transit Network"
if docker network inspect pmtud-transit &>/dev/null; then
  MTU=$(docker network inspect pmtud-transit --format '{{index .Options "com.docker.network.driver.mtu"}}')
  SUBNET=$(docker network inspect pmtud-transit --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}')
  echo "  ✓ pmtud-transit (subnet=$SUBNET, mtu=${MTU:-default})"
else
  echo "  ✗ pmtud-transit (not found)"
fi
echo ""

# go-pmtud pods
echo "## go-pmtud Pods"
if kubectl --context "kind-pmtud" get nodes &>/dev/null 2>&1; then
  kubectl --context "kind-pmtud" -n kube-system get pods \
    -l app.kubernetes.io/name=go-pmtud \
    --no-headers 2>/dev/null | \
    awk '{printf "  %s: %s\n", $1, $3}' || echo "  (no pods)"
else
  echo "  (cluster unreachable)"
fi
echo ""
