// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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

var mutex sync.Mutex

type Resolver struct {
	Log logr.Logger
	Cfg *config.Config
}

func (r *Resolver) Resolve(ip string) (string, error) {
	// avoid ARP DDoS towards single node
	time.Sleep(time.Duration(r.Cfg.RandDelay) * time.Millisecond)

	log := r.Log.WithName("arp-resolver").WithValues("ip", ip)
	ifi, err := net.InterfaceByName(r.Cfg.ReplicationInterface)
	if err != nil {
		log.Error(err, "error getting interface")
		return "", err
	}

	// Lock so only one ARP resolver runs at a time
	mutex.Lock()
	c, err := mdarp.Dial(ifi)
	if err != nil {
		log.Error(err, "error dialing")
		return "", err
	}
	defer func() {
		err = c.Close()
		if err != nil {
			log.Error(err, "error closing arp client")
		}
		mutex.Unlock()
	}()
	err = c.SetDeadline(time.Now().Add(time.Duration(r.Cfg.ArpRequestTimeoutSeconds) * time.Second))
	if err != nil {
		log.Error(err, "error setting deadline")
		return "", err
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		log.Error(err, "error parsing ip address")
		return "", err
	}
	mac, err := c.Resolve(addr)
	if err != nil {
		log.Error(err, "error resolving mac for ip")
		return "", err
	}

	return mac.String(), nil
}
