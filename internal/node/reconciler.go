// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package node

import (
	"context"
	"strings"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sapcc/go-pmtud/internal/config"
)

type Reconciler struct {
	Log    logr.Logger
	Client client.Client
	Cfg    *config.Config
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("node", request.Name)

	// Exclude own node
	if strings.Compare(r.Cfg.NodeName, request.Name) == 0 {
		return reconcile.Result{}, nil
	}

	var node corev1.Node
	err := r.Client.Get(ctx, request.NamespacedName, &node)
	if err != nil {
		if k8serr.IsNotFound(err) {
			log.Info("node deleted, removing from peer list")
			r.Cfg.PeerMutex.Lock()
			delete(r.Cfg.PeerList, request.Name)
			r.Cfg.PeerMutex.Unlock()
			return reconcile.Result{}, nil
		}
		log.Error(err, "error getting node")
		return reconcile.Result{}, err
	}

	ip := getInternalIP(&node)
	if ip == "" {
		log.Info("no InternalIP found for node, skipping")
		return reconcile.Result{}, nil
	}

	log.Info("updating peer", "ip", ip)
	r.Cfg.PeerMutex.Lock()
	r.Cfg.PeerList[request.Name] = ip
	r.Cfg.PeerMutex.Unlock()

	return reconcile.Result{}, nil
}

func getInternalIP(node *corev1.Node) string {
	for _, addr := range node.Status.Addresses {
		if addr.Type == corev1.NodeInternalIP {
			return addr.Address
		}
	}
	// Fallback to first address if no InternalIP found
	if len(node.Status.Addresses) > 0 {
		return node.Status.Addresses[0].Address
	}
	return ""
}
