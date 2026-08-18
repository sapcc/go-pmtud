<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# go-pmtud Local Lab

A reproducible Kind-based lab for testing go-pmtud UDP replication across L3 boundaries with MTU mismatches.

## Architecture

```
┌─────────────────────────────────────────────────────────┐
│              Docker Host / Kubernetes Cluster           │
│                                                         │
│  ┌──────────────────────────────────────────────────┐   │
│  │  pmtud-cluster (1 CP + 2 Workers)                │   │
│  │  172.30.0.0/16, MTU: 9000                        │   │
│  │                                                  │   │
│  │  ┌──────────────┐  ┌──────────────────────────┐  │   │
│  │  │  Control     │  │                          │  │   │
│  │  │  Plane Node  │  │  (Acts as relay hop)     │  │   │
│  │  │  MTU: 1280   │  │  MTU: 1280 via dummy net │  │   │
│  │  └──────┬───────┘  │                          │  │   │
│  │         │          └──────────────────────────┘  │   │
│  │  ┌──────▼──────────┐  ┌──────────────────────┐   │   │
│  │  │  Worker-1      │  │  Worker-2             │   │   │
│  │  │  MTU: 9000     │  │  MTU: 9000            │   │   │
│  │  └────────────────┘  └──────────────────────-┘   │   │
│  └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────┘
```

Single-cluster topology with control-plane as the relay hop. Packets exceeding the control-plane's MTU (1280) with DF bit set trigger ICMP type 3 code 4 (fragmentation needed).
go-pmtud captures these via NFLOG and replicates to peer nodes via UDP port 4390 or Kubernetes CRD.

## How It Works

The lab simulates a **cross-zone L3 boundary** within a single cluster:

- **Control-plane node:** Simulates a relay hop with reduced MTU (1280) via a dummy network interface
- **Worker nodes:** Full MTU (9000) for intra-zone communication
- **Trigger:** Packets from workers destined through the control-plane that exceed 1280 bytes with DF bit set prompt ICMP fragmentation-needed
- **Native capture:** The originating worker receives the ICMP directly via NFLOG
- **Relay test:** Peer workers rely on go-pmtud's relay (UDP or CRD) to propagate the PMTU constraint, proving the transport works end-to-end without L2 reachability

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

## Simulating the MTU Boundary

The lab uses the control-plane node as a relay hop with reduced MTU. Setup:

1. **Provision** creates a single Kind cluster with 1 control-plane and 2 workers
2. On the control-plane, a dummy interface (`pmtudlab0`, MTU 1280) simulates the L3 boundary
3. Routes on worker nodes direct traffic destined for external IPs through the control-plane
4. **Deploy** loads locally-built go-pmtud images and applies DaemonSet + CRD/RBAC
5. **Test** uses `ping -M do -s 1400` or sustained transfers to generate packets exceeding the control-plane's MTU
6. Control-plane interface sends ICMP fragmentation-needed back to the source
7. go-pmtud captures via NFLOG, replicates to peers via UDP (or CRD)
8. Peers inject via TUN device → kernel PMTU cache updated

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

## Real-Cluster Validation

Beyond the Kind lab, go-pmtud can be validated on real Kubernetes clusters with actual MTU boundaries (cross-zone links, node MTU asymmetries, etc.). See [RUNBOOK-real-cluster.md](RUNBOOK-real-cluster.md) for step-by-step procedures covering:

- Prerequisites (≥2 nodes, MTU boundary or ability to create one)
- Deployment (image build, RBAC, CRD, DaemonSet)
- Trigger (existing or simulated MTU boundary)
- Observation (logs, metrics, PMTUNodeRelay objects)
- Cleanup

## Known Limitations

- Docker Desktop on macOS may not fully honor custom network MTUs (Linux recommended)
- Kind uses kindnet CNI — cross-cluster pod routing requires NodePort or host networking
- Resource requirements: ~4GB RAM, 4 CPU cores (6 containers minimum)
- The lab does not test IPv6 Packet Too Big scenarios
