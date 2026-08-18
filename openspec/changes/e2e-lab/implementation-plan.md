<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# E2E Lab Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Convert the existing two-cluster + router e2e lab into a single-cluster lab whose control-plane acts as a low-MTU forwarding hop, triggered by `ping -M do`, proving PMTU replication reaches non-originating peers for both relay backends.

**Architecture:** One Kind cluster (1 control-plane + 2 workers). The control-plane enables forwarding and owns a `dummy` interface (`veth` fallback) clamped to MTU 1280 on a blackhole subnet. A worker pings that subnet with the DF bit set; the control-plane returns a real ICMP frag-needed the worker's NFLOG rule captures; go-pmtud relays it to the peer worker, whose kernel PMTU converges. Asserted via `ip route get`.

**Tech Stack:** Go 1.26, `sigs.k8s.io/kind` (Kind Go API), controller-runtime client, Ginkgo/Gomega, `docker exec` of `ip`/`ping`/`sysctl`. All `lab/` + `test/e2e/` files carry `//go:build e2e` (except `lab/cmd/labctl`).

## Global Constraints

- SPDX headers on every new file: `// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company` + `// SPDX-License-Identifier: Apache-2.0` (`#`-comments for YAML).
- `//go:build e2e` tag on all `lab/` and `test/e2e/` `.go` files except `lab/cmd/labctl/main.go`.
- Never edit the generated root `Makefile`; only the hand-maintained `lab/Makefile`.
- Blackhole/hop constants are fixed: cluster name `pmtud`, iface `pmtudlab0`, hop IP `10.99.0.1/24`, blackhole IP `10.99.0.2`, hop MTU `1280`, ping size `1400`.
- Pure-helper tests run without docker via `go test -tags e2e ./lab/ -run <Name>`; infra funcs are validated only by `make -C lab e2e` (Task 10).
- `go test ./...` (no tag) must stay green — the suite is excluded by the build tag.

---

### Task 1: Single-cluster Kind config

**Files:**
- Create: `lab/configs/kind-cluster.yaml`
- Delete: `lab/configs/kind-cluster-a.yaml`, `lab/configs/kind-cluster-b.yaml`

- [ ] **Step 1: Create the config**

```yaml
# SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
# SPDX-License-Identifier: Apache-2.0
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: pmtud
networking:
  disableDefaultCNI: false
  podSubnet: "10.244.0.0/16"
  serviceSubnet: "10.96.0.0/12"
nodes:
  - role: control-plane
  - role: worker
  - role: worker
```

- [ ] **Step 2: Delete the two-cluster configs**

```bash
git rm lab/configs/kind-cluster-a.yaml lab/configs/kind-cluster-b.yaml
```

- [ ] **Step 3: Commit**

```bash
git add lab/configs/kind-cluster.yaml
git commit -m "feat(lab): add single-cluster kind config, drop two-cluster configs"
```

---

### Task 2: Node-line parser + control-plane discovery

**Files:**
- Modify: `lab/cluster.go` (add `parseNodeLines`, `controlPlaneContainer`; use parser in `workerContainers`; set `ControlPlane` in `createCluster`; add `ControlPlane` field to `Cluster` in `lab.go` — see Task 3)
- Modify: `lab/cluster_test.go` (test `parseNodeLines`, drop local `parseWorkerLines`)

**Interfaces:**
- Produces: `parseNodeLines(out string) []string`; `controlPlaneContainer(clusterName string) string`; `Cluster.ControlPlane string`.

- [ ] **Step 1: Rewrite the test to target `parseNodeLines`**

Replace the body of `lab/cluster_test.go` (keep header + build tag):

