# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

.PHONY: e2e e2e-reuse e2e-keep \
        observe-router observe-node observe-replication \
        down status help

SCRIPTS := scripts
GINKGO_FLAGS ?=

## Go e2e test suite

observe-router: ## tcpdump ICMP frag-needed on router
	@$(SCRIPTS)/observe.sh router

observe-node: ## tcpdump ICMP frag-needed on cluster node (CLUSTER=a|b NODE=worker|worker2)
	@$(SCRIPTS)/observe.sh node

observe-replication: ## tcpdump UDP 4390 replication traffic (CLUSTER=a|b NODE=worker|worker2)
	@$(SCRIPTS)/observe.sh replication

## Status

status: ## Show lab component status
	@$(SCRIPTS)/status.sh

## Cleanup

down: ## Remove lab (clusters, networks, router)
	kind delete cluster --name pmtud-cluster-a 2>/dev/null || true
	kind delete cluster --name pmtud-cluster-b 2>/dev/null || true
	docker rm -f pmtud-router 2>/dev/null || true
	docker network rm pmtud-net-a pmtud-net-b pmtud-transit 2>/dev/null || true

## Go e2e suite

e2e: ## Run Go e2e suite (provisions, tests udp+crd, tears down)
	cd .. && REPO_ROOT=$$(pwd) go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-reuse: ## Run e2e without reprovisioning
	cd .. && REPO_ROOT=$$(pwd) LAB_REUSE=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-keep: ## Run e2e but keep lab after (for manual inspection)
	cd .. && REPO_ROOT=$$(pwd) LAB_KEEP=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
