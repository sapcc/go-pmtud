<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

## 1. Config and CLI cleanup

- [x] 1.1 Remove L2-specific fields from `internal/config/config.go` (ReplicationInterface, PeerEntry.Mac, InterfaceMtu, ArpCacheTimeoutMinutes, ArpRequestTimeoutSeconds) and change PeerList to map[string]string (nodeName → IP)
- [x] 1.2 Update CLI flags in `internal/cmd/cmd.go`: remove `--iface_names`, `--iface_mtu`, `--arp-timeout-seconds`, `--node-timeout-minutes`; add `--replication-port` (default 4390)
- [x] 1.3 Remove `GetReplicationInterface` from `internal/util/util.go` and its call in `preRunRootCmd`

## 2. Remove ARP package

- [x] 2.1 Delete `internal/arp/` package entirely
- [x] 2.2 Remove ARP-related metrics (ArpResolveError) from `internal/metrics/`

## 3. Refactor node reconciler

- [x] 3.1 Rewrite `internal/node/reconciler.go` to store peer IPs (InternalIP from node.Status.Addresses) instead of MAC addresses
- [x] 3.2 Handle node deletion: remove peer from PeerList when node is deleted
- [x] 3.3 Exclude own node using `--nodename` comparison (preserve existing behavior)

## 4. UDP sender in nflog controller

- [x] 4.1 Rewrite `internal/nflog/controller.go` to send ICMP payload via UDP unicast to each peer IP on the replication port instead of raw Ethernet frames
- [x] 4.2 Remove imports of `github.com/mdlayher/ethernet` and `github.com/mdlayher/packet`
- [x] 4.3 Update metrics labels: use peer IP instead of MAC address for SentPacketsPeer/SentError

## 5. UDP receiver and packet injection

- [x] 5.1 Create `internal/receiver/receiver.go`: UDP listener on replication port that receives ICMP payloads
- [x] 5.2 Validate received payloads (must be valid ICMP type 3 code 4 packets using `internal/packet` parser)
- [x] 5.3 Inject valid packets into local network stack via raw ICMP socket (AF_INET, SOCK_RAW, IPPROTO_ICMP)
- [x] 5.4 Add receiver as a runnable to the controller-runtime manager in `internal/cmd/cmd.go`

## 6. Dependency cleanup

- [x] 6.1 Remove `github.com/mdlayher/arp`, `github.com/mdlayher/ethernet`, `github.com/mdlayher/packet` from go.mod and run `go mod tidy`

## 7. Tests and validation

- [x] 7.1 Add unit test for UDP sender (mock UDP connection, verify payload sent to all peers)
- [x] 7.2 Add unit test for UDP receiver (validate packet parsing and rejection of invalid payloads)
- [x] 7.3 Verify project compiles cleanly with `go build ./...`

## 8. Fix: Packet injection via TUN device (replaces raw socket sendto)

- [x] 8.1 Replace raw socket injection in `internal/receiver/receiver.go` with TUN device: create a TUN device on startup, write received ICMP packets to it so they traverse the kernel receive path (ip_input → icmp_rcv → icmp_unreach → PMTU update)
- [x] 8.2 Add TUN device lifecycle management (create on Start, close on context cancellation)
- [x] 8.3 Add `//go:build linux` tag to receiver.go; create `receiver_other.go` stub for non-Linux platforms that returns an error

## 9. Fix: Loop prevention via --ignore-networks + auto peer IP filtering

- [x] 9.1 Add `IgnoreNetworks []string` field to config and `--ignore-networks` CLI flag (comma-separated CIDRs)
- [x] 9.2 Parse CIDRs into `[]*net.IPNet` at startup in `preRunRootCmd` and store in config
- [x] 9.3 In nflog callback, check outer ICMP source IP against ignore networks — skip replication if matched
- [x] 9.4 Add unit test for ignore-networks filtering logic
- [x] 9.5 In nflog callback, auto-filter packets whose source IP matches any known peer node IP (zero-config loop prevention)
- [x] 9.6 Add unit test for peer IP filtering logic

## 10. Fix: Persistent UDP socket for sending (replace per-packet Dial)

- [x] 10.1 In nflog controller, create a single unconnected `*net.UDPConn` (via `net.ListenUDP`) at Start and use `WriteTo()` for each peer instead of `net.Dial()` per peer per packet
- [x] 10.2 Close the send socket on context cancellation

## 11. Fix: Align design with implementation

- [x] 11.1 Remove the 2-byte length prefix claim from design (UDP is already framed, implementation sends raw payload)
- [x] 11.2 Update `go mod tidy` after adding TUN dependency (e.g., `github.com/songgao/water` or raw ioctl)

## 12. Final validation

- [x] 12.1 Verify `go build ./...` passes on current platform
- [x] 12.2 Run `go test ./...` — all tests pass
- [x] 12.3 Verify `go vet ./...` has no issues

## 13. Fix: Receiver source IP validation (review finding: unauthenticated injection)

- [x] 13.1 In receiver, check that `remoteAddr.IP` is in the current peer list before TUN injection — reject and log if not a known peer
- [x] 13.2 Pass `*config.Config` peer list access to the receiver so it can check dynamically
- [x] 13.3 Add unit test: packet from unknown source IP is rejected
- [x] 13.4 Add unit test: packet from known peer IP is accepted

## 14. Fix: Deterministic TUN device name + loop prevention via interface exclusion (review finding: recapture loop)

- [x] 14.1 Set TUN device name to `pmtud0` (use `water.Config.Name` field) instead of kernel-assigned name
- [x] 14.2 Document required iptables rule: `iptables -A OUTPUT -p icmp --icmp-type 3/4 ! -i pmtud0 -j NFLOG --nflog-group 33`
- [x] 14.3 Log the TUN device name and required iptables rule at startup for operator visibility

## 15. Re-validation

- [x] 15.1 Verify `go build ./...` passes
- [x] 15.2 Run `go test ./...` — all tests pass
- [x] 15.3 Verify `go vet ./...` has no issues
