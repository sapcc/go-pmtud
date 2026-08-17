// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
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

func (l *Lab) Teardown(ctx context.Context) error {
	return nil // stub
}
