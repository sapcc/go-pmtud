<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# go-pmtud

[![CI](https://github.com/sapcc/go-pmtud/actions/workflows/ci.yaml/badge.svg)](https://github.com/sapcc/go-pmtud/actions/workflows/ci.yaml)
[![Go Report Card](https://goreportcard.com/badge/github.com/sapcc/go-pmtud)](https://goreportcard.com/report/github.com/sapcc/go-pmtud)

`go-pmtud` is a simplified implementation of [cloudflare/pmtud](https://github.com/cloudflare/pmtud) in Go.

## Problem

Using ECMP (Equal Cost Multi Path) on bare metal Kubernetes clusters makes load sharing of traffic possible (e.g. by using service addresses of type `ExternalIP`).

Hosts (and Pods) try to leverage full MTU size that is derived from their interface configuration (e.g. 9000 bytes).

If `(1)` MTU is smaller somewhere in the path between sender and receiver and `(2)` packet has a DF (do-not-fragment) bit set, router sends ICMP Destination Unreachable message (type 3 code 4 message) to sender (originator of too large packets).

In case of ECMP it may not reach the original sender, thus breaking the communication.

More details in this blog post by Cloudflare: [Path MTU discovery in practice](https://blog.cloudflare.com/path-mtu-discovery-in-practice/).

go-pmtud replicates ICMP Destination Unreachable packets to all nodes in same Kubernetes cluster, so that the sender gets awareness that it has to use smaller packets for a particular destination.

## Architecture

```
iptables (NFLOG group 33)
        │
        ▼
  nflog controller          ← captures ICMP type 3 code 4 packets
        │
        ▼
  relay backend             ← distributes packets to all (on other node)
        │
        ▼
  TUN device (pmtud0)       ← injects replicated packets into the network stack
```

Each node runs go-pmtud as a DaemonSet. When an ICMP frag-needed packet arrives, iptables redirects it to an NFLOG group. go-pmtud reads it, replicates it via the configured relay backend, and re-injects it on every other node. The `udp` backend injects via the `pmtud0` TUN device; the `l2` backend sends raw Ethernet frames and requires no TUN device.

## Relay Backends

go-pmtud relays captured ICMP packets between nodes through a pluggable `Relay` interface (`internal/relay`). The backend is selected with `--relay-backend`; `l2` is the default.

### `l2` (default)

Relay packets are sent as raw Ethernet frames directly over the interface specified by `--iface_names`. Nodes must share L2 adjacency (same VLAN). This backend requires `CAP_NET_RAW` and creates no TUN device.

NFLOG rule — capture on the primary replication interface:

```sh
iptables -t raw -A PREROUTING -i <iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group 33
```

L2-specific flags: `--iface_names` (required), `--iface_mtu` (default `1500`), `--node-timeout-minutes` (default `5`), `--arp-timeout-seconds` (default `1`).

### `udp`

Relay packets are sent via UDP unicast to peer node IPs on `--replication-port` (default `4390`). Peer addresses are discovered from the Kubernetes Node API. This backend works across L3 boundaries, requires `CAP_NET_RAW` + `CAP_NET_ADMIN`, and injects received packets via the `pmtud0` TUN device. The UDP port must be reachable between nodes.

NFLOG rule — must exclude the TUN interface to prevent relay loops:

```sh
iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33
```

### Migration

Existing `l2` deployments upgrade to this image with **no manifest change** — `l2` remains the default and all L2 flags are preserved. To adopt the `udp` backend, set `--relay-backend=udp` and switch the NFLOG rule to the `! -i pmtud0` form above.

## CLI Options

| Flag | Default | Description |
|---|---|---|
| `--nodename` | | Node hostname, used as a metric label |
| `--relay-backend` | `l2` | Relay backend: `l2` (default, raw Ethernet) or `udp` (UDP unicast) |
| `--iface_names` | | Replication interface names (L2 backend, required) |
| `--iface_mtu` | `1500` | MTU for the replication interface (L2 backend) |
| `--node-timeout-minutes` | `5` | ARP cache entry timeout in minutes (L2 backend) |
| `--arp-timeout-seconds` | `1` | ARP request timeout in seconds (L2 backend) |
| `--replication-port` | `4390` | UDP port for packet replication (UDP backend) |
| `--nflog_group` | `33` | NFLOG group number |
| `--ttl` | `1` | TTL of re-injected ICMP packets |
| `--ignore-networks` | | Comma-separated CIDRs — packets from these sources are not relayed |
| `--metrics_port` | `:30040` | Prometheus metrics endpoint |
| `--health_port` | `:30041` | Healthz endpoint |
| `--kube_context` | | Kubeconfig context to use |

All flags can also be set via environment variables prefixed with `PMTUD_` (e.g. `PMTUD_NODENAME`).

## Metrics

Prometheus metrics are exposed on `--metrics_port` (default `:30040`). Every metric carries a `node` label. The two stages of the relay pipeline (capture and inject) each map to exactly one counter:

| Metric | Labels | Meaning |
|---|---|---|
| `go_pmtud_recv_packets_total` | `node`, `source_ip` | ICMP frag-needed packets **captured from the kernel** (nflog) on this node. Capture only. |
| `go_pmtud_injected_packets_total` | `node`, `source` | Packets received from a peer and injected into the local stack via the `pmtud0` TUN device (`udp` backend only; stays zero in `l2` mode). `source` is the relaying peer IP. |

Supporting metrics:

| Metric | Labels | Meaning |
|---|---|---|
| `go_pmtud_sent_packets_total` | `node` | Packets sent to peers, per successful send. |
| `go_pmtud_sent_packets_peer` | `node`, `peer` | Packets sent, per peer. |
| `go_pmtud_sent_error_peer_total` | `node`, `peer` | Send errors, per peer. |
| `go_pmtud_error_total` | `node` | General error counter. |
| `go_pmtud_callback_duration_seconds` | `node` | Histogram of nflog callback duration. |

Useful queries:

```promql
# ICMP frag-needed captured cluster-wide, per minute
sum(rate(go_pmtud_recv_packets_total[1m])) * 60

# Packets injected from peers cluster-wide, per minute
sum(rate(go_pmtud_injected_packets_total[1m])) * 60
```

## Build

```sh
go mod download
go build -o go-pmtud ./cmd/go-pmtud
```

Docker image:

```sh
docker build -t go-pmtud .
```

## iptables and NFlog

Each node needs an iptables rule that redirects ICMP Destination Unreachable packets to the NFLOG group. The correct rule depends on the relay backend in use.

**`l2` backend** (default) — capture on the primary replication interface:

```sh
iptables -t raw -A PREROUTING -i <iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group 33
```

**`udp` backend** — the rule **must** exclude the `pmtud0` TUN interface to prevent replication loops:

```sh
iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33
```

Optionally use `--ignore-networks` to suppress packets from known infrastructure networks (e.g. node subnets) as an additional safety layer.

## Example — DaemonSet

go-pmtud is designed to run as a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). A Helm chart is available at [sapcc/helm-charts](https://github.com/sapcc/helm-charts/blob/master/system/go-pmtud).

## Container Lifecycle

The binary manages firewall state during its runtime:

**On startup** ([`internal/cmd/command.go:96-105`](internal/cmd/command.go#L96-L105)): The binary calls `firewall.Manager.Setup()`, which:
- Sets `net.ipv4.conf.all.rp_filter=0` and `net.ipv4.conf.<interface>.rp_filter=0` for each configured interface
- Creates an nftables rule in the `raw` chain (priority -300, prerouting hook) that copies ICMP type 3 code 4 packets from the default-route interface to the configured NFLOG group

**On shutdown**: A deferred `firewall.Manager.Teardown()` call removes the nftables rule when the binary receives SIGTERM, ensuring cleanup within the pod's termination grace period.

See [`internal/firewall/`](internal/firewall/) for the implementation (Linux only; non-Linux platforms get a no-op stub).

## License

This project is licensed under the Apache 2.0 License — see the [LICENSE](LICENSE) file for details.
