// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/packet"

	"github.com/sapcc/go-pmtud/internal/arp"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/relay"
)

// frameConn is the subset of *packet.Conn used by the backend; a seam for tests.
type frameConn interface {
	WriteTo(b []byte, addr net.Addr) (int, error)
	Close() error
}

type backend struct {
	cfg   *config.Config
	log   logr.Logger
	ifi   *net.Interface
	conn  frameConn
	cache *macCache
}

// New creates an L2 (raw Ethernet) relay backend bound to the replication
// interface configured via --iface_names.
func New(d relay.Deps) (relay.Relay, error) {
	if d.Cfg.ReplicationInterface == "" {
		return nil, fmt.Errorf("l2 backend requires a replication interface (set --iface_names)")
	}
	ifi, err := net.InterfaceByName(d.Cfg.ReplicationInterface)
	if err != nil {
		return nil, fmt.Errorf("l2 backend: interface %q: %w", d.Cfg.ReplicationInterface, err)
	}
	conn, err := packet.Listen(ifi, packet.Raw, int(ethernet.EtherTypeIPv4), nil)
	if err != nil {
		return nil, fmt.Errorf("l2 backend: listen on %q: %w", d.Cfg.ReplicationInterface, err)
	}
	res := &arp.Resolver{Log: d.Log.WithName("arp"), Cfg: d.Cfg}
	ttl := time.Duration(d.Cfg.ArpCacheTimeoutMinutes) * time.Minute
	return &backend{
		cfg:   d.Cfg,
		log:   d.Log,
		ifi:   ifi,
		conn:  conn,
		cache: newMACCache(res, ttl),
	}, nil
}

func (lb *backend) peerIPs() []string {
	lb.cfg.PeerMutex.Lock()
	defer lb.cfg.PeerMutex.Unlock()
	ips := make([]string, 0, len(lb.cfg.PeerList))
	for _, ip := range lb.cfg.PeerList {
		ips = append(ips, ip)
	}
	return ips
}

func (lb *backend) Send(_ context.Context, pkt relay.RelayPacket) error {
	for _, ip := range lb.peerIPs() {
		mac, err := lb.cache.get(ip)
		if err != nil {
			metrics.Error.WithLabelValues(lb.cfg.NodeName).Inc()
			metrics.SentError.WithLabelValues(lb.cfg.NodeName, ip).Inc()
			lb.log.Error(err, "failed to resolve peer MAC", "peer", ip)
			continue
		}
		frameBytes, err := buildFrame(lb.ifi.HardwareAddr, mac, pkt.Payload)
		if err != nil {
			metrics.Error.WithLabelValues(lb.cfg.NodeName).Inc()
			lb.log.Error(err, "failed to marshal frame", "peer", ip)
			continue
		}
		if _, err := lb.conn.WriteTo(frameBytes, &packet.Addr{HardwareAddr: mac}); err != nil {
			metrics.Error.WithLabelValues(lb.cfg.NodeName).Inc()
			metrics.SentError.WithLabelValues(lb.cfg.NodeName, ip).Inc()
			lb.log.Error(err, "failed to send frame to peer", "peer", ip)
			continue
		}
		metrics.SentPackets.WithLabelValues(lb.cfg.NodeName).Inc()
		metrics.SentPacketsPeer.WithLabelValues(lb.cfg.NodeName, ip).Inc()
	}
	return nil
}

// Start blocks until ctx is done. The L2 backend has no application-level
// receive loop: replicated frames are delivered to peers' real interface MACs
// and processed natively by the receiving kernel. No TUN device is created.
func (lb *backend) Start(ctx context.Context) error {
	lb.log.Info("Starting L2 relay backend", "interface", lb.cfg.ReplicationInterface)
	lb.log.Info("IMPORTANT: NFLOG rule MUST capture on the primary interface only",
		"required_rule", "iptables -t raw -A PREROUTING -i <primary-iface> -p icmp -m icmp --icmp-type 3/4 -j NFLOG --nflog-group <group>")
	<-ctx.Done()
	return lb.conn.Close()
}
