// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"testing"
)

func TestParseMTU(t *testing.T) {
	tests := []struct {
		yaml string
		want string
	}{
		{`--opt "com.docker.network.driver.mtu=9000"`, "9000"},
		{`--opt "com.docker.network.driver.mtu=1500"`, "1500"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := parseMTU(tt.yaml); got != tt.want {
			t.Errorf("parseMTU(%q) = %q, want %q", tt.yaml, got, tt.want)
		}
	}
}

func TestNetworkDefaults(t *testing.T) {
	if len(DefaultNetworks) != 3 {
		t.Fatalf("DefaultNetworks has %d entries, want 3", len(DefaultNetworks))
	}
	if DefaultNetworks[0].Name != "pmtud-net-a" {
		t.Errorf("first network name = %q, want pmtud-net-a", DefaultNetworks[0].Name)
	}
}