```go
package lab

import "testing"

func TestParseNodeLines(t *testing.T) {
	got := parseNodeLines("pmtud-worker\npmtud-worker2\n")
	if len(got) != 2 || got[0] != "pmtud-worker" || got[1] != "pmtud-worker2" {
		t.Fatalf("parseNodeLines = %v, want [pmtud-worker pmtud-worker2]", got)
	}
	if len(parseNodeLines("  \n")) != 0 {
		t.Errorf("parseNodeLines on blank input should be empty")
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -tags e2e ./lab/ -run TestParseNodeLines -v`
Expected: FAIL — `undefined: parseNodeLines`.

- [ ] **Step 3: Add `parseNodeLines` + `controlPlaneContainer` and use them in `cluster.go`**

Add to `lab/cluster.go`:

```go
func parseNodeLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func controlPlaneContainer(clusterName string) string {
	out, err := exec.Command("docker", "ps",
		"--filter", "label=io.x-k8s.kind.cluster="+clusterName,
		"--filter", "label=io.x-k8s.kind.role=control-plane",
		"--format", "{{.Names}}").CombinedOutput()
	if err != nil {
		return ""
	}
	if names := parseNodeLines(string(out)); len(names) > 0 {
		return names[0]
	}
	return ""
}
```

Replace the loop body inside `workerContainers` with `return parseNodeLines(string(out))` (drop the manual split loop). In `createCluster`, change the final return to set the control-plane:

```go
	return &Cluster{
		Name:           name,
		KubeconfigPath: kcPath,
		Client:         cl,
		Workers:        workerContainers(name),
		ControlPlane:   controlPlaneContainer(name),
	}, nil
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test -tags e2e ./lab/ -run TestParseNodeLines -v`
Expected: PASS. (Compilation of `Cluster.ControlPlane` depends on Task 3's struct change; if building the whole package now fails on the missing field, complete Task 3 Step 1 first — they commit together.)

- [ ] **Step 5: Commit**

```bash
git add lab/cluster.go lab/cluster_test.go
git commit -m "feat(lab): add control-plane discovery and shared node-line parser"
```

---

### Task 3: Single-cluster lifecycle (`lab.go`) + delete two-cluster plumbing

**Files:**
- Modify: `lab/lab.go` (rewrite `Cluster`/`Lab` structs, `Provision`, `Teardown`; add constants)
- Delete: `lab/network.go`, `lab/network_test.go`, `lab/router.go`

**Interfaces:**
- Consumes: `createCluster`, `deleteCluster`, `controlPlaneContainer` (Task 2); `configureHop` (Task 4).
- Produces: `Lab{ Cluster *Cluster; Hop string; BlackholeIP string }`; `Cluster.ControlPlane string`; constants `ClusterName`, `HopIfaceName`, `HopIP`, `HopSubnet`, `BlackholeIP` (const `blackholeIP`), `HopMTU`.

- [ ] **Step 1: Rewrite `lab/lab.go`**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterName  = "pmtud"
	HopIfaceName = "pmtudlab0"
	HopIP        = "10.99.0.1"
	HopSubnet    = "10.99.0.0/24"
	blackholeIP  = "10.99.0.2"
	HopMTU       = 1280
)

type Cluster struct {
	Name           string
	KubeconfigPath string
	Client         client.Client
	Workers        []string
	ControlPlane   string // docker container of the control-plane node (low-MTU hop)
}

type Lab struct {
	Cluster     *Cluster
	BlackholeIP string
}

func Provision(ctx context.Context) (*Lab, error) {
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}
	config := filepath.Join(repoRoot, "lab/configs/kind-cluster.yaml")

	c, err := createCluster(ctx, ClusterName, config)
	if err != nil {
		return nil, err
	}
	if c.ControlPlane == "" {
		return nil, fmt.Errorf("no control-plane node discovered for cluster %s", ClusterName)
	}
	if len(c.Workers) < 2 {
		return nil, fmt.Errorf("need >=2 workers, found %d", len(c.Workers))
	}

	l := &Lab{Cluster: c, BlackholeIP: blackholeIP}

	if err := configureHop(ctx, l); err != nil {
		return nil, err
	}
	if err := ensurePing(l.Cluster.Workers[0]); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Lab) Teardown(ctx context.Context) error {
	if os.Getenv("LAB_KEEP") != "" {
		return nil
	}
	return deleteCluster(ctx, ClusterName)
}

