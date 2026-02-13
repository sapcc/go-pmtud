// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package config

import (
	"sync"
	"time"
)

type PeerEntry struct {
	LastUpdated time.Time
	Mac         string
}

type Config struct {
	// Peers []string
	InterfaceNames []string
	NodeName       string
	MetricsPort    string
	HealthPort     string
	TimeToLive     int
	NfGroup        uint16
	KubeContext    string

	ReplicationInterface     string
	DefaultInterface         string
	InterfaceMtu             int
	PeerMutex                sync.Mutex
	PeerList                 map[string]PeerEntry
	ArpCacheTimeoutMinutes   int
	ArpRequestTimeoutSeconds int

	RandDelay int
}
