<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Contributing

## Requirements

- Go 1.27+

`make prepare-static-check` installs all static analysis tools.

## Running tests

```sh
make check
```

This runs all unit tests (with coverage) and the full static analysis suite.

### Unit tests only

```sh
go test ./...
```

### Integration tests (kind)

Integration tests verify that go-pmtud works correctly inside a real multi-node cluster, including actual NFLOG capture, TUN injection, and UDP relay across nodes. An automated single-cluster harness lives under `lab/` — run it with `make -C lab e2e`. To test manually with [kind](https://kind.sigs.k8s.io/):

1. Create a multi-node cluster: `kind create cluster --config kind-config.yaml` (use a config with at least two worker nodes)
2. Build and load the image: `docker build -t go-pmtud . && kind load docker-image go-pmtud`
3. Deploy as a DaemonSet with the appropriate iptables init container; use `--relay-backend=udp` to test the UDP path (the default is `l2`)
4. Trigger an ICMP frag-needed packet (e.g. send a large UDP packet with DF set toward a destination with a lower MTU path)
5. Verify the packet appears on other nodes via `kubectl logs` or by inspecting the route cache MTU entry on the target node

### End-to-end tests (real cluster)

End-to-end testing in a real cluster (production or staging) is performed manually before releases. It follows the same steps as the kind integration test but validates behaviour under real ECMP traffic patterns.

To confirm a relay is working, check the route cache on a node that should have received the relayed packet:

```sh
ip route get <destination-IP>
# Expected: cache entry with a reduced mtu value
```

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
