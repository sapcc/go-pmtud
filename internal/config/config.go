// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"net"
	"sync"
	"time"
)

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

	RelayBackend    string        // "udp" (default) or "crd"
	RelayNamespace  string        // namespace for CRD relay objects
	RelayGCInterval time.Duration // CRD GC sweep interval
}
