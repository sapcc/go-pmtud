// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package packet

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
)

// ICMPFragNeededInfo contains parsed information from an ICMP fragmentation needed packet.
type ICMPFragNeededInfo struct {
	MTU     uint16
	SrcIP   net.IP
	DstIP   net.IP
	SrcPort uint16
	DstPort uint16
}

// ParseICMPFragNeeded parses an ICMP "fragmentation needed" packet and extracts relevant information.
// The packet should contain:
//   - Outer IP header
//   - ICMP header (Type 3, Code 4)
//   - Inner IP header (from original packet)
//   - First 8 bytes of inner transport header (TCP/UDP)
func ParseICMPFragNeeded(packet []byte) (*ICMPFragNeededInfo, error) {
	// Minimum length: 20 (outer IP) + 8 (ICMP) + 20 (inner IP) + 8 (transport header) = 56 bytes
	if len(packet) < 56 {
		return nil, fmt.Errorf("packet too short: %d bytes", len(packet))
	}

	// Parse outer IP header
	outerIPVersion := packet[0] >> 4
	if outerIPVersion != 4 {
		return nil, fmt.Errorf("invalid IP version: %d", outerIPVersion)
	}

	outerIPHeaderLen := int(packet[0]&0x0f) * 4
	if len(packet) < outerIPHeaderLen+8 {
		return nil, fmt.Errorf("packet too short for IP header: %d bytes", len(packet))
	}

	// Parse ICMP header
	icmpStart := outerIPHeaderLen
	icmpType := packet[icmpStart]
	icmpCode := packet[icmpStart+1]

	if icmpType != 3 || icmpCode != 4 {
		return nil, fmt.Errorf("not a fragmentation needed packet: type=%d, code=%d", icmpType, icmpCode)
	}

	// Extract MTU from ICMP header (bytes 6-7)
	mtu := binary.BigEndian.Uint16(packet[icmpStart+6 : icmpStart+8])

	// Parse inner IP header
	innerIPStart := icmpStart + 8
	if len(packet) < innerIPStart+20 {
		return nil, errors.New("packet too short for inner IP header")
	}

	innerIPVersion := packet[innerIPStart] >> 4
	if innerIPVersion != 4 {
		return nil, fmt.Errorf("invalid inner IP version: %d", innerIPVersion)
	}

	innerIPHeaderLen := int(packet[innerIPStart]&0x0f) * 4
	if innerIPHeaderLen < 20 {
		return nil, fmt.Errorf("invalid inner IP header length: %d", innerIPHeaderLen)
	}

	// Extract source and destination IPs from inner IP header
	srcIP := net.IP(packet[innerIPStart+12 : innerIPStart+16])
	dstIP := net.IP(packet[innerIPStart+16 : innerIPStart+20])

	// Parse inner transport header (first 8 bytes contain src/dst ports)
	transportStart := innerIPStart + innerIPHeaderLen
	if len(packet) < transportStart+8 {
		return nil, errors.New("packet too short for transport header")
	}

	srcPort := binary.BigEndian.Uint16(packet[transportStart : transportStart+2])
	dstPort := binary.BigEndian.Uint16(packet[transportStart+2 : transportStart+4])

	return &ICMPFragNeededInfo{
		MTU:     mtu,
		SrcIP:   srcIP,
		DstIP:   dstIP,
		SrcPort: srcPort,
		DstPort: dstPort,
	}, nil
}
