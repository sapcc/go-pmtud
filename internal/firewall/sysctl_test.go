// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSysctl(t *testing.T) {
	root := t.TempDir()
	path := "net/ipv4/conf/all/rp_filter"
	full := filepath.Join(root, filepath.FromSlash(path))

	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeSysctl(root, path, 0); err != nil {
		t.Fatalf("writeSysctl: %v", err)
	}

	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0" {
		t.Errorf("got %q, want %q", string(got), "0")
	}
}
