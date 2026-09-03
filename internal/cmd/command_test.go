// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package cmd

import (
	"testing"

	conf "github.com/sapcc/go-pmtud/internal/config"
)

func TestDefaultBackendIsL2(t *testing.T) {
	if cfg.RelayBackend != conf.BackendL2 {
		t.Errorf("default relay backend = %q, want %q", cfg.RelayBackend, conf.BackendL2)
	}
}

func TestL2FlagsRegistered(t *testing.T) {
	for _, name := range []string{"iface_names", "iface_mtu", "node-timeout-minutes", "arp-timeout-seconds"} {
		if rootCmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("flag %q not registered", name)
		}
	}
}

// TestLegacyCLIFlagDefaults pins every flag that existed before --relay-backend
// was introduced. A regression (removal or default change) would silently break
// existing deployments that rely on the old invocation.
func TestLegacyCLIFlagDefaults(t *testing.T) {
	tests := []struct {
		flag    string
		defVal  string
	}{
		{"nodename", ""},
		{"metrics_port", ":30040"},
		{"health_port", ":30041"},
		{"nflog_group", "33"},
		{"ttl", "1"},
		{"kube_context", ""},
		{"iface_names", "[]"},
		{"iface_mtu", "1500"},
		{"node-timeout-minutes", "5"},
		{"arp-timeout-seconds", "1"},
	}
	for _, tc := range tests {
		f := rootCmd.PersistentFlags().Lookup(tc.flag)
		if f == nil {
			t.Errorf("flag %q no longer registered (backward-compat breakage)", tc.flag)
			continue
		}
		if f.DefValue != tc.defVal {
			t.Errorf("flag %q default = %q, want %q (backward-compat breakage)", tc.flag, f.DefValue, tc.defVal)
		}
	}
}
