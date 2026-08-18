<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# E2E Lab — Design

**Goal:** A single-binary Go end-to-end test that provisions a Kind cluster,
deploys go-pmtud, proves an ICMP fragmentation-needed captured on one node is
replicated to a non-originating peer (for both the `udp` and `crd` relay
backends), and tears down — reliably, in CI, and on developer macOS. Plus a
manual runbook for the same check against a real cluster.

## What is under test

The daemon runs as a DaemonSet, `hostNetwork: true`, `CAP_NET_RAW`. Its init
container installs the capture rule:

```
iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 \
  -j NFLOG --nflog-group 33
```

Chain:

1. A node **receives** an ICMP frag-needed (type 3 code 4) on `PREROUTING`.
2. The NFLOG runnable (`internal/nflog`) reads it, applies loop prevention (skip
   ignored networks and peer IPs), and calls `Relay.Send`.
3. Peers' `Relay.Start(inject)` receive it and write it to the `pmtud0` TUN
   device (`internal/relay/inject_linux.go`).
4. The peer kernel processes the injected frag-needed and updates its route-cache
   PMTU for the inner packet's destination.

Peers are discovered automatically: the `internal/node` Reconciler adds every
**other** node's `InternalIP` to the shared `Config.PeerList` (own node excluded,
mutex-protected). In a single cluster with ≥2 workers, each worker is already a
peer of the others — no topology configuration is needed.

**Consequence for the trigger:** the rule matches *received* frag-needed on
`PREROUTING`. A locally-originated oversized packet only returns `EMSGSIZE` to the
socket; the kernel does not deliver a frag-needed to its own input path, so NFLOG
never fires. A forwarding hop with a low egress MTU is therefore required — but it
is a plain node with a `dummy` interface, not a router container.

## Topology

```
┌──────────────────── single Kind cluster (one docker network) ─────────────────┐
│                                                                                │
│  control-plane  ──── acts as low-MTU forwarding hop                            │
│    ip_forward=1                                                                │
│    pmtudlab0  mtu 1280   10.99.0.1/24   (blackhole subnet, directly connected) │
│                                                                                │
│  worker-A (originator)                 worker-B (peer under test)              │
│    route: 10.99.0.0/24 via <CP ip>       (no special routes)                   │
│    ping -M do -s 1400 10.99.0.2                                                │
│                                                                                │
│  go-pmtud DaemonSet on all nodes (udp | crd backend, matrix)                   │
└────────────────────────────────────────────────────────────────────────────┘
```

Flow: `worker-A → control-plane` fits the shared Kind bridge (MTU 1500);
`control-plane → pmtudlab0` (MTU 1280) with the DF bit set is too big, so the
control-plane emits an ICMP frag-needed (mtu 1280) back to worker-A. worker-A's
NFLOG rule captures it, converges its own route cache natively, and relays to
peers; each peer's daemon injects the frag-needed into its `pmtud0` TUN.

**What the peer assertion proves.** Asserting a *route-cache* PMTU exception on
the peer is not achievable in this single-cluster lab: the relayed frag-needed
is addressed to worker-A's unique node IP (not local on the peer, so the peer
kernel never hands it to `icmp_rcv`), and its inner packet is an ICMP echo the
peer never originated. Production converges only because the flow uses a shared
anycast/ECMP source IP local on every node. The lab therefore asserts that
replication **reached** the peer — the peer daemon's `go_pmtud_recv_packets_total`
counter increments (it received and injected the relayed packet) — and asserts
native route-cache convergence only on the originator. See
`debug-findings.md` for the kernel-counter evidence.

**Hop return route (loop-prevention interaction).** The reconciler adds every
other node's InternalIP to each node's PeerList, so the control-plane is a peer
of worker-A. If the hop's frag-needed sources from the CP node IP, worker-A's
daemon drops it as peer-originated. `configureHop` therefore pins the CP's
return route to worker-A with `src <HopIP>`, so the error sources from the
non-node hop address (per RFC 1191) and the relay fires.

