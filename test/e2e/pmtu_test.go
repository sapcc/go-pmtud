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
				for _, w := range testLab.Cluster.Workers {
					gomega.Expect(testLab.FlushRouteCache(w)).To(gomega.Succeed())
				}

				// ping errors unless the hop returned a frag-needed
				gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

				// originator converges natively; peers converge via the relay
				for _, w := range testLab.Cluster.Workers {
					gomega.Eventually(func() (int, error) {
						return testLab.PMTUTo(w, testLab.BlackholeIP)
					}).
						WithTimeout(30 * time.Second).
						WithPolling(2 * time.Second).
						Should(gomega.Equal(1280),
							"worker %s PMTU must converge to 1280 via %s relay", w, backend)
				}
			})
		})
	}
})
