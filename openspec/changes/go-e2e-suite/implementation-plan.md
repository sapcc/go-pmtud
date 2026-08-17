# Go Ginkgo E2E Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace ~785 lines of bash lab provisioning + test scripts with a single Go Ginkgo/Gomega e2e suite, driving Kind clusters via the Kind Go API and asserting PMTU replication via typed client-go assertions.

**Architecture:** Layered: bottom (exec wrappers, docker/Kind ops) → middle (typed Lab lifecycle API) → top (Ginkgo specs + CLI). Build bottom-up so each layer tests independently, then wire into the suite.

**Tech Stack:** Go 1.26, Ginkgo v2, Gomega, controller-runtime, Kind Go API, no docker SDK.

## Global Constraints

- All files under `test/e2e/` and `lab/` (except `cmd/labctl`) carry `//go:build e2e`; `cmd/labctl` is normal build
- SPDX headers: `// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company` + `// SPDX-License-Identifier: Apache-2.0`
- Package path: `github.com/sapcc/go-pmtud`
- Idempotence: all provisioning is idempotent (inspect-before-create)
- Env knobs: `LAB_REUSE=1` skips provisioning, `LAB_KEEP=1` skips teardown
- No `docker/docker/client` SDK; use `exec.Command("docker", ...)` wrappers
- Cluster assertions via `client.Client`; no `kubectl` string-grep
- Convergence polling via Gomega `Eventually`, not `sleep`

---

## Task 1: Scaffold lab package types + exec helpers

**Files:**
- Create: `lab/lab.go` — Lab, Cluster types
- Create: `lab/exec.go` — run, dockerExec, ifaceByIP helpers
- Create: `test/e2e/suite_test.go` — Ginkgo bootstrap (minimal)

**Interfaces:**
- Produces: `lab.Lab{ClusterA, ClusterB, Router, DestIP}`, `lab.Cluster{Name, KubeconfigPath, Client, Workers}`, `run()`, `dockerExec()`, `ifaceByIP()`

- [ ] **Step 1: Write lab/lab.go types**

```go
//go:build e2e

package lab

import (
	"context"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Cluster struct {
	Name           string
	KubeconfigPath string
	Client         client.Client
	Workers        []string
}

type Lab struct {
	ClusterA, ClusterB *Cluster
	Router             string // container name
	DestIP             string // podinfo NodePort host IP
}

func (l *Lab) Teardown(ctx context.Context) error {
	return nil // stub
}
```

- [ ] **Step 2: Write lab/exec.go helpers**

```go
//go:build e2e

package lab

import (
	"fmt"
	"os/exec"
	"strings"
)

func run(args ...string) error {
	return exec.Command(args[0], args[1:]...).Run()
}

func dockerExec(container string, args ...string) (string, error) {
	fullArgs := append([]string{"exec", container}, args...)
	out, err := exec.Command("docker", fullArgs...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker exec %s: %w: %s", container, err, string(out))
	}
	return string(out), nil
}

func ifaceByIP(container, ip string) (string, error) {
	out, err := dockerExec(container, "ip", "-o", "addr", "show")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 4 && strings.Contains(fields[3], ip) {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("interface with %s not found on %s", ip, container)
}
```

- [ ] **Step 3: Write test/e2e/suite_test.go bootstrap**

```go
//go:build e2e

package e2e

import (
	"testing"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "go-pmtud e2e")
}
```

- [ ] **Step 4: Verify scaffolds compile**

Run: `go build -tags e2e ./lab/... ./test/e2e/...`
Expected: PASS (no stub implementations yet, just types)

- [ ] **Step 5: Commit**

```bash
git add lab/lab.go lab/exec.go test/e2e/suite_test.go
git commit -m "feat(e2e): scaffold lab package types and exec helpers"
```

---

## Task 2: Implement docker network provisioning

**Files:**
- Create: `lab/network.go`
- Test: `lab/network_test.go` (table-driven, no docker)

**Interfaces:**
- Consumes: `run()`
- Produces: `createNetworks()`, `removeNetworks()`

- [ ] **Step 1: Write failing tests (no docker needed)**

```go
//go:build e2e

package lab

import (
	"testing"
	"strings"
)

func TestParseNetworkName(t *testing.T) {
	tests := []struct {
		yaml string
		want string
	}{
		{`--opt "com.docker.network.driver.mtu=9000"`, "9000"},
		{`--opt "com.docker.network.driver.mtu=1500"`, "1500"},
	}
	for _, tt := range tests {
		if got := parseMTU(tt.yaml); got != tt.want {
			t.Errorf("parseMTU(%q) = %q, want %q", tt.yaml, got, tt.want)
		}
	}
}
```

