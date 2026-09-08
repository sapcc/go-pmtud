#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

echo "============================================"
echo "  go-pmtud Lab Status"
echo "============================================"
echo ""

# Networks
echo "## Docker Networks"
for net in pmtud-net-a pmtud-net-b pmtud-transit; do
  if docker network inspect "$net" &>/dev/null; then
    MTU=$(docker network inspect "$net" --format '{{index .Options "com.docker.network.driver.mtu"}}')
    SUBNET=$(docker network inspect "$net" --format '{{range .IPAM.Config}}{{.Subnet}}{{end}}')
    echo "  ✓ $net (subnet=$SUBNET, mtu=${MTU:-default})"
  else
    echo "  ✗ $net (not found)"
  fi
done
echo ""

# Router
echo "## Router"
if docker ps --format '{{.Names}}' | grep -q "^pmtud-router$"; then
  echo "  ✓ pmtud-router (running)"
  docker exec pmtud-router ip -o addr show 2>/dev/null | grep "inet " | awk '{printf "    %s: %s\n", $2, $4}'
else
  echo "  ✗ pmtud-router (not running)"
fi
echo ""

# Clusters
echo "## Kind Clusters"
for cluster in pmtud-cluster-a pmtud-cluster-b; do
  if kind get clusters 2>/dev/null | grep -q "^${cluster}$"; then
    NODE_COUNT=$(docker ps --filter "label=io.x-k8s.kind.cluster=${cluster}" --format '{{.Names}}' | wc -l | tr -d ' ')
    echo "  ✓ $cluster ($NODE_COUNT nodes)"
  else
    echo "  ✗ $cluster (not found)"
  fi
done
echo ""

# go-pmtud pods
echo "## go-pmtud Pods"
for ctx in kind-pmtud-cluster-a kind-pmtud-cluster-b; do
  echo "  $ctx:"
  if kubectl --context "$ctx" -n default get ns kube-system &>/dev/null 2>&1; then
    kubectl --context "$ctx" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud --no-headers 2>/dev/null | \
      awk '{printf "    %s: %s\n", $1, $3}' || echo "    (no pods)"
  else
    echo "    (cluster unreachable)"
  fi
done
echo ""

# Podinfo
echo "## Podinfo (cluster-b)"
if kubectl --context "kind-pmtud-cluster-b" -n default get ns podinfo &>/dev/null 2>&1; then
  kubectl --context "kind-pmtud-cluster-b" -n podinfo get pods --no-headers 2>/dev/null | \
    awk '{printf "  %s: %s\n", $1, $3}' || echo "  (no pods)"
else
  echo "  (not deployed)"
fi
echo ""
