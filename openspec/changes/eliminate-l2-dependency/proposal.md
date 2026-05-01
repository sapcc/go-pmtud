<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Why

go-pmtud currently requires a dedicated Layer 2 replication interface shared between all cluster nodes to broadcast ICMP fragmentation-needed packets via raw Ethernet frames. This imposes a network topology constraint: all nodes must be on the same L2 segment. Modern Kubernetes clusters often span multiple L2 domains (e.g., different racks, availability zones, or cloud regions), making this L2 requirement a deployment blocker.

## What Changes

- **BREAKING**: Remove the dedicated replication interface (`iface_names`, `iface_mtu` flags) and ARP-based peer discovery
- Replace raw Ethernet frame transmission with IP-level (UDP) unicast to peer nodes
- Use node IP addresses from the Kubernetes API (already discovered) for direct UDP communication
- Remove the `internal/arp` package entirely
- Simplify node reconciler to store peer IPs instead of MAC addresses
- The nflog controller sends ICMP payloads over UDP to all peer node IPs
- Add a UDP listener on each node to receive replicated ICMP packets and inject them into the local network stack via raw socket

## Capabilities

### New Capabilities
- `udp-replication`: Replicate ICMP fragmentation-needed packets between nodes using UDP unicast over the default interface, eliminating L2 adjacency requirements

### Modified Capabilities

## Impact

- **Code**: Major refactor of `internal/nflog` (sender), `internal/node` (peer tracking), `internal/config` (remove L2 fields), `internal/cmd` (flags), removal of `internal/arp`
- **APIs/Flags**: `--iface_names`, `--iface_mtu`, `--arp-timeout-seconds`, `--node-timeout-minutes` removed; new `--replication-port` flag added
- **Dependencies**: Remove `github.com/mdlayher/arp`, `github.com/mdlayher/ethernet`, `github.com/mdlayher/packet`; no new external dependencies needed
- **Network**: Requires UDP port open between nodes (firewall/security-group consideration)
- **Deployment**: DaemonSet no longer needs host-network L2 interface configuration; simpler deployment model
