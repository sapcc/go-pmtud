// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/nftables"

	"github.com/sapcc/go-pmtud/internal/config"
)

// Manager owns the host-level firewall state required by go-pmtud:
// rp_filter=0 on the relevant interfaces, and the NFLOG nftables rule.
type Manager struct {
	cfg    *config.Config
	log    logr.Logger
	fsRoot string // injectable for tests; "/" in production
}

// New returns a Manager. In production pass cfg and log; fsRoot is set to "/".
func New(cfg *config.Config, log logr.Logger) *Manager {
	return &Manager{cfg: cfg, log: log, fsRoot: "/"}
}

// Setup sets rp_filter=0 and installs the NFLOG nftables rule.
// Must be called after cfg.DefaultInterface and cfg.InterfaceNames are populated
// (i.e. after preRunRootCmd).
func (m *Manager) Setup() error {
	if err := m.setupSysctl(); err != nil {
		return fmt.Errorf("firewall sysctl setup: %w", err)
	}
	if err := m.setupNFT(); err != nil {
		return fmt.Errorf("firewall nft setup: %w", err)
	}
	m.log.Info("firewall ready", "iifname", m.cfg.DefaultInterface, "nflog_group", m.cfg.NfGroup)
	return nil
}

// Teardown deletes the pmtud nftables table. rp_filter is not restored.
// Safe to call even if Setup was never called or failed partway through.
func (m *Manager) Teardown() error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("firewall teardown: nftables.New: %w", err)
	}
	table, _, _ := buildNFTObjects(m.cfg.DefaultInterface, m.cfg.NfGroup)
	conn.DelTable(table)
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("firewall teardown: flush: %w", err)
	}
	m.log.Info("firewall torn down")
	return nil
}

func (m *Manager) setupSysctl() error {
	paths := []string{"net/ipv4/conf/all/rp_filter"}
	for _, iface := range m.cfg.InterfaceNames {
		paths = append(paths, fmt.Sprintf("net/ipv4/conf/%s/rp_filter", iface))
	}
	for _, p := range paths {
		m.log.Info("setting sysctl", "path", p, "value", 0)
		if err := writeSysctl(m.fsRoot, p, 0); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

func (m *Manager) setupNFT() error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("nftables.New: %w", err)
	}
	table, chain, rule := buildNFTObjects(m.cfg.DefaultInterface, m.cfg.NfGroup)
	// Delete any existing pmtud table first to make Setup idempotent across restarts.
	conn.DelTable(table)
	if err := conn.Flush(); err != nil {
		// Ignore "no such table" — it just means we're starting fresh.
		m.log.V(1).Info("pre-cleanup flush (ignore if table didn't exist)", "err", err)
	}
	// Fresh connection after the delete flush.
	conn, err = nftables.New()
	if err != nil {
		return fmt.Errorf("nftables.New (post-cleanup): %w", err)
	}
	conn.AddTable(table)
	conn.AddChain(chain)
	conn.AddRule(rule)
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}
