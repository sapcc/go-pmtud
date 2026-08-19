// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"errors"
	"net"
)

// CalcSrcDst takes the byte sequence of the ICMP 3/4 packet and returns the inner packet's
// source and destination IPv4 addresses.
// Byte layout: outer IP header (0–19), ICMP header (20–27), inner IP header (28–47),
// with inner source and destination IPs at offsets 40–43 and 44–47.
func CalcSrcDst(b []byte) (srcIP, dstIP net.IP, err error) {
	if len(b) < 48 {
		return nil, nil, errors.New("payload too short for ICMP frag-needed inner header")
	}
	src := b[40:44]
	dst := b[44:48]

	srcIP = src
	dstIP = dst

	// validate if parsed IPs are valid IPv4 addresses
	if (net.ParseIP(srcIP.String()) == nil) || (net.ParseIP(dstIP.String()) == nil) {
		return nil, nil, errors.New("invalid IP in inner packet")
	}
	return srcIP, dstIP, nil
}
