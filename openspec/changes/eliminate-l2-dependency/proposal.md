<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Why

go-pmtud originally required a dedicated Layer 2 replication interface shared between all cluster nodes to broadcast ICMP fragmentation-needed packets via raw Ethernet frames. This imposes a network topology constraint: all nodes must be on the same L2 segment. Modern Kubernetes clusters often span multiple L2 domains (racks, availability zones, cloud regions), making a mandatory L2 requirement a deployment blocker.

At the same time, go-pmtud is already running in production landscapes that rely on L2 connectivity via VLANs. Those installations must keep working unchanged. So the goal is to make L2 **optional** — add an IP-level (UDP) transport as an alternative — rather than to remove L2.

## What Changes

- Add a pluggable `Relay` interface (`internal/relay`) selecting the inter-node transport via a `--relay-backend` flag that accepts `l2` and `udp` and **defaults to `l2`**
- Retain the existing L2 (raw Ethernet + ARP) transport as the default backend so current production installs stay untouched on upgrade
- Add an optional UDP unicast backend that works across L3 boundaries:
  - The nflog controller sends ICMP payloads over UDP to all peer node IPs
  - A UDP listener on each node receives replicated packets and injects them into the local stack via a TUN device (`pmtud0`) — forcing the kernel receive path that updates the PMTU cache
  - The TUN device is a private detail of the UDP backend (created/destroyed with the backend), not a shared daemon object
- Make the node reconciler transport-agnostic: it stores peer **IPs** (InternalIP from `node.Status.Addresses`) for both backends; the L2 backend layers ARP-based MAC resolution on top
- Keep NFLOG capture (`internal/nflog`) transport-agnostic — it only calls `Relay.Send`

## Capabilities

### New Capabilities
- `udp-replication`: Optionally replicate ICMP fragmentation-needed packets between nodes using UDP unicast over the default interface, eliminating the L2 adjacency requirement when selected

### Modified Capabilities

## Impact

- **Code**: New `internal/relay` package (Relay interface + `l2` and `udp` backends). UDP backend owns the TUN injector (`internal/relay/udp`, Linux-gated). L2 backend reuses the `internal/arp` package. `internal/nflog` calls `Relay.Send`; `internal/node` stores peer IPs; `internal/config` and `internal/cmd` carry both backends' flags.
- **APIs/Flags**: L2 flags **retained** (`--iface_names`, `--iface_mtu`, `--node-timeout-minutes`, `--arp-timeout-seconds`); new `--replication-port` (default 4390, UDP-only); new `--relay-backend` (`l2`|`udp`, default `l2`)
- **Dependencies**: `github.com/mdlayher/arp`, `github.com/mdlayher/ethernet`, `github.com/mdlayher/packet` retained (L2 backend); no new external dependency for UDP/TUN (custom netlink injector)
- **Network**: UDP mode requires the UDP replication port open between nodes (firewall/security-group consideration); L2 mode unchanged
- **iptables**: per-backend NFLOG rule — L2 binds capture to the primary interface (`-i <iface>`), UDP excludes the TUN device (`! -i pmtud0`)
- **Deployment**: existing L2 DaemonSets upgrade with no manifest change; opting into UDP requires setting `--relay-backend=udp` and the `! -i pmtud0` NFLOG rule
