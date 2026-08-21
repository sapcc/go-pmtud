<!-- SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Real-Cluster Validation Runbook

This runbook walks through manual validation of go-pmtud on a real Kubernetes cluster. Unlike the Kind-based lab, real clusters may already have MTU boundaries between zones or nodes; this procedure helps you trigger and observe go-pmtud's PMTU cache coherence in production-like settings.

## Prerequisites

- **Cluster:** ≥2 nodes (at least one worker; control-plane can be a worker too)
- **MTU boundary:** Either
  - Existing cross-node MTU mismatch (check with `ip link show` on each node), or
  - Permission to add a temporary low-MTU interface on one node for testing
- **Tooling:** 
  - `kubectl` access to the cluster
  - `ssh` or `kubectl debug` to reach node shells
  - Ability to exec into running pods
- **Build environment:** Docker/containerd, Go 1.22+, and network access to push the image to your registry

## Step 1: Build and Push Image

From the go-pmtud repo root:

```bash
# Build the image
docker build -t myregistry.azurecr.io/go-pmtud:test .

# Push to registry accessible from cluster
docker push myregistry.azurecr.io/go-pmtud:test
```

If your cluster is air-gapped or uses a private registry, ensure the image URI and any required pull secrets are in place before proceeding.

## Step 2: Deploy RBAC, CRD, and DaemonSet

### Apply RBAC and CRD

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: ServiceAccount
metadata:
  name: go-pmtud
  namespace: kube-system

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: go-pmtud
rules:
- apiGroups: [""]
  resources: ["nodes"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["pmtud.sap.com"]
  resources: ["pmtunoderelays"]
  verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]

---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: go-pmtud
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: go-pmtud
subjects:
- kind: ServiceAccount
  name: go-pmtud
  namespace: kube-system

---
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: pmtunoderelays.pmtud.sap.com
spec:
  group: pmtud.sap.com
  names:
    kind: PMTUNodeRelay
    plural: pmtunoderelays
  scope: Namespaced
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              packet:
                type: string
                description: Base64-encoded ICMP packet
              sourceNode:
                type: string
EOF
```

### Apply DaemonSet (CRD relay backend)

```bash
kubectl apply -f - <<'EOF'
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: go-pmtud
  namespace: kube-system
spec:
  selector:
    matchLabels:
      app: go-pmtud
  template:
    metadata:
      labels:
        app: go-pmtud
    spec:
      serviceAccountName: go-pmtud
      hostNetwork: true
      containers:
      - name: go-pmtud
        image: myregistry.azurecr.io/go-pmtud:test
        args: 
        - --relay-backend=crd
        - --relay-namespace=kube-system
        securityContext:
          privileged: true
        env:
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
EOF
```

For UDP backend, replace `--relay-backend=crd` with `--relay-backend=udp`.

### Wait for Rollout

```bash
kubectl rollout status daemonset/go-pmtud -n kube-system --timeout=5m
```

Verify pods are running:
```bash
kubectl get pods -n kube-system -l app=go-pmtud
```

## Step 3: Induce ICMP Fragmentation-Needed

### Option A: Existing MTU Boundary

If nodes already have asymmetric MTU (e.g., one node at MTU 1500, another at MTU 9000), skip to Step 4. Traffic crossing that boundary will naturally trigger ICMP fragmentation-needed.

### Option B: Create a Temporary Low-MTU Hop

On one worker node, create a temporary interface and route to simulate the lab topology:

**1. SSH into the node (or use `kubectl debug` to reach its host namespace):**
```bash
kubectl debug node/worker-1 -it --image=ubuntu
# Inside debug container, chroot to host
chroot /host
```

**2. Create dummy interface:**
```bash
sysctl -w net.ipv4.ip_forward=1
ip link add pmtudlab0 type dummy
ip link set pmtudlab0 mtu 1280 up
ip addr add 10.255.255.1/24 dev pmtudlab0
```

**3. On a second worker, route traffic to the dummy interface:**
```bash
# From worker-2 (or another node's debug container)
ip route add 10.255.255.0/24 via <worker-1-internal-ip>
```

**4. Generate large packet with DF bit set:**
```bash
# From any node or pod (with network tools):
ping -M do -s 1400 10.255.255.1
```

Expected result: ICMP "Fragmentation Needed" responses (ping shows `Frag needed` in error output).

## Step 4: Observe go-pmtud Behavior

### Check PMTU Cache on a Non-Originating Node

On a node that did *not* generate the ICMP but received it via relay:

```bash
# Shell into node (debug container + chroot, or ssh)
ip route get 10.255.255.1  # or the destination you pinged
# Should show reduced MTU (e.g., "mtu 1280")
```

### Check go-pmtud Logs

```bash
# View logs from the relay backend
kubectl logs -n kube-system -l app=go-pmtud --tail=50

