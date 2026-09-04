// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"strconv"
)

// configureHop turns the control-plane node into a low-MTU forwarding hop and
// routes the blackhole subnet from worker-A through it, so a DF-set ping from
// worker-A elicits a real ICMP frag-needed the NFLOG rule can capture.
func configureHop(ctx context.Context, l *Lab) error {
	cp := l.Cluster.ControlPlane

	if _, err := dockerExec(cp, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("enable ip_forward on hop: %w", err)
	}
	if err := createHopIface(cp); err != nil {
		return err
	}
	if _, err := dockerExec(cp, "ip", "addr", "replace", HopIP+"/24", "dev", HopIfaceName); err != nil {
		return fmt.Errorf("assign hop address: %w", err)
	}

	// Route the blackhole subnet from worker-A via the control-plane's TRANSIT
	// IP (eth1), so the DF-set ping traverses eth1 — the capture interface.
	cpTransitIP, err := containerIPOnNetwork(cp, TransitNetwork)
	if err != nil {
		return err
	}
	worker := l.Cluster.Workers[0]
	if _, err := dockerExec(worker, "ip", "route", "replace", HopSubnet, "via", cpTransitIP); err != nil {
		return fmt.Errorf("route worker-A -> hop subnet: %w", err)
	}

	// The hop must source its ICMP frag-needed from a NON-node address (per RFC
	// 1191). Pin the CP's return route to worker-A out eth1 with src HopIP so the
	// error sources from the low-MTU hop address, not the CP node IP (which is in
	// worker-A's PeerList and would be dropped as peer-originated).
	workerATransitIP, err := containerIPOnNetwork(worker, TransitNetwork)
	if err != nil {
		return err
	}
	if _, err := dockerExec(cp, "ip", "route", "replace", workerATransitIP+"/32",
		"dev", "eth1", "src", HopIP); err != nil {
		return fmt.Errorf("pin hop return route to worker-A: %w", err)
	}
	return nil
}

// createHopIface creates pmtudlab0 clamped to HopMTU. Prefers a dummy device;
// falls back to a veth pair (far end left down) if the dummy module is absent.
func createHopIface(node string) error {
	dockerExec(node, "ip", "link", "del", HopIfaceName) // best-effort cleanup

	if _, err := dockerExec(node, "ip", "link", "add", HopIfaceName, "type", "dummy"); err != nil {
		if _, err2 := dockerExec(node, "ip", "link", "add", HopIfaceName,
			"type", "veth", "peer", "name", HopIfaceName+"p"); err2 != nil {
			return fmt.Errorf("create hop iface (dummy: %v; veth: %w)", err, err2)
		}
	}
	if _, err := dockerExec(node, "ip", "link", "set", HopIfaceName, "mtu", strconv.Itoa(HopMTU)); err != nil {
		return fmt.Errorf("set hop mtu: %w", err)
	}
	if _, err := dockerExec(node, "ip", "link", "set", HopIfaceName, "up"); err != nil {
		return fmt.Errorf("bring up hop iface: %w", err)
	}
	return nil
}

// ensurePing installs iputils-ping on the given node if ping is missing.
func ensurePing(node string) error {
	if err := run("docker", "exec", node, "sh", "-c",
		"command -v ping >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y --no-install-recommends iputils-ping)"); err != nil {
		return fmt.Errorf("ensure ping on %s: %w", node, err)
	}
	return nil
}
