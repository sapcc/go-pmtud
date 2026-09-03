// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"bytes"
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/mdlayher/ethernet"
	"github.com/mdlayher/packet"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/relay"
)

// fakeConn records frames written via WriteTo.
type fakeConn struct {
	mu     sync.Mutex
	frames [][]byte
	addrs  []net.HardwareAddr
	closed bool
}

func (f *fakeConn) WriteTo(b []byte, addr net.Addr) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]byte, len(b))
	copy(cp, b)
	f.frames = append(f.frames, cp)
	if pa, ok := addr.(*packet.Addr); ok {
		f.addrs = append(f.addrs, pa.HardwareAddr)
	}
	return len(b), nil
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeConn) snapshot() ([][]byte, []net.HardwareAddr, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.frames, f.addrs, f.closed
}

// fixedResolver returns a deterministic MAC derived from the last IP octet.
type fixedResolver struct{}

func (fixedResolver) Resolve(ip string) (net.HardwareAddr, error) {
	// Derive a stable, unique MAC per IP for assertions.
	sum := byte(0)
	for i := 0; i < len(ip); i++ {
		sum += ip[i]
	}
	return net.HardwareAddr{0x02, 0, 0, 0, 0, sum}, nil
}

func newTestBackend() (*backend, *fakeConn) {
	fc := &fakeConn{}
	src, _ := net.ParseMAC("11:22:33:44:55:66")
	cfg := &config.Config{
		NodeName: "test-node",
		PeerList: map[string]string{},
	}
	return &backend{
		cfg:   cfg,
		log:   logr.Discard(),
		ifi:   &net.Interface{Name: "eth0", HardwareAddr: src},
		conn:  fc,
		cache: newMACCache(fixedResolver{}, time.Minute),
	}, fc
}

func TestL2SendWritesFramePerPeer(t *testing.T) {
	lb, fc := newTestBackend()
	lb.cfg.PeerList["a"] = "10.0.0.1"
	lb.cfg.PeerList["b"] = "10.0.0.2"

	payload := []byte{0x45, 0x00, 0x00, 0x1c}
	if err := lb.Send(context.Background(), relayPacket(payload)); err != nil {
		t.Fatalf("Send: %v", err)
	}

	frames, addrs, _ := fc.snapshot()
	if len(frames) != 2 {
		t.Fatalf("wrote %d frames, want 2 (one per peer)", len(frames))
	}
	if len(addrs) != 2 {
		t.Fatalf("recorded %d dst addrs, want 2", len(addrs))
	}
	// Each frame must be a valid Ethernet frame carrying our payload prefix,
	// destined to the resolved MAC (ethernet zero-pads payload to 46 bytes).
	for i, raw := range frames {
		var f ethernet.Frame
		if err := f.UnmarshalBinary(raw); err != nil {
			t.Fatalf("frame %d unmarshal: %v", i, err)
		}
		if f.EtherType != ethernet.EtherTypeIPv4 {
			t.Errorf("frame %d ethertype = %v, want IPv4", i, f.EtherType)
		}
		if !bytes.Equal(f.Payload[:len(payload)], payload) {
			t.Errorf("frame %d payload prefix = %v, want %v", i, f.Payload[:len(payload)], payload)
		}
		if !bytes.Equal(f.Destination, addrs[i]) {
			t.Errorf("frame %d destination = %v, want %v", i, f.Destination, addrs[i])
		}
	}
}

func TestL2StartClosesConnOnContextCancel(t *testing.T) {
	// The L2 backend creates no TUN device (it has no injector by
	// construction). Start must block until ctx is done, then close the conn.
	lb, fc := newTestBackend()
	lb.cfg.ReplicationInterface = "eth0"

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- lb.Start(ctx) }()

	// Start should still be blocking; conn not yet closed.
	time.Sleep(20 * time.Millisecond)
	if _, _, closed := fc.snapshot(); closed {
		t.Fatal("conn closed before context cancellation")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start did not return after context cancellation")
	}
	if _, _, closed := fc.snapshot(); !closed {
		t.Error("Start did not close the conn")
	}
}

func relayPacket(payload []byte) relay.RelayPacket {
	return relay.RelayPacket{Payload: payload, SrcNode: "test-node"}
}
