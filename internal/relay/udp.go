// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/packet"
)

const maxPacketSizeUDP = 1500

type udpBackend struct {
	cfg      *config.Config
	log      logr.Logger
	sendConn *net.UDPConn
}

// newUDPBackend creates a UDP relay backend.
// It creates an unconnected send socket for broadcasting to peers.
func newUDPBackend(d Deps) (Relay, error) {
	// Create an unconnected UDP socket for sending
	sendConn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create send socket: %w", err)
	}

	return &udpBackend{
		cfg:      d.Cfg,
		log:      d.Log,
		sendConn: sendConn,
	}, nil
}

// peers returns a snapshot of known peer IPs (thread-safe).
func (ub *udpBackend) peers() []net.IP {
	ub.cfg.PeerMutex.Lock()
	defer ub.cfg.PeerMutex.Unlock()

	var peerIPs []net.IP
	for _, peerIP := range ub.cfg.PeerList {
		peerIPs = append(peerIPs, net.ParseIP(peerIP))
	}
	return peerIPs
}

// isKnownPeer checks if the given IP is a known peer (thread-safe).
func (ub *udpBackend) isKnownPeer(ip net.IP) bool {
	ub.cfg.PeerMutex.Lock()
	defer ub.cfg.PeerMutex.Unlock()

	for _, peerIP := range ub.cfg.PeerList {
		if ip.Equal(net.ParseIP(peerIP)) {
			return true
		}
	}
	return false
}

// Send sends a relay packet to all known peers.
func (ub *udpBackend) Send(ctx context.Context, pkt RelayPacket) error {
	peerIPs := ub.peers()

	for _, peerIP := range peerIPs {
		remoteAddr := &net.UDPAddr{
			IP:   peerIP,
			Port: ub.cfg.ReplicationPort,
		}

		_, err := ub.sendConn.WriteToUDP(pkt.Payload, remoteAddr)
		if err != nil {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			metrics.SentError.WithLabelValues(ub.cfg.NodeName, peerIP.String()).Inc()
			ub.log.Error(err, "failed to send packet to peer", "peer", peerIP.String())
			continue
		}

		metrics.SentPackets.WithLabelValues(ub.cfg.NodeName).Inc()
		metrics.SentPacketsPeer.WithLabelValues(ub.cfg.NodeName, peerIP.String()).Inc()
	}

	return nil
}

// Start listens for incoming relayed packets from peers and injects them via callback.
func (ub *udpBackend) Start(ctx context.Context, inject func([]byte) error) error {
	addr := fmt.Sprintf(":%d", ub.cfg.ReplicationPort)
	ub.log.Info("Starting UDP relay listener", "addr", addr)

	listenAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to resolve listen address: %w", err)
	}

	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}
	defer conn.Close()

	// Handle context cancellation
	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, maxPacketSizeUDP)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
				ub.log.Error(err, "error reading from UDP")
				continue
			}
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])

		// Validate sender is a known peer
		if !ub.isKnownPeer(remoteAddr.IP) {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Info("rejected packet from unknown source", "remote", remoteAddr.IP.String())
			continue
		}

		// Validate the packet is ICMP type 3 code 4
		_, err = packet.ParseICMPFragNeeded(payload)
		if err != nil {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Info("received invalid packet, discarding", "remote", remoteAddr, "error", err.Error())
			continue
		}

		// Inject the packet via callback
		if err := inject(payload); err != nil {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Error(err, "error injecting packet", "remote", remoteAddr.IP.String())
			continue
		}

		metrics.RecvPackets.WithLabelValues(ub.cfg.NodeName, remoteAddr.IP.String()).Inc()
		ub.log.Info("injected relayed ICMP packet", "from", remoteAddr.IP.String())
	}
}