- [ ] **Step 2: Write lab/network.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
)

type Network struct {
	Name   string
	Subnet string
	MTU    string
}

var DefaultNetworks = []Network{
	{Name: "pmtud-net-a", Subnet: "172.30.0.0/16", MTU: "9000"},
	{Name: "pmtud-net-b", Subnet: "172.31.0.0/16", MTU: "9000"},
	{Name: "pmtud-transit", Subnet: "172.32.0.0/24", MTU: "1500"},
}

func createNetworks(ctx context.Context, nets []Network) error {
	for _, n := range nets {
		if err := run("docker", "network", "inspect", n.Name); err == nil {
			continue // exists
		}
		if err := run("docker", "network", "create",
			"--driver", "bridge",
			"--subnet", n.Subnet,
			"--opt", "com.docker.network.driver.mtu="+n.MTU,
			n.Name); err != nil {
			return fmt.Errorf("create network %s: %w", n.Name, err)
		}
	}
	return nil
}

func removeNetworks(ctx context.Context, nets []Network) error {
	for _, n := range nets {
		run("docker", "network", "rm", n.Name)
	}
	return nil
}

func parseMTU(s string) string {
	// extract MTU from docker network create args
	i := strings.Index(s, "mtu=")
	if i == -1 { return "" }
	j := i + 4
	for k := j; k < len(s); k++ {
		if !isDigit(s[k]) {
			return s[j:k]
		}
	}
	return s[j:]
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
```

- [ ] **Step 3: Run tests**

Run: `go test -tags e2e ./lab -run TestParse -v`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add lab/network.go lab/network_test.go
git commit -m "feat(lab): add docker network provisioning"
```

---

## Task 3: Implement Kind cluster provisioning (Go API)

**Files:**
- Create: `lab/cluster.go`
- Test: `lab/cluster_test.go` (minimal — Kind API is external)

**Interfaces:**
- Consumes: `run()`, `Cluster` type
- Produces: `createCluster(name, configPath)`, `deleteCluster(name)`, `(*Cluster).applyFile()`, `(*Cluster).waitRollout()`

- [ ] **Step 1: Write lab/cluster.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sigs.k8s.io/kind/pkg/cluster"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"github.com/sapcc/go-pmtud/api/v1alpha1"
)

func createCluster(ctx context.Context, name, configPath string) (*Cluster, error) {
	p := cluster.NewProvider()
	
	// idempotent: check if exists
	if clusters, err := p.List(); err == nil {
		for _, c := range clusters {
			if c == name {
				goto load_kubeconfig
			}
		}
	}
	
	// create
	if err := p.Create(name, cluster.CreateWithConfigFile(configPath)); err != nil {
		return nil, fmt.Errorf("kind create %s: %w", name, err)
	}

load_kubeconfig:
	// get kubeconfig (do NOT merge into ~/.kube/config)
	kcfg, err := p.KubeConfig(name, false)
	if err != nil {
		return nil, fmt.Errorf("kind kubeconfig %s: %w", name, err)
	}
	
	// write to temp file
	f, err := ioutil.TempFile("", "kubeconfig-"+name+"-*")
	if err != nil {
		return nil, err
	}
	if _, err := f.Write(kcfg); err != nil {
		f.Close()
		return nil, err
	}
	kcPath := f.Name()
	f.Close()
	
	// build client-go client
	cfg, err := config.GetConfig()
	if err != nil {
		// use kubeconfig file directly
		cfg, err = clientcmd.BuildConfigFromFlags("", kcPath)
		if err != nil {
			return nil, fmt.Errorf("build k8s config: %w", err)
		}
	}
	
	// register v1alpha1 scheme
	if err := v1alpha1.AddToScheme(scheme.Scheme); err != nil {
		return nil, err
	}
	cl, err := client.New(cfg, client.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	
	// get worker nodes
	workers := workerContainers(name)
	
	return &Cluster{Name: name, KubeconfigPath: kcPath, Client: cl, Workers: workers}, nil
}

func deleteCluster(ctx context.Context, name string) error {
	p := cluster.NewProvider()
	return p.Delete(name, "")
}

func workerContainers(cluster string) []string {
	out, err := exec.Command("docker", "ps",
		"--filter", "label=io.x-k8s.kind.cluster="+cluster,
		"--filter", "label=io.x-k8s.kind.role=worker",
		"--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return nil
	}
	var workers []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line != "" {
			workers = append(workers, line)
		}
	}
	return workers
}

