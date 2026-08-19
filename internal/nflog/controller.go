// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package nflog

import (
	"context"
	"net"
	"time"

	"github.com/florianl/go-nflog/v2"
	"github.com/go-logr/logr"
	"golang.org/x/net/ipv4"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/relay"
	"github.com/sapcc/go-pmtud/internal/util"
)

const nfBufsize = 2 * 1024 * 1024
const readBufsize = 2 * 1024 * 1024

// Controller receives ICMP frag-needed packets from the kernel via NFLOG,
// filters them, and forwards each captured packet to peer nodes via the Relay.
type Controller struct {
	Log   logr.Logger
	Cfg   *config.Config
	Relay relay.Relay
}

func (nfc *Controller) Start(startCtx context.Context) error {
	log := nfc.Log
	cfg := nfc.Cfg
	log.Info("Starting")

	ctx, cancel := context.WithCancel(startCtx)

	// ensure counters are reported
	metrics.RecvPackets.WithLabelValues(cfg.NodeName, "").Add(0)
	metrics.Error.WithLabelValues(cfg.NodeName).Add(0)

	nfConfig := nflog.Config{
		Group:    cfg.NfGroup,
		Copymode: nflog.CopyPacket,
		Bufsize:  nfBufsize,
	}
	log.Info("Operating with", "buffersize", nfConfig.Bufsize)
	nf, err := nflog.Open(&nfConfig)
	if err != nil {
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		log.Error(err, "nflog error")
		cancel()
		return err
	}
	defer nf.Close()
	err = nf.Con.SetReadBuffer(readBufsize)
	if err != nil {
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		log.Error(err, "error setting read buffer")
		cancel()
		return err
	}

	nflogCallback := func(attrs nflog.Attribute) int {
		// Payload is nil when the kernel sent no packet copy (e.g. --nflog-size 0); nothing to relay.
		if attrs.Payload == nil {
			return 0
		}

		var peerIPs []string
		cfg.PeerMutex.Lock()
		for _, ip := range cfg.PeerList {
			peerIPs = append(peerIPs, ip)
		}
		cfg.PeerMutex.Unlock()

		start := time.Now()

		b := append(make([]byte, 0, len(*attrs.Payload)), *attrs.Payload...)

		rcvHeader, err := ipv4.ParseHeader(b)
		if err != nil {
			// Drop the bad packet; a non-zero return or cancel() would tear down
			// the nflog receive goroutine permanently (manager won't restart it).
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "Unable to read source IP address")
			return 0
		}
		sourceIP := rcvHeader.Src

		// Check if source IP is in ignore-networks (loop prevention)
		if isIgnoredNetwork(sourceIP, cfg.IgnoreNetworks) {
			log.Info("skipping packet from ignored network", "source", sourceIP)
			return 0
		}

		// Defense-in-depth: skip if source IP matches any peer node IP
		// (prevents loops if a peer-injected packet is re-captured)
		if isPeerIP(sourceIP, peerIPs) {
			log.Info("skipping packet from peer node", "source", sourceIP)
			return 0
		}

		s, d, err := util.CalcSrcDst(b)
		if err != nil {
			// Drop, don't tear down (see ParseHeader path above).
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "Unable to calculate inner source and destination IP addresses")
			return 0
		}

		metrics.RecvPackets.WithLabelValues(cfg.NodeName, s.String()).Inc()

		log.Info("ICMP frag-needed received, resending packet.", "ICMP source", sourceIP,
			"source IP", s, "could not send to destination IP", d)

		if err := nfc.Relay.Send(ctx, relay.RelayPacket{Payload: b, SrcNode: cfg.NodeName}); err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "relay send failed")
		}

		duration := time.Since(start)
		metrics.CallbackDuration.WithLabelValues(cfg.NodeName).Observe(duration.Seconds())
		return 0
	}

	err = nf.RegisterWithErrorFunc(ctx, nflogCallback, func(err error) int {
		log.Error(err, "nflog error")
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		cancel()
		return -1
	})

	if err != nil {
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		log.Error(err, "nflog register error")
		return err
	}

	<-ctx.Done()
	cancel()

	return nil
}

func isIgnoredNetwork(ip net.IP, networks []*net.IPNet) bool {
	for _, network := range networks {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func isPeerIP(ip net.IP, peerIPs []string) bool {
	for _, peer := range peerIPs {
		if ip.Equal(net.ParseIP(peer)) {
			return true
		}
	}
	return false
}
