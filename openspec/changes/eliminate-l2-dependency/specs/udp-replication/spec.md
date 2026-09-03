<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## ADDED Requirements

### Requirement: Pluggable relay transport interface
The system SHALL relay captured ICMP fragmentation-needed packets between nodes through a single `Relay` interface with a send responsibility (capture side) and a `Start(ctx)` receive/delivery responsibility (a `manager.Runnable`). Each backend SHALL own its own receive and delivery mechanism; the interface SHALL NOT require a shared injection callback. Packet capture (NFLOG) SHALL be transport-agnostic and usable by any backend.

#### Scenario: Capture side calls the active backend
- **WHEN** an ICMP type 3 code 4 packet is captured via NFLOG
- **THEN** the system calls the active backend's `Send` with the raw IP packet payload and the capturing node's name

#### Scenario: Backend owns its delivery
- **WHEN** the active backend's `Start` is run by the manager
- **THEN** the backend performs its own receive/delivery (L2: kernel-native on the replication interface; UDP: UDP listener + TUN injection) without a caller-supplied inject callback

### Requirement: Runtime backend selection with L2 default
The system SHALL select the relay backend via a `--relay-backend` flag that accepts `l2` and `udp` and defaults to `l2`. Any other value SHALL be rejected at startup with a clear error.

#### Scenario: Default backend is L2
- **WHEN** no `--relay-backend` flag is provided
- **THEN** the system uses the L2 (raw Ethernet) backend

#### Scenario: UDP backend selected explicitly
- **WHEN** `--relay-backend udp` is provided
- **THEN** the system uses the UDP backend

#### Scenario: Invalid backend rejected
- **WHEN** `--relay-backend` is set to any value other than `l2` or `udp`
- **THEN** the system fails to start with a clear error

### Requirement: Existing L2 deployments remain untouched on upgrade
The system SHALL retain the L2 transport and its configuration flags so that an existing L2 deployment upgraded to this version, with its manifest unchanged, continues to replicate via raw Ethernet frames.

#### Scenario: L2 flags still parse
- **WHEN** the binary is started with `--iface_names`, `--iface_mtu`, `--node-timeout-minutes`, and `--arp-timeout-seconds` and no `--relay-backend`
- **THEN** the flags are accepted and the system runs the L2 backend using the given replication interface

#### Scenario: Behavior-preserving image bump
- **WHEN** an existing L2 deployment is updated only to this image version (same flags)
- **THEN** replication continues over the L2 replication interface exactly as before, with no TUN device created

### Requirement: L2 backend sends via raw Ethernet frames
When the L2 backend is active, the system SHALL send each captured ICMP packet as a raw Ethernet frame (EtherType 0x0800, payload = raw IP packet) addressed to each peer node's MAC address on the configured replication interface.

#### Scenario: Frame sent to each peer MAC
- **WHEN** an ICMP type 3 code 4 packet is captured and there are peer nodes with resolved MAC addresses
- **THEN** the system writes an Ethernet frame carrying the raw IP payload to each peer's MAC on the replication interface

#### Scenario: Peer MAC resolved from peer IP via ARP
- **WHEN** the L2 backend needs a peer's MAC address for a peer IP in the peer list
- **THEN** the system resolves it via ARP on the replication interface and caches the result

### Requirement: L2 backend uses kernel-native receive (no TUN device)
When the L2 backend is active, the system SHALL NOT create a TUN device; received replicated frames SHALL be processed by the kernel natively on the replication interface.

#### Scenario: No TUN device in L2 mode
- **WHEN** the L2 backend's `Start` runs
- **THEN** no `pmtud0` TUN device is created, and the backend blocks until the context is done

### Requirement: UDP-based packet replication sending
When the UDP backend is active, the system SHALL send captured ICMP fragmentation-needed packets to all known peer nodes via UDP unicast on the configured replication port using a persistent unconnected UDP socket.

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
When the UDP backend is active, the system SHALL listen on the configured replication port for incoming UDP datagrams containing ICMP packet payloads from peer nodes.

#### Scenario: Receive and inject valid ICMP packet
- **WHEN** a UDP datagram arrives on the replication port containing a valid ICMP type 3 code 4 IP packet
- **THEN** the system injects the packet into the local network stack via TUN device so the kernel updates its PMTU cache

#### Scenario: Reject invalid payload
- **WHEN** a UDP datagram arrives that does not contain a valid ICMP type 3 code 4 packet
- **THEN** the system discards the payload, logs a warning, and increments an error metric

### Requirement: Packet injection via TUN device (UDP backend)
When the UDP backend is active, the system SHALL inject received ICMP packets by writing them to a TUN device, ensuring the kernel processes them through the IP receive path (ip_input → icmp_rcv → icmp_unreach) which updates the PMTU cache.

#### Scenario: Kernel PMTU cache updated after injection
- **WHEN** a valid ICMP frag-needed packet is injected via the TUN device
- **THEN** the kernel updates the route cache MTU for the inner source IP to the MTU value from the ICMP packet

#### Scenario: TUN device lifecycle
- **WHEN** the UDP backend starts
- **THEN** the system creates a TUN device for packet injection and removes it when the backend stops

### Requirement: TUN injector owned by the UDP backend
When the UDP backend is active, the system SHALL own exactly one `pmtud0` TUN device within the UDP backend, used for all injection, preserving the `! -i pmtud0` loop-prevention contract.

#### Scenario: Single TUN owner
- **WHEN** the UDP backend starts
- **THEN** exactly one `pmtud0` TUN device is created and used for all injection

