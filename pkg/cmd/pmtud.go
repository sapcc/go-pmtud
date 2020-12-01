package cmd

import (
	"context"
	"errors"
	"os/signal"
	"strings"
	"syscall"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/sapcc/go-pmtud/pkg/metrics"
	"github.com/vishvananda/netlink"

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
var ignoreNets []string
var iface string
var nodeName string
var metricsPort int
var ttl int
var nfGroup uint16

// var hostIP net.IP

func init() {
	flag.StringVar(&iface, "iface", "", "Network interface to work on")
	flag.StringVar(&nodeName, "nodename", "", "Node hostname")
	flag.StringSliceVar(&peers, "peers", nil, "Resend ICMP frag-needed packets to this peer list (comma separated)")
	flag.StringSliceVar(&ignoreNets, "ignore-networks", nil, "Do not resend ICMP frag-needed packets originated from specified networks (comma separated)")
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

	var nodeIface string
	if iface != "" {
		nodeIface = iface
	} else {
		// find outgoing interface of default gateway if interface was not specified
		var err error
		nodeIface, err = getIface()
		if err != nil {
			return err
		}
	}
	// get own IP
	klog.Infof("Working with iface %s", nodeIface)
	myIP, err := getIfaceIP(nodeIface)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Fatalf("Unable to get own IP address: %v", err)
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

	// Print ignored source networks
	if len(ignoreNets) > 0 {
		klog.Infof("Ignoring ICMP frag-needed packets from networks: %s", strings.Join(ignoreNets, ", "))
	} else {
		klog.Fatalf("Warning! ignore-networks is not specified - possibility to create a message loop!")
	}

	//ensure counters are reported
	metrics.RecvPackets.WithLabelValues(nodeName).Add(0)
	metrics.Error.WithLabelValues(nodeName).Add(0)

	for _, d := range peerList {
		metrics.SentError.WithLabelValues(nodeName, d).Add(0)
		metrics.SentPacketsPeer.WithLabelValues(nodeName, d).Add(0)
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

		sendPackets := false

		b := append(make([]byte, 0, len(*attrs.Payload)), *attrs.Payload...)

		rcvHeader, err := ipv4.ParseHeader(b)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("Unable to read source IP address: %v", err)
		}
		sourceIP := rcvHeader.Src

		klog.Infof("Received ICMP frag-needed from %s", sourceIP)

		if len(ignoreNets) > 0 {
			for _, n := range ignoreNets {
				_, network, err := net.ParseCIDR(n)
				if err != nil {
					metrics.Error.WithLabelValues(nodeName).Inc()
					klog.Errorf("Unable to convert network: %v", err)
				}
				if network.Contains(sourceIP) {
					return 0 // do nothing because received from ignore-network
				} else {
					sendPackets = true
				}
			}
		}

		if sendPackets == true {

			klog.Infof("ICMP frag-needed received from %s which is not part of ignore-network, resending packet.", sourceIP)
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
				klog.Infof("Resending ICMP frag-needed packet to %s", dstIP)

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
		}
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
func getIfaceIP(intf string) (string, error) {
	i, err := net.InterfaceByName(intf)
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

// getIface gets an interface of the default route
func getIface() (string, error) {
	// Internet is where 8.8.8.8 lives
	var defaultRoute, _ , _ = net.ParseCIDR("8.8.8.8/32")
	route, err := netlink.RouteGet(defaultRoute)
	if err != nil {
		klog.Fatalf("could not get default route: %v", err)
	}
	ifindex := route[0].LinkIndex
	iface, err := net.InterfaceByIndex(ifindex)
	if err != nil {
		klog.Fatalf("could not get default route interface index: %v", err)
	}
	return iface.Name, nil
}