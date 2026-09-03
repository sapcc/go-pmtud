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

		// Snapshot peer counters before traffic. UDP tracks injected packets on
		// the peer; L2 has no receive-side counter so we use sent_packets on the
		// peer (the peer relays back in Kind's single-interface topology, which
		// proves the frame was delivered and processed end-to-end).
		base := make(map[string]int, len(workers))
		for _, w := range workers[1:] {
			var (
				n   int
				err error
			)
			if backend == "udp" {
				n, err = testLab.InjectedPackets(w)
			} else {
				n, err = testLab.SentPackets(w)
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

		// peers: replication delivered
		for _, w := range workers[1:] {
			if backend == "udp" {
				gomega.Eventually(func() (int, error) {
					return testLab.InjectedPackets(w)
				}).
					WithTimeout(30 * time.Second).
					WithPolling(2 * time.Second).
					Should(gomega.BeNumerically(">", base[w]),
						"peer %s must receive a relayed frag-needed via %s", w, backend)
			} else {
				// L2: peer received the frame natively (NFLOG fired), processed it,
				// and forwarded — SentPackets increments proves end-to-end delivery.
				gomega.Eventually(func() (int, error) {
					return testLab.SentPackets(w)
				}).
					WithTimeout(30 * time.Second).
					WithPolling(2 * time.Second).
					Should(gomega.BeNumerically(">", base[w]),
						"peer %s must forward a relayed frag-needed via %s", w, backend)
			}
		}
	})
}
