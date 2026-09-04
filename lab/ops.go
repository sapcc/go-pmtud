// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// metricsPort is the daemon's Prometheus endpoint (config default; the
// DaemonSet does not override --metrics_port). The daemon runs hostNetwork, so
// the endpoint is reachable on the node's host netns.
const metricsPort = "30040"

// GenerateTraffic sends a DF-set ping larger than the hop MTU from worker-A.
// The hop returns an ICMP frag-needed; ping exits non-zero (blackhole), so the
// reported "mtu = N" line — not the exit code — is the success signal.
func (l *Lab) GenerateTraffic(ctx context.Context) error {
	if len(l.Cluster.Workers) == 0 {
		return fmt.Errorf("no workers")
	}
	worker := l.Cluster.Workers[0]
	b, _ := exec.CommandContext(ctx, "docker", "exec", worker,
		"ping", "-M", "do", "-s", "1400", "-c", "3", "-W", "2", l.BlackholeIP).CombinedOutput()
	if parsePingFragNeeded(string(b)) > 0 {
		return nil
	}
	return fmt.Errorf("no ICMP frag-needed in ping output: %s", string(b))
}

// parsePingFragNeeded returns the MTU reported in a ping frag-needed line, or 0.
func parsePingFragNeeded(out string) int {
	return firstIntAfter(out, "mtu")
}

func (l *Lab) PMTUTo(node, dst string) (int, error) {
	out, err := dockerExec(node, "ip", "route", "get", dst)
	if err != nil {
		return 0, err
	}
	if m := parseRouteMTU(out); m > 0 {
		return m, nil
	}
	return 0, fmt.Errorf("no MTU in route output: %s", out)
}

func parseRouteMTU(out string) int {
	return firstIntAfter(out, "mtu")
}

// firstIntAfter finds `key`, skips non-digits, and returns the first integer run
// (0 if none). Handles both "mtu 1280" and "mtu=1280".
func firstIntAfter(s, key string) int {
	i := indexOf(s, key)
	if i == -1 {
		return 0
	}
	j := i + len(key)
	for j < len(s) && (s[j] < '0' || s[j] > '9') {
		j++
	}
	n, started := 0, false
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		n = n*10 + int(s[j]-'0')
		j++
		started = true
	}
	if !started {
		return 0
	}
	return n
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func (l *Lab) FlushRouteCache(node string) error {
	dockerExec(node, "ip", "route", "flush", "cache") // best-effort; no-op on kernels without route cache
	return nil
}

// InjectedPackets returns the summed go_pmtud_injected_packets_total counter
// scraped from the daemon on the node's host netns. On a peer this counts
// relay-injected packets — the peer never captures a frag-needed natively (only
// worker-A routes through the hop) — so an increase after GenerateTraffic proves
// a replicated frag-needed was delivered and injected on that peer.
func (l *Lab) InjectedPackets(node string) (int, error) {
	out, err := dockerExec(node, "curl", "-s", "http://127.0.0.1:"+metricsPort+"/metrics")
	if err != nil {
		return 0, fmt.Errorf("scrape metrics on %s: %w", node, err)
	}
	return sumMetric(out, "go_pmtud_injected_packets_total"), nil
}

// SentPackets returns the total go_pmtud_sent_packets_total on node.
// Used to verify the L2 backend relayed at least one packet.
func (l *Lab) SentPackets(node string) (int, error) {
	out, err := dockerExec(node, "curl", "-s", "http://127.0.0.1:"+metricsPort+"/metrics")
	if err != nil {
		return 0, fmt.Errorf("scrape metrics on %s: %w", node, err)
	}
	return sumMetric(out, "go_pmtud_sent_packets_total"), nil
}

// sumMetric sums the values of all non-comment samples of a Prometheus metric.
func sumMetric(out, name string) int {
	total := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) == 0 || line[0] == '#' || !strings.HasPrefix(line, name) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
			total += int(v)
		}
	}
	return total
}

// sumMetricPeer sums the values of metric `name` samples whose peer label
// equals peerIP (e.g. go_pmtud_sent_packets_peer{...,peer="172.18.0.5"}).
func sumMetricPeer(out, name, peerIP string) int {
	needle := `peer="` + peerIP + `"`
	total := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) == 0 || line[0] == '#' || !strings.HasPrefix(line, name) {
			continue
		}
		if !strings.Contains(line, needle) {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if v, err := strconv.ParseFloat(fields[len(fields)-1], 64); err == nil {
			total += int(v)
		}
	}
	return total
}

// SentPacketsPeer returns go_pmtud_sent_packets_peer scraped from `node`, summed
// over samples whose peer label is peerNode's kind-network InternalIP. This is
// the L2/legacy peer-delivery observable, read on the originator: it proves the
// relay resolved that peer's MAC (ARP on the replication interface) and wrote a
// frame to it. Taking a node name keeps it consistent with the rest of the API.
func (l *Lab) SentPacketsPeer(node, peerNode string) (int, error) {
	peerIP, err := containerIP(peerNode)
	if err != nil {
		return 0, err
	}
	out, err := dockerExec(node, "curl", "-s", "http://127.0.0.1:"+metricsPort+"/metrics")
	if err != nil {
		return 0, fmt.Errorf("scrape metrics on %s: %w", node, err)
	}
	return sumMetricPeer(out, "go_pmtud_sent_packets_peer", peerIP), nil
}
