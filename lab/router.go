// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
)

func createRouter(ctx context.Context) (string, error) {
	name := "pmtud-router"

	// check if running (idempotent)
	if err := run("docker", "ps", "-q", "-f", "name=^"+name+"$"); err == nil {
		return name, nil // exists
	}

	// build router image
	if err := run("docker", "build", "-t", "pmtud-router:local", "lab/configs/router/"); err != nil {
		return "", fmt.Errorf("build router image: %w", err)
	}

	// remove stale container
	run("docker", "rm", "-f", name)

	// start router on transit network
	if err := run("docker", "run", "-d",
		"--name", name,
		"--privileged",
		"--network", "pmtud-transit",
		"--ip", "172.32.0.10",
		"pmtud-router:local"); err != nil {
		return "", fmt.Errorf("start router: %w", err)
	}

	// connect to cluster networks
	for _, net := range []string{"pmtud-net-a", "pmtud-net-b"} {
		ip := "172.30.0.10"
		if net == "pmtud-net-b" {
			ip = "172.31.0.10"
		}
		if err := run("docker", "network", "connect", "--ip", ip, net, name); err != nil {
			return "", fmt.Errorf("connect %s to %s: %w", name, net, err)
		}
	}

	// configure interfaces
	if err := configureRouterInterfaces(ctx, name); err != nil {
		return "", err
	}

	return name, nil
}

func configureRouterInterfaces(ctx context.Context, router string) error {
	// net-a: 172.30.0.10 → MTU 9000
	// net-b: 172.31.0.10 → MTU 1500
	// transit: 172.32.0.10 → MTU 1500

	mtuMap := map[string]string{
		"172.30.0.10": "9000",
		"172.31.0.10": "1500",
		"172.32.0.10": "1500",
	}

	for ip, mtu := range mtuMap {
		iface, err := ifaceByIP(router, ip)
		if err != nil {
			continue // interface may not exist yet
		}

		// set MTU
		if err := run("docker", "exec", router, "ip", "link", "set", iface, "mtu", mtu); err != nil {
			return fmt.Errorf("set mtu on %s: %w", iface, err)
		}

		// disable offloads
		dockerExec(router, "ethtool", "-K", iface, "gso", "off", "gro", "off", "tso", "off")
	}

	return nil
}

func removeRouter(ctx context.Context) error {
	run("docker", "rm", "-f", "pmtud-router")
	return nil
}
