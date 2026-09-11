<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Context

go-pmtud replicates ICMP fragmentation-needed packets between Kubernetes cluster nodes so that every node's kernel PMTU cache learns about path-MTU limits any node discovers. The original mechanism uses raw Ethernet frames over a dedicated Layer 2 replication interface. Each node:
1. Discovers peer nodes via the Kubernetes API
2. Resolves peer MAC addresses via ARP on the replication interface
3. Captures ICMP type 3 code 4 packets via NFLOG
4. Constructs raw Ethernet frames and sends them to each peer's MAC address

This L2 mechanism requires all nodes to share a common L2 segment, which is increasingly incompatible with modern cluster topologies (multiple racks, availability zones, or regions).

This change adds an alternative **UDP unicast** transport that works across L3 boundaries, and puts both transports behind a pluggable `Relay` interface selected by `--relay-backend`.

**Coexistence requirement**: go-pmtud is already deployed in production landscapes that rely on L2 connectivity via VLANs. Those installations MUST keep working unchanged when this version is rolled out. Therefore the L2 backend is **retained**, the UDP (L3) backend is **optional**, and `--relay-backend` **defaults to `l2`**. Bumping to this image without changing flags keeps the existing L2 behavior. Operators opt into the L3 transport explicitly with `--relay-backend=udp`.

## Goals / Non-Goals

**Goals:**
- Make L2 adjacency **optional** rather than mandatory, by offering a UDP transport that works across L3 boundaries
- Preserve the existing L2 (raw Ethernet) transport unchanged so current production installs stay untouched on upgrade
- Default to the L2 backend so an image bump is behavior-preserving for existing deployments
- Use UDP unicast over the node's default interface for the L3 transport
- Maintain the same ICMP replication semantics (all peers get the packet) for both backends
- For UDP, ensure injected packets actually trigger kernel PMTU cache updates
- Prevent replication loops in both backends

**Non-Goals:**
- Changing how ICMP packets are captured (NFLOG stays for both backends)
- Removing or deprecating the L2 backend (it remains a first-class, default option)
- Encryption or authentication of replicated packets (can be added later)
- Supporting IPv6 ICMP (Packet Too Big) — future work
- Implementing reliability/retransmission for UDP (ICMP replication is best-effort by nature)

## Decisions

### 1. Pluggable relay transport seam with two backends

**Decision**: The replication transport sits behind a `Relay` interface (`internal/relay`). Each backend owns both its send path and its own receive/delivery path; there is no shared injector callback. A `--relay-backend` flag selects the backend; it accepts `l2` and `udp` and **defaults to `l2`**.

```go
type RelayPacket struct {
    Payload []byte // raw IP packet (ICMP 3/4) as captured by NFLOG
    SrcNode string // cfg.NodeName of the capturing node
}

type Relay interface {
    // Send relays a captured packet to peer node(s). Called from the nflog
    // callback (hot path) — implementations must be non-blocking or fast.
    Send(ctx context.Context, pkt RelayPacket) error

    // Start runs the receive/delivery loop until ctx is done (manager.Runnable).
    // Each backend owns its own receive and injection mechanism.
    Start(ctx context.Context) error
}
```

**Rationale**: The two transports differ not only in wire format but in how received packets reach the kernel receive path (see Decision 3). Forcing a shared `inject` callback onto the interface only fits the UDP backend; the L2 backend has no application-level receive step at all. Making each backend own its delivery keeps the interface honest and lets `Start` be a plain `manager.Runnable`. NFLOG capture (`internal/nflog`) stays transport-agnostic — it only calls `Send`.

**Alternatives considered**:
- Keep the earlier `Start(ctx, inject func([]byte) error)` signature with a daemon-owned shared TUN injector: clean when UDP was the only backend, but the injector is meaningless for L2, so the shared-injector abstraction leaks. Injector ownership moves into the UDP backend instead (Decision 3).
- Inline both transports directly in the nflog controller: couples capture to transport and forecloses future backends.

### 2. UDP unicast for the L3 transport

**Decision**: The UDP backend uses a single persistent unconnected `*net.UDPConn` for sending, calling `WriteTo()` per peer instead of `Dial()`+`Write()`+`Close()` per peer per packet.

**Rationale**: UDP is the simplest IP-level transport that works across L3 boundaries — no connection state, no handshake, matching the fire-and-forget nature of ICMP replication. The nflog callback is a hot path; under burst traffic, creating N sockets per packet causes FD churn and GC pressure. A single unconnected socket with `WriteTo()` is the standard multi-destination pattern.

**Alternatives considered**:
- TCP / gRPC / HTTP: overkill, connection-oriented or serialization-heavy for forwarding raw packets
- IP-in-IP tunneling / raw IP sockets: require `CAP_NET_RAW` and add complexity for no benefit over UDP
- Per-packet Dial or connected socket per peer: FD churn, or reconnection bookkeeping when the peer list changes

### 3. Receive/delivery paths differ by backend

The kernel PMTU cache is updated by `icmp_unreach()` on the ICMP **receive** path (`ip_local_deliver()` → `icmp_rcv()` → `icmp_unreach()`). A packet must traverse that receive path to update the cache. The two backends reach it differently:

**L2 backend**: The sender writes a raw Ethernet frame addressed to the peer's real interface MAC on the shared replication interface. On the receiving node the kernel accepts the frame **natively** on that interface and the inner IP/ICMP packet flows up the normal receive path. There is **no application-level receive loop and no TUN device** in L2 mode. `Start` sets up the send-side socket/ARP state and then blocks until `ctx` is done.

