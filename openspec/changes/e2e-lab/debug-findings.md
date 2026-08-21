<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
SPDX-License-Identifier: Apache-2.0
-->

# E2E debugging — why peer PMTU never converges

Live cluster `pmtud` (kept): control-plane 172.18.0.12 (hop), worker2 172.18.0.11
= **worker-A** (originator, route to 10.99.0.2 via CP), worker 172.18.0.13 =
**worker-B** (peer under test). Blackhole 10.99.0.2 on CP `pmtudlab0` @ mtu 1280.

## Root cause #1 — CONFIRMED + FIX VALIDATED

The CP hop's frag-needed is sourced from the CP **node InternalIP** (172.18.0.12).
The reconciler adds *every other node* to `PeerList`, so 172.18.0.12 is a peer of
worker-A. The daemon's loop-prevention `isPeerIP(sourceIP)` in
`internal/nflog/controller.go` then **drops** the captured frag-needed →
`Relay.Send` never fires. Log proof: `skipping packet from peer node
{"source":"172.18.0.12"}`.

This is correct production behavior (don't relay node-sourced frag-needed). The
**lab topology** is wrong: per RFC 1191 the frag-needed should source from the
low-MTU router iface (`pmtudlab0` = 10.99.0.1, a non-node IP).

**Fix that works** (apply on CP in `configureHop`): pin the return route so the
ICMP error sources from 10.99.0.1:
```
ip route replace <workerA_InternalIP>/32 dev eth0 src 10.99.0.1
```
After this: ping shows `From 10.99.0.1 ... Frag needed (mtu=1280)`, worker-A logs
`ICMP frag-needed received, resending packet {"ICMP source":"10.99.0.1"}`, the CR
is created (crd backend), and worker-B `pmtud0` RX increments (inject happens).
So relay + transport + inject all work end-to-end after fix #1.

## Root cause #2 — CONFIRMED (mechanism nailed via ICMP counters)

Even with relay+inject working, worker-B `ip route get 10.99.0.2` never shows
mtu 1280. The injected ICMP frag-needed does **not** install a dst-keyed PMTU
route exception on the peer. Two gates, both proven with `/proc/net/snmp`
`Icmp:` counter deltas on worker-B (host netns):

**Gate 1 — outer dst must be LOCAL for the peer kernel to even process it.**
Daemon injects the frag-needed **verbatim** (crd.go `inject(raw)`, no rewrite),
so outer dst = worker-A's node IP 172.18.0.11 — NOT local on worker-B.

| inject | pmtud0 RX | InMsgs / InDestUnreachs on worker-B |
|---|---|---|
| real relay, verbatim (outer dst 172.18.0.11) | +1 (8→9) | **unchanged** (3→3) — kernel drops, dst not local |
| crafted, outer dst 172.18.0.13 (worker-B local) | +1 (9→10) | **+1** (3→4) — kernel processes as dest-unreach |

So bytes reach the TUN every time, but the kernel only hands the packet to
`icmp_rcv` when the outer dst is a local address. Verbatim relay fails gate 1.

**Gate 2 — even when processed, no route fnhe for an ICMP-echo the peer never
sent.** With outer dst local (InDestUnreachs +1), `ip route get 10.99.0.2` on
worker-B STILL shows no mtu and `ip route show cache` is empty. The inner is an
ICMP **echo** that worker-B never originated (no ping socket / no flow to
10.99.0.2). worker-A got its fnhe precisely because it *did* originate that
echo, so the error matched a real flow. A spoofed inner src does not.

Helpers in /tmp/pmtudfix/{craft,decode}.go. craft.go rewrites outer dst + inner
src and recomputes IP+ICMP checksums.

### Root explanation (both gates → one cause)
The lab sources the ping from **worker-A's unique node IP**. The frag-needed is
addressed back to that unique IP, and its inner echo belongs only to worker-A.
No peer owns either. Production works because the flow uses a **shared
anycast/ECMP source IP present (local) on every node** (README example
192.100.0.50 on the peer): outer dst is then local on every peer (gate 1 ok),
and a TCP/UDP flow to that shared src produces a route-level PMTU exception
(gate 2 ok).

### Leading hypotheses for #2 (test after compact)
1. **Verification query is flow-scoped, not dst-only.** Try
   `ip route get 10.99.0.2 from 172.18.0.13` and `ip route show cache` on
   worker-B. (worker-A *does* show it via plain `ip route get` — but that is
   native receipt, not inject.)
2. **ICMP-echo inner can't drive a route exception without a matching ping/raw
   socket.** Kernel `icmp_err → ipv4_update_pmtu` may need the inner flow to
   resolve to a local output route / socket. Production uses TCP/UDP flows whose
   src is a **shared ECMP/anycast address present on all nodes** (README:
   src 192.100.0.50 shown on the *peer*). The lab pings from worker-A's unique
   node IP, so no peer can own that flow.
3. Possibly need an active flow on worker-B (send to 10.99.0.2 first) so a route
   cache entry exists for the fnhe to attach.

### Likely real fix directions (decide after testing hypotheses)
- Give all workers a **shared source IP** (anycast-like), ping from worker-A with
  that src, and solve the frag-needed return path back to that src via the CP.
  Then the injected error matches a peer-ownable flow. OR
- Change what the e2e asserts: verify replication reached the peer via **daemon
  RecvPackets metric + tcpdump of the injected frag-needed on pmtud0**, rather
  than a route-cache exception — because a route-cache exception on a peer may be
  unobservable with an ICMP-echo trigger and no shared src.

## Resolution (Option A — assert delivery reached peer)

Both bugs fixed; fresh `make -C lab e2e` passes 4/4 specs (udp+crd) in ~113s.
- **Fix #1** in `lab/routes.go configureHop`: pin CP return route to worker-A
  with `src <HopIP>` so the frag-needed sources from the non-node hop address
  (relay now fires).
- **Fix #2** (assertion change): `test/e2e/pmtu_test.go` asserts native
  route-cache convergence (==1280) only on the **originator**; for **peers** it
  asserts `go_pmtud_recv_packets_total` strictly increases (delivery + inject).
  New helper `Lab.RecvPackets` (`lab/ops.go`) scrapes the daemon metric via
  `curl` on the node host netns (port 30040).
- Full-fidelity peer route-cache convergence (shared anycast src + TCP/UDP flow)
  deferred as a possible future openspec change (Option B).

## State of the live cluster (historical — cluster since deleted)
- Backend currently `crd` (patched daemonset arg). CP src-route fix #1 applied
  manually and is live. rp_filter disabled on worker-B (manual, revert or
  redeploy to reset). Stale clusters `pmtud-cluster-a`/`-b` still exist — delete.
- Fix #1 must be added to `lab/routes.go configureHop`; it is NOT yet in code.
