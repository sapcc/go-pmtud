// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

// makeRoot creates a tempdir that mimics /proc/sys/net/ipv4/conf/<iface>/rp_filter
// for each of the given interface names (plus "all"). Returns the root path.
func makeRoot(t *testing.T, ifaces ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, iface := range append([]string{"all"}, ifaces...) {
		dir := filepath.Join(root, "proc", "sys", "net", "ipv4", "conf", iface)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "rp_filter"), []byte("1\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestSetupSysctl_HappyPath(t *testing.T) {
	root := makeRoot(t, "eth0")
	m := &Manager{
		cfg:    &config.Config{InterfaceNames: []string{"eth0"}},
		log:    logr.Discard(),
		fsRoot: root,
	}

	if err := m.setupSysctl(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for _, iface := range []string{"all", "eth0"} {
		p := filepath.Join(root, "proc", "sys", "net", "ipv4", "conf", iface, "rp_filter")
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != "0" {
			t.Errorf("iface %s: got %q, want \"0\"", iface, string(got))
		}
	}
}

// TestSetupSysctl_MissingIface verifies that a per-interface path that does not
// exist in procfs is silently skipped (the interface may not be present on every
// node of the DaemonSet).
func TestSetupSysctl_MissingIface(t *testing.T) {
	// Only "all" exists; "eth0" does not.
	root := makeRoot(t)
	m := &Manager{
		cfg:    &config.Config{InterfaceNames: []string{"eth0"}},
		log:    logr.Discard(),
		fsRoot: root,
	}

	if err := m.setupSysctl(); err != nil {
		t.Fatalf("missing iface should be skipped, got error: %v", err)
	}

	// "all" must still have been written.
	allPath := filepath.Join(root, "proc", "sys", "net", "ipv4", "conf", "all", "rp_filter")
	got, err := os.ReadFile(allPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0" {
		t.Errorf("all/rp_filter: got %q, want \"0\"", string(got))
	}
}

// TestSetupSysctl_AllMissing verifies that a missing "all/rp_filter" path is fatal.
func TestSetupSysctl_AllMissing(t *testing.T) {
	root := t.TempDir() // empty — no procfs entries at all
	m := &Manager{
		cfg:    &config.Config{},
		log:    logr.Discard(),
		fsRoot: root,
	}

	if err := m.setupSysctl(); err == nil {
		t.Fatal("expected error for missing all/rp_filter, got nil")
	}
}
