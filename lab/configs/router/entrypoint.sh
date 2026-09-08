#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

echo "Enabling IP forwarding..."
echo 1 > /proc/sys/net/ipv4/ip_forward

echo "Setting up iptables FORWARD policy..."
iptables -P FORWARD ACCEPT

echo "Router ready, waiting..."
exec sleep infinity
