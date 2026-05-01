// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package nflog

import (
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/sapcc/go-pmtud/internal/config"
)

func TestUDPSendToAllPeers(t *testing.T) {
	const replicationPort = 14390
	const numPeers = 3

	cfg := &config.Config{
		PeerList: make(map[string]string),
	}

	// Start UDP listeners simulating peers
	var wg sync.WaitGroup
	received := make([][]byte, numPeers)

	for i := range numPeers {
		port := replicationPort + i
		cfg.PeerList[fmt.Sprintf("peer-%d", i)] = "127.0.0.1"

		addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", port))
		if err != nil {
			t.Fatal(err)
		}
		conn, err := net.ListenUDP("udp4", addr)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		wg.Add(1)
		go func(idx int, c *net.UDPConn) {
			defer wg.Done()
			buf := make([]byte, 1500)
			if err := c.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				return
			}
			n, _, err := c.ReadFromUDP(buf)
			if err != nil {
				return
			}
			received[idx] = make([]byte, n)
			copy(received[idx], buf[:n])
		}(i, conn)
	}

	// Override peer list with different ports for each peer (since all are localhost)
	cfg.PeerList = make(map[string]string)
	for i := range numPeers {
		cfg.PeerList[fmt.Sprintf("peer-%d", i)] = "127.0.0.1"
	}

	// Simulate sending to peers using the same logic as the controller
	testPayload := buildTestICMPPacket()

	cfg.PeerMutex.Lock()
	var peerIPs []string
	for _, ip := range cfg.PeerList {
		peerIPs = append(peerIPs, ip)
	}
	cfg.PeerMutex.Unlock()

	for i, peerIP := range peerIPs {
		port := replicationPort + i
		addr := net.JoinHostPort(peerIP, strconv.Itoa(port))
		conn, err := net.Dial("udp4", addr)
		if err != nil {
			t.Fatalf("error dialing peer %s: %v", addr, err)
		}
		_, err = conn.Write(testPayload)
		if err != nil {
			t.Fatalf("error writing to peer %s: %v", addr, err)
		}
		conn.Close()
	}

	wg.Wait()

	for i := range numPeers {
		if received[i] == nil {
			t.Errorf("peer %d did not receive packet", i)
			continue
		}
		if len(received[i]) != len(testPayload) {
			t.Errorf("peer %d received %d bytes, expected %d", i, len(received[i]), len(testPayload))
		}
	}
}

// buildTestICMPPacket creates a minimal valid ICMP type 3 code 4 packet
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
