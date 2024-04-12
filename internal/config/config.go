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
	MetricsPort    int
	HealthPort     int
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
