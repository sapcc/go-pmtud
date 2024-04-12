package util

import (
	"errors"
	"net"
	"strings"

	"github.com/go-logr/logr"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/vishvananda/netlink"
)

func GetReplicationInterface(cfg *config.Config, log logr.Logger) error {
	interFaces, err := net.Interfaces()
	if err != nil {
		log.Error(err, "error listing interfaces")
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		return err
	}
	for _, name := range cfg.InterfaceNames {
		for _, interFace := range interFaces {
			if interFace.MTU != cfg.InterfaceMtu {
				continue
			}
			if strings.Compare(interFace.Name, name) == 0 {
				cfg.ReplicationInterface = name
				return nil
			}
		}
	}
	err = errors.New("no configured interface found")
	log.Error(err, "error getting replication interface")
	return err
}

// GetDefaultInterface gets the interface with the default route
func GetDefaultInterface(cfg *config.Config, log logr.Logger) error {
	// Internet is where 8.8.8.8 lives :)
	defaultRoute, _, err := net.ParseCIDR("8.8.8.8/32")
	if err != nil {
		log.Error(err, "could not parse cidr")
		return err
	}
	route, err := netlink.RouteGet(defaultRoute)
	if err != nil {
		log.Error(err, "could not get default route")
		return err
	}
	if len(route) == 0 {
		err := errors.New("no default route found")
		log.Error(err, "error getting default route")
		return err
	}
	ifindex := route[0].LinkIndex
	interFace, err := net.InterfaceByIndex(ifindex)
	if err != nil {
		log.Error(err, "could not get default interface")
		return err
	}

	cfg.DefaultInterface = interFace.Name
	return nil
}

func GetInterfaceIP(name string, log logr.Logger) (string, error) {
	interFace, err := net.InterfaceByName(name)
	if err != nil {
		log.Error(err, "error listing interfaces")
		return "", err
	}
	addrs, err := interFace.Addrs()
	if err != nil {
		log.Error(err, "error listing interface addresses")
		return "", err
	}
	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		default:
			// ???
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue // not an ipv4 address
		}
		return ip.String(), nil
	}
	err = errors.New("Interface is not connected to the network")
	log.Error(err, "error finding interface ip")
	return "", err
}