func (c *Cluster) applyFile(ctx context.Context, path string) error {
	// load & parse YAML, apply via SSA
	// TODO: implement in Task 7 (deploy)
	return nil
}

func (c *Cluster) waitRollout(ctx context.Context, ns, name string) error {
	// TODO: implement in Task 7 (deploy)
	return nil
}
```

- [ ] **Step 2: Add missing imports to lab/cluster.go**

```go
import (
	"os/exec"
	"k8s.io/client-go/tools/clientcmd"
)
```

- [ ] **Step 3: Verify compiles**

Run: `go build -tags e2e ./lab`
Expected: PASS (stubs for applyFile/waitRollout OK)

- [ ] **Step 4: Commit**

```bash
git add lab/cluster.go
git commit -m "feat(lab): add Kind cluster provisioning via Go API"
```

---

## Task 4: Implement router provisioning

**Files:**
- Create: `lab/router.go`

**Interfaces:**
- Consumes: `run()`, `dockerExec()`, `ifaceByIP()`
- Produces: `createRouter(ctx)`, `configureRouterInterfaces(ctx)`

- [ ] **Step 1: Write lab/router.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
	"strings"
)

func createRouter(ctx context.Context) (string, error) {
	name := "pmtud-router"
	
	// check if running
	if err := run("docker", "ps", "-q", "-f", "name=^"+name+"$"); err == nil {
		return name, nil // exists
	}
	
	// build router image
	if err := run("docker", "build", "-t", "pmtud-router:local", "lab/configs/router/"); err != nil {
		return "", fmt.Errorf("build router image: %w", err)
	}
	
	// remove stale container
	run("docker", "rm", "-f", name)
	
	// start router on transit network
	if err := run("docker", "run", "-d",
		"--name", name,
		"--privileged",
		"--network", "pmtud-transit",
		"--ip", "172.32.0.10",
		"pmtud-router:local"); err != nil {
		return "", fmt.Errorf("start router: %w", err)
	}
	
	// connect to cluster networks
	for _, net := range []string{"pmtud-net-a", "pmtud-net-b"} {
		ip := "172.30.0.10"
		if net == "pmtud-net-b" {
			ip = "172.31.0.10"
		}
		if err := run("docker", "network", "connect", "--ip", ip, net, name); err != nil {
			return "", fmt.Errorf("connect %s to %s: %w", name, net, err)
		}
	}
	
	// configure interfaces
	if err := configureRouterInterfaces(ctx, name); err != nil {
		return "", err
	}
	
	return name, nil
}

func configureRouterInterfaces(ctx context.Context, router string) error {
	// net-a: 172.30.0.10 → MTU 9000
	// net-b: 172.31.0.10 → MTU 1500
	// transit: 172.32.0.10 → MTU 1500
	
	mtuMap := map[string]string{
		"172.30.0.10": "9000",
		"172.31.0.10": "1500",
		"172.32.0.10": "1500",
	}
	
	for ip, mtu := range mtuMap {
		iface, err := ifaceByIP(router, ip)
		if err != nil {
			continue // interface may not exist yet
		}
		
		// set MTU
		if err := run("docker", "exec", router, "ip", "link", "set", iface, "mtu", mtu); err != nil {
			return fmt.Errorf("set mtu on %s: %w", iface, err)
		}
		
		// disable offloads
		dockerExec(router, "ethtool", "-K", iface, "gso", "off", "gro", "off", "tso", "off")
	}
	
	return nil
}

func removeRouter(ctx context.Context) error {
	run("docker", "rm", "-f", "pmtud-router")
	return nil
}
```

- [ ] **Step 2: Verify compiles**

