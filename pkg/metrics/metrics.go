package metrics

import (
    "fmt"
    "github.com/prometheus/client_golang/prometheus"
    "net"
    "net/http"
    //"sync"

    "k8s.io/klog"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

var SentError = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "go_pmtud_sent_error_peer_total",
    Help: "Number of errors per peer",
}, []string{"node", "peer"})

var Error = prometheus.NewCounterVec(prometheus.CounterOpts{
    Name: "go_pmtud_error_total",
    Help: "Number of general errors in go-pmtud",
}, []string{"node"})

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

// ServeMetrics starts the Prometheus metrics collector.
func ServeMetrics(host net.IP, port int) {
    addr := fmt.Sprintf("%s:%d", host.String(), port)
    l, err := net.Listen("tcp", addr)

    if err != nil {
        klog.Errorf("Failed to serve Prometheus metrics: %v", err)
        return
    }
    defer l.Close()

    klog.Infof("Serving Prometheus metrics on %s", addr)
    http.Serve(l, promhttp.Handler())
}

