// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build linux

package firewall

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeSysctl writes value to fsRoot/path (a /proc/sys-style path, forward-slash separated).
// fsRoot is "/" in production; injectable for tests.
func writeSysctl(fsRoot, path string, value int) error {
	full := filepath.Join(fsRoot, filepath.FromSlash(path))
	return os.WriteFile(full, fmt.Appendf(nil, "%d", value), 0644)
}
