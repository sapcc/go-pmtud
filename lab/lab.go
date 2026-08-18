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

type Cluster struct {
	Name           string
	KubeconfigPath string
	Client         client.Client
	Workers        []string
}

type Lab struct {
	ClusterA, ClusterB *Cluster
	Router             string // container name
	DestIP             string // podinfo NodePort host IP
}

func Provision(ctx context.Context) (*Lab, error) {
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}

	if err := createNetworks(ctx, DefaultNetworks); err != nil {
		return nil, err
	}

	configA := filepath.Join(repoRoot, "lab/configs/kind-cluster-a.yaml")
	configB := filepath.Join(repoRoot, "lab/configs/kind-cluster-b.yaml")

	a, err := createCluster(ctx, "pmtud-cluster-a", configA)
	if err != nil {
		return nil, err
	}
	b, err := createCluster(ctx, "pmtud-cluster-b", configB)
	if err != nil {
		return nil, err
	}

	// Connect all cluster nodes to their dedicated networks so that the
	// gateway IPs (172.30.0.10 / 172.31.0.10) are reachable from workers.
	for _, node := range clusterContainers("pmtud-cluster-a") {
		if err := run("docker", "network", "connect", "pmtud-net-a", node); err != nil {
			return nil, fmt.Errorf("connect %s to pmtud-net-a: %w", node, err)
		}
	}
	for _, node := range clusterContainers("pmtud-cluster-b") {
		if err := run("docker", "network", "connect", "pmtud-net-b", node); err != nil {
			return nil, fmt.Errorf("connect %s to pmtud-net-b: %w", node, err)
		}
	}

	// Install curl on cluster-a workers for traffic generation
	for _, w := range a.Workers {
		if err := run("docker", "exec", w, "sh", "-c",
			"apt-get update -qq && apt-get install -y --no-install-recommends curl"); err != nil {
			return nil, fmt.Errorf("install curl on %s: %w", w, err)
		}
	}

	r, err := createRouter(ctx)
	if err != nil {
		return nil, err
	}

	l := &Lab{ClusterA: a, ClusterB: b, Router: r}

	if err := configureRoutes(ctx, l); err != nil {
		return nil, err
	}
	if err := disableOffloads(ctx, l); err != nil {
		return nil, err
	}

	// Discover cluster-b worker IP on pmtud-net-b for traffic generation
	if len(b.Workers) > 0 {
		ip, err := ipOnSubnet(b.Workers[0], "172.31.")
		if err != nil {
			return nil, fmt.Errorf("discover cluster-b dest IP: %w", err)
		}
		l.DestIP = ip
	} 

	return l, nil
}

func (l *Lab) Teardown(ctx context.Context) error {
	if os.Getenv("LAB_KEEP") != "" {
		return nil
	}
	removeRouter(ctx)
	deleteCluster(ctx, "pmtud-cluster-a")
	deleteCluster(ctx, "pmtud-cluster-b")
	removeNetworks(ctx, DefaultNetworks)
	return nil
}

func Attach(ctx context.Context) (*Lab, error) {
	// TODO: discover running lab; connect clients
	// Only needed if LAB_REUSE mode is used
	return nil, fmt.Errorf("not implemented")
}
