//go:build linux

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
)

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
