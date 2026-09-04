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

func TestSumMetricPeer(t *testing.T) {
	out := `# HELP go_pmtud_sent_packets_peer Number of sent ICMP packets per peer
# TYPE go_pmtud_sent_packets_peer counter
go_pmtud_sent_packets_peer{node="pmtud-worker",peer="172.18.0.5"} 2
go_pmtud_sent_packets_peer{node="pmtud-worker",peer="172.18.0.6"} 1
`
	if got := sumMetricPeer(out, "go_pmtud_sent_packets_peer", "172.18.0.5"); got != 2 {
		t.Errorf(`peer 172.18.0.5 = %d, want 2`, got)
	}
	if got := sumMetricPeer(out, "go_pmtud_sent_packets_peer", "10.0.0.9"); got != 0 {
		t.Errorf(`absent peer = %d, want 0`, got)
	}
}
