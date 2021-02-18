package cmd

import (
	"context"
	"errors"
	"fmt"
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

	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/raw"
)

var peers []string
//var ignoreNets []string
var ifaceNames []string
var nodeName string
var metricsPort int
var ttl int
var nfGroup uint16

// var hostIP net.IP

func init() {
	flag.StringSliceVar(&ifaceNames, "iface-names", nil, "Network interface names to work on")
	flag.StringVar(&nodeName, "nodename", "", "Node hostname")
	flag.StringSliceVar(&peers, "peers", nil, "Resend ICMP frag-needed packets to this peer list (comma separated)")
	//flag.StringSliceVar(&ignoreNets, "ignore-networks", nil, "Do not resend ICMP frag-needed packets originated from specified networks (comma separated)")
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
	if len(ifaceNames) == 0 {
		metrics.Error.WithLabelValues(nodeName).Inc()
		return fmt.Errorf("no interface names given")
	} else {
		// find outgoing interface of default gateway if interface was not specified
		var err error
		nodeIface, err = getReplIface()
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("getReplIface error: %v", err)
			return err
		}
	}
	// get own IP
	klog.Infof("Working with iface %s", nodeIface)

	// Serve metrics on same interface
	metricsIface, err := getIface()
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Errorf("getIface error: %v", err)
		return err
	}
	metricsIp, err := getIfaceIP(metricsIface)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Errorf("getIfaceIP error: %v", err)
		return err
	}
	go metrics.ServeMetrics(net.ParseIP(metricsIp), metricsPort)

	myMac, err := net.InterfaceByName(nodeIface)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Errorf("InterfaceByName error: %v", err)
		return err
	}
	peerList := peers
	// Remove own IP address from peer list
	for i, p := range peers {
		if strings.Compare(p,  myMac.HardwareAddr.String()) == 0 {
			klog.Infof("Removing own MAC %s from the peer list", p)
			peerList = append(peerList[:i], peerList[i+1:]...)
		}
	}

	// Print ignored source networks
	// if len(ignoreNets) > 0 {
	//	klog.Infof("Ignoring ICMP frag-needed packets from networks: %s", strings.Join(ignoreNets, ", "))
	//} else {
	//	err := fmt.Errorf("ignore-networks is not specified - possibility to create a message loop")
	//	klog.Error(err)
	//	return err
	//}

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
		klog.Errorf("nflog error: %v", err)
		return err
	}
	defer nf.Close()

	fn := func(attrs nflog.Attribute) int {
		metrics.RecvPackets.WithLabelValues(nodeName).Inc()
		start := time.Now()

		b := append(make([]byte, 0, len(*attrs.Payload)), *attrs.Payload...)

		rcvHeader, err := ipv4.ParseHeader(b)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("Unable to read source IP address: %v", err)
			return 0
		}
		sourceIP := rcvHeader.Src

		klog.Infof("ICMP frag-needed received from %s, resending packet.", sourceIP)

		interFace, err := net.InterfaceByName(nodeIface)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("unable to get interface with name: %v, %v", nodeIface, err)
			return 0
		}
		conn, err := raw.ListenPacket(interFace, 0x0800, nil)
		if err != nil {
			metrics.Error.WithLabelValues(nodeName).Inc()
			klog.Errorf("unable to create listen socket for interface: %v", err)
			return 0
		}
		for _, d := range peerList {
			hwAddr, err := net.ParseMAC(d)
			if err != nil {
				metrics.Error.WithLabelValues(nodeName).Inc()
				klog.Errorf("error parsing peer: %v, %v", d, err)
				return 0
			}
			frame := ethernet.Frame{
				Source: interFace.HardwareAddr,
				Destination: hwAddr,
				EtherType: 0x0800,
				Payload: b,
			}
			bin, err := frame.MarshalBinary()
			if err != nil {
				metrics.Error.WithLabelValues(nodeName).Inc()
				klog.Errorf("error marshalling frame: %v", err)
				return 0
			}
			addr := &raw.Addr{
				HardwareAddr: hwAddr,
			}
			if _, err := conn.WriteTo(bin, addr); err != nil {
				metrics.Error.WithLabelValues(nodeName).Inc()
				klog.Errorf("error writing packet: %v", err)
				return 0
			}
			metrics.SentPackets.WithLabelValues(nodeName).Inc()
			metrics.SentPacketsPeer.WithLabelValues(nodeName, d).Inc()
		}

		duration := time.Since(start)
		metrics.CallbackDuration.WithLabelValues(nodeName).Observe(duration.Seconds())
		return 0
	}

	err = nf.Register(ctx, fn)
	if err != nil {
		metrics.Error.WithLabelValues(nodeName).Inc()
		klog.Errorf("nflog register error: %v", err)
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

func getReplIface() (string, error) {
	interFaces, err := net.Interfaces()
	//klog.Infof("discovered interfaces: %v", interFaces)
	if err != nil {
		return "", err
	}
	for _, name := range ifaceNames {
		for _, in := range interFaces {
			if strings.Compare(in.Name, name) == 0 {
				return name, nil
			}
		}
	}
	return "", fmt.Errorf("no interface found with names: %v", ifaceNames)
}

// getIface gets an interface of the default route
func getIface() (string, error) {
	// Internet is where 8.8.8.8 lives
	var defaultRoute, _ , _ = net.ParseCIDR("8.8.8.8/32")
	route, err := netlink.RouteGet(defaultRoute)
	if err != nil {
		err2 := fmt.Errorf("could not get default route: %v", err)
		klog.Error(err2)
		return "", err2
	}
	ifindex := route[0].LinkIndex
	iface, err := net.InterfaceByIndex(ifindex)
	if err != nil {
		err2 := fmt.Errorf("coudl not get default route interface index: %v", err)
		klog.Error(err2)
		return "", err2
	}
	return iface.Name, nil
}