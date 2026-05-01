//go:build linux

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/go-logr/logr"
	"golang.org/x/sys/unix"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/packet"
)

const maxPacketSize = 1500
const tunDeviceName = "pmtud0"
const ifnamsiz = 16 // IFNAMSIZ on Linux

type Controller struct {
	Log logr.Logger
	Cfg *config.Config
}

func (rc *Controller) Start(ctx context.Context) error {
	log := rc.Log
	cfg := rc.Cfg

	addr := fmt.Sprintf(":%d", cfg.ReplicationPort)
	log.Info("Starting UDP receiver", "addr", addr)

	// Create TUN device for packet injection
	tunFD, err := createTUN(tunDeviceName)
	if err != nil {
		log.Error(err, "error creating TUN device")
		return err
	}
	tunFile := os.NewFile(uintptr(tunFD), "/dev/net/tun") //#nosec G115 -- fd is non-negative from unix.Open
	defer tunFile.Close()

	log.Info("TUN device created", "name", tunDeviceName)
	log.Info("IMPORTANT: Ensure iptables NFLOG rule excludes TUN interface to prevent loops",
		"required_rule", "iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i "+tunDeviceName+" -j NFLOG --nflog-group <group>")

	// Bring up TUN interface and assign address
	if err := configureTUNNetlink(tunDeviceName); err != nil {
		log.Error(err, "error configuring TUN device")
		return err
	}

	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		log.Error(err, "error resolving UDP address")
		return err
	}

	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		log.Error(err, "error listening on UDP")
		return err
	}
	defer conn.Close()

	go func() {
		<-ctx.Done()
		conn.Close()
		tunFile.Close()
	}()

	buf := make([]byte, maxPacketSize)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				metrics.Error.WithLabelValues(cfg.NodeName).Inc()
				log.Error(err, "error reading from UDP")
				continue
			}
		}

		payload := make([]byte, n)
		copy(payload, buf[:n])

		// Validate sender is a known peer (prevents unauthorized PMTU injection)
		if !rc.isKnownPeer(remoteAddr.IP) {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Info("rejected packet from unknown source", "remote", remoteAddr.IP.String())
			continue
		}

		// Validate the packet is ICMP type 3 code 4
		_, err = packet.ParseICMPFragNeeded(payload)
		if err != nil {
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Info("received invalid packet, discarding", "remote", remoteAddr, "error", err.Error())
			continue
		}

		// Inject the packet via TUN device — forces it through kernel receive path
		// (ip_input → icmp_rcv → icmp_unreach → PMTU cache update)
		if _, err := tunFile.Write(payload); err != nil {
			if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
				metrics.Error.WithLabelValues(cfg.NodeName).Inc()
				log.Info("TUN write backpressure, dropping packet")
				continue
			}
			metrics.Error.WithLabelValues(cfg.NodeName).Inc()
			log.Error(err, "error injecting packet via TUN")
			continue
		}

		metrics.RecvPackets.WithLabelValues(cfg.NodeName, remoteAddr.IP.String()).Inc()
		log.Info("injected replicated ICMP packet via TUN", "from", remoteAddr.IP.String())
	}
}

// createTUN opens /dev/net/tun and creates a TUN device with the given name.
// Returns the file descriptor for the TUN device.
func createTUN(name string) (int, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open /dev/net/tun: %w", err)
	}

	var ifr [ifnamsiz + 64]byte
	copy(ifr[:ifnamsiz], name)
	// IFF_TUN: layer 3 tunnel, IFF_NO_PI: no packet info header
	flags := uint16(unix.IFF_TUN | unix.IFF_NO_PI)
	ifr[ifnamsiz] = byte(flags & 0xff)          //#nosec G115
	ifr[ifnamsiz+1] = byte((flags >> 8) & 0xff) //#nosec G115

	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr[0]))) //#nosec G115 -- fd is non-negative from unix.Open
	if errno != 0 {
		unix.Close(fd)
		return -1, fmt.Errorf("ioctl TUNSETIFF: %w", errno)
	}

	return fd, nil
}
