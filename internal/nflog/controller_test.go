// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package nflog

import (
	"context"
	"net"
	"testing"

	"github.com/florianl/go-nflog/v2"
	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/relay"
)

type fakeRelay struct {
	sent []relay.RelayPacket
}

func (f *fakeRelay) Send(_ context.Context, p relay.RelayPacket) error {
	f.sent = append(f.sent, p)
	return nil
}

func (f *fakeRelay) Start(context.Context, func([]byte) error) error {
	return nil
}

func newTestController(cfg *config.Config, r relay.Relay) *Controller {
	return &Controller{
		Log:   logr.Discard(),
		Cfg:   cfg,
		Relay: r,
	}
}

func attrsWithPayload(payload []byte) nflog.Attribute {
	return nflog.Attribute{Payload: &payload}
}

// TestHandlePacket_RelaysValidPacket verifies that a well-formed ICMP frag-needed
// packet is forwarded to the relay with the correct payload and source node.
func TestHandlePacket_RelaysValidPacket(t *testing.T) {
	cfg := &config.Config{NodeName: "node-a", PeerList: make(map[string]string)}
	fr := &fakeRelay{}
	c := newTestController(cfg, fr)

	payload := buildTestICMPPacket()
	c.handlePacket(context.Background(), attrsWithPayload(payload))

	if len(fr.sent) != 1 {
		t.Fatalf("expected 1 relay.Send call, got %d", len(fr.sent))
	}
	if fr.sent[0].SrcNode != "node-a" {
		t.Errorf("SrcNode = %q, want %q", fr.sent[0].SrcNode, "node-a")
	}
	if len(fr.sent[0].Payload) != len(payload) {
		t.Errorf("payload len = %d, want %d", len(fr.sent[0].Payload), len(payload))
	}
}

// TestHandlePacket_NilPayload verifies that a callback with no packet copy is dropped.
func TestHandlePacket_NilPayload(t *testing.T) {
	cfg := &config.Config{NodeName: "node-a", PeerList: make(map[string]string)}
	fr := &fakeRelay{}
	c := newTestController(cfg, fr)

	c.handlePacket(context.Background(), nflog.Attribute{Payload: nil})

	if len(fr.sent) != 0 {
		t.Errorf("expected no relay.Send call for nil payload, got %d", len(fr.sent))
	}
}

// TestHandlePacket_IgnoredNetwork verifies that packets from configured ignore-networks
// are dropped before reaching the relay.
func TestHandlePacket_IgnoredNetwork(t *testing.T) {
	_, ignored, err := net.ParseCIDR("192.168.1.0/24")
	if err != nil {
		t.Fatalf("failed to parse CIDR: %v", err)
	}

	cfg := &config.Config{
		NodeName:       "node-a",
		PeerList:       make(map[string]string),
		IgnoreNetworks: []*net.IPNet{ignored},
	}
	fr := &fakeRelay{}
	c := newTestController(cfg, fr)

	// buildTestICMPPacket has outer source IP 192.168.1.1 — inside the ignored network
	c.handlePacket(context.Background(), attrsWithPayload(buildTestICMPPacket()))

	if len(fr.sent) != 0 {
		t.Errorf("expected no relay.Send call for ignored-network packet, got %d", len(fr.sent))
	}
}

// TestHandlePacket_PeerIP verifies that packets whose outer source IP matches a peer
// are dropped as a loop-prevention measure.
func TestHandlePacket_PeerIP(t *testing.T) {
	cfg := &config.Config{
		NodeName: "node-a",
		PeerList: map[string]string{"peer-b": "192.168.1.1"}, // matches outer src in test packet
	}
	fr := &fakeRelay{}
	c := newTestController(cfg, fr)

	c.handlePacket(context.Background(), attrsWithPayload(buildTestICMPPacket()))

	if len(fr.sent) != 0 {
		t.Errorf("expected no relay.Send call for peer-IP packet, got %d", len(fr.sent))
	}
}

