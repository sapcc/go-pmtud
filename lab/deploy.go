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
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}
	if err := run("docker", "build", "-t", "go-pmtud:local", repoRoot); err != nil {
		return fmt.Errorf("build go-pmtud image: %w", err)
	}

	c := l.Cluster
	if err := run("kind", "load", "docker-image", "go-pmtud:local", "--name", c.Name); err != nil {
		return fmt.Errorf("load image to %s: %w", c.Name, err)
	}
	if err := c.applyFile(ctx, repoRoot+"/lab/manifests/rbac.yaml"); err != nil {
		return fmt.Errorf("apply RBAC: %w", err)
	}
	if err := c.applyDaemonSet(ctx, repoRoot+"/lab/manifests/pmtud-daemonset.yaml", backend); err != nil {
		return fmt.Errorf("apply daemonset: %w", err)
	}
	if err := c.waitRollout(ctx, "kube-system", "go-pmtud"); err != nil {
		return fmt.Errorf("wait rollout: %w", err)
	}
	return nil
}
