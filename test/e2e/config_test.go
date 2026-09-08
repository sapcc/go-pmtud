// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package e2e

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func configSpecs(backend string) {
	ginkgo.It("deploys with correct config", func(ctx ginkgo.SpecContext) {
		var ds appsv1.DaemonSet
		gomega.Expect(testLab.Cluster.Client.Get(ctx,
			client.ObjectKey{Namespace: "kube-system", Name: "go-pmtud"}, &ds)).
			To(gomega.Succeed())

		args := ds.Spec.Template.Spec.Containers[0].Args

		if backend == "legacy" {
			// Old manifests have no --relay-backend flag at all; the daemon must
			// default to l2 without it being present.
			for _, arg := range args {
				gomega.Expect(arg).NotTo(gomega.HavePrefix("--relay-backend="),
					"legacy manifest must not carry --relay-backend (daemon should default to l2)")
			}
			return
		}

		found := false
		for _, arg := range args {
			if arg == "--relay-backend="+backend {
				found = true
				break
			}
		}
		gomega.Expect(found).To(gomega.BeTrue(), "daemonset must have --relay-backend="+backend)
	})
}
