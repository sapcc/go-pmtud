#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

create_network() {
  local name="$1"
  local subnet="$2"
  local mtu="$3"

  if docker network inspect "$name" &>/dev/null; then
    echo "Network '$name' already exists, skipping"
    return 0
  fi

  echo "Creating network '$name' (subnet=$subnet, mtu=$mtu)"
  docker network create \
    --driver bridge \
    --subnet "$subnet" \
    --opt "com.docker.network.driver.mtu=$mtu" \
    "$name"
}

create_network "pmtud-net-a" "172.30.0.0/16" "9000"
create_network "pmtud-net-b" "172.31.0.0/16" "9000"
create_network "pmtud-transit" "172.32.0.0/24" "1500"

echo "All networks ready"
