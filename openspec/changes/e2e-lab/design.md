<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# E2E Lab — Design

**Goal:** A single-binary Go end-to-end test that provisions a Kind cluster,
deploys go-pmtud, and proves that an ICMP fragmentation-needed captured on one
node is relayed to a non-originating peer — for **both** relay backends (`l2`,
its `legacy` no-flag variant, and `udp`) — then tears down reliably, in CI and on
developer macOS. Plus a manual runbook for the same check against a real cluster.

## What is under test

The daemon runs as a DaemonSet, `hostNetwork: true`, `CAP_NET_RAW`/`NET_ADMIN`.
It installs its own NFLOG capture rule (in-binary nftables) bound to the
**capture interface** — the interface `util.GetDefaultInterface` resolves via
`netlink.RouteGet(8.8.8.8)`:

```
… icmp type 3 code 4, iifname == <capture-iface> → NFLOG group 33
```

Chain:

1. A node **receives** an ICMP frag-needed (type 3 code 4) on the capture
   interface.
2. The NFLOG runnable (`internal/nflog`) reads it, applies loop prevention (skip
   ignored networks and peer IPs), increments `go_pmtud_recv_packets_total`, and
   calls `Relay.Send`.
3. `Relay.Send` fans the packet out to every peer over the **replication
   interface** (`eth0`, node InternalIP), incrementing
   `go_pmtud_sent_packets_total` and `go_pmtud_sent_packets_peer{peer=<ip>}`:
   - **L2 backend:** resolves each peer's MAC via ARP on the replication
     interface and writes a raw Ethernet frame. It has **no receive loop** and no
     receive-side counter.
   - **UDP backend:** sends a unicast datagram to each peer's `:4390`. Peers'
     receive loop injects it into the `pmtud0` TUN device and increments
     `go_pmtud_injected_packets_total{source=<relaying-peer-ip>}`.

Peers are discovered automatically: the `internal/node` Reconciler adds every
**other** node's `InternalIP` to the shared `Config.PeerList` (own node excluded,
mutex-protected). In a single cluster with ≥2 workers, each worker is already a
peer of the others — no peer configuration is needed.

**Consequence for the trigger:** the rule matches *received* frag-needed. A
locally-originated oversized packet only returns `EMSGSIZE` to the socket; the
kernel does not deliver a frag-needed to its own input path, so NFLOG never fires.
A forwarding hop with a low egress MTU is therefore required — a plain node with a
`dummy` interface, not a router container.

## Topology — one cluster, two interfaces

The lab uses a **static two-interface topology**, provisioned once and never
mutated mid-suite. **Capture runs on a dedicated transit interface (`eth1`);
replication stays on `eth0`.** Capture ≠ replication is what makes L2 faithful:
without it, a node re-captures the frames it replicates (an amplification storm).

```
┌──────────── single Kind cluster ────────────────────────────────────────────┐
│  Two docker networks per node:                                               │
│    eth0  → "kind" network (node InternalIP)      = REPLICATION interface     │
│    eth1  → "pmtud-transit" 172.32.0.0/24         = CAPTURE interface         │
│                                                                              │
│  control-plane ── low-MTU forwarding hop                                     │
│    ip_forward=1                                                              │
│    pmtudlab0  mtu 1280  10.99.0.1/24  (blackhole subnet, directly connected) │
│                                                                              │
│  worker-A (originator)                    worker-B (peer under test)         │
│    8.8.8.8/32 via <cp eth1> dev eth1        8.8.8.8/32 via <cp eth1> dev eth1│
│      → capture flips to eth1                  → capture flips to eth1        │
│    10.99.0.0/24 via <cp eth1>                                               │
│    ping -M do -s 1400 10.99.0.2                                             │
│                                                                              │
│  go-pmtud DaemonSet on all nodes (backend under test; --iface_mtu=<eth0 MTU>)│
└──────────────────────────────────────────────────────────────────────────┘
```