func Attach(ctx context.Context) (*Lab, error) {
	// TODO: discover a running lab for LAB_REUSE; unchanged pre-existing limitation.
	return nil, fmt.Errorf("not implemented")
}
```

- [ ] **Step 2: Delete two-cluster plumbing**

```bash
git rm lab/network.go lab/network_test.go lab/router.go
```

- [ ] **Step 3: Build (expect failures pointing at Task 4/5 funcs)**

Run: `go build -tags e2e ./lab/... 2>&1 | head`
Expected: errors only for `undefined: configureHop` and `undefined: ensurePing` (implemented next). No errors about `network`/`router`/`clusterContainers`.

- [ ] **Step 4: Commit**

```bash
git add lab/lab.go
git commit -m "feat(lab): single-cluster lifecycle, remove docker networks and router"
```

---

### Task 4: Forwarding hop + route (`routes.go`)

**Files:**
- Modify: `lab/routes.go` (replace two-cluster routes/offloads with hop setup)
- Modify: `lab/exec.go` (add `containerIP`; remove now-unused `ifaceByIP`, `ipOnSubnet`)

**Interfaces:**
- Consumes: `dockerExec`, `run` (exec.go); `Lab`, `HopIfaceName`, `HopIP`, `HopSubnet`, `HopMTU` (Task 3).
- Produces: `configureHop(ctx context.Context, l *Lab) error`; `createHopIface(node string) error`; `containerIP(name string) (string, error)`; `ensurePing(node string) error`.

- [ ] **Step 1: Add `containerIP` to `exec.go`, drop unused helpers**

Add:

```go
// containerIP returns the first docker-network IP of a container.
func containerIP(name string) (string, error) {
	out, err := exec.Command("docker", "inspect", "-f",
		"{{range .NetworkSettings.Networks}}{{.IPAddress}} {{end}}", name).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w: %s", name, err, string(out))
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		return "", fmt.Errorf("no IP for container %s", name)
	}
	return fields[0], nil
}
```

Delete `ifaceByIP` and `ipOnSubnet` from `exec.go` (unused after the router/dest-discovery removal). Keep `run` and `dockerExec`. Ensure imports remain `fmt`, `os`, `os/exec`, `strings`.

- [ ] **Step 2: Replace `lab/routes.go`**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"strconv"
)

// configureHop turns the control-plane node into a low-MTU forwarding hop and
// routes the blackhole subnet from worker-A through it, so a DF-set ping from
// worker-A elicits a real ICMP frag-needed the NFLOG rule can capture.
func configureHop(ctx context.Context, l *Lab) error {
	cp := l.Cluster.ControlPlane

	if _, err := dockerExec(cp, "sysctl", "-w", "net.ipv4.ip_forward=1"); err != nil {
		return fmt.Errorf("enable ip_forward on hop: %w", err)
	}
	if err := createHopIface(cp); err != nil {
		return err
	}
	if _, err := dockerExec(cp, "ip", "addr", "replace", HopIP+"/24", "dev", HopIfaceName); err != nil {
		return fmt.Errorf("assign hop address: %w", err)
	}

	cpIP, err := containerIP(cp)
	if err != nil {
		return err
	}
	worker := l.Cluster.Workers[0]
	if _, err := dockerExec(worker, "ip", "route", "replace", HopSubnet, "via", cpIP); err != nil {
		return fmt.Errorf("route worker-A -> hop subnet: %w", err)
	}
	return nil
}

// createHopIface creates pmtudlab0 clamped to HopMTU. Prefers a dummy device;
// falls back to a veth pair (far end left down) if the dummy module is absent.
func createHopIface(node string) error {
	dockerExec(node, "ip", "link", "del", HopIfaceName) // best-effort cleanup

	if _, err := dockerExec(node, "ip", "link", "add", HopIfaceName, "type", "dummy"); err != nil {
		if _, err2 := dockerExec(node, "ip", "link", "add", HopIfaceName,
			"type", "veth", "peer", "name", HopIfaceName+"p"); err2 != nil {
			return fmt.Errorf("create hop iface (dummy: %v; veth: %w)", err, err2)
		}
	}
	if _, err := dockerExec(node, "ip", "link", "set", HopIfaceName, "mtu", strconv.Itoa(HopMTU)); err != nil {
		return fmt.Errorf("set hop mtu: %w", err)
	}
	if _, err := dockerExec(node, "ip", "link", "set", HopIfaceName, "up"); err != nil {
		return fmt.Errorf("bring up hop iface: %w", err)
	}
	return nil
}

// ensurePing installs iputils-ping on the given node if ping is missing.
func ensurePing(node string) error {
	if err := run("docker", "exec", node, "sh", "-c",
		"command -v ping >/dev/null 2>&1 || (apt-get update -qq && apt-get install -y --no-install-recommends iputils-ping)"); err != nil {
		return fmt.Errorf("ensure ping on %s: %w", node, err)
	}
	return nil
}
```

