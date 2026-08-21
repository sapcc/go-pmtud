# In-binary firewall lifecycle (drop init container + preStop)

- **Date:** 2026-08-21
- **Branch:** `d053727/runtime-image-from-scratch`
- **Status:** Approved (design)

## Problem

The runtime image was migrated to `FROM scratch` (see [Dockerfile](../../../Dockerfile)),
which contains no shell and no `iptables`/`ip` binaries. The helm chart
([sapcc/helm-charts `system/go-pmtud`](https://github.com/sapcc/helm-charts/tree/master/system/go-pmtud))
relies on shell scripts for the packet-capture plumbing:

- An **init container** (separate `iptables` image) runs `iptables-init.sh`, which:
  1. disables `rp_filter` (`sysctl net.ipv4.conf.all.rp_filter=0` plus one per replication interface), and
  2. installs an NFLOG rule in the `raw`/`PREROUTING` chain that copies incoming
     ICMP `frag-needed` packets (type 3, code 4) on the default-route interface to
     an NFLOG group.
- A **`preStop` lifecycle hook** on the main pmtud container runs `iptables-stop.sh`
  (`#!/usr/bin/env bash`) to delete that NFLOG rule.

The `preStop` hook executes **inside the pmtud container**. On `scratch` there is no
shell/`env`/`ip`/`iptables-nft`, so the hook fails with `ENOENT` and the NFLOG rule is
never removed on shutdown — leaving stale host `nftables` rules that accumulate on
every pod restart/reschedule. The init container itself is *not* the blocker (it runs
in its own image), but the `preStop` hook is.

## Goal

Move the full NFLOG rule lifecycle **and** the `rp_filter` sysctl setup into the
go-pmtud binary, so:

- the `scratch` image needs no shell or external binaries, and
- the helm chart can drop **both** the init container and the `preStop` hook.

## Non-goals

- Restoring `rp_filter` to its prior value on shutdown (see Decisions).
- Changing the packet-capture semantics (same match: ICMP type 3 code 4 on the
  default-route interface, copied to the configured NFLOG group).
- Adding new command-line flags or config (the binary already has everything needed).

## Decisions

1. **Full lifecycle ownership.** The binary creates the rule on startup and deletes it
   on shutdown, using its **own dedicated `nftables` table**. This avoids trying to
   pattern-match and delete a rule created by the `iptables-nft` translation layer,
   which is fragile.
2. **Move `rp_filter` sysctls into the binary too**, letting the chart drop the init
   container entirely.
3. **Do not restore `rp_filter` on shutdown.** Matches current production behavior.
   `net.ipv4.conf.all.rp_filter` is shared, node-global state (hostNetwork) that pmtud
   does not exclusively own; restoring a remembered value can clobber other writers and
   causes `0 → 1 → 0` flapping on every DaemonSet pod cycle, momentarily re-enabling the
   reverse-path filtering that pmtud's L3 resend mechanism depends on. `rp_filter=0` is
   pmtud's intended steady state on a node that runs it.
4. **Library:** `github.com/google/nftables` — pure-Go, netlink-based, no external
   binary, works on `scratch`. The repo already depends on `vishvananda/netlink` for
   interface discovery; `google/nftables` is the standard pure-Go choice for rule
   management.

## Design

### New package `internal/firewall`

A `Manager` owns the two host mutations, mirroring exactly what the shell scripts did.

```go
type Manager struct {
    Cfg *config.Config
    Log logr.Logger
    // seams for testing (see Testability)
}

func New(cfg *config.Config, log logr.Logger) *Manager

// Setup replaces iptables-init.sh: sets rp_filter=0, then creates the nftables
// table/chain/rule. Idempotent (safe to re-run: table is (re)created cleanly).
func (m *Manager) Setup() error

// Teardown replaces iptables-stop.sh: deletes the pmtud nftables table.
// Best-effort and idempotent (deleting a non-existent table is not an error).
func (m *Manager) Teardown() error
```

#### `Setup()`

1. **rp_filter:** write `0` to `/proc/sys/net/ipv4/conf/all/rp_filter` and to
   `/proc/sys/net/ipv4/conf/<iface>/rp_filter` for each `cfg.InterfaceNames`.
   (Both `all` and per-interface are required because the effective value is
   `max(all, <iface>)`.)
2. **nftables rule:**
   - Table: family `ip` (IPv4), name `pmtud`.
   - Chain: base chain `prerouting`, type `filter`, hook `prerouting`,
     priority `raw` (-300), policy `accept`.
   - Rule (expressions), matching `-i <default-iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group <group>`:
     1. `meta iifname == cfg.DefaultInterface`
     2. `ip protocol == icmp` (network-header offset 9, len 1, == 1)
     3. ICMP `type == 3` (transport-header offset 0, len 1)
     4. ICMP `code == 4` (transport-header offset 1, len 1)
     5. `log group <cfg.NfGroup>` (NFLOG; non-terminating, so packets continue —
        matches `-j NFLOG`).
   - Applied by (re)adding the table then flushing the connection. To keep `Setup`
     idempotent across restarts, delete any existing `pmtud` table first, then add.

`cfg.DefaultInterface` (default-route interface) and `cfg.InterfaceNames` are already
populated by `preRunRootCmd` via `util.GetDefaultInterface` / `util.GetReplicationInterface`,
and `cfg.NfGroup` is an existing flag — **no new configuration is needed**.

#### `Teardown()`

Delete the `pmtud` table (removes its chain and rule in one operation) and flush.
`rp_filter` is intentionally left as-is.

### Lifecycle integration ([internal/cmd/command.go](../../../internal/cmd/command.go))

In `runRootCmd`, before `mgr.Start`:

```go
fw := firewall.New(&cfg, log.WithName("firewall"))
if err := fw.Setup(); err != nil {
    log.Error(err, "firewall setup failed")
    return err
}
defer func() {
    if err := fw.Teardown(); err != nil {
        log.Error(err, "firewall teardown failed")
    }
}()

// ... existing manager/controller wiring ...

err = mgr.Start(signals.SetupSignalHandler())
```

`signals.SetupSignalHandler()` already cancels the context on SIGTERM/SIGINT, so
`mgr.Start` returns on shutdown and the deferred `Teardown()` runs — comfortably within
the chart's `terminationGracePeriodSeconds: 5`. `Setup()` runs before `mgr.Start`
(replacing the init container's "before main container" ordering) so the rule and
sysctls are in place before the NFLOG reader starts.

> Note: `Setup` is placed after the `os.Exit`-prone config/manager construction and
> before `mgr.Start`, so the remaining path returns errors (not `os.Exit`) and the
> deferred teardown always runs.

## Testability

- **sysctl:** the writer takes an injectable filesystem root (default `/`). Unit test
  with a temp dir asserts `0` is written to the correct `.../conf/all/rp_filter` and
  per-interface paths — runs unprivileged.
- **nftables construction:** split rule/table/chain **construction** (pure, returns the
  `google/nftables` structs) from **application** (the netlink flush). Unit-test the
  built expressions against the intended rule without touching the kernel.
- **Integration (optional, skipped by default):** a real `Setup`/`Teardown` round-trip
  inside a fresh network namespace, guarded to skip when not running as root
  (`CAP_NET_ADMIN`). Default CI (`make build/cover.out` on `ubuntu-latest`, unprivileged)
  cannot apply nftables rules, so this test must self-skip there.

## Companion helm-chart change (separate repo — not in this repo)

These land in `sapcc/helm-charts` `system/go-pmtud` and **must ship together** with the
image change (otherwise the init container's rule and the binary's rule coexist, causing
duplicate NFLOG delivery and duplicate ICMP resends during rollout):

- Remove the `initContainers` block (`iptables-init`).
- Remove the `lifecycle.preStop` hook from the pmtud container.
- Remove the `pmtud` configMap volume and its `volumeMounts` from both containers.
- Delete the `go-pmtud-configmap.yaml` template and the `etc/_iptables_init.tpl` /
  `etc/_iptables_stop.tpl` scripts.
- Remove `images.iptables` from `values.yaml` (and the iptables image build).
- Keep `securityContext.privileged: true` and `hostNetwork: true` on the pmtud
  container (required to write `/proc/sys` and manage nftables).
- Bump `images.pmtud.tag` to the new scratch image built from this branch.

## Risks / mitigations

- **Rollout coexistence:** duplicate rules if only one repo is deployed → land both PRs
  together; document the ordering in the chart PR.
- **Privileges:** nftables and `/proc/sys` writes require `CAP_NET_ADMIN` / privileged;
  already satisfied by the existing pod securityContext.
- **`google/nftables` availability on scratch:** pure-Go, uses netlink sockets only — no
  runtime dependency; verified by the goal of a shell-free image.
