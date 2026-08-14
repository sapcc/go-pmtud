//go:build !linux

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import "fmt"

const (
	TUNDeviceName = "pmtud0"
	ifnamsiz      = 16
	maxPacketSize = 1500
)

// Injector provides packet injection via TUN device
type Injector struct{}

// newInjector creates a new TUN injector (stub on non-linux platforms)
func newInjector(name string) (*Injector, error) {
	return nil, fmt.Errorf("TUN injection is only supported on Linux")
}

// Inject writes payload to the TUN device (stub)
func (inj *Injector) Inject(payload []byte) error {
	return fmt.Errorf("TUN injection is only supported on Linux")
}

// Close closes the TUN device (stub)
func (inj *Injector) Close() error {
	return nil
}