**Capture-interface flip.** The daemon binds NFLOG to whatever
`GetDefaultInterface` returns for `RouteGet(8.8.8.8)`. Adding a scoped
`8.8.8.8/32 via <cp-eth1> dev eth1` host route on each worker flips capture to
`eth1` without touching the default route — so replication still egresses `eth0`.
This is a test-topology route only; no production code is changed.

Flow: `worker-A → control-plane` over the transit network (eth1) fits; the hop
forwards toward `pmtudlab0` (MTU 1280) with DF set, which is too big, so the hop
emits an ICMP frag-needed (mtu 1280) back to worker-A over eth1. worker-A's NFLOG
rule (on eth1) captures it, converges its own route cache natively, and relays to
peers over eth0; the relayed frame is **not** re-captured because eth0 ≠ eth1.

**Why `--iface_mtu` matters.** L2/legacy resolve a replication interface via
`util.GetReplicationInterface`, which requires an **exact** MTU match against
`--iface_mtu`. Kind's `eth0` MTU is environment-dependent (65535 on Docker Desktop,
1500 on Linux CI). The lab detects `eth0`'s MTU and passes it as `--iface_mtu`, so
the daemon comes up healthy everywhere. Without it, L2/legacy crash-loop and the
peer metrics scrape fails with `exit status 7`. UDP ignores the iface flags, so a
single templated manifest serves all three backends.

### What each peer assertion proves

Asserting a *route-cache* PMTU exception on the peer is not achievable in this
lab: the relayed frag-needed is addressed to worker-A's node IP (not local on the
peer, so the peer kernel never hands it to `icmp_rcv`), and its inner packet is an
ICMP echo the peer never originated. Production converges only because the flow
uses a shared anycast/ECMP source IP local on every node. The lab therefore
verifies **relay delivery**, per backend:

- **UDP** — the peer's `go_pmtud_injected_packets_total` increments: the peer
  received the relayed datagram and injected it via `pmtud0`. This is genuine
  end-to-end delivery, because UDP has a receive loop and counter.
- **L2 / legacy** — the **originator's** `go_pmtud_sent_packets_peer{peer=<peerIP>}`
  increments: the relay resolved that peer's MAC via ARP on the replication
  interface and wrote the frame to it. L2 has no receive-side counter by design,
  so delivery is verified at the send boundary. (On a functioning shared L2
  segment, a successful ARP + frame write is delivery.)

Both backends additionally assert **native route-cache convergence on the
originator** (`ip route get <blackhole>` → mtu 1280), confirming the
capture→relay pipeline ran end-to-end on the originating node.

**Hop return route (loop-prevention interaction).** The reconciler adds the
control-plane's InternalIP to worker-A's PeerList, so a frag-needed sourced from
the CP node IP would be dropped as peer-originated. `configureHop` pins the CP's
return route to worker-A (out `eth1`) with `src <HopIP>`, so the error sources
from the non-node hop address (per RFC 1191) and the relay fires.

### Packet-size arithmetic

`ping -s 1400` → 1400 payload + 8 ICMP + 20 IP = **1428 bytes**.
- `worker-A → CP` over the transit network: fits.
- `CP → pmtudlab0` (1280): 1428 > 1280 with DF ⇒ frag-needed carrying `mtu = 1280`.

`10.99.0.2` is on `pmtudlab0`'s directly-connected subnet, so the CP attempts
egress out `pmtudlab0` and hits the MTU check; the address need not answer. `ping`
prints `From <hop> … Frag needed and DF set (mtu = 1280)` and then times out per
probe (100% loss), exiting non-zero — the trigger treats the reported frag-needed
string, not the exit code, as success.

### Forwarding-hop interface: `dummy` with `veth` fallback

Primary: `ip link add pmtudlab0 type dummy`. The `dummy` module is near-universal
(including LinuxKit). If `type dummy` fails, fall back to a `veth` pair with the
far end clamped to MTU 1280 and left down/blackholed. The suite tries `dummy`
first and falls back automatically.

### Why an interface MTU, not a docker-network MTU

