// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package udp

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/relay"
)

func newTestBackend(t *testing.T, peers map[string]string) *backend {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	return &backend{
		cfg: &config.Config{
			NodeName:        "test-node",
			ReplicationPort: port,
			PeerList:        peers,
		},
		log: logr.Discard(),
	}
}

func TestNew(t *testing.T) {
	d := relay.Deps{
		Cfg: &config.Config{NodeName: "n", PeerList: make(map[string]string)},
		Log: logr.Discard(),
	}
	r, err := New(d)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if r == nil {
		t.Fatal("New() returned nil")
	}
}

func TestIsKnownPeer(t *testing.T) {
	ub := newTestBackend(t, map[string]string{
		"peer-1": "10.0.0.1",
		"peer-2": "10.0.0.2",
	})

	testCases := []struct {
		name     string
		ip       net.IP
		expected bool
	}{
		{name: "known peer 1", ip: net.ParseIP("10.0.0.1"), expected: true},
		{name: "known peer 2", ip: net.ParseIP("10.0.0.2"), expected: true},
		{name: "unknown peer", ip: net.ParseIP("192.168.99.99"), expected: false},
		{name: "nil IP", ip: nil, expected: false},
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
	inject := func(_ []byte) error { //nolint:unparam
		injected = true
		return nil
	}

	ub := newTestBackend(t, map[string]string{"peer-1": "10.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx, inject) }()
	time.Sleep(10 * time.Millisecond)

	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()
	if _, err = conn.Write(pkt); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if injected {
		t.Error("packet from unknown source should have been rejected")
	}
}

func TestInvalidPayload(t *testing.T) {
	injected := false
	inject := func(_ []byte) error { //nolint:unparam
		injected = true
		return nil
	}

	ub := newTestBackend(t, map[string]string{"localhost": "127.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx, inject) }()
	time.Sleep(10 * time.Millisecond)

	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()
	if _, err = conn.Write([]byte{0x45, 0x00, 0x00, 0x38}); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if injected {
		t.Error("invalid payload should have been rejected")
	}
}

func TestValidPayload(t *testing.T) {
	var injectedPayload []byte
	inject := func(payload []byte) error { //nolint:unparam
		injectedPayload = make([]byte, len(payload))
		copy(injectedPayload, payload)
		return nil
	}

	ub := newTestBackend(t, map[string]string{"localhost": "127.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx, inject) }()
	time.Sleep(10 * time.Millisecond)

	pkt := buildValidICMPFragNeeded(1500, net.ParseIP("10.0.0.1"), net.ParseIP("10.0.0.2"))
	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()
	if _, err = conn.Write(pkt); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	<-errCh

	if injectedPayload == nil {
		t.Error("valid payload from known peer should have been injected")
	}
	if len(injectedPayload) != len(pkt) {
		t.Errorf("injected payload length mismatch: got %d, want %d", len(injectedPayload), len(pkt))
	}
}

func buildValidICMPFragNeeded(mtu uint16, srcIP, dstIP net.IP) []byte {
	pkt := []byte{
		0x45, 0x00, 0x00, 0x38,
		0x00, 0x00, 0x00, 0x00,
		0x40, 0x01, 0x00, 0x00,
		0xc0, 0xa8, 0x01, 0x01,
		0xc0, 0xa8, 0x01, 0x02,
	}
	pkt = append(pkt, 0x03, 0x04, 0x00, 0x00, 0x00, 0x00, byte(mtu>>8), byte(mtu&0xff)) //#nosec G115
	pkt = append(pkt,
		0x45, 0x00, 0x00, 0x3c,
		0x12, 0x34, 0x40, 0x00,
		0x40, 0x06, 0x00, 0x00,
	)
	pkt = append(pkt, srcIP.To4()...)
	pkt = append(pkt, dstIP.To4()...)
	pkt = append(pkt, 0x30, 0x39, 0x00, 0x50, 0x00, 0x00, 0x00, 0x00)
	return pkt
}
