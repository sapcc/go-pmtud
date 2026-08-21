<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## ADDED Requirements

### Requirement: UDP-based packet replication sending
The system SHALL send captured ICMP fragmentation-needed packets to all known peer nodes via UDP unicast on the configured replication port using a persistent unconnected UDP socket.

#### Scenario: Successful replication to all peers
- **WHEN** an ICMP type 3 code 4 packet is captured via NFLOG and there are 3 peer nodes registered
- **THEN** the system sends the full raw IP packet payload via UDP to each of the 3 peer node IPs on the replication port

#### Scenario: Peer unreachable does not block other peers
- **WHEN** an ICMP packet is captured and one peer is unreachable (UDP send fails)
- **THEN** the system logs the error, increments the error metric, and continues sending to remaining peers

#### Scenario: Persistent socket reuse
- **WHEN** multiple ICMP packets are captured in rapid succession
- **THEN** the system reuses the same UDP socket for all sends (no per-packet socket creation)

### Requirement: UDP listener receives replicated packets
The system SHALL listen on the configured replication port for incoming UDP datagrams containing ICMP packet payloads from peer nodes.

#### Scenario: Receive and inject valid ICMP packet
- **WHEN** a UDP datagram arrives on the replication port containing a valid ICMP type 3 code 4 IP packet
- **THEN** the system injects the packet into the local network stack via TUN device so the kernel updates its PMTU cache

#### Scenario: Reject invalid payload
- **WHEN** a UDP datagram arrives that does not contain a valid ICMP type 3 code 4 packet
- **THEN** the system discards the payload, logs a warning, and increments an error metric

### Requirement: Packet injection via TUN device
The system SHALL inject received ICMP packets by writing them to a TUN device, ensuring the kernel processes them through the IP receive path (ip_input → icmp_rcv → icmp_unreach) which updates the PMTU cache.

#### Scenario: Kernel PMTU cache updated after injection
- **WHEN** a valid ICMP frag-needed packet is injected via the TUN device
- **THEN** the kernel updates the route cache MTU for the inner source IP to the MTU value from the ICMP packet

#### Scenario: TUN device lifecycle
- **WHEN** the process starts
- **THEN** the system creates a TUN device for packet injection and removes it on shutdown

### Requirement: Receiver validates UDP source against peer list
The system SHALL reject incoming UDP packets from IP addresses not in the current peer list, preventing unauthorized PMTU injection from arbitrary network actors.

#### Scenario: Packet from known peer accepted
- **WHEN** a UDP datagram arrives on the replication port from IP 10.0.1.2 and 10.0.1.2 is a registered peer node IP
- **THEN** the system processes and injects the packet

#### Scenario: Packet from unknown source rejected
- **WHEN** a UDP datagram arrives on the replication port from IP 203.0.113.99 and that IP is NOT a registered peer
- **THEN** the system discards the packet and logs a warning

### Requirement: TUN device named deterministically for iptables coordination
The system SHALL create the TUN device with a fixed name (`pmtud0`) so that iptables NFLOG rules can reliably exclude it to prevent replication loops.

#### Scenario: TUN device name
- **WHEN** the receiver starts and creates the TUN device
- **THEN** the device is named `pmtud0`

#### Scenario: NFLOG rule excludes TUN interface
- **WHEN** the iptables NFLOG rule includes `! -i pmtud0`
- **THEN** packets injected via the TUN device are never captured by NFLOG, preventing replication loops

### Requirement: Loop prevention via ignore-networks
The system SHALL accept an optional `--ignore-networks` flag (comma-separated CIDRs) and skip replication of ICMP packets whose outer source IP matches any of the specified networks.

#### Scenario: Packet from ignored network not replicated
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 10.0.1.5 and `--ignore-networks` includes 10.0.1.0/24
- **THEN** the system does NOT replicate the packet to peers

#### Scenario: Packet from external source replicated normally
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 203.0.113.1 and `--ignore-networks` does NOT include that network
- **THEN** the system replicates the packet to all peers

### Requirement: Auto-derived peer IP filtering
The system SHALL automatically skip replication of ICMP packets whose outer source IP matches any known peer node IP from the current peer list, providing zero-config loop prevention.

#### Scenario: Packet from peer node IP not replicated
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 10.0.1.2 and 10.0.1.2 is a registered peer node IP
- **THEN** the system does NOT replicate the packet to peers

#### Scenario: Packet from non-peer source replicated
- **WHEN** an ICMP frag-needed packet is captured with outer source IP 203.0.113.1 and no peer has that IP
- **THEN** the system replicates the packet to all peers

### Requirement: Peer discovery uses node IP addresses
The system SHALL discover peer node IPs from the Kubernetes Node resource `Status.Addresses` field (preferring InternalIP) without requiring ARP resolution or L2 adjacency.

#### Scenario: Node added to cluster
- **WHEN** a new Node resource appears in the Kubernetes API with an InternalIP address
- **THEN** the system adds the node's IP to the peer list for replication

#### Scenario: Node removed from cluster
- **WHEN** a Node resource is deleted from the Kubernetes API
- **THEN** the system removes the node's IP from the peer list

#### Scenario: Own node is excluded
- **WHEN** the node reconciler processes the local node (matching --nodename)
- **THEN** the system does NOT add it to the peer list

### Requirement: Configurable replication port
The system SHALL accept a `--replication-port` flag to configure the UDP port used for both sending and receiving replicated ICMP packets.

#### Scenario: Default port
- **WHEN** no `--replication-port` flag is provided
- **THEN** the system uses port 4390

#### Scenario: Custom port
- **WHEN** `--replication-port 5000` is provided
- **THEN** the system listens on UDP port 5000 and sends to peers on port 5000

### Requirement: No Layer 2 interface dependency
The system SHALL NOT require a dedicated replication interface or L2 adjacency between nodes. All replication traffic MUST use the node's default routable interface.

#### Scenario: Nodes on different L2 segments
- **WHEN** two nodes are on different L2 network segments but have IP connectivity
- **THEN** ICMP packet replication works correctly between them via UDP

### Requirement: Packet format preservation
The system SHALL transmit the complete raw IP packet (as captured by NFLOG) as the UDP payload without modification.

#### Scenario: Payload integrity
- **WHEN** an ICMP packet is captured and sent to a peer
- **THEN** the UDP payload contains the exact bytes of the original IP packet as captured by NFLOG

### Requirement: Ignore packets from peer nodes
The system SHALL NOT re-replicate packets that were received from other peer nodes (loop prevention).

#### Scenario: Received packet not re-broadcast
- **WHEN** the system receives an ICMP packet via the UDP listener from a peer
- **THEN** the system injects it locally but does NOT send it to other peers

### Requirement: Linux-only injection with cross-platform compilation
The system SHALL use build tags to gate Linux-specific injection code (TUN device, syscall), allowing compilation on non-Linux platforms.

#### Scenario: Build on macOS
- **WHEN** `go build ./...` is run on macOS
- **THEN** compilation succeeds (injection code is stubbed out)

#### Scenario: Runtime on Linux
- **WHEN** the binary runs on Linux
- **THEN** TUN device injection works correctly with CAP_NET_ADMIN