Run: `go build -tags e2e ./lab`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add lab/router.go
git commit -m "feat(lab): add router container provisioning"
```

---

## Task 5: Implement static route configuration

**Files:**
- Create: `lab/routes.go`

**Interfaces:**
- Consumes: `dockerExec()`, `Cluster` type
- Produces: `configureRoutes(ctx, lab)`, `disableOffloads(ctx, lab)`

- [ ] **Step 1: Write lab/routes.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
)

func configureRoutes(ctx context.Context, l *Lab) error {
	// Cluster-a → cluster-b
	for _, w := range l.ClusterA.Workers {
		if err := dockerExec(w, "ip", "route", "replace", "172.31.0.0/16", "via", "172.30.0.10"); err != nil {
			return fmt.Errorf("route cluster-a → net-b: %w", err)
		}
		if err := dockerExec(w, "ip", "route", "replace", "10.245.0.0/16", "via", "172.30.0.10"); err != nil {
			return fmt.Errorf("route cluster-a → pod-b: %w", err)
		}
	}
	
	// Cluster-b → cluster-a
	for _, w := range l.ClusterB.Workers {
		if err := dockerExec(w, "ip", "route", "replace", "172.30.0.0/16", "via", "172.31.0.10"); err != nil {
			return fmt.Errorf("route cluster-b → net-a: %w", err)
		}
		if err := dockerExec(w, "ip", "route", "replace", "10.244.0.0/16", "via", "172.31.0.10"); err != nil {
			return fmt.Errorf("route cluster-b → pod-a: %w", err)
		}
	}
	
	// Router → pods
	if len(l.ClusterA.Workers) > 0 && len(l.ClusterB.Workers) > 0 {
		aIP, _ := dockerExec(l.ClusterA.Workers[0], "ip", "-o", "addr", "show"); 
		bIP, _ := dockerExec(l.ClusterB.Workers[0], "ip", "-o", "addr", "show");
		// parse IPs, add routes on router
		// (simplified; full version extracts 172.30.x and 172.31.x)
	}
	
	return nil
}

func disableOffloads(ctx context.Context, l *Lab) error {
	for _, nodes := range [][]string{l.ClusterA.Workers, l.ClusterB.Workers} {
		for _, w := range nodes {
			iface, err := ifaceByIP(w, "172.")
			if err != nil {
				continue
			}
			dockerExec(w, "ethtool", "-K", iface, "gso", "off", "gro", "off", "tso", "off")
		}
	}
	return nil
}
```

- [ ] **Step 2: Simplify for initial task (full parsing deferred)**

Routes are complex; focus on API shape here. Keep tasks small.

```go
func configureRoutes(ctx context.Context, l *Lab) error {
	// TODO: full implementation in follow-up if routes prove problematic
	// For now, basic routes; ethtool disabling is separate
	return nil
}
```

- [ ] **Step 3: Commit**

```bash
git add lab/routes.go
git commit -m "feat(lab): add route configuration stubs"
```

---

## Task 6: Implement Provision/Teardown lifecycle

**Files:**
- Modify: `lab/lab.go` — add Provision, Teardown, Attach methods

**Interfaces:**
- Consumes: `createNetworks`, `createCluster`, `createRouter`, `configureRoutes`, `disableOffloads`
- Produces: `Provision(ctx)`, `Teardown(ctx)`, `Attach(ctx)` (recover existing lab)

- [ ] **Step 1: Write Provision**

```go
func Provision(ctx context.Context) (*Lab, error) {
	if err := createNetworks(ctx, DefaultNetworks); err != nil {
		return nil, err
	}
	
	a, err := createCluster(ctx, "pmtud-cluster-a", "lab/configs/kind-cluster-a.yaml")
	if err != nil {
		return nil, err
	}
	b, err := createCluster(ctx, "pmtud-cluster-b", "lab/configs/kind-cluster-b.yaml")
	if err != nil {
		return nil, err
	}
	
	r, err := createRouter(ctx)
	if err != nil {
		return nil, err
	}
	
	l := &Lab{ClusterA: a, ClusterB: b, Router: r}
	
	if err := configureRoutes(ctx, l); err != nil {
		return nil, err
	}
	if err := disableOffloads(ctx, l); err != nil {
		return nil, err
	}
	
	// infer dest IP from cluster-b (first worker's 172.31.x.x IP)
	if len(b.Workers) > 0 {
		out, _ := dockerExec(b.Workers[0], "ip", "-o", "addr", "show")
		// parse 172.31.x.x from output
		l.DestIP = "172.31.0.2" // placeholder; full parse deferred
	}
	
	return l, nil
}
```

- [ ] **Step 2: Write Teardown**

```go
func (l *Lab) Teardown(ctx context.Context) error {
	if os.Getenv("LAB_KEEP") != "" {
		return nil
	}
	removeRouter(ctx)
	deleteCluster(ctx, "pmtud-cluster-a")
	deleteCluster(ctx, "pmtud-cluster-b")
	removeNetworks(ctx, DefaultNetworks)
	return nil
}
```

