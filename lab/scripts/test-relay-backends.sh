#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0
# Test both UDP and CRD relay backends in Kind clusters

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAB_DIR="$(dirname "$SCRIPT_DIR")"

test_backend() {
	local backend=$1
	local test_name="relay-backend-$backend"
	
	echo "================================"
	echo "Testing $backend backend..."
	echo "================================"
	
	# Deploy with the specified backend
	RELAY_BACKEND="$backend" bash "$SCRIPT_DIR/deploy-pmtud.sh"
	
	# Verify pods are running
	echo "Verifying go-pmtud pods in cluster-a..."
	kubectl --context "kind-pmtud-cluster-a" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud
	local pod_count_a=$(kubectl --context "kind-pmtud-cluster-a" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud --no-headers 2>/dev/null | wc -l)
	if [ "$pod_count_a" -lt 1 ]; then
		echo "ERROR: No go-pmtud pods found in cluster-a"
		return 1
	fi
	
	echo "Verifying go-pmtud pods in cluster-b..."
	kubectl --context "kind-pmtud-cluster-b" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud
	local pod_count_b=$(kubectl --context "kind-pmtud-cluster-b" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud --no-headers 2>/dev/null | wc -l)
	if [ "$pod_count_b" -lt 1 ]; then
		echo "ERROR: No go-pmtud pods found in cluster-b"
		return 1
	fi
	
	# Verify backend is set correctly
	echo "Verifying backend configuration..."
	local pod_a=$(kubectl --context "kind-pmtud-cluster-a" -n kube-system get pods -l app.kubernetes.io/name=go-pmtud -o jsonpath='{.items[0].metadata.name}')
	local backend_config=$(kubectl --context "kind-pmtud-cluster-a" -n kube-system get pod "$pod_a" -o jsonpath='{.spec.containers[0].args}' | grep -o 'relay-backend=[^ ]*' || echo "not-found")
	
	if [[ "$backend_config" == "relay-backend=$backend" ]]; then
		echo "✓ Backend correctly set to: $backend"
	else
		echo "ERROR: Backend not correctly set. Found: $backend_config"
		return 1
	fi
	
	# If CRD backend, verify CRD is installed
	if [ "$backend" = "crd" ]; then
		echo "Verifying PMTUNodeRelay CRD installation..."
		if kubectl --context "kind-pmtud-cluster-a" get crd pmtunoderelays.pmtud.cloud.sap &>/dev/null; then
			echo "✓ PMTUNodeRelay CRD found in cluster-a"
		else
			echo "ERROR: PMTUNodeRelay CRD not found in cluster-a"
			return 1
		fi
		
		if kubectl --context "kind-pmtud-cluster-b" get crd pmtunoderelays.pmtud.cloud.sap &>/dev/null; then
			echo "✓ PMTUNodeRelay CRD found in cluster-b"
		else
			echo "ERROR: PMTUNodeRelay CRD not found in cluster-b"
			return 1
		fi
	fi
	
	# Verify POD_NAMESPACE env var is set
	echo "Verifying POD_NAMESPACE environment variable..."
	local pod_namespace=$(kubectl --context "kind-pmtud-cluster-a" -n kube-system get pod "$pod_a" -o jsonpath='{.spec.containers[0].env[?(@.name=="POD_NAMESPACE")]}')
	if [ -n "$pod_namespace" ]; then
		echo "✓ POD_NAMESPACE env var is configured"
	else
		echo "ERROR: POD_NAMESPACE env var not found in pod spec"
		return 1
	fi
	
	echo "✓ $backend backend test passed!"
	echo ""
}

main() {
	# Ensure clusters are running
	if ! kubectl --context "kind-pmtud-cluster-a" cluster-info &>/dev/null; then
		echo "ERROR: kind-pmtud-cluster-a is not running"
		echo "Run: make -C lab pmtu-up"
		return 1
	fi
	
	if ! kubectl --context "kind-pmtud-cluster-b" cluster-info &>/dev/null; then
		echo "ERROR: kind-pmtud-cluster-b is not running"
		echo "Run: make -C lab pmtu-up"
		return 1
	fi
	
	# Test both backends
	test_backend "udp" || return 1
	test_backend "crd" || return 1
	
	echo "================================"
	echo "All relay backend tests passed!"
	echo "================================"
}

main "$@"
