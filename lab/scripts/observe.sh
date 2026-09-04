#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 <subcommand> [options]

Subcommands:
  router              tcpdump ICMP frag-needed on the router container
  node                tcpdump ICMP frag-needed on a cluster node
  replication         tcpdump UDP 4390 (go-pmtud replication) on a cluster node

Options for 'node' and 'replication':
  CLUSTER=a|b         Which cluster (default: a)
  NODE=worker|worker2 Which node (default: first worker)

Examples:
  $0 router
  CLUSTER=a $0 node
  CLUSTER=b NODE=worker2 $0 replication
EOF
  exit 1
}

SUBCOMMAND="${1:-}"
CLUSTER="${CLUSTER:-a}"
NODE="${NODE:-worker}"

get_cluster_node() {
  local cluster="pmtud-cluster-${CLUSTER}"
  local nodes
  nodes=$(docker ps --filter "label=io.x-k8s.kind.cluster=${cluster}" --filter "label=io.x-k8s.kind.role=worker" --format '{{.Names}}')

  if [ "$NODE" = "worker2" ]; then
    echo "$nodes" | tail -1
  else
    echo "$nodes" | head -1
  fi
}

case "$SUBCOMMAND" in
  router)
    echo "Observing ICMP frag-needed on pmtud-router..."
    echo "Filter: icmp and icmp[0] == 3 and icmp[1] == 4"
    echo "---"
    docker exec pmtud-router tcpdump -ni any 'icmp and icmp[0] == 3 and icmp[1] == 4' -nvvv
    ;;

  node)
    CONTAINER=$(get_cluster_node)
    echo "Observing ICMP frag-needed on $CONTAINER..."
    echo "Filter: icmp and icmp[0] == 3 and icmp[1] == 4"
    echo "---"
    docker exec "$CONTAINER" bash -c 'which tcpdump >/dev/null 2>&1 || apt-get update -qq && apt-get install -y -qq tcpdump >/dev/null 2>&1; tcpdump -ni any "icmp and icmp[0] == 3 and icmp[1] == 4" -nvvv'
    ;;

  replication)
    CONTAINER=$(get_cluster_node)
    echo "Observing go-pmtud UDP replication on $CONTAINER (port 4390)..."
    echo "Filter: udp port 4390"
    echo "---"
    docker exec "$CONTAINER" bash -c 'which tcpdump >/dev/null 2>&1 || apt-get update -qq && apt-get install -y -qq tcpdump >/dev/null 2>&1; tcpdump -ni any "udp port 4390" -nvvv'
    ;;

  *)
    usage
    ;;
esac
