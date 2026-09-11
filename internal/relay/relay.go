// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"net"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

// RelayPacket represents a packet to be relayed between nodes
type RelayPacket struct {
	Payload []byte
	SrcNode string
}

// Relay interface defines the contract for inter-node transport backends
type Relay interface {
	Send(ctx context.Context, pkt RelayPacket) error
	Start(ctx context.Context) error
	// PeerRemoved is called when a peer node is removed from or has its IP
	// changed in the peer list, so backends can release per-peer resources.
	PeerRemoved(nodeName string, ip net.IP)
}

// Deps contains dependencies for Relay backends
type Deps struct {
	Cfg *config.Config
	Log logr.Logger
}
