<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# go-pmtud Local Lab

A reproducible Kind-based lab for testing go-pmtud UDP replication across L3 boundaries with MTU mismatches.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Docker Host                                    │
│                                                                         │
│  ┌─────────────────────┐         ┌─────────────────────┐                │
│  │  pmtud-net-a        │         │  pmtud-net-b        │                │
│  │  172.30.0.0/16      │         │  172.31.0.0/16      │                │
│  │  MTU: 9000          │         │  MTU: 9000          │                │
│  │                     │         │                     │                │
│  │  ┌───────────────┐  │         │  ┌───────────────┐  │                │
│  │  │ pmtud-cluster-a│  │        │  │ pmtud-cluster-b│ │                │
│  │  │ (1 CP + 2 W)  │  │         │  │ (1 CP + 2 W)  │  │                │
│  │  └───────────────┘  │         │  └───────────────┘  │                │
│  └──────────┬──────────┘         └──────────┬──────────┘                │
│             │                               │                           │
│             │    ┌───────────────────┐      │                           │
│             └────┤  pmtud-router     ├──────┘                           │
│                  │  (Alpine + fwd)   │                                  │
│                  └────────┬──────────┘                                  │
│                           │                                             │
│                  ┌────────┴──────────┐                                  │
│                  │  pmtud-transit    │                                  │
│                  │  172.32.0.0/24    │                                  │
│                  │  MTU: 1500        │                                  │
│                  └──────────────────-┘                                  │
└─────────────────────────────────────────────────────────────────────────┘
```

Traffic from cluster-a → cluster-b traverses the router via the transit network (MTU 1500).
Packets >1500 bytes with DF bit set trigger ICMP type 3 code 4 (fragmentation needed).
go-pmtud captures these via NFLOG and replicates to peer nodes via UDP port 4390.

## Why Two Clusters?

The two Kind clusters mimic a **multi-availability-zone (AZ) scenario**: nodes spread across AZs with in-AZ MTU 9000 (fast) but cross-AZ links capped at MTU 1500 (real-world networking constraint).

**Why not two worker nodes in one cluster?** Kind pins all nodes of a single cluster to one docker bridge, guaranteeing full node↔node L2 reachability. You cannot tell Kind "put worker1 on network-a, worker2 on network-b" — it will add both to the same bridge. So to place node groups across **separate L2 domains joined only by an L3 router**, you need two clusters.

**What actually cuts L2:** The three docker networks (`net-a`, `net-b`, `transit`) are separate L2 domains. The router sits on all three and provides the only L3 path between them. Its `net-b` interface is clamped to MTU 1500, creating the bottleneck that generates ICMP fragmentation-needed.

**The relay test:** Each cluster has 2 workers. The originating worker catches the ICMP natively; peers in the same cluster rely on go-pmtud's relay (UDP or CRD) to propagate the PMTU constraint — proving the transport works end to end without requiring L2 reachability.

## Prerequisites

- Docker (or Docker Desktop)
- [kind](https://kind.sigs.k8s.io/) v0.20+
- kubectl
- Go 1.22+ (to build go-pmtud image)

## Quick Start

```bash
cd lab/

# Go e2e test suite (recommended)
make e2e                          # provision + test both backends + teardown
LAB_REUSE=1 make e2e-reuse       # skip provisioning (fast iteration)
LAB_KEEP=1 make e2e-keep         # keep lab after test (manual inspection)

# Observability (manual inspection only)
make observe-router              # tcpdump ICMP packets on router
make status                       # check lab status
```

## Go e2e Test Suite

The `e2e`, `e2e-reuse`, and `e2e-keep` targets run the Go test suite with Ginkgo. They handle provisioning, testing both UDP and CRD relay backends, and cleanup.

**Default (full run):**
```bash
make e2e
```
Provisions lab, runs all e2e tests (20m timeout), tears down.

**Fast iteration (reuse lab):**
```bash
LAB_REUSE=1 make e2e-reuse
```
Skips provisioning, reuses existing lab — useful when iterating on test logic.

**Manual inspection (keep lab):**
```bash
LAB_KEEP=1 make e2e-keep
```
Runs tests but keeps the lab running after completion — inspect clusters, logs, or state manually. Clean up with `make down`.

**Custom Ginkgo flags:**
```bash
make e2e GINKGO_FLAGS="-v --fail-fast"
LAB_REUSE=1 make e2e-reuse GINKGO_FLAGS="-v --focus=UDP"
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `e2e` | Run full e2e suite: provision, test both backends, teardown |
| `e2e-reuse` | Skip provisioning, reuse existing lab (fast iteration) |
| `e2e-keep` | Run tests but keep lab for manual inspection |
| `observe-router` | tcpdump ICMP packets on router |
| `status` | Show lab component status |

## How It Works

1. **Setup** creates two Docker networks (MTU 9000) and a transit network (MTU 1500)
2. Two Kind clusters are created, one per network
3. A router container bridges the networks — its transit interface has MTU 1500
4. Static routes on Kind nodes direct cross-cluster traffic through the router
5. **Deploy** loads locally-built go-pmtud images and applies DaemonSet + podinfo
6. **Test** generates large TCP transfers (DF set) that exceed 1500 bytes
7. The router sends ICMP fragmentation-needed back to the source
8. go-pmtud captures via NFLOG, replicates to peers via UDP (or CRD)
9. Peers inject via TUN device → kernel PMTU cache updated

## Relay Backends

go-pmtud supports two inter-node relay backends for distributing ICMP frag-needed packets:

### CRD Backend (default)
Routes packets through Kubernetes API server using custom PMTUNodeRelay resources. Useful in multi-namespace scenarios and Gardener clusters. Each packet is stored as a namespaced CRD object with automatic garbage collection.

### UDP Backend
Sends captured packets directly via UDP port 4390 to peer nodes. Fast and simple, suitable for lab and trusted networks. To use:

```bash
RELAY_BACKEND=udp make -C lab deploy
```

#### CRD Backend Fast-Path: 4-Namespace NetNS Isolation

In large clusters, PMTU cache coherence across 4+ separate namespaces with distinct network namespaces (e.g., different tenant CNI stacks) follows this fast-path:

1. Each node's go-pmtud daemon subscribes to PMTUNodeRelay events in its configured relay namespace.
2. When a frag-needed packet is captured on node-a, a PMTUNodeRelay resource is created (or updated) with the packet payload and source node annotation.
3. The Kubernetes etcd acts as the signaling plane — all node informers watch the same namespace, triggering inject callbacks immediately.
4. The local TUN device injects the packet back into the kernel, updating the PMTU cache.
5. The resource is deleted after injection (or expires after TTL), ensuring minimal API server load.

**Advantages:**
- No direct node-to-node connectivity needed
- Leverages existing Kubernetes RBAC and audit trails
- Works transparently across NetNS boundaries

**Limitations:**
- Higher latency than UDP (API round-trip vs. L3 UDP)
- API server becomes a throughput bottleneck under high-loss scenarios
- Not suitable for non-Kubernetes environments

## Known Limitations

- Docker Desktop on macOS may not fully honor custom network MTUs (Linux recommended)
- Kind uses kindnet CNI — cross-cluster pod routing requires NodePort or host networking
- Resource requirements: ~4GB RAM, 4 CPU cores (6 containers minimum)
- The lab does not test IPv6 Packet Too Big scenarios
