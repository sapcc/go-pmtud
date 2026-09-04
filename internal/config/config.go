// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net"
	"sync"
)

type Backend string

const (
	BackendL2  Backend = "l2"
	BackendUDP Backend = "udp"
)

func (b Backend) String() string { return string(b) }

func (b *Backend) Set(s string) error {
	switch Backend(s) {
	case BackendL2, BackendUDP:
		*b = Backend(s)
		return nil
	default:
		return fmt.Errorf("must be %q or %q", BackendL2, BackendUDP)
	}
}

func (b Backend) Type() string { return "backend" }

type Config struct {
	NodeName        string
	MetricsPort     string
	HealthPort      string
	TimeToLive      int
	NfGroup         uint16
	KubeContext     string
	ReplicationPort int

	DefaultInterface string

	// L2 backend configuration
	InterfaceNames           []string
	ReplicationInterface     string
	InterfaceMtu             int
	ArpCacheTimeoutMinutes   int
	ArpRequestTimeoutSeconds int

	PeerMutex sync.Mutex
	PeerList  map[string]string // nodeName → IP

	IgnoreNetworksRaw []string     // raw CIDR strings from CLI
	IgnoreNetworks    []*net.IPNet // parsed CIDRs

	RelayBackend Backend
}
