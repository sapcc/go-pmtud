// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// Package arp resolves peer node IPs to MAC addresses over the L2 replication
// interface. It is only used by the L2 relay backend.
package arp

import (
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/go-logr/logr"
	mdarp "github.com/mdlayher/arp"

	"github.com/sapcc/go-pmtud/internal/config"
)

// mutex serializes ARP dials so only one resolver runs at a time.
var mutex sync.Mutex

type Resolver struct {
	Log logr.Logger
	Cfg *config.Config
}

// Resolve returns the MAC address for the given peer IP, resolved via ARP on
// the configured replication interface.
func (r *Resolver) Resolve(ip string) (net.HardwareAddr, error) {
	log := r.Log.WithName("arp-resolver").WithValues("ip", ip)

	ifi, err := net.InterfaceByName(r.Cfg.ReplicationInterface)
	if err != nil {
		log.Error(err, "error getting interface")
		return nil, err
	}

	mutex.Lock()
	defer mutex.Unlock()

	c, err := mdarp.Dial(ifi)
	if err != nil {
		log.Error(err, "error dialing")
		return nil, err
	}
	defer func() {
		if cerr := c.Close(); cerr != nil {
			log.Error(cerr, "error closing arp client")
		}
	}()

	if err := c.SetDeadline(time.Now().Add(time.Duration(r.Cfg.ArpRequestTimeoutSeconds) * time.Second)); err != nil {
		log.Error(err, "error setting deadline")
		return nil, err
	}

	addr, err := netip.ParseAddr(ip)
	if err != nil {
		log.Error(err, "error parsing ip address")
		return nil, err
	}

	mac, err := c.Resolve(addr)
	if err != nil {
		log.Error(err, "error resolving mac for ip")
		return nil, err
	}
	return mac, nil
}
