// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"net"
	"testing"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sapcc/go-pmtud/internal/config"
)

// TestReconcileIdempotency verifies that reconciling the same peer IP
// multiple times only updates the peer list on the first update and when the IP changes.
func TestReconcileIdempotency(t *testing.T) {
	cfg := &config.Config{
		NodeName: "local-node",
		PeerList: make(map[string]net.IP),
	}

	node := &corev1.Node{
		Name: "peer-node",
		Status: corev1.NodeStatus{
			Addresses: []corev1.NodeAddress{
				{
					Type:    corev1.NodeInternalIP,
					Address: "10.0.0.1",
				},
			},
		},
	}

	cli := fake.NewClientBuilder().WithObjects(node).Build()

	r := &Reconciler{
		Log:    logr.Discard(),
		Client: cli,
		Cfg:    cfg,
	}

	req := reconcile.Request{
		Name: "peer-node",
	}

	// First reconciliation: should update peer list
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("first reconcile failed: %v", err)
	}

	if cfg.PeerList["peer-node"].String() != "10.0.0.1" {
		t.Fatalf("peer IP not updated: got %q", cfg.PeerList["peer-node"])
	}

	// Second reconciliation with same IP: should not modify peer list (idempotent)
	_, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("second reconcile failed: %v", err)
	}

	if cfg.PeerList["peer-node"].String() != "10.0.0.1" {
		t.Fatalf("peer IP should remain unchanged: got %q", cfg.PeerList["peer-node"])
	}

	// Third reconciliation with different IP: should update
	node.Status.Addresses[0].Address = "10.0.0.2"
	err = cli.Status().Update(context.Background(), node)
	if err != nil {
		t.Fatalf("failed to update node status: %v", err)
	}

	_, err = r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("third reconcile failed: %v", err)
	}

	if cfg.PeerList["peer-node"].String() != "10.0.0.2" {
		t.Fatalf("peer IP not updated to new value: got %q", cfg.PeerList["peer-node"])
	}
}

// TestReconcileExcludesOwnNode verifies that reconciling own node doesn't add it to peer list.
func TestReconcileExcludesOwnNode(t *testing.T) {
	cfg := &config.Config{
		NodeName: "local-node",
		PeerList: make(map[string]net.IP),
	}

	r := &Reconciler{
		Log:    logr.Discard(),
		Client: nil,
		Cfg:    cfg,
	}

	req := reconcile.Request{
		Name: "local-node",
	}

	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("reconcile own node failed: %v", err)
	}

	if len(cfg.PeerList) != 0 {
		t.Fatalf("own node should not be added to peer list")
	}
}

// TestGetInternalIP tests IP extraction from node addresses.
func TestGetInternalIP(t *testing.T) {
	tests := []struct {
		name     string
		node     *corev1.Node
		expected string
	}{
		{
			name: "finds InternalIP",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalIP, Address: "203.0.113.1"},
						{Type: corev1.NodeInternalIP, Address: "10.0.0.1"},
					},
				},
			},
			expected: "10.0.0.1",
		},
		{
			name: "falls back to first address",
			node: &corev1.Node{
				Status: corev1.NodeStatus{
					Addresses: []corev1.NodeAddress{
						{Type: corev1.NodeExternalDNS, Address: "node.example.com"},
					},
				},
			},
			expected: "node.example.com",
		},
		{
			name:     "returns empty string when no addresses",
			node:     &corev1.Node{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getInternalIP(tt.node)
			if got != tt.expected {
				t.Errorf("getInternalIP() = %q, want %q", got, tt.expected)
			}
		})
	}
}
