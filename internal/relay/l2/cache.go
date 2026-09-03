// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package l2

import (
	"net"
	"sync"
	"time"
)

// resolver resolves a peer IP to a MAC address (satisfied by *arp.Resolver).
type resolver interface {
	Resolve(ip string) (net.HardwareAddr, error)
}

type macEntry struct {
	mac     net.HardwareAddr
	expires time.Time
}

// macCache caches IP→MAC resolutions with a TTL.
type macCache struct {
	mu      sync.Mutex
	entries map[string]macEntry
	ttl     time.Duration
	res     resolver
	now     func() time.Time
}

func newMACCache(res resolver, ttl time.Duration) *macCache {
	return &macCache{
		entries: make(map[string]macEntry),
		ttl:     ttl,
		res:     res,
		now:     time.Now,
	}
}

// get returns the cached MAC for ip, resolving (and caching) it on a miss or
// after the TTL has elapsed.
func (c *macCache) get(ip string) (net.HardwareAddr, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if e, ok := c.entries[ip]; ok && c.now().Before(e.expires) {
		return e.mac, nil
	}
	mac, err := c.res.Resolve(ip)
	if err != nil {
		return nil, err
	}
	c.entries[ip] = macEntry{mac: mac, expires: c.now().Add(c.ttl)}
	return mac, nil
}
