//go:build !linux

// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package receiver

import (
	"context"
	"errors"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
)

type Controller struct {
	Log logr.Logger
	Cfg *config.Config
}

func (rc *Controller) Start(_ context.Context) error {
	return errors.New("UDP receiver with TUN injection is only supported on Linux")
}
