// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package node

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/sapcc/go-pmtud/internal/arp"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
)

type Reconciler struct {
	Log    logr.Logger
	Client client.Client
	Cfg    *config.Config
}

func (r *Reconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("node", request.Name)

	// We do not consider our own node
	if strings.Compare(r.Cfg.NodeName, request.Name) == 0 {
		return reconcile.Result{}, nil
	}

	// We do not want to update every mac on every update
	e, ok := r.Cfg.PeerList[request.Name]
	if ok {
		if time.Now().Before(e.LastUpdated.Add(time.Duration(r.Cfg.ArpCacheTimeoutMinutes) * time.Minute)) {
			return reconcile.Result{}, nil
		}
	}

	var node = corev1.Node{}
	err := r.Client.Get(ctx, request.NamespacedName, &node)
	if err != nil {
		if k8serr.IsNotFound(err) {
			log.Info("node not found, skip", "node", request.NamespacedName)
			// Node could have been deleted
			return reconcile.Result{}, nil
		}
		log.Error(err, "error getting node", "node", request.NamespacedName)
		return reconcile.Result{}, err
	}
	if len(node.Status.Addresses) == 0 {
		err = errors.New("no ip found for node")
		return reconcile.Result{}, err
	}
	log.Info(node.Status.Addresses[0].Address)
	res := arp.Resolver{
		Log: log,
		Cfg: r.Cfg,
	}
	mac, err := res.Resolve(node.Status.Addresses[0].Address)
	if err != nil {
		err = errors.New("could not resolve mac address for node")
		metrics.ArpResolveError.WithLabelValues(r.Cfg.NodeName, request.Name).Inc()
		return reconcile.Result{}, err
	}
	log.Info("found mac " + mac)
	entry := config.PeerEntry{
		LastUpdated: time.Now(),
		Mac:         mac,
	}
	r.Cfg.PeerMutex.Lock()
	r.Cfg.PeerList[request.Name] = entry
	r.Cfg.PeerMutex.Unlock()
	return reconcile.Result{}, nil
}
