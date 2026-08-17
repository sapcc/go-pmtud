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
				// flush route caches
				for _, w := range testLab.ClusterA.Workers {
					gomega.Expect(testLab.FlushRouteCache(w)).To(gomega.Succeed())
				}

				// start ICMP capture on router
				icmp := testLab.CaptureICMPAsync(ctx, 15*time.Second)

				// generate traffic
				gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

				// wait for ICMP
				gomega.Eventually(func() int { return icmp.Count }).
					WithTimeout(20 * time.Second).
					Should(gomega.BeNumerically(">", 0),
						"router must generate ICMP frag-needed")

				// wait for PMTU convergence on all workers
				for _, w := range testLab.ClusterA.Workers {
					gomega.Eventually(func() (int, error) {
						return testLab.PMTUTo(w, testLab.DestIP)
					}).
						WithTimeout(30 * time.Second).
						WithPolling(2 * time.Second).
						Should(gomega.Equal(1500),
							"worker %s PMTU must converge to 1500 via %s relay", w, backend)
				}
			})
		})
	}
})
