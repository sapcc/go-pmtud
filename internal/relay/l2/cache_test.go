// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"net"
	"testing"
	"time"
)

type fakeResolver struct {
	calls int
	mac   net.HardwareAddr
	err   error
}

func (f *fakeResolver) Resolve(string) (net.HardwareAddr, error) {
	f.calls++
	return f.mac, f.err
}

func TestMACCacheHitWithinTTL(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	f := &fakeResolver{mac: mac}
	now := time.Unix(0, 0)
	c := newMACCache(f, time.Minute)
	c.now = func() time.Time { return now }

	if _, err := c.get("10.0.0.1"); err != nil {
		t.Fatalf("first get: %v", err)
	}
	now = now.Add(30 * time.Second) // within TTL
	if _, err := c.get("10.0.0.1"); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if f.calls != 1 {
		t.Errorf("resolver called %d times, want 1 (cache hit expected)", f.calls)
	}
}

func TestMACCacheExpiry(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	f := &fakeResolver{mac: mac}
	now := time.Unix(0, 0)
	c := newMACCache(f, time.Minute)
	c.now = func() time.Time { return now }

	if _, err := c.get("10.0.0.1"); err != nil {
		t.Fatalf("first get: %v", err)
	}
	now = now.Add(2 * time.Minute) // past TTL
	if _, err := c.get("10.0.0.1"); err != nil {
		t.Fatalf("second get: %v", err)
	}
	if f.calls != 2 {
		t.Errorf("resolver called %d times, want 2 (expiry expected)", f.calls)
	}
}
