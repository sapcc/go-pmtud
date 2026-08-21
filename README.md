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

Each node runs go-pmtud as a DaemonSet. When an ICMP frag-needed packet arrives, iptables redirects it to an NFLOG group. go-pmtud reads it, replicates it via the configured relay backend, and re-injects it on every other node through a TUN device.

## Relay Backends

go-pmtud supports two relay backends, selected with `--relay-backend`.

### `crd` (default)

Relay packets are stored as `PMTUNodeRelay` Kubernetes CRD objects. Every node watches the API server for new objects and injects those originating from other nodes. Expired objects are garbage-collected by each node for its own packets.

This backend requires no direct network connectivity between nodes — it uses the Kubernetes API server as the transport. It does require the `PMTUNodeRelay` CRD to be installed in the cluster:

```sh
kubectl apply -f crd/pmtud.cloud.sap_pmtunoderelays.yaml
```

Relay objects are namespaced; set the namespace via `--relay-namespace` or the `POD_NAMESPACE` environment variable.

### `udp`

Relay packets are sent directly to a peer list over UDP. This is the original replication mechanism. It requires all node IPs to be provided via the node controller and needs UDP port `4390` (configurable) to be open between nodes.

## CLI Options

| Flag | Default | Description |
|---|---|---|
| `--nodename` | | Node hostname, used as a metric label |
| `--relay-backend` | `crd` | Relay backend: `crd` or `udp` |
| `--relay-namespace` | `$POD_NAMESPACE` | Namespace for CRD relay objects (`crd` backend only) |
| `--relay-gc-interval` | `60s` | How often expired CRD relay objects are garbage-collected |
| `--replication-port` | `4390` | UDP port for packet replication (`udp` backend only) |
| `--nflog_group` | `33` | NFLOG group number |
| `--ttl` | `1` | TTL of re-injected ICMP packets |
| `--ignore-networks` | | Comma-separated CIDRs — packets from these sources are not relayed |
| `--metrics_port` | `:30040` | Prometheus metrics endpoint |
| `--health_port` | `:30041` | Healthz endpoint |
| `--kube_context` | | Kubeconfig context to use |

All flags can also be set via environment variables prefixed with `PMTUD_` (e.g. `PMTUD_NODENAME`).

## Metrics

Prometheus metrics are exposed on `--metrics_port` (default `:30040`). Every metric carries a `node` label. The three stages of the relay pipeline each map to exactly one counter:

| Metric | Labels | Meaning |
|---|---|---|
| `go_pmtud_recv_packets_total` | `node`, `source_ip` | ICMP frag-needed packets **captured from the kernel** (nflog) on this node. Capture only. |
| `go_pmtud_relay_send_total` | `node`, `result` | Relay send attempts by outcome: `created`, `deduplicated`, or `error` (`crd` backend). |
| `go_pmtud_injected_packets_total` | `node`, `source` | Packets **received from a peer** and injected via the TUN device. `source` is the relaying peer (node name for `crd`, peer IP for `udp`). |

Supporting metrics:

| Metric | Labels | Meaning |
|---|---|---|
| `go_pmtud_sent_packets_total` | `node` | Packets sent to peers (`udp` backend, per successful send). |
| `go_pmtud_sent_packets_peer` | `node`, `peer` | Packets sent, per peer (`udp` backend). |
| `go_pmtud_sent_error_peer_total` | `node`, `peer` | Send errors, per peer (`udp` backend). |
| `go_pmtud_error_total` | `node` | General error counter. |
| `go_pmtud_callback_duration_seconds` | `node` | Histogram of nflog callback duration. |

Useful queries:

```promql
# ICMP frag-needed captured cluster-wide, per minute
sum(rate(go_pmtud_recv_packets_total[1m])) * 60

# CR creation rate (actual etcd writes) — crd backend
sum(rate(go_pmtud_relay_send_total{result="created"}[1m])) * 60

# Deduplication ratio — how many sends collapsed onto an existing CR
sum(rate(go_pmtud_relay_send_total{result="deduplicated"}[1m]))
  / sum(rate(go_pmtud_relay_send_total{result=~"created|deduplicated"}[1m]))
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

Each node needs an iptables rule that redirects ICMP Destination Unreachable packets to the NFLOG group. The rule **must** exclude the `pmtud0` TUN interface to prevent replication loops:

```sh
iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33
```

Optionally use `--ignore-networks` to suppress packets from known infrastructure networks (e.g. node subnets) as an additional safety layer.

## Example — DaemonSet

go-pmtud is designed to run as a [DaemonSet](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/). A Helm chart is available at [sapcc/helm-charts](https://github.com/sapcc/helm-charts/blob/master/system/go-pmtud).

## License

This project is licensed under the Apache 2.0 License — see the [LICENSE](LICENSE) file for details.
