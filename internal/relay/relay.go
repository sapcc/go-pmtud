// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/go-pmtud/internal/config"
)

// Backend constants
const (
	BackendUDP = "udp"
	BackendCRD = "crd"
)

// RelayPacket represents a packet to be relayed between nodes
type RelayPacket struct {
	Payload []byte
	SrcNode string
}

// Relay interface defines the contract for inter-node transport backends
type Relay interface {
	Send(ctx context.Context, pkt RelayPacket) error
	Start(ctx context.Context, inject func(payload []byte) error) error
}

// Cache defines a simple cache interface
type Cache interface {
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
}

// Deps contains dependencies for Relay backends
type Deps struct {
	Cfg    *config.Config
	Log    logr.Logger
	Client client.Client
	Cache  Cache
}

// New creates a new Relay backend based on the given backend type
func New(backend string, d Deps) (Relay, error) {
	switch backend {
	case BackendUDP:
		return newUDPBackend(d)
	case BackendCRD:
		return newCRDBackend(d)
	default:
		return nil, fmt.Errorf("unknown backend: %s", backend)
	}
}

// newUDPBackend creates a UDP relay backend (stub for Task 4)
func newUDPBackend(d Deps) (Relay, error) {
	return nil, fmt.Errorf("not implemented")
}

// newCRDBackend creates a CRD relay backend (stub for Task 6)
func newCRDBackend(d Deps) (Relay, error) {
	return nil, fmt.Errorf("not implemented")
}
