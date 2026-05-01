#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LAB_DIR="$(dirname "$SCRIPT_DIR")"

CONTEXT="kind-pmtud-cluster-b"

echo "Deploying podinfo to cluster-b..."
# Apply namespace first, then resources (avoids namespace mismatch with kubectl wrappers)
kubectl --context "$CONTEXT" apply -f - <<'EOF'
apiVersion: v1
kind: Namespace
metadata:
  name: podinfo
EOF
kubectl --context "$CONTEXT" -n podinfo apply -f "$LAB_DIR/manifests/podinfo.yaml"

echo "Waiting for podinfo deployment to be ready..."
kubectl --context "$CONTEXT" -n podinfo rollout status deployment/podinfo --timeout=120s

# Generate a 1MB test file inside the podinfo pod for large downloads
echo "Generating 1MB test file in podinfo pod..."
POD=$(kubectl --context "$CONTEXT" -n podinfo get pods -l app=podinfo -o jsonpath='{.items[0].metadata.name}')
kubectl --context "$CONTEXT" -n podinfo exec "$POD" -- sh -c 'dd if=/dev/urandom of=/tmp/testfile bs=1024 count=1024 2>/dev/null && cp /tmp/testfile /home/app/testfile'

echo "Podinfo deployed and test file ready"
echo "  Service: podinfo.podinfo:9898 (NodePort 30080)"
echo "  Test file: curl http://<cluster-b-node-ip>:30080/testfile"
