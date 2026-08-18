// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os/exec"
)

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
