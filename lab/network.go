// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"fmt"
	"os/exec"
	"strings"
)

const (
	// TransitNetwork is the second docker network attached to every node so the
	// daemon can capture NFLOG on eth1 while replication stays on eth0. Kind
	// attaches its default network first (eth0); this one becomes eth1.
	TransitNetwork = "pmtud-transit"
	TransitSubnet  = "172.32.0.0/24"
	transitIface   = "eth1"
)

// setupTransitNetwork creates the transit docker network, attaches every node
// (adding eth1), and installs a scoped 8.8.8.8/32 route via the control-plane's
// transit IP on each worker. util.GetDefaultInterface resolves the capture
// interface with RouteGet(8.8.8.8), so this flips NFLOG capture to eth1 while
// leaving the default route — and therefore replication over eth0 — untouched.
// Idempotent: safe to re-run against an existing lab (LAB_REUSE).
func setupTransitNetwork(l *Lab) error {
	if out, err := exec.Command("docker", "network", "create",
		"--subnet", TransitSubnet, TransitNetwork).CombinedOutput(); err != nil {
		if !strings.Contains(string(out), "already exists") {
			return fmt.Errorf("create transit network: %w: %s", err, string(out))
		}
	}

	nodes := append([]string{l.Cluster.ControlPlane}, l.Cluster.Workers...)
	for _, n := range nodes {
		if out, err := exec.Command("docker", "network", "connect",
			TransitNetwork, n).CombinedOutput(); err != nil {
			s := string(out)
			if !strings.Contains(s, "already exists") && !strings.Contains(s, "already connected") {
				return fmt.Errorf("connect %s to transit: %w: %s", n, err, s)
			}
		}
	}

	cpTransitIP, err := containerIPOnNetwork(l.Cluster.ControlPlane, TransitNetwork)
	if err != nil {
		return err
	}
	for _, w := range l.Cluster.Workers {
		if _, err := dockerExec(w, "ip", "route", "replace", "8.8.8.8/32",
			"via", cpTransitIP, "dev", transitIface); err != nil {
			return fmt.Errorf("capture-flip route on %s: %w", w, err)
		}
	}
	return nil
}

// teardownTransitNetwork removes the transit network (best-effort).
func teardownTransitNetwork() {
	_ = exec.Command("docker", "network", "rm", TransitNetwork).Run()
}
