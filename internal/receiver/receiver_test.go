// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"net"
	"testing"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/packet"
)

func TestValidateValidPacket(t *testing.T) {
	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))

	info, err := packet.ParseICMPFragNeeded(pkt)
	if err != nil {
		t.Fatalf("expected valid packet to parse, got error: %v", err)
	}
	if info.MTU != 1500 {
		t.Errorf("expected MTU 1500, got %d", info.MTU)
	}
	if !info.SrcIP.Equal(net.ParseIP("10.0.0.1")) {
		t.Errorf("expected SrcIP 10.0.0.1, got %v", info.SrcIP)
	}
	if !info.DstIP.Equal(net.ParseIP("10.0.0.2")) {
		t.Errorf("expected DstIP 10.0.0.2, got %v", info.DstIP)
	}
}

func TestValidateInvalidPacketTooShort(t *testing.T) {
	pkt := []byte{0x45, 0x00, 0x00, 0x38}

	_, err := packet.ParseICMPFragNeeded(pkt)
	if err == nil {
		t.Fatal("expected error for short packet, got nil")
	}
}

func TestValidateWrongICMPType(t *testing.T) {
	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	// Change ICMP type to echo request (type 8)
	pkt[20] = 0x08
	pkt[21] = 0x00

	_, err := packet.ParseICMPFragNeeded(pkt)
	if err == nil {
		t.Fatal("expected error for wrong ICMP type, got nil")
	}
}

func TestValidateWrongICMPCode(t *testing.T) {
	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	// Change ICMP code to 1 (host unreachable) instead of 4 (frag needed)
	pkt[21] = 0x01

	_, err := packet.ParseICMPFragNeeded(pkt)
	if err == nil {
		t.Fatal("expected error for wrong ICMP code, got nil")
	}
}

func TestValidateEmptyPayload(t *testing.T) {
	_, err := packet.ParseICMPFragNeeded([]byte{})
	if err == nil {
		t.Fatal("expected error for empty payload, got nil")
	}
}

func buildValidICMPFragNeeded(mtu uint16, srcIP, dstIP net.IP) []byte {
	// Outer IP header (20 bytes)
	pkt := []byte{
		0x45, 0x00, 0x00, 0x38, // Version/IHL, TOS, Total Length
		0x00, 0x00, 0x00, 0x00, // ID, Flags/Fragment Offset
		0x40, 0x01, 0x00, 0x00, // TTL, Protocol (ICMP), Checksum
		0xc0, 0xa8, 0x01, 0x01, // Source IP (placeholder)
		0xc0, 0xa8, 0x01, 0x02, // Destination IP (placeholder)
	}

	// ICMP header (8 bytes)
	pkt = append(pkt, []byte{
		0x03, 0x04, // Type (Dest Unreachable), Code (Frag Needed)
		0x00, 0x00, // Checksum
		0x00, 0x00, // Unused
		byte(mtu >> 8), byte(mtu & 0xff), // Next-hop MTU //#nosec G115
	}...)

	// Inner IP header (20 bytes)
	pkt = append(pkt, []byte{
		0x45, 0x00, 0x00, 0x3c, // Version/IHL, TOS, Total Length
		0x12, 0x34, 0x40, 0x00, // ID, Flags/Fragment Offset
		0x40, 0x06, 0x00, 0x00, // TTL, Protocol (TCP), Checksum
	}...)
	pkt = append(pkt, srcIP.To4()...)
	pkt = append(pkt, dstIP.To4()...)

	// Inner TCP header (8 bytes)
	pkt = append(pkt, []byte{
		0x30, 0x39, // Source port (12345)
		0x00, 0x50, // Destination port (80)
		0x00, 0x00, 0x00, 0x00, // Sequence number
	}...)

	return pkt
}

func TestIsKnownPeer_UnknownSourceRejected(t *testing.T) {
	cfg := &config.Config{
		NodeName: "test-node",
		PeerList: map[string]string{
			"node-a": "10.0.1.1",
			"node-b": "10.0.1.2",
		},
	}
	rc := &Controller{Cfg: cfg}

	unknownIP := net.ParseIP("192.168.99.99")
	if rc.isKnownPeer(unknownIP) {
		t.Errorf("expected unknown IP %v to be rejected, but was accepted", unknownIP)
	}
}

func TestIsKnownPeer_KnownSourceAccepted(t *testing.T) {
	cfg := &config.Config{
		NodeName: "test-node",
		PeerList: map[string]string{
			"node-a": "10.0.1.1",
			"node-b": "10.0.1.2",
		},
	}
	rc := &Controller{Cfg: cfg}

	knownIP := net.ParseIP("10.0.1.2")
	if !rc.isKnownPeer(knownIP) {
		t.Errorf("expected known peer IP %v to be accepted, but was rejected", knownIP)
	}
}
