// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/go-pmtud/api/v1alpha1"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
)

type crdBackend struct {
	cfg    *config.Config
	log    logr.Logger
	client client.Client
	cache  cache.Cache
	ns     string
	ttl    time.Duration
	gcTick time.Duration
}

func newCRDBackend(d Deps) (Relay, error) {
	if d.Client == nil || d.Cache == nil {
		return nil, errors.New("crd backend requires a kube client and cache")
	}
	if d.Cfg.RelayNamespace == "" {
		return nil, errors.New("crd backend requires a namespace (--relay-namespace or POD_NAMESPACE)")
	}
	gcTick := d.Cfg.RelayGCInterval
	if gcTick <= 0 {
		gcTick = 60 * time.Second // guard: time.NewTicker panics on <=0
	}
	return &crdBackend{
		cfg: d.Cfg, log: d.Log, client: d.Client, cache: d.Cache,
		ns: d.Cfg.RelayNamespace,
		// Object lifetime is unrelated to the packet IP-TTL flag; give peers two GC
		// sweeps to consume the relay before the source node deletes it.
		ttl:    2 * gcTick,
		gcTick: gcTick,
	}, nil
}

func relayObjectName(srcNode string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s--%s", srcNode, hex.EncodeToString(sum[:16]))
}

func (c *crdBackend) Send(ctx context.Context, pkt RelayPacket) error {
	obj := &v1alpha1.PMTUNodeRelay{
		ObjectMeta: metav1.ObjectMeta{Name: relayObjectName(pkt.SrcNode, pkt.Payload), Namespace: c.ns},
		Spec: v1alpha1.PMTUNodeRelaySpec{
			SourceNode: pkt.SrcNode,
			Payload:    base64.StdEncoding.EncodeToString(pkt.Payload),
			ExpiresAt:  metav1.NewTime(time.Now().Add(c.ttl)),
		},
	}
	err := c.client.Create(ctx, obj)
	if apierrors.IsAlreadyExists(err) {
		return nil // dedup: same event already relayed
	}
	if err != nil {
		metrics.Error.WithLabelValues(c.cfg.NodeName).Inc()
		return err
	}
	metrics.SentPackets.WithLabelValues(c.cfg.NodeName).Inc()
	return nil
}

func (c *crdBackend) Start(ctx context.Context, inject func([]byte) error) error {
	inf, err := c.cache.GetInformer(ctx, &v1alpha1.PMTUNodeRelay{})
	if err != nil {
		return fmt.Errorf("get informer: %w", err)
	}
	handle := func(obj any) {
		r, ok := obj.(*v1alpha1.PMTUNodeRelay)
		if !ok || r.Namespace != c.ns || r.Spec.SourceNode == c.cfg.NodeName {
			return // skip foreign namespace / own captures (loop guard)
		}
		raw, err := base64.StdEncoding.DecodeString(r.Spec.Payload)
		if err != nil {
			c.log.Error(err, "decode relay payload", "obj", r.Name)
			return
		}
		// Inject is idempotent (kernel just updates the PMTU cache), so re-delivery
		// on an informer relist is harmless. Object lifecycle is owned by the
		// source node's TTL GC, not the consumer — consumers never delete, which
		// avoids N-1 cross-node delete amplification and inject-vs-delete races.
		if err := inject(raw); err != nil {
			metrics.Error.WithLabelValues(c.cfg.NodeName).Inc()
			c.log.Error(err, "inject relayed packet", "obj", r.Name)
			return
		}
		metrics.RecvPackets.WithLabelValues(c.cfg.NodeName, r.Spec.SourceNode).Inc()
	}
	if _, err := inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc: handle,
	}); err != nil {
		return fmt.Errorf("add event handler: %w", err)
	}
	go c.gcLoop(ctx)
	<-ctx.Done()
	return nil
}

func (c *crdBackend) gcLoop(ctx context.Context) {
	t := time.NewTicker(c.gcTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.gcExpired(ctx)
		}
	}
}

func (c *crdBackend) gcExpired(ctx context.Context) {
	var list v1alpha1.PMTUNodeRelayList
	if err := c.client.List(ctx, &list, client.InNamespace(c.ns)); err != nil {
		c.log.Error(err, "gc list")
		return
	}
	now := time.Now()
	for i := range list.Items {
		// Each node GCs only the objects it created; no cross-node delete contention.
		if list.Items[i].Spec.SourceNode != c.cfg.NodeName {
			continue
		}
		if list.Items[i].Spec.ExpiresAt.After(now) {
			continue
		}
		if err := c.client.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			c.log.Error(err, "gc delete", "obj", list.Items[i].Name)
		}
	}
}
