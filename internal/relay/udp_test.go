// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"net"
	"testing"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

func newTestUDP() *udpBackend {
	return &udpBackend{
		cfg: &config.Config{
			NodeName:        "test-node",
			ReplicationPort: 0, // Use dynamic port
			PeerList: map[string]string{
				"peer-1": "10.0.0.1",
				"peer-2": "10.0.0.2",
			},
		},
		log: logr.Discard(),
	}
}

func TestIsKnownPeer(t *testing.T) {
	ub := newTestUDP()

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
