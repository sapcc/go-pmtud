// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"os"
	"testing"
)

func TestRelayObjectNameStable(t *testing.T) {
	p := []byte{1, 2, 3, 4}
	a := relayObjectName("node-a", p)
	b := relayObjectName("node-a", p)
	if a != b {
		t.Fatalf("name not deterministic: %q vs %q", a, b)
	}
	if relayObjectName("node-b", p) == a {
		t.Fatal("different source nodes must yield different names")
	}
	// <node>--<32 hex chars> (16 bytes of sha256)
	if len(a) != len("node-a")+2+32 {
		t.Fatalf("unexpected name shape: %q", a)
	}
}

func TestCRDSendCreatesAndDedups(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("envtest assets not available")
	}
	// envtest integration test - skipped if KUBEBUILDER_ASSETS not set
	// TODO: implement full envtest boilerplate when needed
}
