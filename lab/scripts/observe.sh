#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

usage() {
  cat <<EOF
Usage: $0 <subcommand> [options]

Subcommands:
  node          tcpdump ICMP frag-needed on a cluster worker node
  replication   tcpdump UDP 4390 (go-pmtud replication) on a cluster worker node

Options:
  NODE=worker|worker2   Which worker node (default: worker)

Examples:
  $0 node
  NODE=worker2 $0 replication
EOF
  exit 1
}

SUBCOMMAND="${1:-}"
NODE="${NODE:-worker}"

get_worker_container() {
  local nodes
  nodes=$(docker ps \
    --filter "label=io.x-k8s.kind.cluster=pmtud" \
    --filter "label=io.x-k8s.kind.role=worker" \
    --format '{{.Names}}')

  if [ "$NODE" = "worker2" ]; then
    echo "$nodes" | tail -1
  else
    echo "$nodes" | head -1
  fi
}

ensure_tcpdump() {
  local container="$1"
  docker exec "$container" bash -c \
    'which tcpdump >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y -qq tcpdump >/dev/null 2>&1)'
}

case "$SUBCOMMAND" in
  node)
    CONTAINER=$(get_worker_container)
    echo "Observing ICMP frag-needed on $CONTAINER..."
    echo "Filter: icmp and icmp[0] == 3 and icmp[1] == 4"
    echo "---"
    ensure_tcpdump "$CONTAINER"
    docker exec "$CONTAINER" tcpdump -ni any 'icmp and icmp[0] == 3 and icmp[1] == 4' -nvvv
    ;;

  replication)
    CONTAINER=$(get_worker_container)
    echo "Observing go-pmtud UDP replication on $CONTAINER (port 4390)..."
    echo "Filter: udp port 4390"
    echo "---"
    ensure_tcpdump "$CONTAINER"
    docker exec "$CONTAINER" tcpdump -ni any 'udp port 4390' -nvvv
    ;;

  *)
    usage
    ;;
esac
