<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Why

Testing the `feature/eliminate-l2-dependency` UDP replication requires simulating MTU mismatches between nodes across L3 boundaries. Currently this is done manually on real infrastructure (adjusting MTU on core routers, SSHing into nodes, flushing route caches). A reproducible local lab using Kind clusters in separate Docker networks enables fast iteration, CI integration, and eliminates dependency on production/QA infrastructure.

## What Changes

- Add a Kind-based local development lab with two clusters in separate Docker networks connected by a simulated router container
- Provide scripts to set up MTU mismatches between the networks (simulating a reduced-MTU path)
- Include a test workload (podinfo) that generates traffic large enough to trigger ICMP fragmentation-needed
- Provide helper scripts for tcpdump observation of ICMP type 3/4 packets and PMTU replication
- Document the lab setup and validation workflow

## Capabilities

### New Capabilities
- `kind-lab-setup`: Docker network topology with two Kind clusters, a router container bridging them, and configurable MTU mismatches between networks
- `kind-lab-validation`: Test scripts and procedures to verify go-pmtud correctly replicates ICMP fragmentation-needed packets across the simulated L3 boundary

### Modified Capabilities

## Impact

- **Code**: New `lab/` directory with Kind configs, scripts, and Dockerfiles (no changes to main source)
- **Dependencies**: Requires `kind`, `docker`, `kubectl` on developer machine (development-time only)
- **CI**: Optionally integrable into GitHub Actions for automated integration testing
- **Documentation**: New README in `lab/` explaining setup, usage, and teardown
