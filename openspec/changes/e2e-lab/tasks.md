<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Tasks — E2E Lab

Ordered by dependency. Each task is completable in one coding session. All `lab/`
and `test/e2e/` files carry `//go:build e2e` except `lab/cmd/labctl`.

## 1. Scaffolding
- [ ] `lab` package: `Lab`/`Cluster` types; `exec.go` with `run()`, `dockerExec()`,
      `ifaceByIP()`, `ipOnSubnet()`.
- [ ] `lab/configs/kind-cluster.yaml`: 1 control-plane + 2 workers (SPDX header).

## 2. Cluster provisioning (`cluster.go`)
- [ ] Kind Go API create/delete; isolated temp kubeconfig (never `~/.kube/config`).
- [ ] `controller-runtime` client with `v1alpha1` scheme registered.
- [ ] Worker + control-plane container discovery via `io.x-k8s.kind.role` labels.

## 3. Lifecycle (`lab.go`)
- [ ] `Provision`: create cluster, discover CP + workers, set `BlackholeIP`.
- [ ] `Teardown`: delete cluster; no-op on `LAB_KEEP`.
- [ ] `Attach` for `LAB_REUSE`.

## 4. Forwarding hop + route (`routes.go`)
- [ ] Control-plane: `ip_forward=1`; `pmtudlab0` (`dummy`, `veth` fallback) @ MTU
      1280; assign `10.99.0.1/24`.
- [ ] worker-A: `ip route replace 10.99.0.0/24 via <CP InternalIP>`.

## 5. Deploy (`deploy.go`)
- [ ] Build image, `kind load`.
- [ ] Apply RBAC + daemonset (inject `--relay-backend` + `POD_NAMESPACE`) + CRD
      (crd backend); `waitRollout`.

## 6. Trigger + inspection (`ops.go`)
- [ ] `GenerateTraffic`: `ping -M do -s 1400 -c3 -W2 <BlackholeIP>` on worker-A;
      success on `Frag needed`/`mtu = 1280`, ignoring the non-zero exit; error if
      absent. Install `iputils-ping` on worker-A if missing (or raw-socket sender).
- [ ] `PMTUTo` (`ip route get` MTU parse) and `FlushRouteCache`.

## 7. labctl (`cmd/labctl`, no build tag)
- [ ] Lifecycle verbs (up / deploy / test / down) for manual use.

## 8. Suite (`test/e2e/`)
- [ ] `suite_test.go`: `RunSpecs` + `BeforeSuite`/`AfterSuite`; `LAB_REUSE`/`LAB_KEEP`.
- [ ] `config_test.go`: per-backend daemonset arg/env assertions; CRD established
      for `crd`.
- [ ] `pmtu_test.go`: `udp`+`crd` matrix; assert `PMTUTo(w, BlackholeIP) == 1280`
      for every worker; `ReportAfterEach` failure diagnostics (route + hop link).

## 9. Helper unit tests (`e2e` tag)
- [ ] `ping`-output frag-needed parser.
- [ ] Control-plane discovery parser.
- [ ] `ip route get` MTU parser.

## 10. Runbook + README + Makefile
- [ ] `lab/RUNBOOK-real-cluster.md` (deploy → induce frag-needed → observe peer
      convergence + metrics/logs → cleanup).
- [ ] `lab/README.md`: single-cluster topology, commands, `LAB_REUSE`/`LAB_KEEP`,
      prerequisites; link the runbook.
- [ ] `lab/Makefile` (hand-maintained): `e2e` / `e2e-reuse` / `e2e-keep` targets
      (`go test -tags e2e -timeout 20m ./test/e2e/...`).

## 11. Build verification
- [ ] `go build -tags e2e ./...` and `go vet -tags e2e ./...` clean.
- [ ] `go test ./...` (no tag) green (suite excluded).
- [ ] `make -C lab e2e` passes `udp` and `crd` on Linux and macOS Docker Desktop.

_Note: never edit the generated root `Makefile`; only `lab/Makefile` or `Makefile.maker.yaml`._
