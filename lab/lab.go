// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterName  = "pmtud"
	HopIfaceName = "pmtudlab0"
	HopIP        = "10.99.0.1"
	HopSubnet    = "10.99.0.0/24"
	blackholeIP  = "10.99.0.2"
	HopMTU       = 1280
)

type Cluster struct {
	Name           string
	KubeconfigPath string
	Client         client.Client
	Workers        []string
	ControlPlane   string // docker container of the control-plane node (low-MTU hop)
}

type Lab struct {
	Cluster     *Cluster
	BlackholeIP string
}

func Provision(ctx context.Context) (*Lab, error) {
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}
	config := filepath.Join(repoRoot, "lab/configs/kind-cluster.yaml")

	c, err := createCluster(ctx, ClusterName, config)
	if err != nil {
		return nil, err
	}
	if c.ControlPlane == "" {
		return nil, fmt.Errorf("no control-plane node discovered for cluster %s", ClusterName)
	}
	if len(c.Workers) < 2 {
		return nil, fmt.Errorf("need >=2 workers, found %d", len(c.Workers))
	}

	l := &Lab{Cluster: c, BlackholeIP: blackholeIP}

	if err := setupTransitNetwork(l); err != nil {
		return nil, err
	}

	if err := configureHop(ctx, l); err != nil {
		return nil, err
	}
	if err := ensurePing(l.Cluster.Workers[0]); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Lab) Teardown(ctx context.Context) error {
	if os.Getenv("LAB_KEEP") != "" {
		return nil
	}
	err := deleteCluster(ctx, ClusterName)
	teardownTransitNetwork()
	return err
}
