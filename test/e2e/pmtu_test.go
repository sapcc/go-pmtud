// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func pmtuSpecs(backend string) {
	ginkgo.It("replicates PMTU to peer nodes", func(ctx ginkgo.SpecContext) {
		workers := testLab.Cluster.Workers
		originator := workers[0]

		gomega.Expect(testLab.FlushRouteCache(originator)).To(gomega.Succeed())

		// Snapshot the per-backend peer-delivery baseline. UDP verifies peer
		// ingest directly (injected via TUN). L2/legacy have no receive-side
		// counter, so we verify the originator relayed to each peer
		// (sent_packets_peer{peer=<peerIP>}); capture (eth1) != replication
		// (eth0) means no self-recapture storm.
		base := make(map[string]int, len(workers))
		for _, w := range workers[1:] {
			var (
				n   int
				err error
			)
			if backend == "udp" {
				n, err = testLab.InjectedPackets(w)
			} else {
				n, err = testLab.SentPacketsPeer(originator, w)
			}
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			base[w] = n
		}

		gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

		// originator converges natively (real kernel route-cache apply)
		gomega.Eventually(func() (int, error) {
			return testLab.PMTUTo(originator, testLab.BlackholeIP)
		}).
			WithTimeout(30 * time.Second).
			WithPolling(2 * time.Second).
			Should(gomega.Equal(1280),
				"originator %s must converge natively to 1280 (%s)", originator, backend)

		// peers: relay delivered
		for _, w := range workers[1:] {
			if backend == "udp" {
				gomega.Eventually(func() (int, error) {
					return testLab.InjectedPackets(w)
				}).
					WithTimeout(30 * time.Second).
					WithPolling(2 * time.Second).
					Should(gomega.BeNumerically(">", base[w]),
						"peer %s must inject a relayed frag-needed (udp)", w)
			} else {
				gomega.Eventually(func() (int, error) {
					return testLab.SentPacketsPeer(originator, w)
				}).
					WithTimeout(30 * time.Second).
					WithPolling(2 * time.Second).
					Should(gomega.BeNumerically(">", base[w]),
						"originator must relay a frag-needed to peer %s (%s)", w, backend)
			}
		}
	})
}
