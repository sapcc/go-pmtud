// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	labelNode     = "node"
	labelPeer     = "peer"
	labelSourceIP = "source_ip"
	labelSource   = "source"
)

var SentError = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_error_peer_total",
	Help: "Number of errors per peer",
}, []string{labelNode, labelPeer})

var Error = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_error_total",
	Help: "Number of general errors in go-pmtud",
}, []string{labelNode})

var SentPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_packets_total",
	Help: "Number of sent ICMP packets",
}, []string{labelNode})

var SentPacketsPeer = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_packets_peer",
	Help: "Number of sent ICMP packets per peer",
}, []string{labelNode, labelPeer})

var RecvPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_recv_packets_total",
	Help: "Number of ICMP frag-needed packets captured from the kernel (nflog) on this node",
}, []string{labelNode, labelSourceIP})

// InjectedPackets counts packets received from a peer's relay and injected into
// the local network stack via the TUN device. The source label is the relaying
// peer IP.
var InjectedPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_injected_packets_total",
	Help: "Number of relayed ICMP packets received from a peer and injected via the TUN device",
}, []string{labelNode, labelSource})

var CallbackDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "go_pmtud_callback_duration_seconds",
	Buckets: []float64{0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09},
	Help:    "Duration of NFlog callback in seconds",
}, []string{labelNode})
