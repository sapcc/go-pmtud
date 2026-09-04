// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import "testing"

func TestBackendSet(t *testing.T) {
	cases := []struct {
		in      string
		want    Backend
		wantErr bool
	}{
		{"l2", BackendL2, false},
		{"udp", BackendUDP, false},
		{"tcp", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		var b Backend
		err := b.Set(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("Set(%q): expected error, got nil", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("Set(%q): unexpected error %v", c.in, err)
		}
		if b != c.want {
			t.Errorf("Set(%q) = %q, want %q", c.in, b, c.want)
		}
	}
}
