// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"fmt"
	"net"
	"sync"
	"time"
)

type Backend string

const (
	BackendCRD Backend = "crd"
	BackendUDP Backend = "udp"
)

func (b Backend) String() string { return string(b) }

func (b *Backend) Set(s string) error {
	switch Backend(s) {
	case BackendCRD, BackendUDP:
		*b = Backend(s)
		return nil
	default:
		return fmt.Errorf("must be %q or %q", BackendCRD, BackendUDP)
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
	PeerMutex        sync.Mutex
	PeerList         map[string]string // nodeName → IP

	IgnoreNetworksRaw []string     // raw CIDR strings from CLI
	IgnoreNetworks    []*net.IPNet // parsed CIDRs

	RelayBackend    Backend
	RelayNamespace  string        // namespace for CRD relay objects
	RelayGCInterval time.Duration // CRD GC sweep interval
}
