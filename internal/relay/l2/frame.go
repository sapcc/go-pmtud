// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"net"

	"github.com/mdlayher/ethernet"
)

// buildFrame wraps a raw IPv4 packet in an Ethernet frame addressed to dst.
func buildFrame(src, dst net.HardwareAddr, payload []byte) ([]byte, error) {
	frame := ethernet.Frame{
		Source:      src,
		Destination: dst,
		EtherType:   ethernet.EtherTypeIPv4,
		Payload:     payload,
	}
	return frame.MarshalBinary()
}
