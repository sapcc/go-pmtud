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

		base := make(map[string]int, len(workers))
		for _, w := range workers[1:] {
			n, err := testLab.RecvPackets(w)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			base[w] = n
		}

		gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

		gomega.Eventually(func() (int, error) {
			return testLab.PMTUTo(originator, testLab.BlackholeIP)
		}).
			WithTimeout(30 * time.Second).
			WithPolling(2 * time.Second).
			Should(gomega.Equal(1280),
				"originator %s must converge natively to 1280 (%s)", originator, backend)

		for _, w := range workers[1:] {
			gomega.Eventually(func() (int, error) {
				return testLab.RecvPackets(w)
			}).
				WithTimeout(30 * time.Second).
				WithPolling(2 * time.Second).
				Should(gomega.BeNumerically(">", base[w]),
					"peer %s must receive a relayed frag-needed via %s", w, backend)
		}
	})
}