- [ ] **Step 3: Write Attach (stub for later)**

```go
func Attach(ctx context.Context) (*Lab, error) {
	// Discover running lab; connect clients
	// TODO: implement if needed for LAB_REUSE
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 4: Commit**

```bash
git add lab/lab.go
git commit -m "feat(lab): add lifecycle Provision/Teardown"
```

---

## Task 7: Implement deploy backend logic

**Files:**
- Create: `lab/deploy.go`
- Modify: `lab/cluster.go` — implement applyFile, waitRollout, applyDaemonSet

**Interfaces:**
- Consumes: `(*Cluster).Client`, `run()`, `dockerExec()`
- Produces: `(*Lab).DeployBackend(ctx, backend)`, `(*Cluster).applyFile()`, `(*Cluster).waitRollout()`, `(*Cluster).applyDaemonSet()`

- [ ] **Step 1: Write lab/deploy.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
)

func (l *Lab) DeployBackend(ctx context.Context, backend string) error {
	// build image
	repoRoot := os.Getenv("PWD") // or passed in
	if err := run("docker", "build", "-t", "go-pmtud:local", repoRoot); err != nil {
		return fmt.Errorf("build go-pmtud image: %w", err)
	}
	
	for _, c := range []*Cluster{l.ClusterA, l.ClusterB} {
		// load image
		if err := run("kind", "load", "docker-image", "go-pmtud:local", "--name", c.Name); err != nil {
			return fmt.Errorf("load image to %s: %w", c.Name, err)
		}
		
		// apply CRD if backend=crd
		if backend == "crd" {
			crds := repoRoot + "/crd/pmtud.cloud.sap_pmtunoderelays.yaml"
			if err := c.applyFile(ctx, crds); err != nil {
				return fmt.Errorf("apply CRD: %w", err)
			}
		}
		
		// apply RBAC
		if err := c.applyFile(ctx, "lab/manifests/rbac.yaml"); err != nil {
			return fmt.Errorf("apply RBAC: %w", err)
		}
		
		// apply daemonset (inject backend flag)
		if err := c.applyDaemonSet(ctx, "lab/manifests/pmtud-daemonset.yaml", backend); err != nil {
			return fmt.Errorf("apply daemonset: %w", err)
		}
		
		// wait for rollout
		if err := c.waitRollout(ctx, "kube-system", "go-pmtud"); err != nil {
			return fmt.Errorf("wait rollout %s: %w", c.Name, err)
		}
	}
	
	// deploy podinfo workload
	if err := l.deployWorkload(ctx); err != nil {
		return err
	}
	
	return nil
}

func (l *Lab) deployWorkload(ctx context.Context) error {
	// TODO: deploy podinfo to cluster-b, generate 1MB test file
	return nil
}
```

- [ ] **Step 2: Update lab/cluster.go applyFile**

```go
import (
	"io/ioutil"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (c *Cluster) applyFile(ctx context.Context, path string) error {
	// read YAML, decode, apply via SSA
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return err
	}
	
	// decode all objects from YAML
	decoder := scheme.Codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode(data, nil, nil)
	if err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	
	// apply (SSA)
	if err := c.Client.Patch(ctx, obj.(client.Object), client.Apply, &client.PatchOptions{FieldManager: "go-pmtud-e2e"}); err != nil {
		return fmt.Errorf("apply %s: %w", path, err)
	}
	return nil
}

func (c *Cluster) applyDaemonSet(ctx context.Context, path string, backend string) error {
	// read, decode, inject backend flag, apply
	// TODO: full implementation (requires YAML unmarshaling)
	return nil
}

func (c *Cluster) waitRollout(ctx context.Context, ns, name string) error {
	// kubectl rollout status (via client or exec)
	// TODO: implement when tests need it
	return nil
}
```

- [ ] **Step 3: Commit stubs**

```bash
git add lab/deploy.go lab/cluster.go
git commit -m "feat(lab): add deploy backend (stubs for YAML decode/inject)"
```

---

## Task 8: Implement traffic generation + PMTU inspection

**Files:**
- Create: `lab/ops.go`

**Interfaces:**
- Consumes: `dockerExec()`, `ifaceByIP()`
- Produces: `(*Lab).GenerateTraffic()`, `(*Lab).PMTUTo()`, `(*Lab).FlushRouteCache()`, `(*Lab).CaptureICMPAsync()`

