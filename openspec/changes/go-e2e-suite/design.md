<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Go E2E Suite — Design

**Goal:** Replace the bash lab provisioning + test scripts with a single Go
Ginkgo/Gomega end-to-end suite that drives the full lifecycle (provision →
deploy → test → teardown), tests both relay backends in one run, and stays out
of the `go test ./...` path via a build tag.

## Scope

**In scope**
- Go `lab` package: docker networks, Kind clusters (Kind Go API), router
  container, static routes, offload disabling, per-backend deploy, traffic
  generation, PMTU/ICMP inspection.
- Ginkgo suite: lifecycle bootstrap, `udp`+`crd` backend matrix, PMTU
  convergence specs, per-backend config-validation specs, failure diagnostics.
- `e2e` build tag; `e2e` target in `lab/Makefile`.
- Delete the 11 replaced bash scripts.

**Out of scope**
- Changing lab network topology or MTU semantics.
- `docker/docker/client` SDK (docker ops stay `exec`-based).
- `observe.sh`, `status.sh` (dev-only debug tools; kept as bash).
- Router `Dockerfile`, Kind config YAML, K8s manifest YAML (kept).
- CI wiring (GitHub Actions) — separate follow-up.

## Architecture

```
test/e2e/
  suite_test.go     # Ginkgo RunSpecs + BeforeSuite/AfterSuite (full lifecycle)
  pmtu_test.go      # PMTU replication convergence specs
  config_test.go    # per-backend deployment config validation

lab/                # package lab — provisioning + operations library
  provision.go      # Provision()/Teardown() orchestration
  network.go        # docker network create/remove (exec)
  cluster.go        # Kind cluster create/delete + kubeconfig (Kind Go API)
  router.go         # router container build/run/configure
  routes.go         # static routes + offload disabling (dockerExec)
  deploy.go         # DeployBackend(): build+load image, apply manifests, wait
  ops.go            # GenerateTraffic, PMTUTo, CaptureICMP, FlushRouteCache
  exec.go           # run() + dockerExec() + ifaceByIP() helpers
  configs/          # KEPT YAML: kind-cluster-a.yaml, kind-cluster-b.yaml, router/Dockerfile
  manifests/        # KEPT YAML: pmtud-daemonset.yaml, rbac.yaml, podinfo.yaml
                    #   (CRD applied from repo-root crd/pmtud.cloud.sap_pmtunoderelays.yaml)
  cmd/labctl/       # thin CLI so `make deploy`/dev can drive the lab package
```

All files under `test/e2e/` and `lab/` (except `cmd/labctl`) carry
`//go:build e2e`. `lab/cmd/labctl` is a normal build so `make deploy` works
without the tag.

### Design rationale — where Go helps, where it does not

| Concern | Mechanism | Why |
|---|---|---|
| Kind clusters | Kind Go API | Kills `~/.kube/config` merge corruption; typed kubeconfig |
| Cluster assertions | `client-go`/controller-runtime | No `kubectl -o jsonpath` string-grep |
| Convergence waits | Gomega `Eventually` | Replaces flaky/slow `sleep` |
| docker networks | `exec` wrapper | SDK is a huge dep for `network create` |
| in-container ops (`ip`/`ethtool`/`tcpdump`/`curl`) | `dockerExec` wrapper | SDK exec is ~15 lines/cmd vs one |
| Manifests | YAML + SSA apply | Declarative, versionable, reviewable |

Full-Go provisioning is roughly the same line count as bash for docker parts —
the win is typed errors, single language, one test binary, and the eliminated
kubeconfig-corruption failure class, not fewer lines.

## Components

### `lab` package public surface

```go
package lab

type Cluster struct {
    Name           string
    KubeconfigPath string          // isolated temp file, never ~/.kube/config
    Client         client.Client   // controller-runtime, v1alpha1 scheme registered
    Workers        []string        // docker container names of worker nodes
}

type Lab struct {
    ClusterA, ClusterB *Cluster
    Router             string       // router container name
    DestIP             string       // cluster-b traffic target (podinfo NodePort host)
}

func Provision(ctx context.Context) (*Lab, error)  // networks → clusters → router → routes
func (l *Lab) Teardown(ctx context.Context) error   // reverse order; no-op if LAB_KEEP set

func (l *Lab) DeployBackend(ctx context.Context, backend string) error
func (l *Lab) GenerateTraffic(ctx context.Context) error
func (l *Lab) FlushRouteCache(node string) error
func (l *Lab) PMTUTo(node, dst string) (int, error)        // parse `ip route get`
func (l *Lab) CaptureICMPAsync(ctx context.Context, d time.Duration) *ICMPCapture
```

### Exec helpers (`exec.go`)

