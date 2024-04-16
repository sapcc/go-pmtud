// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

var SentError = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_error_peer_total",
	Help: "Number of errors per peer",
}, []string{"node", "peer"})

var Error = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_error_total",
	Help: "Number of general errors in go-pmtud",
}, []string{"node"})

var ArpResolveError = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_peer_arp_resolve_error",
	Help: "Number of ARP resolution errors per peer",
}, []string{"node", "peer"})

var SentPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_packets_total",
	Help: "Number of sent ICMP packets",
}, []string{"node"})

var SentPacketsPeer = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_sent_packets_peer",
	Help: "Number of sent ICMP packets per peer",
}, []string{"node", "peer"})

var RecvPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "go_pmtud_recv_packets_total",
	Help: "Number of received ICMP packets",
}, []string{"node", "source_ip"})

var CallbackDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "go_pmtud_callback_duration_seconds",
	Buckets: []float64{0.01, 0.02, 0.03, 0.04, 0.05, 0.06, 0.07, 0.08, 0.09},
	Help:    "Duration of NFlog callback in seconds",
}, []string{"node"})
