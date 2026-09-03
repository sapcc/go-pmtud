// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"bytes"
	"net"
	"testing"

	"github.com/mdlayher/ethernet"
)

func TestBuildFrameRoundTrip(t *testing.T) {
	src, _ := net.ParseMAC("11:22:33:44:55:66")
	dst, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	payload := []byte{0x45, 0x00, 0x00, 0x1c} // truncated IPv4 header bytes

	raw, err := buildFrame(src, dst, payload)
	if err != nil {
		t.Fatalf("buildFrame: %v", err)
	}

	var f ethernet.Frame
	if err := f.UnmarshalBinary(raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !bytes.Equal(f.Source, src) {
		t.Errorf("source = %v, want %v", f.Source, src)
	}
	if !bytes.Equal(f.Destination, dst) {
		t.Errorf("destination = %v, want %v", f.Destination, dst)
	}
	if f.EtherType != ethernet.EtherTypeIPv4 {
		t.Errorf("ethertype = %v, want IPv4", f.EtherType)
	}
	// The ethernet library zero-pads short payloads to the 46-byte minimum;
	// compare only the leading bytes that we wrote.
	if !bytes.Equal(f.Payload[:len(payload)], payload) {
		t.Errorf("payload prefix = %v, want %v", f.Payload[:len(payload)], payload)
	}
}
