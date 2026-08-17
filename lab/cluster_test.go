// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"strings"
	"testing"
)

func TestParseWorkerContainerName(t *testing.T) {
	// Test the worker container name parsing (no docker needed)
	output := "pmtud-cluster-a-worker\npmtud-cluster-a-worker2"
	workers := parseWorkerLines(output)
	if len(workers) != 2 {
		t.Fatalf("parseWorkerLines returned %d workers, want 2", len(workers))
	}
	if workers[0] != "pmtud-cluster-a-worker" {
		t.Errorf("first worker = %q, want pmtud-cluster-a-worker", workers[0])
	}
	if workers[1] != "pmtud-cluster-a-worker2" {
		t.Errorf("second worker = %q, want pmtud-cluster-a-worker2", workers[1])
	}
}

func parseWorkerLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
