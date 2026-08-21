// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import "testing"

func TestParsePingFragNeeded(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want int
	}{
		{"iputils", "From 10.99.0.1 icmp_seq=1 Frag needed and DF set (mtu = 1280)\n", 1280},
		{"nospace", "frag needed (mtu=1400)", 1400},
		{"none", "3 packets transmitted, 3 received\n", 0},
	}
	for _, tt := range tests {
		if got := parsePingFragNeeded(tt.out); got != tt.want {
			t.Errorf("%s: parsePingFragNeeded = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestParseRouteMTU(t *testing.T) {
	got := parseRouteMTU("10.99.0.2 via 172.18.0.4 dev eth0 mtu 1280\n")
	if got != 1280 {
		t.Errorf("parseRouteMTU = %d, want 1280", got)
	}
	if parseRouteMTU("10.99.0.2 dev eth0\n") != 0 {
		t.Errorf("parseRouteMTU with no mtu should be 0")
	}
}
