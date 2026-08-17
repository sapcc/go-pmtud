// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
)

func configureRoutes(ctx context.Context, l *Lab) error {
	// Cluster-a → cluster-b
	for _, w := range l.ClusterA.Workers {
		_, err := dockerExec(w, "ip", "route", "replace", "172.31.0.0/16", "via", "172.30.0.10")
		if err != nil {
			return fmt.Errorf("route cluster-a → net-b: %w", err)
		}
		_, err = dockerExec(w, "ip", "route", "replace", "10.245.0.0/16", "via", "172.30.0.10")
		if err != nil {
			return fmt.Errorf("route cluster-a → pod-b: %w", err)
		}
	}

	// Cluster-b → cluster-a
	for _, w := range l.ClusterB.Workers {
		_, err := dockerExec(w, "ip", "route", "replace", "172.30.0.0/16", "via", "172.31.0.10")
		if err != nil {
			return fmt.Errorf("route cluster-b → net-a: %w", err)
		}
		_, err = dockerExec(w, "ip", "route", "replace", "10.244.0.0/16", "via", "172.31.0.10")
		if err != nil {
			return fmt.Errorf("route cluster-b → pod-a: %w", err)
		}
	}

	return nil
}

func disableOffloads(ctx context.Context, l *Lab) error {
	for _, nodes := range [][]string{l.ClusterA.Workers, l.ClusterB.Workers} {
		for _, w := range nodes {
			iface, err := ifaceByIP(w, "172.")
			if err != nil {
				continue
			}
			_, _ = dockerExec(w, "ethtool", "-K", iface, "gso", "off", "gro", "off", "tso", "off")
		}
	}
	return nil
}
