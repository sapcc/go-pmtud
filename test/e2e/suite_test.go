// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sapcc/go-pmtud/lab"
)

var _ = ginkgo.Describe("go-pmtud", func() {
	for _, backend := range []string{"l2", "udp"} {
		ginkgo.Context(backend, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
				gomega.Expect(testLab.DeployBackend(ctx, backend)).To(gomega.Succeed())
			})
			configSpecs(backend)
			pmtuSpecs(backend)
		})
	}
})

var testLab *lab.Lab

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "go-pmtud e2e")
}

var _ = ginkgo.BeforeSuite(func(ctx ginkgo.SpecContext) {
	var err error
	testLab, err = lab.Provision(ctx)

	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func(ctx ginkgo.SpecContext) {
	if testLab != nil {
		gomega.Expect(testLab.Teardown(ctx)).To(gomega.Succeed())
	}
})
