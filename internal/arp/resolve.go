package arp

import (
	"github.com/go-logr/logr"
	mdarp "github.com/mdlayher/arp"
	"github.com/sapcc/go-pmtud/internal/config"
	"net"
	"time"
)

type Resolver struct {
	Log logr.Logger
	Cfg *config.Config
}

func (r *Resolver) Resolve (ip string) (string, error) {
	log := r.Log.WithName("arp-resolver").WithValues("ip", ip)
	ifi, err := net.InterfaceByName(r.Cfg.ReplicationInterface)
	if err != nil {
		log.Error(err, "error getting interface")
		return "", err
	}
	c, err := mdarp.Dial(ifi)
	if err != nil {
		log.Error(err, "error dialing")
		return "", err
	}
	defer c.Close()
	err = c.SetDeadline(time.Now().Add(1*time.Second))
	if err != nil {
		log.Error(err, "error setting deadline")
		return "", err
	}
	netip := net.ParseIP(ip)
	mac, err := c.Resolve(netip)
	if err != nil {
		log.Error(err, "error resolving mac for ip")
		return "", err
	}
	return mac.String(), nil
}
