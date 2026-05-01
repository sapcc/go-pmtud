<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Context

go-pmtud replicates ICMP fragmentation-needed packets between Kubernetes nodes. The `feature/eliminate-l2-dependency` branch replaces L2 broadcast with UDP unicast, enabling replication across L3 boundaries. Validating this requires an environment where:
1. Two sets of nodes exist in different L3 networks
2. A path between them has a lower MTU than the source network
3. Traffic large enough to exceed the path MTU is generated
4. ICMP type 3/4 packets and their replication can be observed

Currently testing is done manually on QA infrastructure (adjusting router MTUs, SSHing to nodes). This is slow, non-reproducible, and ties up shared resources.

## Goals / Non-Goals

**Goals:**
- Reproducible local lab that simulates L3-separated clusters with MTU mismatch
- Two Kind clusters in separate Docker networks connected by a router container
- Configurable MTU on the router's interfaces to simulate path MTU reduction
- Deploy go-pmtud DaemonSet in the clusters
- Provide traffic generation and observation tooling
- Single-command setup and teardown
- Works on Linux and macOS (Docker Desktop) developer machines

**Non-Goals:**
- Full CI pipeline integration (future work, document how but don't implement)
- Testing IPv6 Packet Too Big scenarios
- Performance/load testing at scale
- Replacing existing unit tests
- Multi-architecture support (arm64 lab images)

## Decisions

### 1. Two Kind clusters in separate Docker networks

**Decision**: Create two Docker networks (`pmtud-net-a` with MTU 9000, `pmtud-net-b` with MTU 9000) and a third "transit" network (`pmtud-transit`) with MTU 1500. Each Kind cluster attaches to its respective network. A router container bridges them via the transit network.

**Rationale**: Kind clusters use Docker networks for node connectivity. Separate networks ensure no L2 adjacency. The transit network with lower MTU simulates the real-world scenario where a WAN/backbone path has a smaller MTU than the local LAN.

**Alternatives considered**:
- Single Kind cluster with network policies: Doesn't simulate L3 separation or MTU mismatch
- VMs (Vagrant): Heavier, slower to set up, more dependencies
- Network namespaces without Kind: Loses Kubernetes context, can't test DaemonSet deployment

### 2. Router container with iptables forwarding

**Decision**: Use an Alpine-based container with IP forwarding enabled, connected to all three networks. Its interface on `pmtud-transit` has MTU 1500 while interfaces on `pmtud-net-a` and `pmtud-net-b` have MTU 9000. The router does NOT fragment (DF bit respected), causing it to generate ICMP fragmentation-needed messages.

**Rationale**: This mirrors real router behavior — when a packet with DF bit arrives on a high-MTU interface and must egress on a low-MTU interface, the router drops it and sends ICMP type 3/4. Using a container keeps everything in Docker, no host network changes needed.

**Alternatives considered**:
- tc/netem on Kind nodes: Can simulate loss/delay but not MTU reduction at a router hop
- Docker network MTU alone: Kind nodes would just use the lower MTU from the start, no ICMP generated

### 3. Podinfo as test workload

**Decision**: Deploy podinfo in both clusters serving a large static file (e.g., 1MB). Cross-cluster curl requests trigger TCP segments larger than the transit MTU, producing ICMP fragmentation-needed.

**Rationale**: podinfo is lightweight, well-known, and can serve arbitrary files. The user's existing QA test already uses podinfo for this purpose. TCP with DF bit set naturally triggers PMTU discovery.

### 4. Script-based setup (Makefile + shell scripts)

**Decision**: Provide a `lab/Makefile` with targets: `setup`, `teardown`, `status`, `test`, `logs`. Shell scripts under `lab/scripts/` handle Docker network creation, Kind cluster creation, router container setup, and go-pmtud deployment.

**Rationale**: Makefile targets are discoverable and composable. Shell scripts are debuggable and don't add dependencies. Developers can run individual steps or the full setup.

**Alternatives considered**:
- Docker Compose: Doesn't manage Kind clusters natively
- Terraform: Overkill for local Docker resources
- Go test harness: Higher barrier to entry, harder to debug interactively

### 5. Tcpdump observation helpers

**Decision**: Provide wrapper scripts that exec into cluster nodes or the router container to run tcpdump with pre-built filters for ICMP type 3/4 and go-pmtud replication traffic (UDP port 4390).

**Rationale**: The user's manual test workflow involves specific tcpdump filters. Encoding these in scripts makes observation repeatable and documents the expected packet patterns.

### 6. Lab directory structure

**Decision**:
```
lab/
├── Makefile
├── README.md
├── configs/
│   ├── kind-cluster-a.yaml
│   ├── kind-cluster-b.yaml
│   └── router/
│       └── Dockerfile
├── scripts/
│   ├── setup.sh
│   ├── teardown.sh
│   ├── deploy-pmtud.sh
│   ├── generate-traffic.sh
│   └── observe.sh
└── manifests/
    ├── podinfo.yaml
    └── pmtud-daemonset.yaml
```

**Rationale**: Clear separation of concerns. Configs are declarative, scripts are imperative, manifests are deployable Kubernetes resources.

## Risks / Trade-offs

- **[Docker Desktop MTU limitations]** → Docker Desktop on macOS may not honor custom network MTUs in all cases. Document known limitations and provide a Linux-first recommendation. Mitigation: test on Linux CI runners.
- **[Kind networking complexity]** → Kind uses kindnet CNI by default which may interfere with cross-cluster routing. Mitigation: configure static routes in the router container and use NodePort/HostNetwork for cross-cluster traffic.
- **[Resource consumption]** → Two Kind clusters + router = ~6 containers minimum. Acceptable for developer machines but document minimum resource requirements (4GB RAM, 4 CPU).
- **[go-pmtud image availability]** → Lab needs a go-pmtud container image. Use `kind load docker-image` to load locally-built images. Document the build-then-load workflow.
- **[Privileged containers required]** → go-pmtud needs CAP_NET_RAW, CAP_NET_ADMIN, and host networking. Kind supports this via extra mounts and security context.
- **[Cross-cluster routing]** → Pods in cluster-a need to reach pods in cluster-b through the router. Requires static routes on Kind nodes pointing to the router. Mitigation: setup script adds routes automatically.
