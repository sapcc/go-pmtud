<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## ADDED Requirements

### Requirement: Pluggable relay transport interface
The system SHALL relay captured ICMP fragmentation-needed packets between nodes
through a single `Relay` interface with a send responsibility (capture side) and a
receive responsibility that injects packets via a caller-supplied callback. Packet
capture (NFLOG) and packet injection (TUN device) SHALL be shared, transport-agnostic
code used by every backend.

#### Scenario: Capture side calls the active backend
- **WHEN** an ICMP type 3 code 4 packet is captured via NFLOG
- **THEN** the system calls the active backend's `Send` with the raw IP packet payload and the capturing node's name

#### Scenario: Receive side injects via shared injector
- **WHEN** the active backend delivers a relayed packet from a peer
- **THEN** the system injects the payload into the local stack via the shared TUN injector, independent of which backend delivered it

### Requirement: Runtime backend selection
The system SHALL select exactly one relay backend per process via a
`--relay-backend` flag accepting `udp` or `crd`, defaulting to `udp`.

#### Scenario: Default backend
- **WHEN** no `--relay-backend` flag is provided
- **THEN** the system uses the UDP backend, preserving existing behavior

#### Scenario: CRD backend selected
- **WHEN** `--relay-backend crd` is provided
- **THEN** the system uses the CRD backend and does not open the UDP replication socket

#### Scenario: Invalid backend rejected
- **WHEN** `--relay-backend` is set to a value other than `udp` or `crd`
- **THEN** the system fails to start with a clear error

### Requirement: UDP backend preserves existing behavior
The system SHALL provide a UDP backend implementing the `Relay` interface whose
observable behavior (replication port, raw-packet payload format, per-peer send,
source validation against the peer list, loop prevention) is identical to the
pre-refactor UDP replication path.

#### Scenario: Send to all peers over UDP
- **WHEN** the UDP backend `Send` is called and 3 peers are registered
- **THEN** the raw packet is sent via UDP to each of the 3 peer IPs on the replication port

#### Scenario: Reject unknown UDP source
- **WHEN** a UDP datagram arrives from an IP not in the peer list
- **THEN** the UDP backend discards it, logs a warning, and increments the error metric

### Requirement: CRD backend relays via namespaced custom resources
The system SHALL provide a CRD backend implementing the `Relay` interface that
relays packets by creating namespaced `PMTUNodeRelay` objects and watching for
objects created by peers, requiring no new host ports or firewall rules.

#### Scenario: Send creates a relay object
- **WHEN** the CRD backend `Send` is called with a captured packet
- **THEN** the system creates a `PMTUNodeRelay` object in the configured namespace with the base64-encoded payload, the source node name, and an `expiresAt` timestamp

#### Scenario: Watch injects and deletes
- **WHEN** a `PMTUNodeRelay` object created by a peer node is observed
- **THEN** the system base64-decodes the payload, injects it via the TUN device, and deletes the object

#### Scenario: Own objects skipped
- **WHEN** a `PMTUNodeRelay` object whose `sourceNode` equals the local node name is observed
- **THEN** the system does not inject it (loop prevention)

### Requirement: CRD relay object deduplication
The system SHALL name each `PMTUNodeRelay` object deterministically as
`<sourceNode>--<first 8 hex chars of sha256(payload)>` so identical events from the
same node collapse to a single object.

#### Scenario: Duplicate capture is a no-op
- **WHEN** the CRD backend `Send` is called twice with the same payload from the same node before the first object is deleted
- **THEN** the second create returns AlreadyExists and the system does not create a duplicate object

### Requirement: CRD relay object garbage collection
The system SHALL delete `PMTUNodeRelay` objects whose `expiresAt` timestamp is in
the past, via an in-daemon sweep on a configurable interval (`--relay-gc-interval`,
default 60s). The sweep SHALL delete any expired object regardless of which node
created it, and SHALL treat a NotFound result as success. No separate controller or
CronJob is used.

#### Scenario: Expired object reaped
- **WHEN** a `PMTUNodeRelay` object's `expiresAt` is in the past
- **THEN** the system deletes the object during the next GC pass

#### Scenario: Orphan from a dead creator reaped by a peer
- **WHEN** a `PMTUNodeRelay` object is expired and the node that created it is no longer present
- **THEN** another daemon's sweep deletes it (cleanup is not partitioned by creator)

#### Scenario: Concurrent delete is not an error
- **WHEN** two daemons attempt to delete the same expired object and one succeeds first
- **THEN** the other treats the resulting NotFound as success

### Requirement: CRD backend namespace resolution
The system SHALL determine the namespace for `PMTUNodeRelay` objects from the
`--relay-namespace` flag, falling back to the `POD_NAMESPACE` environment variable,
and SHALL fail fast at startup if the CRD backend is selected and no namespace can
be resolved.

#### Scenario: Namespace from downward API
- **WHEN** `--relay-backend crd` is set, `--relay-namespace` is empty, and `POD_NAMESPACE` is `kube-system`
- **THEN** the system uses `kube-system` for relay objects

#### Scenario: No namespace resolvable
- **WHEN** `--relay-backend crd` is set and neither `--relay-namespace` nor `POD_NAMESPACE` provides a namespace
- **THEN** the system fails to start with a clear error

### Requirement: PMTUNodeRelay CRD generated from Go types
The system SHALL define the `PMTUNodeRelay` API type in Go with kubebuilder markers
(namespaced scope, group `pmtud.cloud.sap`, version `v1alpha1`) and generate the CRD
manifest, RBAC, and deepcopy code via controller-gen (`make generate`), without
hand-written CRD YAML.

#### Scenario: Generated artifacts present
- **WHEN** `make generate` is run
- **THEN** the CRD manifest, deepcopy functions, and RBAC for `pmtunoderelays` are produced from the Go type definitions

### Requirement: Shared TUN injector
The system SHALL own a single `pmtud0` TUN device via a shared injector used by
whichever backend is active, preserving the `! -i pmtud0` loop-prevention contract.

#### Scenario: Single TUN owner
- **WHEN** the process starts with either backend
- **THEN** exactly one `pmtud0` TUN device is created and used for all injection

## MODIFIED Requirements

### Requirement: UDP-based packet replication sending
The system SHALL send captured ICMP fragmentation-needed packets to all known peer
nodes via UDP unicast on the configured replication port using a persistent
unconnected UDP socket, **when the UDP relay backend is active**. This behavior is
now provided by the UDP implementation of the `Relay` interface rather than inline
in the nflog controller.

#### Scenario: Successful replication to all peers via UDP backend
- **WHEN** the UDP backend is active and an ICMP type 3 code 4 packet is captured with 3 peer nodes registered
- **THEN** the system sends the full raw IP packet payload via UDP to each of the 3 peer node IPs on the replication port

#### Scenario: UDP backend inactive under CRD selection
- **WHEN** the CRD backend is active
- **THEN** no UDP replication socket is opened and no UDP datagrams are sent