The kernel honours `ip link set <dev> mtu` via netlink directly. Custom
docker-network MTUs (`com.docker.network.driver.mtu`) are **not** reliably
honoured by Docker Desktop's LinuxKit VM. Clamping a node-local `dummy`/`veth`
interface sidesteps docker networking entirely and works on Linux and macOS alike.
(The `pmtud-transit` network is used only to add a second interface for capture;
its MTU is left at the default and is irrelevant to the frag-needed arithmetic.)

## `lab` package (all `//go:build e2e`)

| File | Responsibility |
|---|---|
| `lab.go` | `Lab{ Cluster *Cluster; BlackholeIP string }`. `Provision`: create cluster, discover control-plane + workers, set up transit network + capture-flip routes, configure hop, add worker-A blackhole route, ensure ping. `Teardown`: delete cluster and remove the transit network (both no-ops under `LAB_KEEP`). |
| `cluster.go` | Kind Go API create/delete; isolated temp kubeconfig; `controller-runtime` client; worker + control-plane discovery via `docker ps --filter label=io.x-k8s.kind.role=…`. `applyDaemonSet` templates `$(RELAY_BACKEND)` and `$(IFACE_MTU)` (detected `eth0` MTU) and strips `--relay-backend` for the `legacy` variant. |
| `network.go` | Create the `pmtud-transit` docker network (172.32.0.0/24), connect all nodes (`eth1`), and install the `8.8.8.8/32 via <cp eth1> dev eth1` capture-flip route on each worker. Idempotent; teardown removes the network. |
| `routes.go` | Hop setup on the control-plane (`ip_forward`, `pmtudlab0` @1280 with `veth` fallback, address) and worker-A's blackhole route + the CP's pinned return route — both over `eth1`. |
| `deploy.go` | `DeployBackend(backend)`: build image, `kind load`, apply RBAC + daemonset, `waitRollout`. |
| `ops.go` | `GenerateTraffic` (DF-set ping; success on the frag-needed signal, exit code ignored). `PMTUTo(node,dst)` parses `ip route get`. `InjectedPackets(node)` sums `go_pmtud_injected_packets_total` (UDP peer assertion). `SentPacketsPeer(node,peerNode)` resolves `peerNode`'s InternalIP and sums `go_pmtud_sent_packets_peer` filtered to `peer=<peerIP>` (L2/legacy originator assertion). `FlushRouteCache(node)`. |
| `exec.go` | `run()` (stream) + `dockerExec()` (CombinedOutput, error includes stderr). `containerIP(name)` returns the **kind-network** InternalIP; `containerIPOnNetwork(name, network)` returns the IP on a named network (used for `eth1`/transit). |

Kept YAML: `lab/configs/kind-cluster.yaml` (1 control-plane + 2 workers),
`lab/manifests/pmtud-daemonset.yaml`, `lab/manifests/rbac.yaml`. No router image,
no `podinfo`, no per-network docker configs.

## Suite (`test/e2e/`, `//go:build e2e`)

`suite_test.go` — `RunSpecs` + `BeforeSuite`/`AfterSuite` drive the lifecycle;
`LAB_REUSE`/`LAB_KEEP` env knobs. Iterates `legacy`, `l2`, `udp`, each in an
`Ordered` context that deploys the backend then runs `configSpecs` + `pmtuSpecs`.

`config_test.go` — assert the deployed DaemonSet carries `--relay-backend=<backend>`
(and, for `legacy`, that no `--relay-backend` flag is present — the daemon must
default to `l2`).

`pmtu_test.go` — per backend (ordered):

