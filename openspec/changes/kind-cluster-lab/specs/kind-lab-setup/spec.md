<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## ADDED Requirements

### Requirement: Docker network topology creation
The lab SHALL create three Docker networks: `pmtud-net-a` (MTU 9000), `pmtud-net-b` (MTU 9000), and `pmtud-transit` (MTU 1500) with non-overlapping subnets.

#### Scenario: Networks created with correct MTUs
- **WHEN** `make setup` is run
- **THEN** three Docker networks exist with the specified MTUs and distinct subnets

#### Scenario: Idempotent creation
- **WHEN** `make setup` is run and networks already exist
- **THEN** the script completes without error (skip existing networks)

### Requirement: Kind cluster creation in separate networks
The lab SHALL create two Kind clusters (`pmtud-cluster-a` and `pmtud-cluster-b`) each with at least 2 worker nodes. Cluster-a nodes MUST be attached to `pmtud-net-a` and cluster-b nodes MUST be attached to `pmtud-net-b`.

#### Scenario: Two clusters created in separate L2 domains
- **WHEN** `make setup` completes
- **THEN** `kind get clusters` lists both `pmtud-cluster-a` and `pmtud-cluster-b`
- **AND** cluster-a node containers are on `pmtud-net-a`
- **AND** cluster-b node containers are on `pmtud-net-b`

#### Scenario: Worker node count
- **WHEN** `make setup` completes
- **THEN** each cluster has at least 2 worker nodes (for intra-cluster go-pmtud replication testing)

### Requirement: Router container with MTU mismatch
The lab SHALL run a router container connected to `pmtud-net-a`, `pmtud-net-b`, and `pmtud-transit`. The router MUST have IP forwarding enabled and MUST NOT fragment packets (respect DF bit). The transit-facing interfaces MUST have MTU 1500.

#### Scenario: Router forwards between networks
- **WHEN** a packet from a cluster-a node is destined for a cluster-b node
- **THEN** the router forwards it through the transit network

#### Scenario: Router generates ICMP fragmentation-needed
- **WHEN** a packet with DF bit set and size > 1500 bytes arrives at the router from `pmtud-net-a` destined for `pmtud-net-b`
- **THEN** the router drops the packet and sends ICMP type 3 code 4 (fragmentation needed, MTU=1500) back to the source

### Requirement: Static routes on Kind nodes
The lab SHALL configure static routes on all Kind cluster nodes so that traffic to the other cluster's pod/node CIDRs is routed through the router container.

#### Scenario: Cross-cluster reachability via router
- **WHEN** a pod in cluster-a sends traffic to a pod IP in cluster-b
- **THEN** the traffic transits through the router container

### Requirement: go-pmtud DaemonSet deployment
The lab SHALL deploy go-pmtud as a DaemonSet in both clusters using locally-built container images loaded via `kind load docker-image`.

#### Scenario: go-pmtud running on all nodes
- **WHEN** `make deploy-pmtud` completes
- **THEN** a go-pmtud pod is Running on every worker node in both clusters

#### Scenario: go-pmtud uses UDP replication
- **WHEN** go-pmtud pods are running
- **THEN** they are configured with `--replication-port=4390` and the TUN device `pmtud0` exists on each node

### Requirement: Single-command teardown
The lab SHALL provide `make teardown` that removes all clusters, networks, and the router container.

#### Scenario: Clean teardown
- **WHEN** `make teardown` is run
- **THEN** both Kind clusters are deleted, all three Docker networks are removed, and the router container is stopped and removed

#### Scenario: Teardown is idempotent
- **WHEN** `make teardown` is run and resources don't exist
- **THEN** the script completes without error
