package node

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"github.com/sapcc/go-pmtud/internal/arp"
	"github.com/sapcc/go-pmtud/internal/config"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"strings"
	"time"
)

type Reconciler struct {
	Log logr.Logger
	Client client.Client
	Cfg *config.Config
}

func (r *Reconciler) Reconcile (ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := r.Log.WithValues("node", request.Name)

	// We do not consider our own node
	if strings.Compare(r.Cfg.NodeName, request.Name) == 0 {
		return reconcile.Result{}, nil
	}

	// We do not want to update every mac on every update
	e, ok := r.Cfg.PeerList[request.Name]
	if ok {
		if time.Now().Before(e.LastUpdated.Add(1 * time.Minute)) {
			return reconcile.Result{}, nil
		}
	}

	var node = corev1.Node{}
	err := r.Client.Get(ctx, request.NamespacedName, &node)
	if err != nil {
		log.Error(err, "error getting node")
		return reconcile.Result{}, err
	}
	if len(node.Status.Addresses) == 0 {
		err = fmt.Errorf("no ip found for node %s", request.Name)
		return reconcile.Result{}, err
	}
	log.Info(node.Status.Addresses[0].Address)
	res := arp.Resolver{
		Log: log,
		Cfg: r.Cfg,
	}
	mac, err := res.Resolve(node.Status.Addresses[0].Address)
	log.Info("found mac " + mac)
	entry := config.PeerEntry{
		LastUpdated: time.Now(),
		Mac: mac,
	}
	r.Cfg.PeerMutex.Lock()
	r.Cfg.PeerList[request.Name] = entry
	r.Cfg.PeerMutex.Unlock()
	return reconcile.Result{}, nil
}
