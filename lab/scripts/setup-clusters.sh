#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAB_DIR="$(dirname "$SCRIPT_DIR")"

create_cluster() {
  local name="$1"
  local config="$2"
  local network="$3"

  if kind get clusters 2>/dev/null | grep -q "^${name}$"; then
    echo "Cluster '$name' already exists, skipping creation"
  else
    echo "Creating Kind cluster '$name'"
    kind create cluster --config "$config"
  fi

  echo "Connecting '$name' nodes to network '$network'"
  for container in $(docker ps --filter "label=io.x-k8s.kind.cluster=${name}" --format '{{.Names}}'); do
    if docker network inspect "$network" | grep -q "\"$container\""; then
      echo "  $container already on $network, skipping"
    else
      echo "  Connecting $container to $network"
      docker network connect "$network" "$container"
    fi
  done
}

create_cluster "pmtud-cluster-a" "$LAB_DIR/configs/kind-cluster-a.yaml" "pmtud-net-a"
create_cluster "pmtud-cluster-b" "$LAB_DIR/configs/kind-cluster-b.yaml" "pmtud-net-b"

echo "All clusters ready"
