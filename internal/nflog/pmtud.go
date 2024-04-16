package nflog

import (
	"context"
	golog "log"
	"net"
	"os"
	"time"

	"github.com/florianl/go-nflog/v2"
	"github.com/go-logr/logr"
	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/packet"
	"golang.org/x/net/ipv4"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/util"
)

const nfBufsize = 2 * 1024 * 1024
const readBufsize = 2 * 1024 * 1024

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
	// ensure counters are reported
	metrics.RecvPackets.WithLabelValues(cfg.NodeName, "").Add(0)
	metrics.Error.WithLabelValues(cfg.NodeName).Add(0)

	// TODO: make this a better logger
	nflogger := golog.New(os.Stdout, "nflog:", golog.Ldate|golog.Ltime|golog.Lshortfile)
	nfConfig := nflog.Config{
		Group:       cfg.NfGroup,
		Copymode:    nflog.CopyPacket,
		ReadTimeout: 100 * time.Millisecond,
		Logger:      nflogger,
		Bufsize:     nfBufsize,
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

	fn := func(attrs nflog.Attribute) int {
		var peerList []string
		cfg.PeerMutex.Lock()
		for _, peer := range cfg.PeerList {
			peerList = append(peerList, peer.Mac)
		}
		cfg.PeerMutex.Unlock()

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
		conn, err := packet.Listen(interFace, packet.Raw, 0x0800, nil)
		if err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "unable to create listen socket", "interface", interFace)
			cancel()
			return 1
		}
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
			addr := &packet.Addr{
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
