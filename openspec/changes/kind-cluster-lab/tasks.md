<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## 1. Directory structure and documentation

- [x] 1.1 Create `lab/` directory with `Makefile`, `README.md`, `configs/`, `scripts/`, `manifests/` subdirectories
- [x] 1.2 Write `lab/README.md` documenting prerequisites (docker, kind, kubectl), architecture diagram (ASCII), usage, and known limitations

## 2. Docker network setup

- [x] 2.1 Create `lab/scripts/setup-networks.sh`: creates `pmtud-net-a` (MTU 9000, subnet 172.30.0.0/16), `pmtud-net-b` (MTU 9000, subnet 172.31.0.0/16), `pmtud-transit` (MTU 1500, subnet 172.32.0.0/24) — idempotent
- [x] 2.2 Create `lab/scripts/teardown-networks.sh`: removes all three networks — idempotent

## 3. Kind cluster configuration

- [x] 3.1 Create `lab/configs/kind-cluster-a.yaml`: 1 control-plane + 2 workers, networking config for `pmtud-net-a`
- [x] 3.2 Create `lab/configs/kind-cluster-b.yaml`: 1 control-plane + 2 workers, networking config for `pmtud-net-b`
- [x] 3.3 Create `lab/scripts/setup-clusters.sh`: creates both Kind clusters, attaches node containers to respective Docker networks

## 4. Router container

- [x] 4.1 Create `lab/configs/router/Dockerfile`: Alpine-based image with iproute2, iptables, tcpdump, ip forwarding enabled
- [x] 4.2 Create `lab/scripts/setup-router.sh`: builds and runs the router container, connects it to all three networks, sets MTU 1500 on transit-facing interface, configures forwarding rules and routes
- [x] 4.3 Add static routes on Kind nodes pointing to router for cross-cluster traffic

## 5. go-pmtud deployment

- [x] 5.1 Create `lab/manifests/pmtud-daemonset.yaml`: DaemonSet with hostNetwork, CAP_NET_RAW, CAP_NET_ADMIN, `--replication-port=4390`, iptables NFLOG rule init container
- [x] 5.2 Create `lab/scripts/deploy-pmtud.sh`: builds go-pmtud image, loads into both clusters via `kind load docker-image`, applies DaemonSet manifest, waits for pods Ready

## 6. Test workload

- [x] 6.1 Create `lab/manifests/podinfo.yaml`: podinfo Deployment + Service (NodePort) in cluster-b serving a large test file
- [x] 6.2 Create `lab/scripts/deploy-workload.sh`: deploys podinfo to cluster-b, generates a 1MB test file in the pod

## 7. Traffic generation and observation

- [x] 7.1 Create `lab/scripts/generate-traffic.sh`: runs curl from a cluster-a pod to podinfo NodePort in cluster-b (large file download triggering PMTU discovery)
- [x] 7.2 Create `lab/scripts/observe.sh`: wrapper for tcpdump on router/nodes with pre-built ICMP type 3/4 and UDP 4390 filters (subcommands: `router`, `node`, `replication`)

## 8. Validation and testing

- [x] 8.1 Create `lab/scripts/verify-pmtu.sh`: checks `ip route get` on all cluster-a worker nodes to verify PMTU cache reflects MTU 1500 for cluster-b destinations
- [x] 8.2 Create `lab/scripts/test-e2e.sh`: full end-to-end test — generates traffic, waits for ICMP, verifies replication on UDP 4390, checks PMTU cache on peers, exit 0/1
- [x] 8.3 Create `lab/scripts/status.sh`: shows state of networks, clusters, router, go-pmtud pods, podinfo

## 9. Makefile targets

- [x] 9.1 Create `lab/Makefile` with targets: `setup` (networks + clusters + router + routes), `deploy` (pmtud + workload), `test` (generate-traffic + verify), `observe-router`, `observe-node`, `observe-replication`, `status`, `teardown`
- [x] 9.2 Add top-level convenience: `make lab-setup`, `make lab-teardown` in a root-level comment or lab/README reference

## 10. Final validation

- [x] 10.1 Run `make setup` on a Linux machine or Docker Desktop — verify all containers and clusters start
- [x] 10.2 Run `make deploy` — verify go-pmtud and podinfo pods are Running
- [x] 10.3 Run `make test` — verify end-to-end PMTU replication works