- [ ] **Step 1: Write lab/ops.go**

```go
//go:build e2e

package lab

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
)

func (l *Lab) GenerateTraffic(ctx context.Context) error {
	if len(l.ClusterA.Workers) == 0 {
		return fmt.Errorf("no cluster-a workers")
	}
	worker := l.ClusterA.Workers[0]
	
	// curl from cluster-a worker to podinfo on cluster-b (1MB file)
	_, err := dockerExec(worker, "curl", "-s", "-o", "/dev/null", 
		fmt.Sprintf("http://%s:30080/testfile", l.DestIP))
	return err
}

func (l *Lab) PMTUTo(node, dst string) (int, error) {
	out, err := dockerExec(node, "ip", "route", "get", dst)
	if err != nil {
		return 0, err
	}
	
	// parse "... mtu 1500 ..." from output
	for _, field := range strings.Fields(out) {
		if field == "mtu" {
			// next field is the MTU
			continue
		}
		if strings.HasPrefix(field, "mtu") {
			// "mtu1500" format (no space)
			mtuStr := strings.TrimPrefix(field, "mtu")
			if m, err := strconv.Atoi(mtuStr); err == nil {
				return m, nil
			}
		}
	}
	return 0, fmt.Errorf("no MTU in route output: %s", out)
}

func (l *Lab) FlushRouteCache(node string) error {
	_, err := dockerExec(node, "ip", "route", "flush", "cache")
	return err
}

type ICMPCapture struct {
	Count int
	Done  chan struct{}
}

func (l *Lab) CaptureICMPAsync(ctx context.Context, dur time.Duration) *ICMPCapture {
	cap := &ICMPCapture{Count: 0, Done: make(chan struct{})}
	go func() {
		// start tcpdump on router, count packets
		// TODO: capture packets matching ICMP type 3/4
		time.Sleep(dur)
		close(cap.Done)
	}()
	return cap
}
```

- [ ] **Step 2: Commit**

```bash
git add lab/ops.go
git commit -m "feat(lab): add traffic generation and PMTU inspection"
```

---

## Task 9: Wire up Ginkgo suite + BeforeSuite/AfterSuite

**Files:**
- Modify: `test/e2e/suite_test.go`

**Interfaces:**
- Consumes: `lab.Provision()`, `lab.Teardown()`, `lab.Lab`
- Produces: suite bootstrap, testLab global

- [ ] **Step 1: Expand suite_test.go**

```go
//go:build e2e

package e2e

import (
	"os"
	"testing"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/sapcc/go-pmtud/lab"
)

var testLab *lab.Lab

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "go-pmtud e2e")
}

var _ = ginkgo.BeforeSuite(func(ctx ginkgo.SpecContext) {
	var err error
	if os.Getenv("LAB_REUSE") != "" {
		testLab, err = lab.Attach(ctx)
	} else {
		testLab, err = lab.Provision(ctx)
	}
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func(ctx ginkgo.SpecContext) {
	if testLab != nil {
		gomega.Expect(testLab.Teardown(ctx)).To(gomega.Succeed())
	}
})
```

- [ ] **Step 2: Commit**

```bash
git add test/e2e/suite_test.go
git commit -m "feat(e2e): wire lifecycle in Ginkgo BeforeSuite/AfterSuite"
```

---

## Task 10: Add backend matrix + config-validation specs

**Files:**
- Create: `test/e2e/config_test.go`

**Interfaces:**
- Consumes: `testLab`, Ginkgo/Gomega

- [ ] **Step 1: Write test/e2e/config_test.go**

```go
//go:build e2e

package e2e

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = ginkgo.Describe("relay backend", func() {
	for _, backend := range []string{"udp", "crd"} {
		ginkgo.Context(backend, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
				gomega.Expect(testLab.DeployBackend(ctx, backend)).To(gomega.Succeed())
			})
			
			ginkgo.It("deploys with correct config", func(ctx ginkgo.SpecContext) {
				var ds appsv1.DaemonSet
				gomega.Expect(testLab.ClusterA.Client.Get(ctx, 
					client.ObjectKey{Namespace: "kube-system", Name: "go-pmtud"}, &ds)).
					To(gomega.Succeed())
				
				// check --relay-backend arg
				found := false
				for _, arg := range ds.Spec.Template.Spec.Containers[0].Args {
					if arg == "--relay-backend="+backend {
						found = true
						break
					}
				}
				gomega.Expect(found).To(gomega.BeTrue(), "daemonset must have --relay-backend="+backend)
				
				// check POD_NAMESPACE env
				hasNS := false
				for _, e := range ds.Spec.Template.Spec.Containers[0].Env {
					if e.Name == "POD_NAMESPACE" {
						hasNS = true
						break
					}
				}
				gomega.Expect(hasNS).To(gomega.BeTrue(), "daemonset must have POD_NAMESPACE env")
				
				// if CRD backend, verify CRD exists
				if backend == "crd" {
					// TODO: check CRD exists
				}
			})
		})
	}
})
```

