#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAB_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$LAB_DIR")"

IMAGE_NAME="go-pmtud:local"
RELAY_BACKEND="${RELAY_BACKEND:-udp}"

echo "Building go-pmtud image..."
docker build -t "$IMAGE_NAME" "$REPO_ROOT"

echo "Loading image into pmtud-cluster-a..."
kind load docker-image "$IMAGE_NAME" --name "pmtud-cluster-a"

echo "Loading image into pmtud-cluster-b..."
kind load docker-image "$IMAGE_NAME" --name "pmtud-cluster-b"

# Deploy CRD if using CRD backend
if [ "$RELAY_BACKEND" = "crd" ]; then
	echo "Deploying PMTUNodeRelay CRD to pmtud-cluster-a..."
	kubectl --context "kind-pmtud-cluster-a" apply -f "$REPO_ROOT/crd/pmtud.cloud.sap_pmtunoderelays.yaml"

	echo "Deploying PMTUNodeRelay CRD to pmtud-cluster-b..."
	kubectl --context "kind-pmtud-cluster-b" apply -f "$REPO_ROOT/crd/pmtud.cloud.sap_pmtunoderelays.yaml"
fi

echo "Deploying RBAC to pmtud-cluster-a..."
kubectl --context "kind-pmtud-cluster-a" -n kube-system apply -f "$LAB_DIR/manifests/rbac.yaml"

echo "Deploying RBAC to pmtud-cluster-b..."
kubectl --context "kind-pmtud-cluster-b" -n kube-system apply -f "$LAB_DIR/manifests/rbac.yaml"

echo "Deploying DaemonSet to pmtud-cluster-a (relay backend: $RELAY_BACKEND)..."
kubectl --context "kind-pmtud-cluster-a" -n kube-system apply -f "$LAB_DIR/manifests/pmtud-daemonset.yaml"

echo "Deploying DaemonSet to pmtud-cluster-b (relay backend: $RELAY_BACKEND)..."
kubectl --context "kind-pmtud-cluster-b" -n kube-system apply -f "$LAB_DIR/manifests/pmtud-daemonset.yaml"

echo "Waiting for go-pmtud pods to be Ready in cluster-a..."
kubectl --context "kind-pmtud-cluster-a" -n kube-system rollout status daemonset/go-pmtud --timeout=120s

echo "Waiting for go-pmtud pods to be Ready in cluster-b..."
kubectl --context "kind-pmtud-cluster-b" -n kube-system rollout status daemonset/go-pmtud --timeout=120s

echo "go-pmtud deployed and ready in both clusters (backend: $RELAY_BACKEND)"
