<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Context

go-pmtud currently replicates ICMP fragmentation-needed packets between Kubernetes cluster nodes using raw Ethernet frames over a dedicated Layer 2 replication interface. Each node:
1. Discovers peer nodes via the Kubernetes API
2. Resolves peer MAC addresses via ARP on the replication interface
3. Captures ICMP type 3 code 4 packets via NFLOG
4. Constructs raw Ethernet frames and sends them to each peer's MAC address

This design requires all nodes to share a common L2 network segment, which is increasingly incompatible with modern cluster topologies.

## Goals / Non-Goals

**Goals:**
- Eliminate the requirement for L2 adjacency between cluster nodes
- Use UDP unicast over the node's default interface for packet replication
- Maintain the same ICMP replication semantics (all peers get the packet)
- Simplify deployment (no dedicated interface configuration needed)
- Preserve existing NFLOG capture mechanism (proven, minimal change)
- Ensure injected packets actually trigger kernel PMTU cache updates
- Prevent replication loops between nodes

**Non-Goals:**
- Changing how ICMP packets are captured (NFLOG stays)
- Encryption or authentication of replicated packets (can be added later)
- Supporting IPv6 ICMP (Packet Too Big) — future work
- Implementing reliability/retransmission for UDP (ICMP replication is best-effort by nature)

## Decisions

### 1. UDP unicast for sending

**Decision**: Use a standard UDP socket to send ICMP payloads to peer node IPs.

**Rationale**: UDP is the simplest IP-level transport that works across L3 boundaries. No connection state, no handshake, aligns with the fire-and-forget nature of ICMP replication. Raw IP sockets would require CAP_NET_RAW and add complexity for no benefit.

**Alternatives considered**:
- TCP: Overkill, connection-oriented, adds latency and state management
- gRPC/HTTP: Heavy for forwarding raw packets, adds serialization overhead
- IP-in-IP tunneling: Would require CAP_NET_RAW, more complex, less portable

### 2. Persistent UDP socket for sending (not per-packet Dial)

**Decision**: Maintain a single unconnected `*net.UDPConn` for sending, using `WriteTo()` for each peer instead of `Dial()` + `Write()` + `Close()` per peer per packet.

**Rationale**: The nflog callback is a hot path — under burst traffic (many flows hitting MTU issues), creating N sockets per packet causes unnecessary FD churn and GC pressure. A single unconnected UDP socket with `WriteTo()` is the standard pattern for sending to multiple destinations.

**Alternatives considered**:
- Per-packet Dial: Simple but creates FD churn under load
- Connected socket per peer: Requires tracking/reconnecting when peer list changes
- Socket pool: Over-engineered for this use case

### 3. Packet injection via TUN device

**Decision**: Inject received ICMP packets into the local network stack by writing to a TUN device, which forces the packet through the kernel's `ip_input()` receive path.

**Rationale**: The kernel PMTU cache is updated by `icmp_unreach()` in the ICMP receive path (`ip_local_deliver()` → `icmp_rcv()` → `icmp_unreach()`). A raw socket `sendto()` goes through the **transmit** path (`ip_output()`) and will NOT trigger PMTU updates. Writing to a TUN device forces the packet through the receive path, correctly triggering the kernel to update route cache MTU entries.

**Alternatives considered**:
- Raw socket sendto: Goes through transmit path — does NOT trigger PMTU update (critical flaw)
- Netlink route manipulation (`ip route change ... mtu`): Fragile, race conditions, requires parsing ICMP to extract MTU and destination, doesn't use kernel's built-in PMTU logic
- NFQUEUE reinject on INPUT chain: Complex setup, requires additional iptables rules

### 4. Loop prevention: TUN interface exclusion from NFLOG + peer IP filtering

**Decision**: The NFLOG iptables rule MUST exclude the TUN device (`! -i pmtud0`) to prevent recapture of injected packets. As defense-in-depth, the nflog callback also auto-filters packets whose outer source IP matches any known peer node IP (from `cfg.PeerList`). An optional `--ignore-networks` flag provides static filtering for infrastructure networks.

