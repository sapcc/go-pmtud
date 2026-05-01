#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAB_DIR="$(dirname "$SCRIPT_DIR")"

ROUTER_NAME="pmtud-router"
ROUTER_IMAGE="pmtud-router:local"

# Build router image
echo "Building router image..."
docker build -t "$ROUTER_IMAGE" "$LAB_DIR/configs/router/"

# Stop existing router if running
if docker ps -a --format '{{.Names}}' | grep -q "^${ROUTER_NAME}$"; then
  echo "Removing existing router container..."
  docker rm -f "$ROUTER_NAME"
fi

# Start router connected to the transit network initially
echo "Starting router container..."
docker run -d \
  --name "$ROUTER_NAME" \
  --privileged \
  --network "pmtud-transit" \
  --ip "172.32.0.10" \
  "$ROUTER_IMAGE"

# Connect router to both cluster networks
echo "Connecting router to pmtud-net-a..."
docker network connect --ip "172.30.0.10" "pmtud-net-a" "$ROUTER_NAME"

echo "Connecting router to pmtud-net-b..."
docker network connect --ip "172.31.0.10" "pmtud-net-b" "$ROUTER_NAME"

# Set MTU on all interfaces
# The transit interface already has MTU 1500 from the network setting
# Explicitly set MTU on the cluster-facing interfaces to 9000
echo "Configuring interface MTUs..."
# Get interface names by IP
for iface in $(docker exec "$ROUTER_NAME" ip -o addr show | grep "172.30.0.10" | awk '{print $2}'); do
  echo "  Setting $iface MTU to 9000 (net-a)"
  docker exec "$ROUTER_NAME" ip link set "$iface" mtu 9000
  echo "  Disabling offloads on $iface"
  docker exec "$ROUTER_NAME" ethtool -K "$iface" gso off gro off tso off 2>/dev/null || true
done

for iface in $(docker exec "$ROUTER_NAME" ip -o addr show | grep "172.31.0.10" | awk '{print $2}'); do
  echo "  Setting $iface MTU to 1500 (net-b — simulates reduced-MTU path)"
  docker exec "$ROUTER_NAME" ip link set "$iface" mtu 1500
  echo "  Disabling offloads on $iface"
  docker exec "$ROUTER_NAME" ethtool -K "$iface" gso off gro off tso off 2>/dev/null || true
done

for iface in $(docker exec "$ROUTER_NAME" ip -o addr show | grep "172.32.0.10" | awk '{print $2}'); do
  echo "  Setting $iface MTU to 1500 (transit)"
  docker exec "$ROUTER_NAME" ip link set "$iface" mtu 1500
done

echo "Router ready at:"
echo "  pmtud-net-a:   172.30.0.10"
echo "  pmtud-net-b:   172.31.0.10"
echo "  pmtud-transit: 172.32.0.10"
