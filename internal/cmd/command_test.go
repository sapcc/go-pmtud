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