### Packet-size arithmetic

`ping -s 1400` → 1400 payload + 8 ICMP + 20 IP = **1428 bytes**.
- `worker-A → CP` over the 1500-MTU bridge: fits.
- `CP → pmtudlab0` (1280): 1428 > 1280 with DF ⇒ frag-needed carrying `mtu = 1280`.

`10.99.0.2` is on `pmtudlab0`'s directly-connected subnet, so the CP attempts
egress out `pmtudlab0` and hits the MTU check; the address need not answer. `ping`
prints `From <CP> ... Frag needed and DF set (mtu = 1280)` and then times out per
probe (100% loss), so it **exits non-zero** — the trigger treats the reported
frag-needed string, not the exit code, as success.

### Forwarding-hop interface: `dummy` with `veth` fallback

Primary: `ip link add pmtudlab0 type dummy`. The `dummy` module is near-universal
(including LinuxKit). If `type dummy` fails, fall back to a `veth` pair with the
far end clamped to MTU 1280 and left down/blackholed. Both are a few `ip`
commands run via privileged `docker exec`. The suite tries `dummy` first and
falls back automatically.

### Why an interface MTU, not a docker-network MTU

The kernel honours `ip link set <dev> mtu` via netlink directly. Custom
docker-network MTUs (`com.docker.network.driver.mtu`) are **not** reliably
honoured by Docker Desktop's LinuxKit VM, which is why an MTU-mismatch lab built
on docker networks fails on macOS. Clamping a node-local `dummy`/`veth` interface
sidesteps docker networking entirely and works on Linux and macOS alike.

## `lab` package (all `//go:build e2e`)

| File | Responsibility |
|---|---|
| `lab.go` | `Lab{ Cluster *Cluster; BlackholeIP string }`. `Provision`: create cluster, discover control-plane + workers, configure hop, add worker-A route. `Teardown`: delete cluster (no-op on `LAB_KEEP`). `Attach` for `LAB_REUSE`. |
| `cluster.go` | Kind Go API create/delete; isolated temp kubeconfig; `controller-runtime` client with `v1alpha1` scheme; worker + control-plane container discovery via `docker ps --filter label=io.x-k8s.kind.role=...`. |
| `routes.go` | Hop setup on the control-plane (`ip_forward`, `pmtudlab0` @1280 with `veth` fallback, address) and the single route on worker-A. |
| `deploy.go` | `DeployBackend(backend)`: build image, `kind load`, apply RBAC + daemonset (inject `--relay-backend` + `POD_NAMESPACE`) + CRD (crd backend), `waitRollout`. |
| `ops.go` | `GenerateTraffic`: `ping -M do -s 1400 -c3 -W2 <BlackholeIP>` on worker-A, success on the `mtu = 1280`/`Frag needed` signal (ignore exit code). `PMTUTo(node,dst)` parses `ip route get`. `RecvPackets(node)` sums `go_pmtud_recv_packets_total` scraped from the node daemon. `FlushRouteCache(node)`. |
| `exec.go` | `run()` (stream stdout/stderr) + `dockerExec()` (CombinedOutput, error includes stderr) + `ifaceByIP()` / `ipOnSubnet()` helpers. |

Kept YAML: `lab/configs/kind-cluster.yaml` (1 control-plane + 2 workers),
`lab/manifests/pmtud-daemonset.yaml`, `lab/manifests/rbac.yaml`, and the repo-root
CRD `crd/pmtud.cloud.sap_pmtunoderelays.yaml`. No router image, no `podinfo`, no
per-network docker configs.

## Suite (`test/e2e/`, `//go:build e2e`)

`suite_test.go` — `RunSpecs` + `BeforeSuite`/`AfterSuite` drive the full
lifecycle; `LAB_REUSE`/`LAB_KEEP` env knobs.

`config_test.go` — per backend (ordered), assert the deployed DaemonSet carries
`--relay-backend=<backend>` and a `POD_NAMESPACE` env; for `crd`, assert the CRD
is established.

