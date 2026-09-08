// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"strings"
	"testing"
)

func TestParseNodeLines(t *testing.T) {
	got := parseNodeLines("pmtud-worker\npmtud-worker2\n")
	if len(got) != 2 || got[0] != "pmtud-worker" || got[1] != "pmtud-worker2" {
		t.Fatalf("parseNodeLines = %v, want [pmtud-worker pmtud-worker2]", got)
	}
	if len(parseNodeLines("  \n")) != 0 {
		t.Errorf("parseNodeLines on blank input should be empty")
	}
}

func TestParseIfaceMTU(t *testing.T) {
	if n, err := parseIfaceMTU("65535\n"); err != nil || n != 65535 {
		t.Fatalf("got %d, %v; want 65535", n, err)
	}
	if n, err := parseIfaceMTU("1500"); err != nil || n != 1500 {
		t.Fatalf("got %d, %v; want 1500", n, err)
	}
	if _, err := parseIfaceMTU("bogus"); err == nil {
		t.Errorf("expected error for non-numeric mtu")
	}
}

func TestPatchDaemonSet(t *testing.T) {
	in := "args:\n- --relay-backend=$(RELAY_BACKEND)\n- --iface_names=eth0\n- --iface_mtu=$(IFACE_MTU)\n"

	l2 := patchDaemonSet(in, "l2", 65535, false)
	if !strings.Contains(l2, "--relay-backend=l2") || !strings.Contains(l2, "--iface_mtu=65535") {
		t.Fatalf("l2 patch wrong:\n%s", l2)
	}

	legacy := patchDaemonSet(in, "l2", 1500, true)
	if strings.Contains(legacy, "--relay-backend") {
		t.Errorf("legacy must strip --relay-backend:\n%s", legacy)
	}
	if !strings.Contains(legacy, "--iface_mtu=1500") {
		t.Errorf("legacy must still carry --iface_mtu:\n%s", legacy)
	}
}
