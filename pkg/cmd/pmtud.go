package cmd

import (
	"context"
	"errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-pmtud/pkg/metrics"
	"os/signal"
	"syscall"

	"log"
	"net"
	"os"
	"time"

	goflag "flag"

	"github.com/florianl/go-nflog"
	flag "github.com/spf13/pflag"
	"golang.org/x/net/ipv4"
	"k8s.io/klog"
)

var peers []string
var iface string
var nodeName string
var metricsPort int
var ttl int
var nfGroup uint16
// var hostIP net.IP

func init() {
	flag.StringVar(&iface, "iface", "", "Network interface to work on")
	flag.StringVar(&nodeName, "nodename", "", "Node hostname")
	flag.StringSliceVar(&peers, "peers",nil, "Resend ICMP packets to this peer list (comma separated)")
	flag.IntVar(&metricsPort, "metrics-port", 30040, "Port for Prometheus metrics")
	flag.Uint16Var(&nfGroup, "nflog-group", 33, "NFLOG group")
	flag.IntVar(&ttl, "ttl", 1, "TTL for resent packets")

 	klog.InitFlags(nil)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	flag.Parse()

	prometheus.MustRegister(metrics.Error)
	prometheus.MustRegister(metrics.SentError)
	prometheus.MustRegister(metrics.SentPackets)
	prometheus.MustRegister(metrics.SentPacketsPeer)
	prometheus.MustRegister(metrics.RecvPackets)
	prometheus.MustRegister(metrics.CallbackDuration)
}

func Run() error {
	klog.Info("Starting go-pmtud")

	ctx, cancel := context.WithCancel(context.Background())

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM, syscall.SIGQUIT)
	defer func() {
		signal.Stop(sigs)
		cancel()
	}()

	// get own IP
	myIP, err := getOwnIP()
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Errorf("Unable to get own IP address: %v", err)
	}

	// Serve metrics on same interface
	go metrics.ServeMetrics(net.ParseIP(myIP), metricsPort)

	peerList := peers
	// Remove own IP address from peer list
	for i, p := range peers {
		if p == myIP {
			klog.Infof("Removing own IP %s from the peer list", p)
			peerList = append(peerList[:i], peerList[i+1:]...)
		}
	}

	nflogger := log.New(os.Stdout, "nflog:", log.Ldate|log.Ltime|log.Lshortfile)
	config := nflog.Config{
		Group:       nfGroup,
		Copymode:    nflog.NfUlnlCopyPacket,
		ReadTimeout: 100 * time.Millisecond,
		Logger:      nflogger,
	}

	nf, err := nflog.Open(&config)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Fatalf("nflog error: %v", err)
		return err
	}
	defer nf.Close()

	fn := func(attrs nflog.Attribute) int {
		metrics.RecvPackets.WithLabelValues(nodeName).Inc()
		start := time.Now()
		b := append(make([]byte, 0, len(*attrs.Payload)), *attrs.Payload...)

		packet := b[20:]

		c, err := net.ListenPacket("ip4:icmp", myIP)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("Unable to create connection: %v", err)
		}
		defer c.Close()

		p, err := ipv4.NewRawConn(c)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("Unable to open new raw connection: %v", err)
		}

		for _, d := range peerList {

			dstIP := net.ParseIP(d)
			klog.Infof("Resending ICMP message to %s", dstIP)

			h := &ipv4.Header{
				Version:  ipv4.Version,
				Len:      ipv4.HeaderLen,
				TotalLen: ipv4.HeaderLen + len(packet),
				ID:       12345,
				Protocol: 1,
				TTL:      ttl,
				Dst:      dstIP.To4(),
			}

			err = p.WriteTo(h, packet, nil)
			if err != nil {
				metrics.SentError.WithLabelValues(nodeName, d).Inc()
				klog.Warningf("unable to send bytes: %v %d", err)
				break
			}

			metrics.SentPacketsPeer.WithLabelValues(nodeName, d).Inc()
			metrics.SentPackets.WithLabelValues(nodeName).Inc()
		}

		duration := time.Since(start)
		metrics.CallbackDuration.WithLabelValues(nodeName).Observe(duration.Seconds())
		return 0
	}

	err = nf.Register(ctx, fn)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Fatalf("nflog register error: %v", err)
		return err
	}

	select {
	case <-sigs:
		klog.Warning("signal received quiting")
		cancel()
	case <-ctx.Done():
	}

	return nil
}

// getOwnIP gets a valid IP address of specified interface
func getOwnIP() (string, error) {
	i, err := net.InterfaceByName(iface)
	if err != nil {
		return "", err
	}
	addrs, err := i.Addrs()

	for _, addr := range addrs {
		var ip net.IP
		switch v := addr.(type) {
		case *net.IPNet:
			ip = v.IP
		case *net.IPAddr:
			ip = v.IP
		}
		if ip == nil || ip.IsLoopback() {
			continue
		}
		ip = ip.To4()
		if ip == nil {
			continue // not an ipv4 address
		}
		return ip.String(), nil
	}
	return "", errors.New("not connected to the network")
}