```go
func run(args ...string) error {
    return exec.Command(args[0], args[1:]...).Run()
}

func dockerExec(container string, args ...string) (string, error) {
    out, err := exec.Command("docker", append([]string{"exec", container}, args...)...).CombinedOutput()
    return string(out), err
}

// ifaceByIP parses `docker exec <c> ip -o addr show` and returns the interface
// carrying the given IP — replaces the bash grep|awk pipelines.
func ifaceByIP(container, ip string) (string, error)
```

### Clusters (`cluster.go`) — Kind Go API

```go
import "sigs.k8s.io/kind/pkg/cluster"

func createCluster(name, configPath string) (*Cluster, error) {
    p := cluster.NewProvider()
    if !exists(p, name) {
        if err := p.Create(name, cluster.CreateWithConfigFile(configPath)); err != nil {
            return nil, err
        }
    }
    kcfg, err := p.KubeConfig(name, false) // internal=false; do NOT merge into ~/.kube/config
    if err != nil { return nil, err }
    path := writeTempKubeconfig(name, kcfg)
    cl := buildClient(path) // controller-runtime client, v1alpha1.AddToScheme
    return &Cluster{Name: name, KubeconfigPath: path, Client: cl, Workers: workerContainers(name)}, nil
}
```

`workerContainers` uses `docker ps --filter label=io.x-k8s.kind.cluster=<name>
--filter label=io.x-k8s.kind.role=worker`.

### Networks / router / routes

Direct translations of `setup-networks.sh` / `setup-router.sh` /
`setup-routes.sh`, idempotent (inspect-before-create), using `run()` +
`dockerExec()` + typed `ifaceByIP()` in place of grep/awk. Router interface MTU
map: net-a=9000, net-b=1500, transit=1500; offloads (`gso/gro/tso off`) disabled
on router + all cluster-node network interfaces.

### Deploy (`deploy.go`) — replaces deploy-pmtud.sh + deploy-workload.sh

```go
func (l *Lab) DeployBackend(ctx context.Context, backend string) error {
    if err := run("docker", "build", "-t", "go-pmtud:local", repoRoot()); err != nil { return err }
    for _, c := range []*Cluster{l.ClusterA, l.ClusterB} {
        run("kind", "load", "docker-image", "go-pmtud:local", "--name", c.Name)
        if backend == "crd" {
            c.applyFile(ctx, repoRoot()+"/crd/pmtud.cloud.sap_pmtunoderelays.yaml")
        }
        c.applyFile(ctx, "manifests/rbac.yaml")
        c.applyDaemonSet(ctx, "manifests/pmtud-daemonset.yaml", backend) // inject --relay-backend
        c.waitRollout(ctx, "kube-system", "go-pmtud")
    }
    return l.deployWorkload(ctx) // podinfo to cluster-b + 1MB test file
}
```

Manifests applied via server-side apply (`client.Patch` with `client.Apply`,
field owner `go-pmtud-e2e`). The daemonset's `--relay-backend` arg and
`POD_NAMESPACE` downward-API env are set on the decoded object before apply.

## Suite structure

### Lifecycle (`suite_test.go`)

```go
//go:build e2e
package e2e

var testLab *lab.Lab

func TestE2E(t *testing.T) {
    RegisterFailHandler(Fail)
    RunSpecs(t, "go-pmtud e2e")
}

var _ = BeforeSuite(func(ctx SpecContext) {
    var err error
    if os.Getenv("LAB_REUSE") != "" {
        testLab, err = lab.Attach(ctx)      // discover existing running lab
    } else {
        testLab, err = lab.Provision(ctx)
    }
    Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func(ctx SpecContext) {
    if os.Getenv("LAB_KEEP") == "" && testLab != nil {
        Expect(testLab.Teardown(ctx)).To(Succeed())
    }
})
```

### Backend matrix (ordered)

```go
var _ = Describe("relay backend", func() {
    for _, backend := range []string{"udp", "crd"} {
        Context(backend, Ordered, func() {
            BeforeAll(func(ctx SpecContext) {
                Expect(testLab.DeployBackend(ctx, backend)).To(Succeed())
            })
            It("deploys with correct config", func(ctx SpecContext) { /* config_test.go */ })
            It("replicates PMTU to peer nodes", func(ctx SpecContext) { /* pmtu_test.go */ })
        })
    }
})
```

### PMTU convergence spec (`pmtu_test.go`)

