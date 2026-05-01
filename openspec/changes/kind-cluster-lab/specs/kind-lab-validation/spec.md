<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## ADDED Requirements

### Requirement: Traffic generation that triggers PMTU discovery
The lab SHALL provide a script/target that generates TCP traffic with DF bit set from cluster-a to cluster-b exceeding 1500 bytes, triggering ICMP fragmentation-needed from the router.

#### Scenario: Large file download triggers ICMP
- **WHEN** `make generate-traffic` is run
- **THEN** a curl/wget from a cluster-a pod downloads a large file (>1MB) from a podinfo service in cluster-b via NodePort
- **AND** the router generates ICMP type 3 code 4 packets

#### Scenario: Podinfo serves test payload
- **WHEN** podinfo is deployed in cluster-b
- **THEN** it serves a test file at a known endpoint that is larger than the transit MTU

### Requirement: ICMP observation tooling
The lab SHALL provide scripts to observe ICMP type 3 code 4 packets on cluster nodes and the router container using tcpdump.

#### Scenario: Observe ICMP on router
- **WHEN** `make observe-router` is run
- **THEN** tcpdump runs on the router showing ICMP fragmentation-needed packets with filter `icmp and icmp[0] == 3 and icmp[1] == 4`

#### Scenario: Observe ICMP on cluster node
- **WHEN** `make observe-node CLUSTER=a NODE=worker` is run
- **THEN** tcpdump runs on the specified node showing ICMP fragmentation-needed packets

#### Scenario: Observe replication traffic
- **WHEN** `make observe-replication CLUSTER=a` is run
- **THEN** tcpdump runs filtering UDP port 4390 showing go-pmtud replication between nodes

### Requirement: PMTU cache verification
The lab SHALL provide a way to verify that go-pmtud replication causes the kernel PMTU cache to update on peer nodes that didn't directly receive the ICMP.

#### Scenario: Peer node PMTU cache updated
- **WHEN** ICMP fragmentation-needed is received by node-1 in cluster-a
- **AND** go-pmtud replicates it to node-2 in cluster-a
- **THEN** `ip route get <destination>` on node-2 shows the updated MTU (1500)

#### Scenario: Route cache flush and re-verify
- **WHEN** the route cache on a node is flushed (`ip route flush cache`)
- **AND** traffic is generated again
- **THEN** go-pmtud replication restores the PMTU cache entry on peer nodes

### Requirement: End-to-end test target
The lab SHALL provide `make test` that runs an automated end-to-end validation: generates traffic, waits for ICMP, and verifies PMTU cache updates on all cluster-a worker nodes.

#### Scenario: Automated validation passes
- **WHEN** `make test` is run after `make setup`
- **THEN** the test generates cross-cluster traffic, verifies ICMP was received, verifies go-pmtud replicated to peers, and checks PMTU cache on all nodes
- **AND** exits 0 on success or non-zero with diagnostic output on failure

### Requirement: Lab status reporting
The lab SHALL provide `make status` showing the state of all lab components.

#### Scenario: Status shows healthy lab
- **WHEN** `make status` is run after successful setup
- **THEN** output shows: networks (up/down), clusters (running/stopped), router (running/stopped), go-pmtud pods (running/pending/error), and podinfo deployment status
