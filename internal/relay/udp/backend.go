// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package udp

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/relay"
)

const maxPacketSizeUDP = 1500

// injector delivers received packets into the local network stack.
// *Injector satisfies this interface; tests supply a recording fake.
type injector interface {
	Inject([]byte) error
	Close() error
}

type backend struct {
	cfg             *config.Config
	log             logr.Logger
	sendConn        *net.UDPConn
	injectorFactory func(string) (injector, error)
}

// New creates a UDP relay backend.
func New(d relay.Deps) (relay.Relay, error) {
	sendConn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create send socket: %w", err)
	}
	return &backend{cfg: d.Cfg, log: d.Log, sendConn: sendConn, injectorFactory: defaultInjectorFactory}, nil
}

func defaultInjectorFactory(name string) (injector, error) {
	inj, err := newInjector(name)
	if err != nil {
		return nil, err
	}
	return inj, nil
}

func (ub *backend) peers() []net.IP {
	ub.cfg.PeerMutex.Lock()
	defer ub.cfg.PeerMutex.Unlock()
	var peerIPs []net.IP
	for _, peerIP := range ub.cfg.PeerList {
		peerIPs = append(peerIPs, net.ParseIP(peerIP))
	}
	return peerIPs
}

func (ub *backend) isKnownPeer(ip net.IP) bool {
	ub.cfg.PeerMutex.Lock()
	defer ub.cfg.PeerMutex.Unlock()
	for _, peerIP := range ub.cfg.PeerList {
		if ip.Equal(net.ParseIP(peerIP)) {
			return true
		}
	}
	return false
}

func (ub *backend) Send(_ context.Context, pkt relay.RelayPacket) error {
	for _, peerIP := range ub.peers() {
		remoteAddr := &net.UDPAddr{IP: peerIP, Port: ub.cfg.ReplicationPort}
		if _, err := ub.sendConn.WriteToUDP(pkt.Payload, remoteAddr); err != nil {
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

func (ub *backend) Start(ctx context.Context) error {
	// Create and own the TUN injector for this backend.
	inj, err := ub.injectorFactory(TUNDeviceName)
	if err != nil {
		return fmt.Errorf("creating TUN injector: %w", err)
	}
	defer inj.Close()

	ub.log.Info("TUN device created", "name", TUNDeviceName)
	ub.log.Info("IMPORTANT: NFLOG rule MUST exclude the TUN interface to prevent loops",
		"required_rule", "iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i "+TUNDeviceName+" -j NFLOG --nflog-group <group>")

	addr := fmt.Sprintf(":%d", ub.cfg.ReplicationPort)
	ub.log.Info("Starting UDP relay listener", "addr", addr)

	// ensure the counter is reported from the start
	metrics.InjectedPackets.WithLabelValues(ub.cfg.NodeName, "").Add(0)

	listenAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return fmt.Errorf("failed to resolve listen address: %w", err)
	}
	conn, err := net.ListenUDP("udp4", listenAddr)
	if err != nil {
		return fmt.Errorf("failed to listen on UDP: %w", err)
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
		if ub.sendConn != nil {
			ub.sendConn.Close()
		}
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

		if !ub.isKnownPeer(remoteAddr.IP) {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Info("rejected packet from unknown source", "remote", remoteAddr.IP.String())
			continue
		}

		if _, err = ParseICMPFragNeeded(payload); err != nil {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Info("received invalid packet, discarding", "remote", remoteAddr, "error", err.Error())
			continue
		}

		if err := inj.Inject(payload); err != nil {
			metrics.Error.WithLabelValues(ub.cfg.NodeName).Inc()
			ub.log.Error(err, "error injecting packet", "remote", remoteAddr.IP.String())
			continue
		}

		metrics.InjectedPackets.WithLabelValues(ub.cfg.NodeName, remoteAddr.IP.String()).Inc()
		ub.log.Info("injected relayed ICMP packet", "from", remoteAddr.IP.String())
	}
}
