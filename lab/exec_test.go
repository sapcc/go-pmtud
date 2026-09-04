// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import "testing"

func TestIPFromNetworksJSON(t *testing.T) {
	js := `{"kind":{"IPAddress":"172.18.0.5"},"pmtud-transit":{"IPAddress":"172.32.0.4"}}`
	if ip, err := ipFromNetworksJSON(js, "kind"); err != nil || ip != "172.18.0.5" {
		t.Fatalf(`kind: got %q, %v; want "172.18.0.5"`, ip, err)
	}
	if ip, err := ipFromNetworksJSON(js, "pmtud-transit"); err != nil || ip != "172.32.0.4" {
		t.Fatalf(`transit: got %q, %v; want "172.32.0.4"`, ip, err)
	}
	if _, err := ipFromNetworksJSON(js, "absent"); err == nil {
		t.Errorf("expected error for absent network")
	}
	if _, err := ipFromNetworksJSON(`{"kind":{"IPAddress":""}}`, "kind"); err == nil {
		t.Errorf("expected error for empty IP")
	}
}
