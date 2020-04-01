# go-pmtud

`go-pmtud` is a simplified implementation of [cloudflare/pmtud](https://github.com/cloudflare/pmtud) in Go.

## Problem

Using ECMP (Equal Cost Multi Path) on bare metal Kubernetes clusters makes load sharing of traffic possible (e.g. by using service addresses of type `ExternalIP`).

Hosts (and Pods) try to leverage full MTU size that is derived from their interface configuration (e.g. 9000 bytes).

If `(1)` MTU is smaller somewhere in the path between sender and receiver and `(2)` packet has a `DF (do-not-fragment) bit` set, router sends `ICMP Destination Unreachable message` (type 3 code 4 message) to sender (originator of too large packets).

In case of ECMP it may not reach the original sender, thus breaking the communication.

More details in this blog post by Cloudflare: [Path MTU discovery in practice](https://blog.cloudflare.com/path-mtu-discovery-in-practice/).

`go-pmtud` replicates `ICMP Destination Unreachable` packets to all nodes in same Kubernetes cluster, so that the sender gets awareness that it has to use smaller packets for particular destination.

## Concept

1. `ICMP Destination Unreachable message` (type 3 code 4 message) packets are filtered and sent to specific NFlog group by `iptables`.

2. `go-pmtud` replicates ICMP packets to all nodes in same Kubernetes cluster, so that the sender pod gets awareness that it has to use smaller packets for particular destination.

```
Exec into the pod:

# ip route get 192.168.100.10
192.168.100.10 via 192.100.0.1 dev eth0 src 192.100.0.50
    cache  expires 484sec mtu 9000  <<<< connection is failing

# ip route get 192.168.100.10
192.168.100.10 via 192.100.0.1 dev eth0 src 192.100.0.50
    cache  expires 484sec mtu 8996  <<<< correct MTU information, connection is working
```

## Build

Build from source:

```
go mod download
go build -v -o /go-pmtud cmd/go-pmtud/main.go
```

Build a `Docker` image:

```
docker build -t go-pmtud .
```

## go-pmtud options

Following options are available:

1. `peers` - list of peers to re-send ICMP packets to.
2. `iface` - interface that listens for ICMP packets and resends them to other peers.
3. `nodename` - node hostname, used for metric label.
4. `nflog-group` - NFLOG group, set to 33 in our case.
5. `metrics-port` - Port for Prometheus metrics (30040 by default).
6. `ttl` - TTL of replicated ICMP packets.

## Example - go-pmtud Daemonset

`go-pmtud` can run as a [Daemonset](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/), [example](https://github.com/sapcc/helm-charts/blob/master/system/go-pmtud).

Example `values.yaml`:

```
images:
  iptables:
    repository: sapcc/iptables
    tag: v20191226161919
  pmtud:
    repository: sapcc/go-pmtud
    tag: latest

iptables:
  nflogGroup: 33
  ignoreSourceNetwork: 192.168.100.0/24

pmtud:
  ttl: 10
  metricsPort: 30040
  interface: eth0
  peers: 192.168.100.2, 192.168.100.3, 192.168.100.4, 192.168.100.5, 192.100.0.50
```

## Example - iptables and NFlog

There is an `iptables` rule on each node that redirects ICMP Destination Unreachable` packets to NFlog group nr. 33:

`iptables -t raw -D PREROUTING -i <interface> ! -s 192.168.100.0/24 -p icmp -m icmp --icmp-type 3/4 --j NFLOG --nflog-group 33`

Important: we ignore packets from summarized source network of all nodes in the local cluster (e.g. `! -s 192.168.100.0/24`) to avoid re-sending loops.

This means a node will not re-send already retransmitted ICMP messages. It will only resend messages that are ussually originated by routers on the path. 

During the container startup an `initContainer` checks if the following rule already exists on the node. If not, rule is created.

On pod termination a `preStop` hook removes the same rule.

```
        lifecycle:
          preStop:
            exec:
              command:
              - /bin/sh
              - -c
              - iptables -t raw -D PREROUTING -i eth0 ! -s 192.168.100.0/24 -p icmp -m icmp --icmp-type 3/4 --j NFLOG --nflog-group 33
```

## License
This project is licensed under the Apache2 License - see the [LICENSE](LICENSE) file for details
