// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("PMTU replication", func() {
	for _, backend := range []string{"udp", "crd"} {
		ginkgo.Context(backend, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
				gomega.Expect(testLab.DeployBackend(ctx, backend)).To(gomega.Succeed())
			})

			ginkgo.It("replicates PMTU to peer nodes", func(ctx ginkgo.SpecContext) {
				workers := testLab.Cluster.Workers
				originator := workers[0]

				gomega.Expect(testLab.FlushRouteCache(originator)).To(gomega.Succeed())

				// baseline peer recv counters before generating traffic
				base := make(map[string]int, len(workers))
				for _, w := range workers[1:] {
					n, err := testLab.RecvPackets(w)
					gomega.Expect(err).NotTo(gomega.HaveOccurred())
					base[w] = n
				}

				// ping errors unless the hop returned a frag-needed
				gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

				// originator converges natively (real kernel PMTU apply)
				gomega.Eventually(func() (int, error) {
					return testLab.PMTUTo(originator, testLab.BlackholeIP)
				}).
					WithTimeout(30 * time.Second).
					WithPolling(2 * time.Second).
					Should(gomega.Equal(1280),
						"originator %s must converge natively to 1280 (%s)", originator, backend)

				// peers: replication delivered — the daemon received and injected
				// the relayed frag-needed, so recv_packets_total strictly increases.
				for _, w := range workers[1:] {
					w := w
					gomega.Eventually(func() (int, error) {
						return testLab.RecvPackets(w)
					}).
						WithTimeout(30 * time.Second).
						WithPolling(2 * time.Second).
						Should(gomega.BeNumerically(">", base[w]),
							"peer %s must receive a relayed frag-needed via %s", w, backend)
				}
			})
		})
	}
})