- [ ] **Step 3: Build + vet**

Run: `go build -tags e2e ./lab/... && go vet -tags e2e ./lab/...`
Expected: clean except `undefined: GenerateTraffic`/`PMTUTo` references (still present from old ops.go — old ops.go still compiles until Task 5, so this should be clean now). If old `ops.go` references removed symbols, finish Task 5 before building.

- [ ] **Step 4: Commit**

```bash
git add lab/routes.go lab/exec.go
git commit -m "feat(lab): configure control-plane as low-MTU forwarding hop"
```

---

### Task 5: Ping trigger + PMTU inspection (`ops.go`)

**Files:**
- Modify: `lab/ops.go` (rewrite `GenerateTraffic`; add `parsePingFragNeeded`; keep `PMTUTo`, `FlushRouteCache`; delete `CaptureICMPAsync` + `ICMPCapture`)
- Create: `lab/ops_test.go` (table tests for `parsePingFragNeeded` and the `PMTUTo` MTU parse)

**Interfaces:**
- Consumes: `Lab`, `Cluster.Workers`, `BlackholeIP` (Task 3).
- Produces: `parsePingFragNeeded(out string) int`; `(*Lab).GenerateTraffic(ctx) error`; `(*Lab).PMTUTo(node, dst string) (int, error)`; `(*Lab).FlushRouteCache(node string) error`.

- [ ] **Step 1: Write the failing parser tests (`lab/ops_test.go`)**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import "testing"