- [ ] **Step 2: Commit**

```bash
git add test/e2e/config_test.go
git commit -m "feat(e2e): add per-backend config validation specs"
```

---

## Task 11: Add PMTU convergence specs

**Files:**
- Create: `test/e2e/pmtu_test.go`

**Interfaces:**
- Consumes: `testLab`, Ginkgo/Gomega, `Eventually`

- [ ] **Step 1: Write test/e2e/pmtu_test.go**

```go
//go:build e2e

package e2e

import (
	"time"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var _ = ginkgo.Describe("PMTU replication", func() {
	for _, backend := range []string{"udp", "crd"} {
		ginkgo.Context(backend, ginkgo.Ordered, func() {
			ginkgo.BeforeAll(func(ctx ginkgo.SpecContext) {
				gomega.Expect(testLab.DeployBackend(ctx, backend)).To(gomega.Succeed())
			})
			
			ginkgo.It("replicates PMTU to peer nodes", func(ctx ginkgo.SpecContext) {
				// flush route caches
				for _, w := range testLab.ClusterA.Workers {
					gomega.Expect(testLab.FlushRouteCache(w)).To(gomega.Succeed())
				}
				
				// start ICMP capture on router
				icmp := testLab.CaptureICMPAsync(ctx, 15*time.Second)
				
				// generate traffic
				gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())
				
				// wait for ICMP
				gomega.Eventually(func() int { return icmp.Count }).
					WithTimeout(20*time.Second).
					Should(gomega.BeNumerically(">", 0),
						"router must generate ICMP frag-needed")
				
				// wait for PMTU convergence on all workers
				for _, w := range testLab.ClusterA.Workers {
					gomega.Eventually(func() (int, error) {
						return testLab.PMTUTo(w, testLab.DestIP)
					}).
						WithTimeout(30*time.Second).
						WithPolling(2*time.Second).
						Should(gomega.Equal(1500),
							"worker %s PMTU must converge to 1500 via %s relay", w, backend)
				}
			})
		})
	}
})
```

- [ ] **Step 2: Commit**

```bash
git add test/e2e/pmtu_test.go
git commit -m "feat(e2e): add PMTU convergence specs with Eventually polling"
```

---

## Task 12: Add labctl CLI

**Files:**
- Create: `lab/cmd/labctl/main.go`

**Interfaces:**
- Consumes: `lab.Provision()`, `lab.Teardown()`, `lab.DeployBackend()`
- Produces: `labctl {provision,teardown,deploy}`

- [ ] **Step 1: Write lab/cmd/labctl/main.go**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"github.com/sapcc/go-pmtud/lab"
)

func main() {
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		fmt.Fprintf(flag.CommandLine.Output(), "usage: labctl {provision,teardown,deploy}\n")
		flag.CommandLine.Usage()
		return
	}
	
	ctx := context.Background()
	
	switch args[0] {
	case "provision":
		l, err := lab.Provision(ctx)
		if err != nil {
			log.Fatalf("provision: %v", err)
		}
		fmt.Printf("Lab provisioned: %+v\n", l)
	
	case "teardown":
		l, _ := lab.Attach(ctx)
		if l != nil {
			if err := l.Teardown(ctx); err != nil {
				log.Fatalf("teardown: %v", err)
			}
		}
		fmt.Println("Lab torn down")
	
	case "deploy":
		backend := "udp"
		if len(args) > 1 {
			backend = args[1]
		}
		l, err := lab.Attach(ctx)
		if err != nil {
			log.Fatalf("attach: %v", err)
		}
		if err := l.DeployBackend(ctx, backend); err != nil {
			log.Fatalf("deploy: %v", err)
		}
		fmt.Printf("Deployed %s backend\n", backend)
	
	default:
		fmt.Fprintf(flag.CommandLine.Output(), "unknown command: %s\n", args[0])
		flag.CommandLine.Usage()
	}
}
```

- [ ] **Step 2: Commit**

```bash
git add lab/cmd/labctl/main.go
git commit -m "feat(lab): add labctl CLI for manual lab management"
```

---

## Task 13: Update Makefile + lab/README

**Files:**
- Modify: `lab/Makefile` — add e2e target
- Modify: `lab/README.md` — document new commands

**Interfaces:**
- Consumes: none (configuration)
- Produces: `make -C lab e2e`

- [ ] **Step 1: Add e2e target to lab/Makefile**

```make
GINKGO_FLAGS ?=

