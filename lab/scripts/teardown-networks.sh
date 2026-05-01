#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

remove_network() {
  local name="$1"

  if ! docker network inspect "$name" &>/dev/null; then
    echo "Network '$name' does not exist, skipping"
    return 0
  fi

  echo "Removing network '$name'"
  docker network rm "$name"
}

remove_network "pmtud-net-a"
remove_network "pmtud-net-b"
remove_network "pmtud-transit"

echo "All networks removed"
