// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"

	"github.com/go-logr/logr"
)

// Runnable manages the TUN device and backend relay Start lifecycle
type Runnable struct {
	Backend Relay
	Log     logr.Logger
}

// Start creates the TUN injector, configures it, logs the required NFLOG warning,
// and starts the backend relay with the injector.
func (r *Runnable) Start(ctx context.Context) error {
	log := r.Log

	// Create TUN injector
	inj, err := newInjector(TUNDeviceName)
	if err != nil {
		log.Error(err, "creating TUN injector")
		return err
	}
	defer inj.Close()

	log.Info("TUN device created", "name", TUNDeviceName)
	log.Info("IMPORTANT: Ensure iptables NFLOG rule excludes TUN interface to prevent loops",
		"required_rule", "iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i "+TUNDeviceName+" -j NFLOG --nflog-group <group>")

	// Start backend relay with injection function
	return r.Backend.Start(ctx, inj.Inject)
}
