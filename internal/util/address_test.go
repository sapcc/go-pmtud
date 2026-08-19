// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package util

import (
	"net"
	"testing"
)

// icmpFragNeeded builds a minimal 48-byte ICMP frag-needed payload:
// 20 bytes outer IP header + 8 bytes ICMP header + 20 bytes inner IP header.
// CalcSrcDst reads inner src at bytes [40:44] and inner dst at bytes [44:48].
func icmpFragNeeded(innerSrc, innerDst net.IP) []byte {
	b := make([]byte, 48)
	copy(b[40:44], innerSrc.To4())
	copy(b[44:48], innerDst.To4())
	return b
}

func TestCalcSrcDst(t *testing.T) {
	tests := []struct {
		name    string
		payload []byte
		wantSrc net.IP
		wantDst net.IP
		wantErr bool
	}{
		{
			name:    "valid inner src and dst",
			payload: icmpFragNeeded(net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2")),
			wantSrc: net.IP{10, 0, 0, 1},
			wantDst: net.IP{10, 0, 0, 2},
		},
		{
			name:    "loopback addresses",
			payload: icmpFragNeeded(net.ParseIP("127.0.0.1"), net.ParseIP("127.0.0.2")),
			wantSrc: net.IP{127, 0, 0, 1},
			wantDst: net.IP{127, 0, 0, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src, dst, err := CalcSrcDst(tt.payload)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !src.Equal(tt.wantSrc) {
				t.Errorf("src = %v, want %v", src, tt.wantSrc)
			}
			if !dst.Equal(tt.wantDst) {
				t.Errorf("dst = %v, want %v", dst, tt.wantDst)
			}
		})
	}
}

// TestCalcSrcDst_ShortPayload documents that a payload shorter than 48 bytes
// causes a panic due to missing bounds checking in CalcSrcDst.
func TestCalcSrcDst_ShortPayload(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for short payload, got none")
		}
	}()
	_, _, _ = CalcSrcDst(make([]byte, 10))
}