### Requirement: Receiver validates UDP source against peer list
When the UDP backend is active, the system SHALL reject incoming UDP packets from IP addresses not in the current peer list, preventing unauthorized PMTU injection from arbitrary network actors.

#### Scenario: Packet from known peer accepted
- **WHEN** a UDP datagram arrives on the replication port from IP 10.0.1.2 and 10.0.1.2 is a registered peer node IP
- **THEN** the system processes and injects the packet

#### Scenario: Packet from unknown source rejected
- **WHEN** a UDP datagram arrives on the replication port from IP 203.0.113.99 and that IP is NOT a registered peer
- **THEN** the system discards the packet and logs a warning

### Requirement: TUN device named deterministically for iptables coordination
When the UDP backend is active, the system SHALL create the TUN device with a fixed name (`pmtud0`) so that iptables NFLOG rules can reliably exclude it to prevent replication loops.

#### Scenario: TUN device name
- **WHEN** the UDP backend starts and creates the TUN device
- **THEN** the device is named `pmtud0`

#### Scenario: NFLOG rule excludes TUN interface
- **WHEN** the iptables NFLOG rule includes `! -i pmtud0`
- **THEN** packets injected via the TUN device are never captured by NFLOG, preventing replication loops

### Requirement: Per-backend loop prevention via NFLOG rule coordination
The system SHALL rely on the operator-supplied NFLOG capture rule appropriate to the active backend for structural loop prevention, and SHALL log the required rule for the active backend at startup.

#### Scenario: L2 capture bound to primary interface
- **WHEN** the L2 backend is active and the NFLOG rule captures on the primary interface (`-i <iface>`)
- **THEN** replicated frames arriving on the replication interface are not recaptured

#### Scenario: UDP capture excludes TUN interface
- **WHEN** the UDP backend is active and the NFLOG rule excludes the TUN device (`! -i pmtud0`)
- **THEN** injected packets are not recaptured

### Requirement: Loop prevention via ignore-networks
The system SHALL accept an optional `--ignore-networks` flag (comma-separated CIDRs) and skip replication of ICMP packets whose outer source IP matches any of the specified networks, regardless of backend.

#### Scenario: Packet from ignored network not replicated
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 10.0.1.5 and `--ignore-networks` includes 10.0.1.0/24
- **THEN** the system does NOT replicate the packet to peers

#### Scenario: Packet from external source replicated normally
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 203.0.113.1 and `--ignore-networks` does NOT include that network
- **THEN** the system replicates the packet to all peers

### Requirement: Auto-derived peer IP filtering
The system SHALL automatically skip replication of ICMP packets whose outer source IP matches any known peer node IP from the current peer list, providing zero-config loop prevention, regardless of backend.

#### Scenario: Packet from peer node IP not replicated
- **WHEN** an ICMP frag-needed packet is captured via NFLOG with outer source IP 10.0.1.2 and 10.0.1.2 is a registered peer node IP
- **THEN** the system does NOT replicate the packet to peers

#### Scenario: Packet from non-peer source replicated
- **WHEN** an ICMP frag-needed packet is captured with outer source IP 203.0.113.1 and no peer has that IP
- **THEN** the system replicates the packet to all peers

### Requirement: Peer discovery uses node IP addresses
The system SHALL discover peer node IPs from the Kubernetes Node resource `Status.Addresses` field (preferring InternalIP) for both backends. The reconciler SHALL NOT perform MAC resolution; the L2 backend resolves MACs from peer IPs internally.

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
When the UDP backend is active, the system SHALL accept a `--replication-port` flag to configure the UDP port used for both sending and receiving replicated ICMP packets.

#### Scenario: Default port
- **WHEN** no `--replication-port` flag is provided
- **THEN** the system uses port 4390

#### Scenario: Custom port
- **WHEN** `--replication-port 5000` is provided
- **THEN** the system listens on UDP port 5000 and sends to peers on port 5000

### Requirement: L3 replication without L2 adjacency in UDP mode
When the UDP backend is active, the system SHALL NOT require a dedicated replication interface or L2 adjacency between nodes; all replication traffic SHALL use the node's default routable interface.

#### Scenario: Nodes on different L2 segments
- **WHEN** two nodes are on different L2 network segments but have IP connectivity and the UDP backend is active
- **THEN** ICMP packet replication works correctly between them via UDP

### Requirement: Packet format preservation
The system SHALL transmit the complete raw IP packet (as captured by NFLOG) as the transport payload without modification, for both backends.

#### Scenario: Payload integrity
- **WHEN** an ICMP packet is captured and sent to a peer
- **THEN** the transport payload contains the exact bytes of the original IP packet as captured by NFLOG

### Requirement: Ignore packets from peer nodes
The system SHALL NOT re-replicate packets that were received from other peer nodes (loop prevention).

#### Scenario: Received packet not re-broadcast
- **WHEN** the system receives a replicated ICMP packet from a peer
- **THEN** the system delivers it locally but does NOT send it to other peers

### Requirement: Linux-only injection with cross-platform compilation
The system SHALL use build tags to gate Linux-specific injection code (TUN device, syscall) in the UDP backend, allowing compilation on non-Linux platforms.

#### Scenario: Build on macOS
- **WHEN** `go build ./...` is run on macOS
- **THEN** compilation succeeds (injection code is stubbed out)

#### Scenario: Runtime on Linux
- **WHEN** the binary runs on Linux with the UDP backend
- **THEN** TUN device injection works correctly with CAP_NET_ADMIN