# Look for messages like:
# "ICMP frag-needed received, resending packet."
# "PMTUNodeRelay created for ..."
```

### Inspect Relay Metrics (UDP backend)

If using UDP backend, check for packet counters (requires metrics scrape or manual inspection):

```bash
# Port forward metrics (if go-pmtud exposes them)
kubectl port-forward -n kube-system ds/go-pmtud 8080:8080 &
curl http://localhost:8080/metrics | grep -i relay
```

### Inspect CRD Objects (CRD backend)

```bash
# List active PMTUNodeRelay objects
kubectl get pmtunoderelays -A

# Inspect a specific relay (shows packet payload and source node)
kubectl describe pmtunoderelay <name> -n kube-system
```

Each relay object contains the captured ICMP packet and metadata about which node originated it. Objects are garbage-collected shortly after creation.

## Step 5: Verify End-to-End Coherence

Run a sustained large transfer between two pods on different nodes:

```bash
# Pod on node-1
kubectl run -it sender --image=ubuntu -- sh
apt-get update && apt-get install -y iperf3
iperf3 -c <receiver-pod-ip> -P 10 -t 30 -M 1400
```

During the transfer:
- **Node-1 (originator):** PMTU cache updated natively when ICMP arrives
- **Node-2 (peer):** PMTU cache updated via relay (UDP or CRD)

Both nodes should show reduced MTU for that destination:
```bash
ip route get <receiver-pod-ip>  # on both nodes → should show lower MTU
```

## Step 6: Cleanup

### Remove DaemonSet and RBAC

```bash
kubectl delete daemonset go-pmtud -n kube-system
kubectl delete serviceaccount go-pmtud -n kube-system
kubectl delete clusterrole go-pmtud
kubectl delete clusterrolebinding go-pmtud
```

### Remove CRD (only if not used elsewhere)

```bash
kubectl delete crd pmtunoderelays.pmtud.sap.com
```

### Remove Temporary Interfaces (if created in Step 3B)

On the worker where you added the dummy interface:

```bash
# Via debug container + chroot
ip link delete pmtudlab0
# Optional: reset ip_forward
sysctl -w net.ipv4.ip_forward=0
```

On the other worker, remove the temporary route:

```bash
ip route delete 10.255.255.0/24 via <worker-1-internal-ip>
```

## Troubleshooting

**Pods failing to start:** Check image registry access and pull secrets.
```bash
kubectl describe pod -n kube-system -l app=go-pmtud
```

**No ICMP packets observed:** Verify the MTU mismatch exists and traffic crosses it.
```bash
# Check MTU on each interface
ip link show
# Check if NFLOG is available
dmesg | grep -i nflog
```

**Relay objects not appearing:** Verify RBAC permissions and CRD is applied.
```bash
kubectl auth can-i create pmtunoderelays --as=system:serviceaccount:kube-system:go-pmtud
```

**Pods stuck in CrashLoopBackOff:** Check logs for configuration or capability errors.
```bash
kubectl logs -n kube-system <pod-name> --previous
```

## References

- [go-pmtud GitHub](https://github.com/sap/go-pmtud)
- [ICMP Fragmentation Needed (RFC 792, Type 3 Code 4)](https://tools.ietf.org/html/rfc792)
- [Kubernetes PMTU Discovery](https://kubernetes.io/docs/concepts/cluster-administration/networking/network-plugins/)