func TestParsePingFragNeeded(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want int
	}{
		{"iputils", "From 10.99.0.1 icmp_seq=1 Frag needed and DF set (mtu = 1280)\n", 1280},
		{"nospace", "frag needed (mtu=1400)", 1400},
		{"none", "3 packets transmitted, 3 received\n", 0},
	}
	for _, tt := range tests {
		if got := parsePingFragNeeded(tt.out); got != tt.want {
			t.Errorf("%s: parsePingFragNeeded = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestParseRouteMTU(t *testing.T) {
	got := parseRouteMTU("10.99.0.2 via 172.18.0.4 dev eth0 mtu 1280\n")
	if got != 1280 {
		t.Errorf("parseRouteMTU = %d, want 1280", got)
	}
	if parseRouteMTU("10.99.0.2 dev eth0\n") != 0 {
		t.Errorf("parseRouteMTU with no mtu should be 0")
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test -tags e2e ./lab/ -run 'TestParsePingFragNeeded|TestParseRouteMTU' -v`
Expected: FAIL — `undefined: parsePingFragNeeded` / `parseRouteMTU`.

- [ ] **Step 3: Rewrite `lab/ops.go`**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os/exec"
)

// GenerateTraffic sends a DF-set ping larger than the hop MTU from worker-A.
// The hop returns an ICMP frag-needed; ping exits non-zero (blackhole), so the
// reported "mtu = N" line — not the exit code — is the success signal.
func (l *Lab) GenerateTraffic(ctx context.Context) error {
	if len(l.Cluster.Workers) == 0 {
		return fmt.Errorf("no workers")
	}
	worker := l.Cluster.Workers[0]
	b, _ := exec.CommandContext(ctx, "docker", "exec", worker,
		"ping", "-M", "do", "-s", "1400", "-c", "3", "-W", "2", l.BlackholeIP).CombinedOutput()
	if parsePingFragNeeded(string(b)) > 0 {
		return nil
	}
	return fmt.Errorf("no ICMP frag-needed in ping output: %s", string(b))
}

// parsePingFragNeeded returns the MTU reported in a ping frag-needed line, or 0.
func parsePingFragNeeded(out string) int {
	return firstIntAfter(out, "mtu")
}

func (l *Lab) PMTUTo(node, dst string) (int, error) {
	out, err := dockerExec(node, "ip", "route", "get", dst)
	if err != nil {
		return 0, err
	}
	if m := parseRouteMTU(out); m > 0 {
		return m, nil
	}
	return 0, fmt.Errorf("no MTU in route output: %s", out)
}

func parseRouteMTU(out string) int {
	return firstIntAfter(out, "mtu")
}

// firstIntAfter finds `key`, skips non-digits, and returns the first integer run
// (0 if none). Handles both "mtu 1280" and "mtu=1280".
func firstIntAfter(s, key string) int {
	i := indexOf(s, key)
	if i == -1 {
		return 0
	}
	j := i + len(key)
	for j < len(s) && (s[j] < '0' || s[j] > '9') {
		j++
	}
	n, started := 0, false
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		n = n*10 + int(s[j]-'0')
		j++
		started = true
	}
	if !started {
		return 0
	}
	return n
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func (l *Lab) FlushRouteCache(node string) error {
	dockerExec(node, "ip", "route", "flush", "cache") // best-effort; no-op on kernels without route cache
	return nil
}
```

- [ ] **Step 4: Run the parser tests to verify pass**

Run: `go test -tags e2e ./lab/ -run 'TestParsePingFragNeeded|TestParseRouteMTU' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add lab/ops.go lab/ops_test.go
git commit -m "feat(lab): ping -M do trigger and route-cache MTU inspection"
```

---

### Task 6: Deploy without the podinfo workload (`deploy.go`)

**Files:**
- Modify: `lab/deploy.go` (single cluster; delete `deployWorkload`)
- Delete: `lab/manifests/podinfo.yaml`

**Interfaces:**
- Consumes: `Lab.Cluster`, `Cluster.applyFile`, `Cluster.applyDaemonSet`, `Cluster.waitRollout`.
- Produces: `(*Lab).DeployBackend(ctx, backend string) error`.

- [ ] **Step 1: Rewrite `lab/deploy.go`**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package lab

import (
	"context"
	"fmt"
	"os"
)

func (l *Lab) DeployBackend(ctx context.Context, backend string) error {
	repoRoot := os.Getenv("REPO_ROOT")
	if repoRoot == "" {
		repoRoot = "."
	}
	if err := run("docker", "build", "-t", "go-pmtud:local", repoRoot); err != nil {
		return fmt.Errorf("build go-pmtud image: %w", err)
	}

	c := l.Cluster
	if err := run("kind", "load", "docker-image", "go-pmtud:local", "--name", c.Name); err != nil {
		return fmt.Errorf("load image to %s: %w", c.Name, err)
	}
	if backend == "crd" {
		if err := c.applyFile(ctx, repoRoot+"/crd/pmtud.cloud.sap_pmtunoderelays.yaml"); err != nil {
			return fmt.Errorf("apply CRD: %w", err)
		}
	}
	if err := c.applyFile(ctx, repoRoot+"/lab/manifests/rbac.yaml"); err != nil {
		return fmt.Errorf("apply RBAC: %w", err)
	}
	if err := c.applyDaemonSet(ctx, repoRoot+"/lab/manifests/pmtud-daemonset.yaml", backend); err != nil {
		return fmt.Errorf("apply daemonset: %w", err)
	}
	if err := c.waitRollout(ctx, "kube-system", "go-pmtud"); err != nil {
		return fmt.Errorf("wait rollout: %w", err)
	}
	return nil
}
```

- [ ] **Step 2: Delete the workload manifest**

```bash
git rm lab/manifests/podinfo.yaml
```

- [ ] **Step 3: Build + vet the whole package**

Run: `go build -tags e2e ./... && go vet -tags e2e ./...`
Expected: clean (the `lab` package now has no dangling references; `test/e2e` still references old field names — fixed in Task 7).

- [ ] **Step 4: Commit**

```bash
git add lab/deploy.go
git commit -m "feat(lab): deploy to single cluster, drop podinfo workload"
```

---

### Task 7: Update the suite specs (`test/e2e/`)

**Files:**
- Modify: `test/e2e/pmtu_test.go` (single cluster; assert 1280 on all workers; drop ICMP-count assertion)
- Modify: `test/e2e/config_test.go` (`ClusterA` → `Cluster`)

**Interfaces:**
- Consumes: `testLab.Cluster.Workers`, `testLab.Cluster.Client`, `testLab.BlackholeIP`, `GenerateTraffic`, `FlushRouteCache`, `PMTUTo`, `DeployBackend`.

- [ ] **Step 1: Rewrite `test/e2e/pmtu_test.go`**

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

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
				for _, w := range testLab.Cluster.Workers {
					gomega.Expect(testLab.FlushRouteCache(w)).To(gomega.Succeed())
				}

				// ping errors unless the hop returned a frag-needed
				gomega.Expect(testLab.GenerateTraffic(ctx)).To(gomega.Succeed())

				// originator converges natively; peers converge via the relay
				for _, w := range testLab.Cluster.Workers {
					gomega.Eventually(func() (int, error) {
						return testLab.PMTUTo(w, testLab.BlackholeIP)
					}).
						WithTimeout(30 * time.Second).
						WithPolling(2 * time.Second).
						Should(gomega.Equal(1280),
							"worker %s PMTU must converge to 1280 via %s relay", w, backend)
				}
			})
		})
	}
})
```

- [ ] **Step 2: Update `test/e2e/config_test.go`**

Change the client access from the two-cluster field to the single cluster:

```go
gomega.Expect(testLab.Cluster.Client.Get(ctx,
	client.ObjectKey{Namespace: "kube-system", Name: "go-pmtud"}, &ds)).
	To(gomega.Succeed())
```

(Leave the rest of `config_test.go` unchanged.)

- [ ] **Step 3: Build + vet with the tag**

Run: `go build -tags e2e ./... && go vet -tags e2e ./...`
Expected: clean.

- [ ] **Step 4: Confirm the untagged build is unaffected**

Run: `go test ./...`
Expected: PASS (e2e suite excluded).

- [ ] **Step 5: Commit**

```bash
git add test/e2e/pmtu_test.go test/e2e/config_test.go
git commit -m "test(e2e): assert single-cluster peer PMTU convergence via ping trigger"
```

---

### Task 8: Makefile cleanup

**Files:**
- Modify: `lab/Makefile` (`down` target; remove `observe-router`)

**Interfaces:** none (make targets).

- [ ] **Step 1: Replace the `down` target and drop `observe-router`**

In `.PHONY`, remove `observe-router`. Replace the `down` recipe with:

```make
down: ## Remove lab (single kind cluster)
	kind delete cluster --name pmtud 2>/dev/null || true
```

Delete the `observe-router:` target block (the router is gone). Leave `observe-node`, `observe-replication`, `status`, and the `e2e*` targets unchanged.

- [ ] **Step 2: Sanity-check make parses**

Run: `make -C lab help`
Expected: target list prints without error; no `observe-router`.

- [ ] **Step 3: Commit**

```bash
git add lab/Makefile
git commit -m "chore(lab): point down target at single cluster, drop observe-router"
```

---

### Task 9: README + real-cluster runbook

**Files:**
- Create: `lab/RUNBOOK-real-cluster.md`
- Modify: `lab/README.md` (rewrite for single-cluster topology; link runbook)

**Interfaces:** none (docs).

- [ ] **Step 1: Write `lab/RUNBOOK-real-cluster.md`**

Content (normal prose, not caveman) covering, in order: prerequisites (≥2-node cluster, a real MTU boundary or a node you can add a low-MTU hop to, `kubectl` + node shell); deploy (build/push image, apply RBAC + CRD, apply DaemonSet with `--relay-backend=udp` then `crd`, wait rollout); induce a frag-needed (rely on the existing MTU mismatch, or replicate the lab hop on one node — `sysctl -w net.ipv4.ip_forward=1`, `ip link add pmtudlab0 type dummy`, `ip link set pmtudlab0 mtu 1280 up`, `ip addr add`, route from another node — then `ping -M do -s 1400 <blackhole-ip>`); observe (`ip route get <dst>` on a **non-originating** node shows the reduced MTU; `RecvPackets` metric increments; log line `ICMP frag-needed received, resending packet.`; for `crd`, `kubectl get pmtunoderelays -A` shows objects appear and get collected); cleanup (delete DaemonSet/RBAC/CRD; remove the temporary hop).

Start the file with the `<!-- SPDX ... -->` HTML-comment header.

- [ ] **Step 2: Rewrite `lab/README.md`**

Replace the two-cluster architecture diagram and "Why Two Clusters" / docker-network-MTU limitation sections with: a single-cluster diagram (control-plane as hop + 2 workers), the `ping -M do` trigger explanation, the `make e2e`/`e2e-reuse`/`e2e-keep` usage (unchanged), and a "Real-cluster validation" section linking `RUNBOOK-real-cluster.md`. Keep the prerequisites (docker, kind, kubectl, Go) and remove the router/podinfo references.

- [ ] **Step 3: Commit**

```bash
git add lab/README.md lab/RUNBOOK-real-cluster.md
git commit -m "docs(lab): single-cluster README and real-cluster runbook"
```

---

### Task 10: Full e2e verification

**Files:** none (verification only).

- [ ] **Step 1: Untagged build/test still green**

Run: `go build ./... && go test ./...`
Expected: PASS.

- [ ] **Step 2: Tagged build/vet clean**

Run: `go build -tags e2e ./... && go vet -tags e2e ./...`
Expected: clean.

- [ ] **Step 3: Full lab run, both backends**

Run: `make -C lab e2e`
Expected: provisions one Kind cluster, `PMTU replication` specs pass for `udp` and `crd` (every worker converges to 1280), config specs pass, teardown succeeds. Run on Linux and on macOS Docker Desktop.

- [ ] **Step 4: If a run fails, capture diagnostics**

`LAB_KEEP=1 make -C lab e2e-keep`, then on failure inspect: `docker exec pmtud-worker ip route get 10.99.0.2`, `docker exec pmtud-control-plane ip link show pmtudlab0`, and `kubectl --kubeconfig <temp> logs -n kube-system -l app.kubernetes.io/name=go-pmtud`. Clean up with `make -C lab down`.

- [ ] **Step 5: Final commit if any fixes were needed**

```bash
git add -A
git commit -m "test(e2e): verify single-cluster lab passes udp and crd backends"
```
