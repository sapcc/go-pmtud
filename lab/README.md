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

## Prerequisites

- Docker (or Docker Desktop)
- [kind](https://kind.sigs.k8s.io/) v0.20+
- kubectl
- Go 1.22+ (to build go-pmtud image)

## Quick Start

```bash
cd lab/

# Go e2e test suite (recommended)
make e2e              # provision + test both backends + teardown
LAB_REUSE=1 make e2e-reuse      # skip provisioning (fast iteration)
LAB_KEEP=1 make e2e-keep        # keep lab after test (manual inspection)

# Manual lab management (legacy)
make pmtu-up          # Create networks, clusters, router, routes
make deploy           # Deploy go-pmtud and test workload
make test             # Generate traffic and verify PMTU replication
make observe-router   # Observe ICMP packets on the router
make status           # Check lab status
make down             # Tear down everything
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
| `pmtu-up` | Create networks, clusters, router, configure routes |
| `deploy` | Build and deploy go-pmtud + podinfo workload |
| `test` | Generate traffic and verify PMTU replication |
| `observe-router` | tcpdump ICMP frag-needed on router |
| `observe-node` | tcpdump ICMP on a cluster node (use `CLUSTER=a NODE=worker`) |
| `observe-replication` | tcpdump UDP 4390 replication traffic |
| `status` | Show state of all lab components |
| `down` | Remove all lab resources |

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

### UDP Backend (default)
Sends captured packets directly via UDP port 4390 to peer nodes. Fast and simple, suitable for lab and trusted networks.

```bash
make -C lab deploy  # Default: RELAY_BACKEND=udp
```

### CRD Backend
Routes packets through Kubernetes API server using custom PMTUNodeRelay resources. Useful in multi-namespace scenarios and Gardener clusters. Each packet is stored as a namespaced CRD object with automatic garbage collection.

```bash
RELAY_BACKEND=crd make -C lab deploy
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

## Running from Repo Root

From the repository root, you can use:

```bash
make -C lab pmtu-up
make -C lab deploy
make -C lab test
make -C lab down
```

## Known Limitations

- Docker Desktop on macOS may not fully honor custom network MTUs (Linux recommended)
- Kind uses kindnet CNI — cross-cluster pod routing requires NodePort or host networking
- Resource requirements: ~4GB RAM, 4 CPU cores (6 containers minimum)
- The lab does not test IPv6 Packet Too Big scenarios
