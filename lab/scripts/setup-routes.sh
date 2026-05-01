#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

# Add static routes on Kind cluster nodes so cross-cluster traffic goes through the router.
#
# Cluster-a nodes (on pmtud-net-a / 172.30.x.x) need routes to:
#   - 172.31.0.0/16 (pmtud-net-b) via router at 172.30.0.10
#   - 10.245.0.0/16 (cluster-b pod CIDR) via router at 172.30.0.10
#
# Cluster-b nodes (on pmtud-net-b / 172.31.x.x) need routes to:
#   - 172.30.0.0/16 (pmtud-net-a) via router at 172.31.0.10
#   - 10.244.0.0/16 (cluster-a pod CIDR) via router at 172.31.0.10

add_routes_to_cluster() {
  local cluster="$1"
  local dest_net="$2"
  local dest_pod_cidr="$3"
  local gateway="$4"

  echo "Adding routes for cluster '$cluster' nodes..."
  for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=${cluster}" --format '{{.Names}}'); do
    # Find the interface on the cluster's network (has the gateway's /16 prefix)
    local gw_prefix
    gw_prefix=$(echo "$gateway" | cut -d. -f1-2)

    local iface
    iface=$(docker exec "$container" ip -o addr show | grep "${gw_prefix}\." | awk '{print $2}' | head -1)

    if [ -z "$iface" ]; then
      echo "  WARNING: $container has no interface on ${gw_prefix}.x.x network, skipping"
      continue
    fi

    echo "  $container: route to $dest_net via $gateway dev $iface"
    docker exec "$container" ip route replace "$dest_net" via "$gateway" dev "$iface" 2>/dev/null || true

    echo "  $container: route to $dest_pod_cidr via $gateway dev $iface"
    docker exec "$container" ip route replace "$dest_pod_cidr" via "$gateway" dev "$iface" 2>/dev/null || true
  done
}

# Cluster-a nodes → route to cluster-b via router's net-a IP
add_routes_to_cluster "pmtud-cluster-a" "172.31.0.0/16" "10.245.0.0/16" "172.30.0.10"

# Cluster-b nodes → route to cluster-a via router's net-b IP
add_routes_to_cluster "pmtud-cluster-b" "172.30.0.0/16" "10.244.0.0/16" "172.31.0.10"

# Router needs routes to pod CIDRs through the respective cluster nodes
# For simplicity, add routes to the entire pod CIDR via the network (the Kind nodes will handle it)
echo "Adding pod CIDR routes on router..."

# Get a cluster-a worker node IP on pmtud-net-a
CLUSTER_A_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)
CLUSTER_A_NODE_IP=$(docker exec "$CLUSTER_A_NODE" ip -o addr show | grep "172\.30\." | awk '{print $4}' | cut -d/ -f1 | head -1)

CLUSTER_B_NODE=$(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-b" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}' | head -1)
CLUSTER_B_NODE_IP=$(docker exec "$CLUSTER_B_NODE" ip -o addr show | grep "172\.31\." | awk '{print $4}' | cut -d/ -f1 | head -1)

if [ -n "$CLUSTER_A_NODE_IP" ]; then
  echo "  Router: route to 10.244.0.0/16 via $CLUSTER_A_NODE_IP"
  docker exec pmtud-router ip route replace "10.244.0.0/16" via "$CLUSTER_A_NODE_IP" 2>/dev/null || true
fi

if [ -n "$CLUSTER_B_NODE_IP" ]; then
  echo "  Router: route to 10.245.0.0/16 via $CLUSTER_B_NODE_IP"
  docker exec pmtud-router ip route replace "10.245.0.0/16" via "$CLUSTER_B_NODE_IP" 2>/dev/null || true
fi

echo "Static routes configured"

# Disable TCP/GSO offloads on cluster nodes' pmtud-net interfaces
# This ensures packets are sent at actual wire size (up to interface MTU)
# Without this, the kernel does GSO and packets never exceed 1500 on the wire
echo "Disabling offloads on cluster node interfaces..."
for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-a" --format '{{.Names}}'); do
  iface=$(docker exec "$container" ip -o addr show | grep "172\.30\." | awk '{print $2}' | head -1)
  if [ -n "$iface" ]; then
    docker exec "$container" ethtool -K "$iface" gso off gro off tso off 2>/dev/null || true
    echo "  $container: offloads disabled on $iface"
  fi
done
for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=pmtud-cluster-b" --format '{{.Names}}'); do
  iface=$(docker exec "$container" ip -o addr show | grep "172\.31\." | awk '{print $2}' | head -1)
  if [ -n "$iface" ]; then
    docker exec "$container" ethtool -K "$iface" gso off gro off tso off 2>/dev/null || true
    echo "  $container: offloads disabled on $iface"
  fi
done

echo "Setup complete"