func TestIsIgnoredNetwork(t *testing.T) {
	tests := []struct {
		name     string
		ip       net.IP
		networks []string
		want     bool
	}{
		{
			name:     "IP in ignored network",
			ip:       net.ParseIP("10.0.1.5"),
			networks: []string{"10.0.1.0/24"},
			want:     true,
		},
		{
			name:     "IP not in ignored network",
			ip:       net.ParseIP("203.0.113.1"),
			networks: []string{"10.0.1.0/24"},
			want:     false,
		},
		{
			name:     "IP in second of multiple networks",
			ip:       net.ParseIP("172.16.0.50"),
			networks: []string{"10.0.0.0/8", "172.16.0.0/16"},
			want:     true,
		},
		{
			name:     "empty network list",
			ip:       net.ParseIP("10.0.1.5"),
			networks: nil,
			want:     false,
		},
		{
			name:     "exact host match /32",
			ip:       net.ParseIP("192.168.1.1"),
			networks: []string{"192.168.1.1/32"},
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var parsed []*net.IPNet
			for _, cidr := range tt.networks {
				_, ipNet, err := net.ParseCIDR(cidr)
				if err != nil {
					t.Fatal(err)
				}
				parsed = append(parsed, ipNet)
			}
			got := isIgnoredNetwork(tt.ip, parsed)
			if got != tt.want {
				t.Errorf("isIgnoredNetwork(%v, %v) = %v, want %v", tt.ip, tt.networks, got, tt.want)
			}
		})
	}
}

func TestIsPeerIP(t *testing.T) {
	peerIPs := []string{"10.0.1.1", "10.0.1.2", "172.16.0.5"}

	tests := []struct {
		name string
		ip   net.IP
		want bool
	}{
		{"matches first peer", net.ParseIP("10.0.1.1"), true},
		{"matches last peer", net.ParseIP("172.16.0.5"), true},
		{"no match", net.ParseIP("192.168.1.1"), false},
		{"empty peer list", net.ParseIP("10.0.1.1"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peers := peerIPs
			if tt.name == "empty peer list" {
				peers = nil
			}
			got := isPeerIP(tt.ip, peers)
			if got != tt.want {
				t.Errorf("isPeerIP(%v, %v) = %v, want %v", tt.ip, peers, got, tt.want)
			}
		})
	}
}

// buildTestICMPPacket creates a minimal valid ICMP type 3 code 4 packet.
// Outer source IP is 192.168.1.1, inner src/dst are 10.0.0.1 and 10.0.0.2.
func buildTestICMPPacket() []byte {
	// Outer IP header (20 bytes)
	packet := []byte{
		0x45, 0x00, 0x00, 0x38, // Version/IHL, TOS, Total Length
		0x00, 0x00, 0x00, 0x00, // ID, Flags/Fragment Offset
		0x40, 0x01, 0x00, 0x00, // TTL, Protocol (ICMP), Checksum
		0xc0, 0xa8, 0x01, 0x01, // Source IP 192.168.1.1
		0xc0, 0xa8, 0x01, 0x02, // Destination IP 192.168.1.2
	}

	// ICMP header (8 bytes) - Type 3, Code 4
	packet = append(packet, []byte{
		0x03, 0x04, // Type (Dest Unreachable), Code (Frag Needed)
		0x00, 0x00, // Checksum
		0x00, 0x00, // Unused
		0x05, 0xDC, // Next-hop MTU (1500)
	}...)

	// Inner IP header (20 bytes)
	packet = append(packet, []byte{
		0x45, 0x00, 0x00, 0x3c, // Version/IHL, TOS, Total Length
		0x12, 0x34, 0x40, 0x00, // ID, Flags/Fragment Offset
		0x40, 0x06, 0x00, 0x00, // TTL, Protocol (TCP), Checksum
		0x0a, 0x00, 0x00, 0x01, // Source IP 10.0.0.1
		0x0a, 0x00, 0x00, 0x02, // Destination IP 10.0.0.2
	}...)

	// Inner TCP header (8 bytes)
	packet = append(packet, []byte{
		0x30, 0x39, // Source port (12345)
		0x00, 0x50, // Destination port (80)
		0x00, 0x00, 0x00, 0x00, // Sequence number
	}...)

	return packet
}
