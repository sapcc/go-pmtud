// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package udp

import (
	"encoding/binary"
	"net"
	"strings"
	"testing"
)

// buildICMPFragNeededPacket assembles a minimal valid ICMP fragmentation needed
// packet: 20-byte outer IP + 8-byte ICMP header + 20-byte inner IP + 8-byte
// transport header = 56 bytes total.
func buildICMPFragNeededPacket(mtu uint16, innerSrc, innerDst net.IP, srcPort, dstPort uint16) []byte {
	pkt := make([]byte, 56)
	// Outer IP header (IHL=5)
	pkt[0] = 0x45
	// ICMP header at offset 20
	pkt[20] = 3 // type: destination unreachable
	pkt[21] = 4 // code: fragmentation needed
	binary.BigEndian.PutUint16(pkt[26:28], mtu)
	// Inner IP header at offset 28 (IHL=5)
	pkt[28] = 0x45
	copy(pkt[40:44], innerSrc.To4())
	copy(pkt[44:48], innerDst.To4())
	// Transport header at offset 48
	binary.BigEndian.PutUint16(pkt[48:50], srcPort)
	binary.BigEndian.PutUint16(pkt[50:52], dstPort)
	return pkt
}

func TestParseICMPFragNeeded(t *testing.T) {
	tests := []struct {
		name    string
		packet  []byte
		wantErr string
		want    *ICMPFragNeededInfo
	}{
		{
			name:   "valid packet",
			packet: buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 1234, 443),
			want: &ICMPFragNeededInfo{
				MTU:     1400,
				SrcIP:   net.ParseIP("10.0.0.1").To4(),
				DstIP:   net.ParseIP("10.0.0.2").To4(),
				SrcPort: 1234,
				DstPort: 443,
			},
		},
		{
			name:    "packet too short",
			packet:  make([]byte, 20),
			wantErr: "packet too short: 20 bytes",
		},
		{
			name: "invalid outer IP version",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[0] = 0x65 // version=6
				return p
			}(),
			wantErr: "invalid IP version: 6",
		},
		{
			name: "outer IP header too long for packet",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[0] = 0x4f // IHL=15 → header 60 bytes; 60+8=68 > 56
				return p
			}(),
			wantErr: "packet too short for IP header",
		},
		{
			name: "wrong ICMP type",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[20] = 8 // echo request
				return p
			}(),
			wantErr: "not a fragmentation needed packet",
		},
		{
			name: "wrong ICMP code",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[21] = 0
				return p
			}(),
			wantErr: "not a fragmentation needed packet",
		},
		{
			// Outer IHL=8 (32 bytes) pushes innerIPStart to 40; 40+20=60 > 56.
			name: "packet too short for inner IP header",
			packet: func() []byte {
				p := make([]byte, 56)
				p[0] = 0x48 // outer IHL=8 (32 bytes)
				icmpOff := 32
				p[icmpOff] = 3
				p[icmpOff+1] = 4
				binary.BigEndian.PutUint16(p[icmpOff+6:icmpOff+8], 1400)
				return p
			}(),
			wantErr: "packet too short for inner IP header",
		},
		{
			name: "invalid inner IP version",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[28] = 0x65 // inner version=6
				return p
			}(),
			wantErr: "invalid inner IP version: 6",
		},
		{
			name: "invalid inner IP header length",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[28] = 0x44 // inner IHL=4 → 16 bytes < 20
				return p
			}(),
			wantErr: "invalid inner IP header length: 16",
		},
		{
			// Inner IHL=6 (24 bytes) pushes transport start to 52; 52+8=60 > 56.
			name: "packet too short for transport header",
			packet: func() []byte {
				p := buildICMPFragNeededPacket(1400, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"), 80, 80)
				p[28] = 0x46 // inner IHL=6 (24 bytes)
				return p
			}(),
			wantErr: "packet too short for transport header",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseICMPFragNeeded(tc.packet)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error %q, got nil", tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("expected error containing %q, got %q", tc.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.MTU != tc.want.MTU {
				t.Errorf("MTU: got %d, want %d", got.MTU, tc.want.MTU)
			}
			if !got.SrcIP.Equal(tc.want.SrcIP) {
				t.Errorf("SrcIP: got %v, want %v", got.SrcIP, tc.want.SrcIP)
			}
			if !got.DstIP.Equal(tc.want.DstIP) {
				t.Errorf("DstIP: got %v, want %v", got.DstIP, tc.want.DstIP)
			}
			if got.SrcPort != tc.want.SrcPort {
				t.Errorf("SrcPort: got %d, want %d", got.SrcPort, tc.want.SrcPort)
			}
			if got.DstPort != tc.want.DstPort {
				t.Errorf("DstPort: got %d, want %d", got.DstPort, tc.want.DstPort)
			}
		})
	}
}