**UDP backend**: `Send` goes through the transmit path on the sender, which does **not** update the receiver's PMTU cache by itself. So the UDP backend runs a UDP listener and injects each received payload into a **TUN device** (`pmtud0`), forcing the packet through the kernel receive path. The TUN device is a **private detail of the UDP backend** — it is created in the UDP backend's `Start` and torn down when `Start` returns. The injector code (`internal/relay/udp`, `//go:build linux`) uses a custom netlink injector via raw `ioctl`; a stub for other platforms allows `go build ./...` on developer machines and returns an error at runtime.

**Alternatives considered (UDP injection)**:
- Raw socket `sendto`: goes through the transmit path — does NOT trigger PMTU update (critical flaw)
- Netlink route manipulation (`ip route change … mtu`): fragile, race-prone, bypasses the kernel's built-in PMTU logic
- NFQUEUE reinject: complex setup, extra iptables rules

### 4. Peer list stores IPs; L2 layers ARP on top

**Decision**: The node reconciler stores peer **IPs** (from `node.Status.Addresses`, preferring InternalIP) in `cfg.PeerList` (`map[string]string`, nodeName→IP) for **both** backends. It performs no MAC resolution. The L2 backend resolves peer MACs from those IPs via ARP on the replication interface, maintaining its own MAC cache.

**Rationale**: Keeping the reconciler transport-agnostic (IP-only) means one code path feeds both backends. L2-specific concerns (ARP, MAC cache, replication interface) stay confined to the L2 backend (`internal/relay/l2`, reusing the resurrected `internal/arp` package). This is cleaner than the original design where the reconciler itself resolved and stored MACs.

### 5. Loop prevention is per-backend

**Decision**: Both backends prevent recapture of replicated packets, but by different mechanisms, coordinated with the operator-supplied iptables NFLOG rule:

- **L2**: structural. The NFLOG capture rule is bound to the primary interface (`-i <iface>`), while replicated frames arrive on the *replication* interface, so they are never recaptured. Rule: `iptables -t raw -A PREROUTING -i <iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group <group>`.
- **UDP**: the NFLOG rule MUST exclude the TUN device (`! -i pmtud0`) so injected packets are never recaptured. Rule: `iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group <group>`.

As defense-in-depth for both backends, the nflog callback auto-filters packets whose outer source IP matches a known peer IP (from `cfg.PeerList`), and an optional `--ignore-networks` flag statically filters infrastructure CIDRs. These are additional layers, not sufficient alone (injected/replicated packets preserve the original router source IP, not a peer IP).

**Alternatives considered**:
- TTL-based filtering / fwmark marking: require extra coordination and are fragile
- Source-IP filtering only: insufficient — replicated packets keep the original router IP

### 6. Configuration and flags: additive, L2 preserved

**Decision**: The L2 flags and config fields are **retained** so existing manifests parse unchanged: `--iface_names`, `--iface_mtu`, `--node-timeout-minutes` (ARP cache timeout), `--arp-timeout-seconds` (ARP request timeout), and the corresponding `Config` fields (`InterfaceNames`/`ReplicationInterface`, `InterfaceMtu`, ARP timeouts, MAC cache). The UDP backend adds `--replication-port` (default 4390, used only when `relay-backend=udp`). `--relay-backend` defaults to `l2`.

**Rationale**: True "untouched on upgrade" requires both the L2 code path and its flags to exist with unchanged defaults. Port 4390 is unassigned by IANA and is only consulted in UDP mode.

### 7. Packet format: raw IP packet as payload (both backends)

**Decision**: The replicated payload is the full raw IP packet (the ICMP packet as captured by NFLOG), with no added framing. L2 carries it as the Ethernet frame payload; UDP carries it as the datagram payload. The receiver validates structure via `ParseICMPFragNeeded()` (bounds-checked) before delivery.

**Rationale**: UDP datagrams and Ethernet frames are already framed; a length prefix adds nothing. Keeping the format identical means the two backends share capture and validation logic.

### 8. Capabilities

**Decision**: L2 mode requires `CAP_NET_RAW` (raw packet socket). UDP mode requires `CAP_NET_RAW` plus `CAP_NET_ADMIN` (TUN device creation). Both are acceptable since the DaemonSet already runs privileged with host networking.

## Risks / Trade-offs

- **[UDP packet loss]** → ICMP replication is best-effort; loss of a single replication message is acceptable (the ICMP will likely be retransmitted by the router). No mitigation beyond what exists today.
- **[Default choice masks the L3 feature]** → Defaulting to `l2` means new adopters must explicitly set `--relay-backend=udp` and apply the `! -i pmtud0` NFLOG rule. Accepted trade-off: behavior-preserving upgrades for existing L2 installs outweigh discoverability of the new backend. Documented prominently in the README/runbook.
- **[Firewall blocking UDP port]** → Document the port requirement (UDP mode only). Clear error logging when peers are unreachable.
- **[Wrong iptables rule for the selected backend]** → The two backends need different NFLOG rules (`-i <iface>` vs `! -i pmtud0`). The daemon logs the required rule for the active backend at startup; docs describe both.
- **[No authentication]** → A malicious actor could send fake ICMP packets. Mitigated for UDP by: (1) receiver validates UDP source IP against the peer list before injection, (2) the port is only reachable within the cluster network, (3) payload validation rejects non-ICMP-3/4. L2 is bounded to the shared segment. HMAC signing can be added later.
- **[MTU of replication path]** → ICMP frag-needed packets are small (<100 bytes payload), so even the 1280 minimum-MTU path suffices. No fragmentation concern.
- **[TUN device management (UDP only)]** → The UDP backend creates/tears down `pmtud0` on `Start`/return; lifecycle tied to the backend, not the whole process. L2 mode creates no TUN device.
- **[Retained ARP/L2 dependency]** → Reintroduces `github.com/mdlayher/arp`, `ethernet`, and `packet`. Accepted: they are only exercised in L2 mode and are required to keep existing installs working.
