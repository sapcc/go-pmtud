# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

.PHONY: integration integration-keep \
        down help

GINKGO_FLAGS ?=

## Cleanup

down: ## Remove lab (single kind cluster + transit network)
	kind delete cluster --name pmtud 2>/dev/null || true
	docker network rm pmtud-transit 2>/dev/null || true

## Go integration suite

integration: ## Run Go integration suite (provisions, tests legacy, l2, udp, tears down)
	cd .. && REPO_ROOT=$$(pwd) go test -tags integration -timeout 20m ./test/integration/... $(GINKGO_FLAGS)

integration-keep: ## Run integration suite but keep lab after (for manual inspection)
	cd .. && REPO_ROOT=$$(pwd) LAB_KEEP=1 go test -tags integration -timeout 20m ./test/integration/... $(GINKGO_FLAGS)

## Help

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'
