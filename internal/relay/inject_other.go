//go:build !linux

// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import "errors"

const (
	TUNDeviceName = "pmtud0"
	ifnamsiz      = 16
	maxPacketSize = 1500
)

// Injector provides packet injection via TUN device
type Injector struct{}

// newInjector creates a new TUN injector (stub on non-linux platforms)
func newInjector(name string) (*Injector, error) {
	return nil, errors.New("not supported")
}

// Inject writes payload to the TUN device (stub)
func (inj *Injector) Inject(payload []byte) error {
	return errors.New("not supported")
}

// Close closes the TUN device (stub)
func (inj *Injector) Close() error {
	return nil
}
