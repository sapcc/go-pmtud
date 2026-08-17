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

	// infer dest IP from cluster-b (first worker's 172.31.x.x IP)
	if len(b.Workers) > 0 {
		out, _ := dockerExec(b.Workers[0], "ip", "-o", "addr", "show")
		// parse 172.31.x.x from output
		_ = out // placeholder; full parse deferred
		l.DestIP = "172.31.0.2" // placeholder; full parse deferred
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