`pmtu_test.go` — per backend (ordered):

```go
It("replicates PMTU to peer nodes", func(ctx SpecContext) {
    originator := testLab.Cluster.Workers[0]
    Expect(testLab.FlushRouteCache(originator)).To(Succeed())

    base := map[string]int{} // peer recv counters before traffic
    for _, w := range testLab.Cluster.Workers[1:] {
        n, err := testLab.RecvPackets(w); Expect(err).NotTo(HaveOccurred())
        base[w] = n
    }
    Expect(testLab.GenerateTraffic(ctx)).To(Succeed()) // errors unless ping saw frag-needed

    // originator converges natively (real kernel route-cache apply)
    Eventually(func() (int, error) { return testLab.PMTUTo(originator, testLab.BlackholeIP) }).
        WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Equal(1280))

    // peers: replication delivered — recv_packets_total strictly increases
    for _, w := range testLab.Cluster.Workers[1:] {
        Eventually(func() (int, error) { return testLab.RecvPackets(w) }).
            WithTimeout(30 * time.Second).WithPolling(2 * time.Second).
            Should(BeNumerically(">", base[w]), "peer %s must receive a relayed frag-needed via %s", w, backend)
    }
})
```

Failure diagnostics: a `ReportAfterEach` hook dumps, on failure, `ip route get
<BlackholeIP>` on each worker and the hop's `ip link show` — the structured
replacement for ad-hoc debug output.

## Loop prevention

The NFLOG rule excludes `-i pmtud0`, and the runnable skips packets whose source
is a peer IP or an ignored network. The injected frag-needed on worker-B enters
via the TUN device (excluded), so it is not re-captured and re-relayed.

## Real-cluster runbook (`lab/RUNBOOK-real-cluster.md`)

Manual, not automated:

1. **Prerequisites** — a real cluster with ≥2 schedulable nodes; a real MTU
   boundary (cross-AZ link) or the ability to add a low-MTU hop on one node;
   `kubectl` + node shell access.
2. **Deploy** — build/push the image; apply RBAC + CRD; apply the DaemonSet with
   `--relay-backend=udp`, then `crd`; wait for rollout.
3. **Induce a frag-needed** — rely on the existing MTU mismatch, or replicate the
   lab hop on one node (`ip_forward`, `dummy` @ low MTU, route) and `ping -M do
   -s <>` from another node.
4. **Observe replication** — on a **non-originating** node, `ip route get <dst>`
   shows the reduced MTU; confirm the `RecvPackets` metric increments and the
   `"ICMP frag-needed received, resending packet."` log line appears; for `crd`,
   watch `PMTUNodeRelay` objects appear and get collected.
5. **Cleanup** — delete the DaemonSet/RBAC/CRD; remove the temporary hop.

Referenced from `lab/README.md`.

## Testing strategy

1. `go build -tags e2e ./...` and `go vet -tags e2e ./...` clean.
2. `go test ./...` (no tag) green — the suite is excluded, so unit CI needs no
   docker/kind.
3. `make -C lab e2e` passes both `udp` and `crd` backends on Linux **and** macOS
   Docker Desktop.
4. Pure helpers (`ping`-output parser, control-plane discovery, `ip route get`
   MTU parser) get small table tests compiled under the `e2e` tag.

## Open considerations

- **`dummy` module availability** — handled by the `veth` fallback.
- **`ping` availability in Kind nodes** — install `iputils-ping` on worker-A only
  if missing (one node), or use a raw-socket sender; decided in implementation.
- **Control-plane running the daemon** — tolerations (`operator: Exists`) mean
  the daemon also runs there. The CP plays the hop role, but its node IP is in
  worker-A's PeerList, so a frag-needed sourced from the CP node IP is dropped
  by worker-A's loop-prevention. `configureHop` pins the return route with
  `src <HopIP>` so the error sources from the non-node hop address instead.
- **Kind Go API version pin** — must match the `kind` binary contract; pin in
  `go.mod` and document the compatible node image.
