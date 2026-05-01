<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# go-pmtud Local Lab

A reproducible Kind-based lab for testing go-pmtud UDP replication across L3 boundaries with MTU mismatches.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────┐
│                          Docker Host                                     │
│                                                                         │
│  ┌─────────────────────┐         ┌─────────────────────┐               │
│  │  pmtud-net-a        │         │  pmtud-net-b        │               │
│  │  172.30.0.0/16      │         │  172.31.0.0/16      │               │
│  │  MTU: 9000          │         │  MTU: 9000          │               │
│  │                     │         │                     │               │
│  │  ┌───────────────┐  │         │  ┌───────────────┐  │               │
│  │  │ pmtud-cluster-a│  │         │  │ pmtud-cluster-b│  │               │
│  │  │ (1 CP + 2 W)  │  │         │  │ (1 CP + 2 W)  │  │               │
│  │  └───────────────┘  │         │  └───────────────┘  │               │
│  └──────────┬──────────┘         └──────────┬──────────┘               │
│             │                               │                           │
│             │    ┌───────────────────┐      │                           │
│             └────┤  pmtud-router     ├──────┘                           │
│                  │  (Alpine + fwd)   │                                  │
│                  └────────┬──────────┘                                  │
│                           │                                             │
│                  ┌────────┴──────────┐                                  │
│                  │  pmtud-transit    │                                  │
│                  │  172.32.0.0/24   │                                  │
│                  │  MTU: 1500       │                                  │
│                  └──────────────────┘                                  │
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

# Full setup (networks + clusters + router + routes)
make pmtu-up

# Deploy go-pmtud and test workload
make deploy

# Run end-to-end validation
make test

# Observe ICMP packets on the router
make observe-router

# Check lab status
make status

# Tear down everything
make down
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
8. go-pmtud captures via NFLOG, replicates to peers via UDP
9. Peers inject via TUN device → kernel PMTU cache updated

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
