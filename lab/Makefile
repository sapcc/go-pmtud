# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

.PHONY: pmtu-up down deploy test status \
        observe-router observe-node observe-replication \
        generate-traffic verify-pmtu test-backends \
        e2e e2e-reuse e2e-keep

SCRIPTS := scripts
GINKGO_FLAGS ?=

## Full lab lifecycle

pmtu-up: ## Create networks, clusters, router, and configure routes
	@$(SCRIPTS)/setup-networks.sh
	@$(SCRIPTS)/setup-clusters.sh
	@$(SCRIPTS)/setup-router.sh
	@$(SCRIPTS)/setup-routes.sh

down: ## Remove all lab resources (clusters, networks, router)
	@echo "Tearing down lab..."
	@kind delete cluster --name pmtud-cluster-a 2>/dev/null || true
	@kind delete cluster --name pmtud-cluster-b 2>/dev/null || true
	@docker rm -f pmtud-router 2>/dev/null || true
	@$(SCRIPTS)/teardown-networks.sh

deploy: deploy-pmtud deploy-workload ## Deploy go-pmtud and test workload

deploy-pmtud: ## Build and deploy go-pmtud to both clusters
	@$(SCRIPTS)/deploy-pmtud.sh

deploy-workload: ## Deploy podinfo test workload to cluster-b
	@$(SCRIPTS)/deploy-workload.sh

## Testing

test: ## Run end-to-end PMTU replication test
	@$(SCRIPTS)/test-e2e.sh

test-backends: ## Test both UDP and CRD relay backends
	@$(SCRIPTS)/test-relay-backends.sh

generate-traffic: ## Generate cross-cluster traffic to trigger PMTU discovery
	@$(SCRIPTS)/generate-traffic.sh

verify-pmtu: ## Verify PMTU cache on cluster-a workers
	@$(SCRIPTS)/verify-pmtu.sh

## Observation

observe-router: ## tcpdump ICMP frag-needed on router
	@$(SCRIPTS)/observe.sh router

observe-node: ## tcpdump ICMP frag-needed on cluster node (CLUSTER=a|b NODE=worker|worker2)
	@$(SCRIPTS)/observe.sh node

observe-replication: ## tcpdump UDP 4390 replication traffic (CLUSTER=a|b NODE=worker|worker2)
	@$(SCRIPTS)/observe.sh replication

## Status

status: ## Show lab component status
	@$(SCRIPTS)/status.sh

## Go e2e suite

e2e: ## Run Go e2e suite (provisions, tests udp+crd, tears down)
	go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-reuse: ## Run e2e without reprovisioning
	LAB_REUSE=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-keep: ## Run e2e but keep lab after (for manual inspection)
	LAB_KEEP=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
