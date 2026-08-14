// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"testing"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

type mockCache struct{}

func (m *mockCache) Get(key string) (interface{}, bool) {
	return nil, false
}

func (m *mockCache) Set(key string, value interface{}) {
}

func TestNewUnknownBackend(t *testing.T) {
	deps := Deps{
		Cfg:    &config.Config{},
		Log:    logr.Discard(),
		Client: nil,
		Cache:  &mockCache{},
	}
	_, err := New("unknown", deps)
	if err == nil {
		t.Fatalf("expected error for unknown backend, got nil")
	}
}
