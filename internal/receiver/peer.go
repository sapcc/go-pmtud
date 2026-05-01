// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package receiver

import "net"

func (rc *Controller) isKnownPeer(ip net.IP) bool {
	rc.Cfg.PeerMutex.Lock()
	defer rc.Cfg.PeerMutex.Unlock()
	for _, peerIP := range rc.Cfg.PeerList {
		if ip.Equal(net.ParseIP(peerIP)) {
			return true
		}
	}
	return false
}