```go
for _, w := range testLab.ClusterA.Workers {
    Expect(testLab.FlushRouteCache(w)).To(Succeed())
}
icmp := testLab.CaptureICMPAsync(ctx, 15*time.Second)
Expect(testLab.GenerateTraffic(ctx)).To(Succeed())

Eventually(icmp.Count).WithTimeout(20 * time.Second).
    Should(BeNumerically(">", 0), "router must emit ICMP frag-needed")

for _, w := range testLab.ClusterA.Workers {
    Eventually(func() int { m, _ := testLab.PMTUTo(w, testLab.DestIP); return m }).
        WithTimeout(30 * time.Second).WithPolling(2 * time.Second).
        Should(Equal(1500), "worker %s PMTU must converge via %s relay", w, backend)
}
```

The originating worker gets PMTU natively; peer workers get it via go-pmtud
replication — the assertion covers all cluster-a workers, matching the bash
intent (the whole point is replication reaches non-originating peers).

### Config-validation spec (`config_test.go`)

```go
var ds appsv1.DaemonSet
Expect(testLab.ClusterA.Client.Get(ctx,
    client.ObjectKey{Namespace: "kube-system", Name: "go-pmtud"}, &ds)).To(Succeed())
Expect(ds.Spec.Template.Spec.Containers[0].Args).
    To(ContainElement("--relay-backend=" + backend))
Expect(envNames(&ds)).To(ContainElement("POD_NAMESPACE"))
if backend == "crd" {
    Expect(testLab.ClusterA.CRDExists(ctx, "pmtunoderelays.pmtud.cloud.sap")).To(BeTrue())
    Expect(testLab.ClusterB.CRDExists(ctx, "pmtunoderelays.pmtud.cloud.sap")).To(BeTrue())
}
```

`CRDExists` uses an apiextensions client (or unstructured GET on the CRD).

## Error handling & diagnostics

- Every helper returns an `error`; specs assert with `Expect(err).NotTo(HaveOccurred())`.
- `run()`/`dockerExec()` include stderr in the returned error (via
  `CombinedOutput`), so failures name the failing command and its output — no
  more silent `|| true`.
- A `ReportAfterEach` hook, on spec failure, dumps to `GinkgoWriter`: router
  interface MTUs (`ip link show`), offload state (`ethtool -k`), and
  `ip route get <dst>` on cluster-a workers — the structured replacement for the
  bash `DIAGNOSTIC:` block.
- Provisioning is idempotent; `LAB_REUSE=1` lets a failed run be re-inspected
  without reprovisioning.

## Build gating & Makefile

- Build tag `//go:build e2e` on all suite + `lab` files (except `cmd/labctl`),
  so `go test ./...` and `make check` never compile or run the e2e suite (no
  docker/kind required in unit CI).
- Hand-maintained `lab/Makefile` gains:

```make
GINKGO_FLAGS ?=
e2e: ## Run the Go e2e suite (provisions, tests udp+crd, tears down)
	go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)
```

- The autogenerated root `Makefile` is not edited. If e2e ever needs to be a
  first-class `make` target, add it via `Makefile.maker.yaml`, not by hand.

## Testing strategy

This change *is* the test harness; its correctness is demonstrated by:
1. `go build -tags e2e ./...` and `go vet -tags e2e ./...` clean.
2. `go test ./...` (no tag) unchanged and green — the suite is excluded.
3. A full `make -C lab e2e` run passing for both `udp` and `crd` backends on a
   Linux host (macOS Docker Desktop MTU caveat noted in `lab/README.md`).
4. Pure helper functions (`ifaceByIP` parsing, daemonset arg injection) get
   small table tests compiled under the `e2e` tag.

## Migration / cutover

1. Land the `lab` package + suite behind the tag (nothing deleted yet).
2. Verify `make -C lab e2e` passes both backends.
3. Delete replaced scripts: `test-e2e.sh`, `verify-pmtu.sh`,
   `generate-traffic.sh`, `test-relay-backends.sh`, `setup-networks.sh`,
   `setup-clusters.sh`, `setup-router.sh`, `setup-routes.sh`, `deploy-pmtud.sh`,
   `deploy-workload.sh`, `teardown-networks.sh`.
4. Repoint `lab/Makefile` `pmtu-up`/`deploy`/`test`/`down` targets at
   `go run ./lab/cmd/labctl <verb>` (or the `e2e` target), keeping the familiar
   verbs working.
5. Update `lab/README.md`: new commands, env knobs (`LAB_REUSE`, `LAB_KEEP`),
   prerequisites (Go replaces python3; docker/kind/kubectl unchanged).

## Open considerations

- **Kind Go API version pin** must match the `kind` binary contract; pin in
  `go.mod` and document the compatible Kind node image.
- **Privileged docker exec** for `ip`/`ethtool` requires the same host
  capabilities the bash lab needed — unchanged.
- **macOS MTU**: Docker Desktop may not honor custom network MTUs; the suite
  targets a Linux host for authoritative runs (pre-existing limitation).
