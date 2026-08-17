// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
)

func (l *Lab) DeployBackend(ctx context.Context, backend string) error {
	// Build image
	repoRoot := os.Getenv("PWD")
	if repoRoot == "" {
		repoRoot = "."
	}
	if err := run("docker", "build", "-t", "go-pmtud:local", repoRoot); err != nil {
		return fmt.Errorf("build go-pmtud image: %w", err)
	}

	// Load image into both clusters and apply manifests
	for _, c := range []*Cluster{l.ClusterA, l.ClusterB} {
		// Load image into Kind cluster
		if err := run("kind", "load", "docker-image", "go-pmtud:local", "--name", c.Name); err != nil {
			return fmt.Errorf("load image to %s: %w", c.Name, err)
		}

		// Apply CRD if backend=crd
		if backend == "crd" {
			crds := repoRoot + "/crd/pmtud.cloud.sap_pmtunoderelays.yaml"
			if err := c.applyFile(ctx, crds); err != nil {
				return fmt.Errorf("apply CRD to %s: %w", c.Name, err)
			}
		}

		// Apply RBAC
		if err := c.applyFile(ctx, "lab/manifests/rbac.yaml"); err != nil {
			return fmt.Errorf("apply RBAC to %s: %w", c.Name, err)
		}

		// Apply daemonset (inject backend flag)
		if err := c.applyDaemonSet(ctx, "lab/manifests/pmtud-daemonset.yaml", backend); err != nil {
			return fmt.Errorf("apply daemonset to %s: %w", c.Name, err)
		}

		// Wait for rollout
		if err := c.waitRollout(ctx, "kube-system", "go-pmtud"); err != nil {
			return fmt.Errorf("wait rollout %s: %w", c.Name, err)
		}
	}

	// Deploy podinfo workload
	if err := l.deployWorkload(ctx); err != nil {
		return fmt.Errorf("deploy workload: %w", err)
	}

	return nil
}

func (l *Lab) deployWorkload(ctx context.Context) error {
	// TODO: deploy podinfo to cluster-b, generate 1MB test file
	return nil
}
