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
}, []string{"node"})

var CallbackDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
    Name: "go_pmtud_callback_duration_seconds",
    Buckets: []float64{0.01,0.02,0.03,0.04,0.05,0.06,0.07,0.08,0.09},
    Help: "Duration of NFlog callback in seconds",
}, []string{"node"})
