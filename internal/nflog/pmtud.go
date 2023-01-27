package nflog

import (
	"context"
	golog "log"
	"net"
	"os"
	"sync"
	"time"

	"github.com/florianl/go-nflog/v2"
	"github.com/go-logr/logr"
	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/raw"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/util"
	"golang.org/x/net/ipv4"
)

const (
	nf_bufsize   = 2 * 1024 * 1024
	read_bufsize = 2 * 1024 * 1024
)

var wg = sync.WaitGroup{}

type Controller struct {
	Log logr.Logger
	Cfg *config.Config
}

func (nfc *Controller) Start(startCtx context.Context) error {
	log := nfc.Log
	cfg := nfc.Cfg
	log.Info("Starting")

	ctx, cancel := context.WithCancel(startCtx)

	nodeIface := cfg.ReplicationInterface
	//ensure counters are reported
	metrics.RecvPackets.WithLabelValues(cfg.NodeName, "").Add(0)
	metrics.Error.WithLabelValues(cfg.NodeName).Add(0)

	//TODO: make this a better logger
	nflogger := golog.New(os.Stdout, "nflog:", golog.Ldate|golog.Ltime|golog.Lshortfile)
	nfConfig := nflog.Config{
		Group:       cfg.NfGroup,
		Copymode:    nflog.CopyPacket,
		ReadTimeout: 100 * time.Millisecond,
		Logger:      nflogger,
		Bufsize:     nf_bufsize,
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
	err = nf.Con.SetReadBuffer(read_bufsize)
	if err != nil {
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		log.Error(err, "error setting read buffer")
		cancel()
		return err
	}

	fn := func(attrs nflog.Attribute) int {

		var peerList []string

		if cfg.Unicast {

			cfg.PeerMutex.Lock()
			for _, peer := range cfg.PeerList {
				peerList = append(peerList, peer.Mac)
			}
			cfg.PeerMutex.Unlock()
		}

		start := time.Now()

		b := append(make([]byte, 0, len(*attrs.Payload)), *attrs.Payload...)

		rcvHeader, err := ipv4.ParseHeader(b)
		if err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "Unable to read source IP address")
			cancel()
			return 1
		}
		sourceIP := rcvHeader.Src

		s, d, err := util.CalcSrcDst(b)
		if err != nil {
			log.Error(err, "Unable to calculate inner source and destination IP addresses")
			return 1
		}

		metrics.RecvPackets.WithLabelValues(cfg.NodeName, s.String()).Inc()

		log.Info("ICMP frag-needed received, resending packet.", "ICMP source", sourceIP,
			"source IP", s,
			"could not send to destination IP", d)

		interFace, err := net.InterfaceByName(nodeIface)
		if err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "unable to get interface", "name", nodeIface)
			cancel()
			return 1
		}
		conn, err := raw.ListenPacket(interFace, 0x0800, nil)
		if err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "unable to create listen socket", "interface", interFace)
			cancel()
			return 1
		}

		if cfg.Unicast {
			for _, d := range peerList {
				hwAddr, err := net.ParseMAC(d)
				if err != nil {
					metrics.Error.WithLabelValues(cfg.NodeName).Inc()
					log.Error(err, "error parsing", "peer", d)
					cancel()
					return 1
				}
				frame := ethernet.Frame{
					Source:      interFace.HardwareAddr,
					Destination: hwAddr,
					EtherType:   0x0800,
					Payload:     b,
				}
				bin, err := frame.MarshalBinary()
				if err != nil {
					metrics.Error.WithLabelValues(cfg.NodeName).Inc()
					log.Error(err, "error marshalling frame")
					cancel()
					return 1
				}
				addr := &raw.Addr{
					HardwareAddr: hwAddr,
				}
				if _, err := conn.WriteTo(bin, addr); err != nil {
					metrics.Error.WithLabelValues(cfg.NodeName).Inc()
					log.Error(err, "error writing packet")
					cancel()
					return 1
				}
				metrics.SentPackets.WithLabelValues(cfg.NodeName).Inc()
				metrics.SentPacketsPeer.WithLabelValues(cfg.NodeName, d).Inc()
			}
		}

		if cfg.Multicast {
			// send in right interface
			conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
			if err != nil {
				log.Error(err, "error listening")
			}

			packet := b[20:]
			log.Info("packet:", "size", len(packet))

			addr := net.ParseIP(cfg.MulticastAddr)
			_, err = conn.WriteToUDP(packet, &net.UDPAddr{IP: addr, Port: cfg.MulticastPort})
			if err != nil {
				log.Error(err, "error sending multicast packet")
				return 1
			}

			/*
				c, err := net.ListenPacket("udp", "239.0.0.10:9999")
				if err != nil {
					metrics.Error.WithLabelValues(cfg.NodeName).Inc()
					klog.Errorf("Unable to create connection: %v", err)
				}
				defer c.Close()

				p, err := ipv4.NewRawConn(c)
				if err != nil {
					metrics.Error.WithLabelValues(cfg.NodeName).Inc()
					klog.Errorf("Unable to open new raw connection: %v", err)
				}

				packet := b[20:]
				h := &ipv4.Header{
					Version:  ipv4.Version,
					Len:      ipv4.HeaderLen,
					TotalLen: ipv4.HeaderLen + len(packet),
					ID:       12345,
					Protocol: 1,
					TTL:      1,
					Dst:      net.ParseIP(srvAddr),
				}

				err = p.WriteTo(h, packet, nil)
				if err != nil {
					log.Error(err, "error sending multicast packet")
					return 1
				}

			*/
		}

		duration := time.Since(start)
		metrics.CallbackDuration.WithLabelValues(cfg.NodeName).Observe(duration.Seconds())
		return 0
	}

	errChan, err := nf.Register(ctx, fn)
	if err != nil {
		metrics.Error.WithLabelValues(cfg.NodeName).Inc()
		log.Error(err, "nflog register error")
		return err
	}

	select {
	case err := <-errChan:
		log.Error(err, "error channel closed")
		cancel()
	case <-ctx.Done():
	}

	return nil
}
