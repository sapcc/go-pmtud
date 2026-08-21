<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Contributing

## Requirements

- Go 1.26+
- [controller-gen](https://pkg.go.dev/sigs.k8s.io/controller-tools/cmd/controller-gen) — for regenerating CRD manifests and deepcopy code
- [setup-envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest) — for running component tests against a real API server binary

`make prepare-static-check` installs all static analysis tools. `make install-setup-envtest` installs setup-envtest.

## Running tests

```sh
make check
```

This runs `generate`, all unit and component tests (with coverage), and the full static analysis suite.

### Unit tests only

```sh
go test ./...
```

### Component tests (envtest)

Component tests verify a single component against a real dependency spun up in-process. The CRD backend tests (`internal/relay/crd`) use [envtest](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/envtest), which starts a real Kubernetes API server binary but requires no cluster. `make check` handles this automatically via `setup-envtest`. To run them in isolation:

```sh
KUBEBUILDER_ASSETS=$(setup-envtest use 1.35 -p path) go test ./internal/relay/crd/...
```

If `KUBEBUILDER_ASSETS` is not set, the test binary prints a notice and exits cleanly (not a failure).

### Integration tests (kind)

Integration tests verify that go-pmtud works correctly inside a real multi-node cluster, including actual NFLOG capture, TUN injection, and CRD relay across nodes. They are not yet automated. To test manually with [kind](https://kind.sigs.k8s.io/):

1. Create a multi-node cluster: `kind create cluster --config kind-config.yaml` (use a config with at least two worker nodes)
2. Install the CRD: `kubectl apply -f crd/`
3. Build and load the image: `docker build -t go-pmtud . && kind load docker-image go-pmtud`
4. Deploy as a DaemonSet with `--relay-backend=crd` and the appropriate iptables init container
5. Trigger an ICMP frag-needed packet (e.g. send a large UDP packet with DF set toward a destination with a lower MTU path)
6. Verify the packet appears on other nodes via `kubectl logs` or by inspecting the route cache MTU entry on the target node

### End-to-end tests (real cluster)

End-to-end testing in a real cluster (production or staging) is performed manually before releases. It follows the same steps as the kind integration test but validates behaviour under real ECMP traffic patterns. No scripted harness exists yet.

To confirm a relay is working, check the route cache on a node that should have received the relayed packet:

```sh
ip route get <destination-IP>
# Expected: cache entry with a reduced mtu value
```

## Code generation

CRD manifests, deepcopy functions, and apply-configuration types are generated from the types in `api/v1alpha1`:

```sh
make generate
```

Run this after any change to the API types and commit the generated files.

## Static analysis

```sh
make static-check
```

Runs golangci-lint, shellcheck, typos, and license-header checks. Individual targets:

| Target | Tool |
|---|---|
| `run-golangci-lint` | golangci-lint |
| `run-shellcheck` | shellcheck |
| `run-typos` | typos spell checker |
| `check-license-headers` | addlicense + reuse |
| `check-dependency-licenses` | go-licence-detector |

## License headers

All `.go` files require an SPDX license header. To add or fix headers:

```sh
make license-headers
```
