<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## Context

After `eliminate-l2-dependency` (PR #69), the relay path is:

```
capture (NFLOG)  ──► send loop (UDP WriteTo per peer)   [internal/nflog]
receive (UDP)    ──► inject (TUN pmtud0 write)           [internal/receiver]
```

Capture and injection are transport-independent in principle, but the transport
(UDP) is hard-wired into both controllers. This change extracts the transport
behind an interface so alternative backends (CRD now, message queue later) plug in
without duplicating capture or injection logic.

The repository uses openspec (`schema: spec-driven`) and go-makefile-maker with
`controllerGen` already enabled — CRDs, RBAC, and deepcopy code are generated from
Go types via `make generate`.

## Goals / Non-Goals

**Goals:**
- A single transport seam (`Relay`) with `Send` (capture side) and receive (`Start`
  with an `inject` callback) responsibilities.
- Capture (NFLOG) and injection (TUN `pmtud0`) become shared, backend-agnostic.
- Refactor the existing UDP path into a `Relay` backend with identical behavior.
- Add a CRD backend that needs no new ports/firewall rules.
- One backend active per process, chosen by flag.
- Layered testing: unit, local integration (netns/Kind), automated E2E.

**Non-Goals:**
- Running multiple backends simultaneously (no migration-overlap mode).
- Targeted CRD delivery / pod→node resolution (broadcast semantics instead — see
  Decision 4).
- Changing capture (NFLOG stays) or the kernel injection mechanism (TUN stays).
- Helm chart changes (separate repo, follow-up).
- IPv6, encryption/authentication of relayed packets beyond what exists today.

## Decisions

### 1. Transport-only seam: the `Relay` interface

**Decision**: The pluggable unit is the transport only. Capture and injection stay
shared. Interface (`internal/relay`):

```go
type RelayPacket struct {
    Payload []byte // raw IP packet (ICMP 3/4) as captured by NFLOG
    SrcNode string // cfg.NodeName of the capturing node (provenance/dedup)
}

type Relay interface {
    // Send relays a captured packet to peer node(s). Called from the nflog
    // callback (hot path) — implementations must be non-blocking or fast.
    Send(ctx context.Context, pkt RelayPacket) error

    // Start runs the receive loop until ctx is done (manager.Runnable).
    // For every packet relayed from a peer it calls inject(payload).
    Start(ctx context.Context, inject func(payload []byte) error) error
}
```

**Rationale**: Keeps the proven NFLOG capture and TUN injection paths untouched and
DRY. A backend only moves bytes from one node's capture to another node's inject.
Adding a message-queue backend later is a new file, no changes to capture/inject.

**Alternatives considered**:
- Full pipeline per backend (each owns capture+transport+inject): duplicates NFLOG
  and TUN logic across backends; rejected.

### 2. Shared TUN injector

**Decision**: Extract `createTUN` + `configureTUNNetlink` from
`internal/receiver/receiver.go` into `internal/relay/tun_linux.go` as an `Injector`
that owns the `pmtud0` fd and exposes `Inject(payload []byte) error`. A small
Linux-only runnable creates the injector and passes its `Inject` method as the
`inject` callback to `relay.Start`. Non-Linux builds keep a stub (existing
`receiver_other.go` pattern).

**Rationale**: Injection is identical regardless of backend. One owner of the TUN
fd avoids double-open and centralizes the loop-prevention contract (`! -i pmtud0`).

### 3. Backend selection via single flag

**Decision**: `--relay-backend=udp|crd`, default `crd`. `internal/cmd` constructs
the chosen `Relay`, hands it to the nflog controller (for `Send`), and registers a
runnable that owns the injector and calls `relay.Start(ctx, injector.Inject)`.
`--replication-port` is rejected at startup if `--relay-backend` is not `udp`.

**Rationale**: A DaemonSet runs one config per node; one active backend matches
reality and avoids inject-side dedup across backends. `crd` default makes the
no-extra-ports path the out-of-the-box experience; `udp` requires an explicit opt-in.

### 4. CRD backend: namespaced, broadcast, dedup by name

**Decision**: `PMTUNodeRelay` is **namespaced** (instances in the daemon's own
namespace), not cluster-scoped. Delivery is **broadcast**: on capture, create one
object; every daemon pod watches the namespace and injects. No pod→node
resolution.

- Object name: `<srcNode>--<sha256(payload)[:8]>` — deterministic; identical events
  collapse to one object (`AlreadyExists` on create is a no-op, prevents storms).
- Spec fields: `sourceNode`, `payload` (base64 raw ICMP packet), `expiresAt`
  (RFC 3339).
- Loop guard: a watcher skips objects where `sourceNode == own NODE_NAME`. The
  capturing node never injects its own capture; only peers do (see Decision 6).

**Rationale for namespaced over cluster-scoped**: Least privilege — a namespaced
`Role` (`create/get/list/watch/delete` on `pmtunoderelays`) instead of a
`ClusterRole` granting write on a new cluster resource. Contained blast radius;
scoped list/watch/GC. The CRD *definition* is still a cluster object (all CRDs are),
but instances need not be. The daemon learns its namespace from `POD_NAMESPACE`
(downward API) or `--relay-namespace`.

**Rationale for broadcast over targeted**: The task doc proposed resolving the
inner-source IP to a node name and addressing one object to it. But
`util.CalcSrcDst` yields the inner sender IP, which in Calico BGP / no-overlay
clusters is a **pod IP**, not a node IP — go-pmtud watches nodes, not pods, so
reverse lookup fails there. Targeting would require a pod→node watch and extra
RBAC. Broadcast mirrors the proven UDP semantics (every peer injects; kernels that
don't route the destination cache harmlessly). Measured volume on eu-de-1 (see
Risks) is higher than the original "cold path / rare" assumption — cluster-wide
capture peaks of ~280–390 ICMP frag-needed/min — but broadcast's cost is in the
**per-CR watch fan-out (× N nodes)**, not the create count itself, so targeting is
still deferred rather than required. The new `relay_send_total` / broadcast metrics
now make this a data-driven call: add targeting if the created-CR rate × node count
becomes a problem.

**Alternatives considered**:
- Cluster-scoped instances (task doc): more privilege, no functional gain; rejected.
- Targeted delivery: needs pod→node resolution; deferred.

### 5. CRD lifecycle & GC

**Decision**: Cleanup of `PMTUNodeRelay` objects happens **in-daemon** — no
separate controller and no CronJob. Two mechanisms:

1. **Delete-after-inject (happy path)**: the peer that injects an object deletes it
   immediately. This reaps virtually all objects, and works even if the creating
   node has since died, as long as at least one peer is watching.
2. **TTL sweep (backstop)**: the CRD backend's `Start` runs a controller-runtime
   watch on `PMTUNodeRelay` in the configured namespace, plus a GC pass on
   reconcile and on a `--relay-gc-interval` ticker (default `60s`). The sweep
   deletes **any** object whose `expiresAt` is in the past — not only objects the
   local node created. `expiresAt` is set on create to `now + ttl` (reuse/rename
   the existing TTL concept).

The sweep must delete *any* expired object (not just own): a "creator cleans its
own" partition would leave orphans exactly when the creator node is dead and cannot
sweep. Redundant deletes across daemon pods are idempotent (`NotFound` is ignored)
and negligible — objects are tiny, rare (cold paths), and short-lived.

**Rationale**: K8s has no native TTL for custom resources (TTL-after-finished is
Jobs-only), so a sweep is required. In-daemon reuses the already-running manager,
client, and watch cache — zero new Deployment, image, RBAC, or leader election. A
dedicated controller or CronJob would add a workload to maintain for a sweep that
deletes a handful of objects — wrong altitude.

**Alternatives considered**:
- Dedicated leader-elected GC controller: single-writer cleanliness but a new
  workload; rejected. If herd deletes ever matter, enable leader election inside
  the existing manager (a flag, not a new deployment) — this is the upgrade path.
- K8s CronJob running `kubectl delete`: separate image/RBAC/schedule, duplicates
  the expiry logic; rejected.
- Native CR TTL: does not exist for CRDs; not an option.

### 6. Capturing node does not inject its own capture

**Decision**: On the capture side, `relay.Send` publishes to peers only; the local
node does not self-inject (it already saw the original ICMP natively if it was the
correct target — the whole point is relaying to the node that did *not*). This
matches UDP today (nflog sends to peers; it does not loop back to itself). For CRD,
watchers skip `sourceNode == own node`.

**Rationale**: Preserves existing semantics and the loop-prevention model.

### 7. CRD types generated, not hand-written

**Decision**: Define `PMTUNodeRelay` in `api/v1alpha1/pmtunoderelay_types.go` with
kubebuilder markers. `make generate` (controller-gen, already enabled) produces the
CRD YAML, RBAC, deepcopy, and applyconfigurations. Do not hand-write CRD YAML.

**Rationale**: Repo convention; single source of truth in Go; avoids schema drift.

### 8. Observability: one event, one metric

**Decision**: Each stage of the relay pipeline gets exactly one counter, so no
metric double-counts across stages or backends:

| Stage | Metric | Labels | Notes |
|---|---|---|---|
| Captured from kernel (nflog) | `go_pmtud_recv_packets_total` | `node`, `source_ip` | Capture only. Incremented once, on the capturing node. |
| Relay send / CR write (crd) | `go_pmtud_relay_send_total` | `node`, `result` | `result` ∈ `created` \| `deduplicated` \| `error`. Emitted by the crd backend. |
| Received from peer & injected | `go_pmtud_injected_packets_total` | `node`, `source` | `source` = relaying peer (node name for crd, peer IP for udp). |

`go_pmtud_recv_packets_total` was previously incremented in three places (capture,
crd receive, udp receive) — conflating two opposite pipeline directions and making
`sum(rate(recv_packets[1m]))` meaningless. It is now **capture-only**, which is what
the existing eu-de-1 dashboard already assumed, so that query keeps working unchanged.

The `result` split on `go_pmtud_relay_send_total` makes the dedup ratio a direct
query — `deduplicated / (created + deduplicated)` — which is the key input for the
volume analysis below (measured create rate = capture rate × the non-dedup fraction).
The udp backend keeps its per-peer `sent_packets*`/`sent_error` counters and does not
emit `relay_send_total` (it has no CRs and no dedup).

**Rationale**: One counter per event makes each metric independently meaningful and
lets operators measure the actual etcd write rate and dedup effectiveness — the
numbers the CRD volume analysis (Decision 4 / Risks) now depends on rather than
assuming.

## Testing Strategy

Three layers. Unit tests are mandatory and run in CI; local integration is for
developer iteration and manual/pre-merge validation; E2E validates the real
mechanism end to end.

### Layer 1 — Unit (CI, every PR)

- **Relay interface**: a fake `Relay` records `Send` calls and drives `inject`;
  assert the nflog controller calls `Send` with the captured payload and that the
  injector runnable forwards received payloads to `Inject`.
- **UDP backend**: preserve the existing `receiver_test.go` cases (valid inject,
  invalid payload rejected, unknown-source rejected, known-peer accepted), moved
  under the backend.
- **CRD backend**: use `envtest` (setup-envtest already in CI via `KUBEBUILDER_ASSETS`)
  with the generated CRD installed. Assert:
  - `Send` creates a `PMTUNodeRelay` with correct name/fields; duplicate `Send`
    is a no-op (`AlreadyExists`).
  - A watch fires `inject` with the decoded payload and then deletes the object.
  - An object with `sourceNode == own node` is skipped (not injected).
  - GC deletes an object past `expiresAt`.
- **Dedup / name derivation**: unit test `sha256(payload)[:8]` naming is stable.

### Layer 2 — Local integration (developer iteration, non-CI)

Two options, both documented in `lab/`:

- **Network namespaces (fast, no cluster)** — a shell harness (`ip netns`) per the
  task doc: `node-a`, `node-b`, `router`, `destination`; destination-side veth MTU
  1400; static routes force the ICMP 3/4 toward the "wrong" node. Run go-pmtud in
  `node-a`/`node-b`. Best for iterating on the injection/loop mechanism. This layer
  is transport-agnostic and primarily exercises capture + TUN inject; the CRD
  backend additionally needs a local API server (k3s/kind/envtest binary).
- **Kind (closest to prod, exercises K8s wiring)** — extend the existing `lab/`
  two-cluster Kind setup (from the `kind-cluster-lab` change). It already validates
  the UDP backend end to end (traffic generator + `verify-pmtu.sh` checking
  `ip route get ... mtu 1400`). Add a parameter to deploy with
  `--relay-backend=crd` and install the CRD + RBAC, then run the same traffic and
  verification. This is the primary way to exercise the CRD backend locally because
  it provides a real API server and multiple daemon pods.

### Layer 3 — E2E (automated preferred)

- **Automated (target)**: parameterize the Kind lab's `test-e2e.sh` over
  `RELAY_BACKEND in {udp, crd}`. For each: bring up clusters/router, deploy
  go-pmtud with that backend, generate oversized traffic, assert the PMTU cache
  updates on the node that did NOT receive the ICMP natively, and assert the
  negative (`tcpdump` shows the ICMP arriving only on the peer, injected on the
  target). Wire this into a CI job (nightly or label-gated) since it needs Docker +
  Kind and is heavier than unit tests. This reuses existing lab scripts, so the
  automation cost is a backend parameter plus a CI workflow entry.
- **Cross-region (manual, out of automation scope)**: the task doc's Phase 2
  (real cross-region cluster via Terraform/Pulumi) validates the true target
  topology. Documented as a manual runbook; not automated here because it needs
  cloud infrastructure. Called out explicitly so it is not mistaken for covered.

**Coverage matrix:**

| Concern | Unit | Netns | Kind | E2E |
|---|---|---|---|---|
| Relay interface contract | ✅ | — | — | — |
| UDP backend behavior (unchanged) | ✅ | ✅ | ✅ | ✅ |
| CRD create/watch/inject/delete | ✅ (envtest) | — | ✅ | ✅ |
| CRD GC of expired objects | ✅ | — | ✅ | — |
| Kernel PMTU cache update | — | ✅ | ✅ | ✅ |
| Loop prevention (`! -i pmtud0`) | — | ✅ | ✅ | ✅ |
| Real cross-region topology | — | — | — | manual |

## Risks / Trade-offs

- **[CRD broadcast fan-out]** → every daemon pod injects every relayed event.
  Harmless per-packet (kernel caches unused entries) and matches UDP, but the load
  is `created-CR-rate × N nodes` watch deliveries — this `× N` term, not the create
  count, is the real scaling cost. Mitigation available (targeting, Decision 4) if
  `relay_send_total` / node count show storms.
- **[Replication volume — measured on eu-de-1]** → the original "low / cold path"
  assumption is optimistic. On eu-de-1 (`master`, L2 build, where
  `sum(rate(go_pmtud_recv_packets_total[1m])) * 60` is a clean cluster-wide capture
  rate with no dedup or fan-out double-count), captures run **~10–50/min at baseline
  with regular spikes to ~280–390/min (≈4.7–6.5 packets/s at peak)**. Implications
  for the CRD backend:
  - **CR creates/min** ≈ capture rate × (1 − dedup fraction). Additive across nodes
    (one `Create` per capture, deduped by name) — **no `× N` multiplier** on creates.
    Worst case (no dedup benefit) ≈ the capture rate above; measure the real figure
    via `rate(go_pmtud_relay_send_total{result="created"}[1m])` and the dedup ratio
    via `deduplicated / (created + deduplicated)`.
  - **Watch fan-out** = creates/min **× N** watchers — the dominant term (see above).
  - **Live object count** ≈ creates/s × TTL. Note the current code diverges from
    Decision 5: it does **not** delete-after-inject, and its GC deletes only the
    *own* node's expired objects, so objects live the full ~120s TTL. At peak that is
    **~700–800 objects**, each watched by all N nodes. Re-aligning GC with Decision 5
    (delete-after-inject + sweep any expired) would cut this sharply and is the first
    lever if object count bites.
- **[etcd write pressure]** → one small object per (event, srcNode); dedup by name
  collapses duplicates; TTL GC bounds accumulation. Objects are tiny and short-lived,
  but at the eu-de-1 peaks above the write + watch load is non-trivial and should be
  tracked via `relay_send_total` before assuming headroom. UDP remains the default
  for high-volume/latency-sensitive topologies.
- **[API server latency vs UDP]** → CRD relay adds API round-trips on the capture
  hot path (`Send` is called from the nflog callback). Acceptable for the measured
  volume, but not latency-critical; UDP remains the default for latency-sensitive
  topologies.
- **[Namespace discovery]** → relies on `POD_NAMESPACE` (downward API) or explicit
  `--relay-namespace`. Fail fast at startup if the CRD backend is selected and no
  namespace resolves.
- **[Backend parity in tests]** → risk of the CRD path drifting from UDP behavior.
  Mitigated by the shared Relay-contract unit test and the backend-parameterized
  E2E running both.
- **[E2E automation feasibility]** → Kind-based E2E needs Docker + privileged
  containers in CI. If unavailable, keep it as a documented manual runbook and
  gate the CI job accordingly (do not silently skip).
