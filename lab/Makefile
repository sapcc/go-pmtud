# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

.PHONY: pmtu-up down deploy test status \
        observe-router observe-node observe-replication \
        generate-traffic verify-pmtu

SCRIPTS := scripts

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

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
