// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import "testing"

func TestParseNodeLines(t *testing.T) {
	got := parseNodeLines("pmtud-worker\npmtud-worker2\n")
	if len(got) != 2 || got[0] != "pmtud-worker" || got[1] != "pmtud-worker2" {
		t.Fatalf("parseNodeLines = %v, want [pmtud-worker pmtud-worker2]", got)
	}
	if len(parseNodeLines("  \n")) != 0 {
		t.Errorf("parseNodeLines on blank input should be empty")
	}
}
