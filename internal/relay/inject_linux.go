//go:build linux

// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"errors"
	"fmt"
	"net"
	"os"
	"syscall"
	"unsafe"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

const (
	TUNDeviceName = "pmtud0"
	ifnamsiz      = 16 // IFNAMSIZ on Linux
)

// Injector provides packet injection via TUN device
type Injector struct {
	fd   int
	file *os.File
}

// newInjector creates a new TUN injector with the given device name
func newInjector(name string) (*Injector, error) {
	fd, err := createTUN(name)
	if err != nil {
		return nil, err
	}

	file := os.NewFile(uintptr(fd), "/dev/net/tun") //#nosec G115 -- fd is non-negative from unix.Open
	if err := configureTUNNetlink(name); err != nil {
		file.Close()
		return nil, err
	}

	return &Injector{
		fd:   fd,
		file: file,
	}, nil
}

// Inject writes payload to the TUN device
func (inj *Injector) Inject(payload []byte) error {
	_, err := inj.file.Write(payload)
	if err != nil {
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("tun write backpressure: %w", err)
		}
		return fmt.Errorf("tun write: %w", err)
	}
	return nil
}

// Close closes the TUN device
func (inj *Injector) Close() error {
	return inj.file.Close()
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

func configureTUNNetlink(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return fmt.Errorf("failed to find link %s: %w", name, err)
	}

	// Assign a link-local address to the TUN so the kernel accepts packets on it
	addr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.ParseIP("169.254.254.1"),
			Mask: net.CIDRMask(32, 32),
		},
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return fmt.Errorf("failed to add address to %s: %w", name, err)
	}

	// Bring the interface up
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("failed to bring up %s: %w", name, err)
	}

	return nil
}
