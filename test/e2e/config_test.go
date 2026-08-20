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

		found := false
		for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
			if arg == "--relay-backend="+backend {
				found = true
				break
			}
		}
		gomega.Expect(found).To(gomega.BeTrue(), "daemonset must have --relay-backend="+backend)

		hasNS := false
		for _, e := range ds.Spec.Template.Spec.Containers[0].Env {
			if e.Name == "POD_NAMESPACE" {
				hasNS = true
				break
			}
		}
		gomega.Expect(hasNS).To(gomega.BeTrue(), "daemonset must have POD_NAMESPACE env")

		if backend == "crd" {
			// TODO: check CRD exists
		}
	})
}