```go
It("replicates PMTU to peer nodes", func(ctx SpecContext) {
    workers := testLab.Cluster.Workers
    originator := workers[0]
    Expect(testLab.FlushRouteCache(originator)).To(Succeed())

    // Snapshot the per-backend peer-delivery baseline.
    base := map[string]int{}
    for _, w := range workers[1:] {
        var n int; var err error
        if backend == "udp" {
            n, err = testLab.InjectedPackets(w)          // read on the peer
        } else {
            n, err = testLab.SentPacketsPeer(originator, w) // read on originator, filtered to peer w
        }
        Expect(err).NotTo(HaveOccurred()); base[w] = n
    }

    Expect(testLab.GenerateTraffic(ctx)).To(Succeed()) // errors unless ping saw frag-needed

    // originator converges natively (real kernel route-cache apply)
    Eventually(func() (int, error) { return testLab.PMTUTo(originator, testLab.BlackholeIP) }).
        WithTimeout(30 * time.Second).WithPolling(2 * time.Second).Should(Equal(1280))

    // peers: relay delivered
    for _, w := range workers[1:] {
        if backend == "udp" {
            Eventually(func() (int, error) { return testLab.InjectedPackets(w) }).
                WithTimeout(30 * time.Second).WithPolling(2 * time.Second).
                Should(BeNumerically(">", base[w]), "peer %s must inject a relayed frag-needed (udp)", w)
        } else {
            Eventually(func() (int, error) { return testLab.SentPacketsPeer(originator, w) }).
                WithTimeout(30 * time.Second).WithPolling(2 * time.Second).
                Should(BeNumerically(">", base[w]), "originator must relay a frag-needed to peer %s (%s)", w, backend)
        }
    }
})
```

Failure diagnostics: a `ReportAfterEach` hook dumps, on failure, `ip route get
<BlackholeIP>` on each worker and the hop's `ip link show`.

## Loop prevention

Two independent mechanisms:

1. **Structural (both backends):** capture is bound to `eth1`, replication egresses
   `eth0`, so a replicated frame is never re-captured. This is what keeps L2
   storm-free in Kind.
2. **Source-based (in the daemon):** the NFLOG runnable skips packets whose source
   is a peer IP or an ignored network; the UDP backend additionally excludes the
   `pmtud0` TUN device.

## Real-cluster runbook (`lab/RUNBOOK-real-cluster.md`)

Manual, not automated:

1. **Prerequisites** — a real cluster with ≥2 schedulable nodes; a real MTU
   boundary (cross-AZ link) or the ability to add a low-MTU hop on one node;
   `kubectl` + node shell access.
2. **Deploy** — build/push the image; apply RBAC; apply the DaemonSet with the
   chosen `--relay-backend`; wait for rollout.
3. **Induce a frag-needed** — rely on the existing MTU mismatch, or replicate the
   lab hop on one node (`ip_forward`, `dummy` @ low MTU, route) and `ping -M do
   -s <>` from another node.
4. **Observe relay** — for UDP, confirm `go_pmtud_injected_packets_total`
   increments on a non-originating node; for L2, confirm
   `go_pmtud_sent_packets_peer` increments on the originator. In production (shared
   anycast/ECMP source IP), also confirm `ip route get <dst>` on a peer shows the
   reduced MTU.
5. **Cleanup** — delete the DaemonSet/RBAC; remove the temporary hop.

Referenced from `lab/README.md`.

## Testing strategy

1. `go build -tags e2e ./...` and `go vet -tags e2e ./...` clean.
2. `go test ./...` (no tag) green — the suite is excluded, so unit CI needs no
   docker/kind.
3. `make -C lab e2e` passes `legacy`, `l2`, and `udp` on Linux **and** macOS
   Docker Desktop, storm-free (peer `recv` stays 0).
4. Pure helpers (`ping`-output parser, `ip route get` MTU parser, transit-IP and
   `sent_packets_peer` label parsing) get small table tests compiled under the
   `e2e` tag.

## Open considerations

- **`dummy` module availability** — handled by the `veth` fallback.
- **`ping` availability in Kind nodes** — `ensurePing` installs `iputils-ping` on
  worker-A only if missing.
- **Transit IP assignment order** — Docker assigns transit IPs at
  `network connect` time in no guaranteed order; the lab inspects each node's IP on
  the **named** `pmtud-transit` network rather than assuming an ordering.
- **Control-plane running the daemon** — tolerations (`operator: Exists`) mean the
  daemon also runs there. The CP plays the hop role; its capture interface is
  irrelevant to the assertions (it never receives a frag-needed on input).
- **Kind Go API version pin** — must match the `kind` binary contract; pin in
  `go.mod` and document the compatible node image.