**Rationale**: The primary loop prevention is structural — the NFLOG rule simply never sees packets on the TUN interface. This is the only reliable mechanism because injected packets preserve the original router source IP (not a peer IP), so source-based filtering alone cannot catch them. The peer IP filter and `--ignore-networks` provide additional safety layers but are not sufficient alone.

**Implementation**: The receiver names its TUN device `pmtud0` deterministically. The required iptables rule is: `iptables -A OUTPUT -p icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33`. The receiver also validates incoming UDP source IPs against the peer list before TUN injection.

**Alternatives considered**:
- TTL-based filtering (`-m ttl --ttl-gt 1`): Requires coordinating TTL rewrite in the receiver; fragile if TTL values change
- Packet marking with fwmark: Requires additional iptables MARK rule coordination
- Source IP filtering only: Doesn't work — injected packets keep original router IP, not peer IP

### 5. Peer list stores IPs directly (no MAC resolution)

**Decision**: The node reconciler stores peer IPs (from `node.Status.Addresses`) directly. No ARP resolution needed.

**Rationale**: With UDP unicast, we only need the peer's IP address. The kernel handles all L2/routing concerns. This eliminates the entire ARP package and the replication interface concept.

### 6. Single configurable UDP port

**Decision**: A single `--replication-port` flag (default: 4390) for both sending and receiving.

**Rationale**: Simple, predictable, easy to configure in firewalls/security groups. Port 4390 is unassigned by IANA.

### 7. Packet format: raw IP packet as UDP payload

**Decision**: UDP payload = raw IP packet (the full ICMP packet as captured by NFLOG), no framing header.

**Rationale**: UDP datagrams are already framed — adding a length prefix provides no benefit. The receiver validates the packet structure via `ParseICMPFragNeeded()` which performs bounds checking. Keeping the format simple means the receiver can inject the payload directly into the TUN device.

### 8. Linux-only build tags for injection code

**Decision**: The receiver's packet injection code (TUN device, syscall usage) is gated behind `//go:build linux` build tags. A stub file for other platforms allows compilation but returns an error at runtime.

**Rationale**: TUN devices and raw sockets are Linux-specific. The DaemonSet only runs on Linux nodes. Build tags allow `go build ./...` to succeed on macOS (developer machines) while keeping the Linux-specific code clean.

## Risks / Trade-offs

- **[UDP packet loss]** → ICMP replication is already best-effort; loss of a single replication message is acceptable since the ICMP will likely be retransmitted by the router. No mitigation needed beyond what exists today.
- **[Firewall blocking UDP port]** → Document the port requirement. Provide clear error logging when peers are unreachable.
- **[CAP_NET_RAW + CAP_NET_ADMIN required]** → TUN device creation requires CAP_NET_ADMIN, raw socket needs CAP_NET_RAW. Both are acceptable since the DaemonSet already runs privileged with host networking.
- **[No authentication]** → A malicious actor could send fake ICMP packets to the UDP port, causing incorrect PMTU entries. Mitigated by: (1) receiver validates UDP source IP against known peer list before injection, (2) port is only reachable within cluster network, (3) packet validation rejects non-ICMP-type-3-code-4 payloads. HMAC signing can be added in a future iteration for stronger guarantees.
- **[MTU of replication path]** → ICMP frag-needed packets are small (typically <100 bytes payload), so even the minimum 1280 MTU path is sufficient. No fragmentation concern.
- **[TUN device management]** → Need to create/teardown TUN device on startup/shutdown. TUN device is named `pmtud0` deterministically for iptables rule coordination. Use `github.com/songgao/water` for TUN creation. Device lifecycle tied to process lifetime.
- **[Replication loops]** → Defense-in-depth with three layers: (1) iptables NFLOG rule excludes TUN interface (`! -i pmtud0`) — primary mechanism, (2) receiver validates UDP source against peer list, (3) nflog callback auto-filters peer source IPs + optional `--ignore-networks`.