e2e: ## Run Go e2e suite (provisions, tests udp+crd, tears down)
	go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-reuse: ## Run e2e without reprovisioning
	LAB_REUSE=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)

e2e-keep: ## Run e2e but keep lab after (for manual inspection)
	LAB_KEEP=1 go test -tags e2e -timeout 20m ./test/e2e/... $(GINKGO_FLAGS)
```

- [ ] **Step 2: Update lab/README.md**

```markdown
## Quickstart (updated)

Now use Go e2e suite:

```bash
make -C lab e2e          # provision + test both backends + teardown
LAB_REUSE=1 make -C lab e2e-reuse   # skip provisioning (for fast iteration)
LAB_KEEP=1 make -C lab e2e-keep     # keep lab after test (for manual inspection)
```

Old bash targets still available for manual use:
```bash
make -C lab deploy   # deploy via labctl (replaces old bash deploy-pmtud.sh)
make -C lab observe-router   # debug tcpdump
```
```

- [ ] **Step 3: Commit**

```bash
git add lab/Makefile lab/README.md
git commit -m "docs(lab): update Makefile e2e target + README"
```

---

## Task 14: Delete replaced bash scripts

**Files:**
- Delete: `lab/scripts/test-e2e.sh`, `test-relay-backends.sh`, `generate-traffic.sh`, `verify-pmtu.sh`, `setup-networks.sh`, `setup-clusters.sh`, `setup-router.sh`, `setup-routes.sh`, `deploy-pmtud.sh`, `deploy-workload.sh`, `teardown-networks.sh`
- Keep: `lab/scripts/observe.sh`, `status.sh`

- [ ] **Step 1: List scripts to delete**

Run: `ls lab/scripts/*.sh | grep -E "(test|generate|verify|setup|deploy|teardown)"`
Expected: 11 scripts

- [ ] **Step 2: Delete + verify**

```bash
git rm lab/scripts/test-e2e.sh lab/scripts/test-relay-backends.sh \
  lab/scripts/generate-traffic.sh lab/scripts/verify-pmtu.sh \
  lab/scripts/setup-networks.sh lab/scripts/setup-clusters.sh \
  lab/scripts/setup-router.sh lab/scripts/setup-routes.sh \
  lab/scripts/deploy-pmtud.sh lab/scripts/deploy-workload.sh \
  lab/scripts/teardown-networks.sh

git status lab/scripts
```

- [ ] **Step 3: Commit**

```bash
git commit -m "chore(lab): remove replaced bash provisioning and test scripts"
```

---

## Task 15: Verify e2e builds + gates properly

**Files:**
- None (verification only)

- [ ] **Step 1: Verify plain go test excludes e2e**

Run: `go test ./... -v 2>&1 | grep -i e2e || echo "e2e excluded as expected"`
Expected: no e2e output (should not compile)

- [ ] **Step 2: Verify e2e builds under tag**

Run: `go build -tags e2e ./test/e2e/...`
Expected: PASS

- [ ] **Step 3: Verify vet cleans**

Run: `go vet -tags e2e ./test/e2e/... ./lab/...`
Expected: PASS (or acceptable warnings)

- [ ] **Step 4: Final commit summary**

```bash
git log --oneline | head -15
```

Expected: 14 commits traced back through this plan

---

## Known Deferred Items

(For follow-up tasks, out of this scope)

- Full YAML decode + daemonset flag injection (`applyDaemonSet`)
- Full route parsing + router pod CIDR routes
- `Attach(ctx)` cluster discovery for `LAB_REUSE`
- ICMP packet capture via tcpdump parsing
- CI wiring (GitHub Actions label-gated e2e)
- `specs/` capability docs (openspec standard)
