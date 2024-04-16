// Copyright 2024 SAP SE
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

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
