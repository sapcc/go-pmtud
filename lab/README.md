<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# go-pmtud Local Lab

A reproducible Kind-based lab for testing go-pmtud ICMP replication. The suite runs three backend contexts in order — `legacy` (no `--relay-backend` flag, proving the upgrade path), `l2` (raw Ethernet), and `udp` (UDP unicast across L3 boundaries) — against the same single Kind cluster.

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
go-pmtud captures these via NFLOG and replicates to peer nodes via the configured relay backend. The suite redeploys the DaemonSet for each backend context.

## How It Works

The lab simulates a **cross-zone L3 boundary** within a single cluster:

- **Control-plane node:** Simulates a relay hop with reduced MTU (1280) via a dummy network interface
- **Worker nodes:** Full MTU (9000) for intra-zone communication
- **Trigger:** Packets from workers destined through the control-plane that exceed 1280 bytes with DF bit set prompt ICMP fragmentation-needed
- **Native capture:** The originating worker receives the ICMP directly via NFLOG
- **Relay test:** Peer workers rely on go-pmtud's relay to propagate the PMTU constraint; the suite redeploys for each backend (`legacy`, `l2`, `udp`) and asserts replication end-to-end for each

## Prerequisites

- Docker (or Docker Desktop)
- [kind](https://kind.sigs.k8s.io/) v0.20+
- kubectl
- Go 1.22+ (to build go-pmtud image)

## Quick Start

```bash
cd lab/

# Go e2e test suite (recommended)
make e2e           # provision + test (legacy, l2, udp) + teardown
make e2e-keep      # same but keep lab after for manual inspection

# Observability (manual inspection only)
make observe-node        # tcpdump ICMP packets on a worker node
make observe-replication # tcpdump UDP 4390 replication traffic
make status              # check lab status
```

## Go e2e Test Suite

The `e2e` and `e2e-keep` targets run the Go test suite with Ginkgo. The suite provisions the cluster, then runs three ordered backend contexts — `legacy`, `l2`, `udp` — and tears down.

**Default (full run):**
```bash
make e2e
```
Provisions lab, runs all e2e tests (20m timeout), tears down.

**Manual inspection (keep lab):**
```bash
make e2e-keep
```
Runs tests but keeps the lab running after completion — inspect clusters, logs, or state manually. Clean up with `make down`.

**Custom Ginkgo flags:**
```bash
make e2e GINKGO_FLAGS="-v --fail-fast"
make e2e GINKGO_FLAGS="-v --focus=legacy"
```

## Makefile Targets

| Target | Description |
|--------|-------------|
| `e2e` | Run full e2e suite: provision, test (legacy + l2 + udp), teardown |
| `e2e-keep` | Run tests but keep lab for manual inspection |
| `observe-node` | tcpdump ICMP frag-needed on a cluster node |
| `observe-replication` | tcpdump UDP 4390 replication traffic |
| `status` | Show lab component status |
| `down` | Delete the Kind cluster |

## Simulating the MTU Boundary

The lab uses the control-plane node as a relay hop with reduced MTU. Setup:

1. **Provision** creates a single Kind cluster with 1 control-plane and 2 workers
2. On the control-plane, a dummy interface (`pmtudlab0`, MTU 1280) simulates the L3 boundary
3. Routes on worker nodes direct traffic destined for external IPs through the control-plane
4. **Deploy** loads locally-built go-pmtud images and applies DaemonSet + RBAC
5. **Test** uses `ping -M do -s 1400` or sustained transfers to generate packets exceeding the control-plane's MTU
6. Control-plane interface sends ICMP fragmentation-needed back to the source
7. go-pmtud captures via NFLOG and replicates to peers via the active relay backend
8. For `udp`: peers inject via TUN device (`pmtud0`) → kernel PMTU cache updated. For `l2`: peers receive natively via raw Ethernet → kernel processes the frame directly

## Relay Backends

The suite tests all three backend scenarios against the same Kind cluster. Each `ginkgo.Context` redeploys the DaemonSet with the appropriate backend before running specs.

### L2 Backend (default in production)
Sends captured packets as raw Ethernet frames over the interface from `--iface_names`. Requires shared L2 adjacency (same VLAN). No TUN device is created. NFLOG rule captures on the primary interface:
```sh
iptables -t raw -A PREROUTING -i <iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group 33
```

### UDP Backend
Sends captured packets directly via UDP port 4390 to peer nodes. Works across L3 boundaries. Injects received packets via the `pmtud0` TUN device. NFLOG rule must exclude the TUN interface:
```sh
iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33
```

## Real-Cluster Validation

Beyond the Kind lab, go-pmtud can be validated on real Kubernetes clusters with actual MTU boundaries (cross-zone links, node MTU asymmetries, etc.). See [RUNBOOK-real-cluster.md](RUNBOOK-real-cluster.md) for step-by-step procedures covering:

- Prerequisites (≥2 nodes, MTU boundary or ability to create one)
- Deployment (image build, RBAC, DaemonSet)
- Trigger (existing or simulated MTU boundary)
- Observation (logs, metrics)
- Cleanup

## Known Limitations

- Docker Desktop on macOS may not fully honor custom network MTUs (Linux recommended)
- Kind uses kindnet CNI — cross-cluster pod routing requires NodePort or host networking
- Resource requirements: ~4GB RAM, 4 CPU cores (6 containers minimum)
- The lab does not test IPv6 Packet Too Big scenarios
