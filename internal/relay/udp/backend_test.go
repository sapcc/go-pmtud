// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package udp

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/relay"
)

type fakeInjector struct {
	mu       sync.Mutex
	injected [][]byte
}

func (f *fakeInjector) Inject(p []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(p))
	copy(cp, p)
	f.injected = append(f.injected, cp)
	return nil
}

func (f *fakeInjector) Close() error { return nil }

func (f *fakeInjector) snapshot() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([][]byte, len(f.injected))
	copy(out, f.injected)
	return out
}

func newTestBackend(t *testing.T, peers map[string]string) (*backend, *fakeInjector) {
	t.Helper()
	listener, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("failed to find available port: %v", err)
	}
	port := listener.LocalAddr().(*net.UDPAddr).Port
	listener.Close()

	fake := &fakeInjector{}
	ub := &backend{
		cfg: &config.Config{
			NodeName:        "test-node",
			ReplicationPort: port,
			PeerList:        peers,
		},
		log:             logr.Discard(),
		injectorFactory: func(string) (injector, error) { return fake, nil },
	}
	return ub, fake
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
	ub, _ := newTestBackend(t, map[string]string{
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
	ub, fake := newTestBackend(t, map[string]string{"peer-1": "10.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx) }()
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

	if err := <-errCh; err != nil {
		t.Errorf("Start returned unexpected error: %v", err)
	}
	if got := len(fake.snapshot()); got != 0 {
		t.Errorf("expected 0 injected packets from unknown source, got %d", got)
	}
}

func TestInvalidPayload(t *testing.T) {
	ub, fake := newTestBackend(t, map[string]string{"localhost": "127.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx) }()
	time.Sleep(10 * time.Millisecond)

	conn, err := net.Dial("udp4", fmt.Sprintf("127.0.0.1:%d", ub.cfg.ReplicationPort))
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()
	if _, err = conn.Write([]byte{0x45, 0x00, 0x00, 0x38}); err != nil {
		t.Fatalf("failed to send: %v", err)
	}

	if err := <-errCh; err != nil {
		t.Errorf("Start returned unexpected error: %v", err)
	}
	if got := len(fake.snapshot()); got != 0 {
		t.Errorf("expected 0 injected packets from invalid payload, got %d", got)
	}
}

func TestValidPayload(t *testing.T) {
	ub, fake := newTestBackend(t, map[string]string{"localhost": "127.0.0.1"})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- ub.Start(ctx) }()
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

	if err := <-errCh; err != nil {
		t.Errorf("Start returned unexpected error: %v", err)
	}
	snapshot := fake.snapshot()
	if got := len(snapshot); got != 1 {
		t.Errorf("expected 1 injected packet from valid payload, got %d", got)
	}
	if len(snapshot) > 0 && !bytes.Equal(snapshot[0], pkt) {
		t.Errorf("injected packet mismatch: got %v, want %v", snapshot[0], pkt)
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
