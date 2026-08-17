// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Network struct {
	Name   string
	Subnet string
	MTU    string
}

var DefaultNetworks = []Network{
	{Name: "pmtud-net-a", Subnet: "172.30.0.0/16", MTU: "9000"},
	{Name: "pmtud-net-b", Subnet: "172.31.0.0/16", MTU: "9000"},
	{Name: "pmtud-transit", Subnet: "172.32.0.0/24", MTU: "1500"},
}

func createNetworks(ctx context.Context, nets []Network) error {
	for _, n := range nets {
		// check if network exists (idempotent)
		cmd := exec.Command("docker", "network", "inspect", n.Name)
		if cmd.Run() == nil {
			continue // network already exists
		}
		// create network
		if err := run("docker", "network", "create",
			"--driver", "bridge",
			"--subnet", n.Subnet,
			"--opt", "com.docker.network.driver.mtu="+n.MTU,
			n.Name); err != nil {
			return fmt.Errorf("create network %s: %w", n.Name, err)
		}
	}
	return nil
}

func removeNetworks(ctx context.Context, nets []Network) error {
	for _, n := range nets {
		// silently ignore removal failures
		run("docker", "network", "rm", n.Name)
	}
	return nil
}

func parseMTU(s string) string {
	// extract MTU value from docker network create args string
	// e.g., `--opt "com.docker.network.driver.mtu=9000"` → "9000"
	i := strings.Index(s, "mtu=")
	if i == -1 {
		return ""
	}
	j := i + 4
	for k := j; k < len(s); k++ {
		if !isDigit(s[k]) {
			return s[j:k]
		}
	}
	return s[j:]
}

func isDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
