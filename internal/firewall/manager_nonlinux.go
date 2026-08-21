// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build !linux
// +build !linux

package firewall

import (
	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

// Manager owns the host-level firewall state required by go-pmtud.
// On non-Linux platforms, this is a no-op stub.
type Manager struct {
	cfg *config.Config
	log logr.Logger
}

// New returns a Manager.
func New(cfg *config.Config, log logr.Logger) *Manager {
	return &Manager{cfg: cfg, log: log}
}

// Setup is a no-op on non-Linux platforms.
func (m *Manager) Setup() error {
	return nil
}

// Teardown is a no-op on non-Linux platforms.
func (m *Manager) Teardown() error {
	return nil
}
