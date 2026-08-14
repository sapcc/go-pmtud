// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

func newTestUDP(t *testing.T, peers map[string]string) *udpBackend {
	// Find an available port by listening on 0 (dynamic allocation)
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	return &udpBackend{
		cfg: &config.Config{
			NodeName:        "test-node",
			ReplicationPort: port,
			PeerList:        peers,
		},
		log: logr.Discard(),
	}
}

func TestIsKnownPeer(t *testing.T) {
	ub := newTestUDP(t, map[string]string{
		"peer-1": "10.0.0.1",
		"peer-2": "10.0.0.2",
	})

	testCases := []struct {
		name     string
		ip       net.IP
		expected bool
	}{
		{
			name:     "known peer 1",
			ip:       net.ParseIP("10.0.0.1"),
			expected: true,
		},
		{
			name:     "known peer 2",
			ip:       net.ParseIP("10.0.0.2"),
			expected: true,
		},
		{
			name:     "unknown peer",
			ip:       net.ParseIP("192.168.99.99"),
			expected: false,
		},
		{
			name:     "nil IP",
			ip:       nil,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ub.isKnownPeer(tc.ip)
			if result != tc.expected {
				t.Errorf("isKnownPeer(%v) = %v, want %v", tc.ip, result, tc.expected)
			}
		})
	}
}

func TestSendToUnknownPeer(t *testing.T) {
	injected := false
	inject := func(payload []byte) error {
		injected = true
		return nil
	}

	// Don't add localhost/127.0.0.1 to peer list - it will be unknown
	ub := newTestUDP(t, map[string]string{
		"peer-1": "10.0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ub.Start(ctx, inject)
	}()

	time.Sleep(10 * time.Millisecond)

	// Build valid ICMP packet
	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))

	// Send from localhost (which is NOT in peer list)
	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(pkt)
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if injected {
		t.Error("packet from unknown source should have been rejected")
	}
}

func TestInvalidPayload(t *testing.T) {
	injected := false
	inject := func(payload []byte) error {
		injected = true
		return nil
	}

	ub := newTestUDP(t, map[string]string{
		"localhost": "127.0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ub.Start(ctx, inject)
	}()

	time.Sleep(10 * time.Millisecond)

	// Create invalid ICMP packet (too short, not valid format)
	invalidPkt := []byte{0x45, 0x00, 0x00, 0x38}

	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(invalidPkt)
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if injected {
		t.Error("invalid payload should have been rejected")
	}
}

func TestValidPayload(t *testing.T) {
	injected := false
	var injectedPayload []byte
	inject := func(payload []byte) error {
		injected = true
		injectedPayload = make([]byte, len(payload))
		copy(injectedPayload, payload)
		return nil
	}

	ub := newTestUDP(t, map[string]string{
		"localhost": "127.0.0.1",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- ub.Start(ctx, inject)
	}()

	time.Sleep(10 * time.Millisecond)

	// Build valid ICMP packet
	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))

	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	_, err = conn.Write(pkt)
	if err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if !injected {
		t.Error("valid payload from known peer should have been injected")
	}
	if len(injectedPayload) != len(pkt) {
		t.Errorf("injected payload length mismatch: got %d, want %d", len(injectedPayload), len(pkt))
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
