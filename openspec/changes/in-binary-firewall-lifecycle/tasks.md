<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# In-binary Firewall Lifecycle — Tasks

**Goal:** Move NFLOG rule creation/deletion and `rp_filter` sysctl setup into the go-pmtud binary so the `scratch` runtime image needs no shell, and remove the init container and `preStop` hook from the helm chart.

**Architecture:** A new `internal/firewall` package exposes a `Manager` with `Setup()` (called before `mgr.Start`) and `Teardown()` (deferred, runs on SIGTERM). `Setup` writes `rp_filter=0` to `/proc/sys` and creates a dedicated `pmtud` nftables table with one NFLOG rule; `Teardown` deletes that table. Both repos (`go-pmtud` and `sapcc-helm-charts`) change together.

**Tech Stack:** `github.com/google/nftables v0.3.0` (already added to `go.mod`), `golang.org/x/sys/unix` for `NFTA_LOG_*` constants.

## Global Constraints

- Go 1.26 (`go.mod`)
- License header required on every new `.go` file: `// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company\n// SPDX-License-Identifier: Apache-2.0`
- `CGO_ENABLED=0` — no cgo; all dependencies must be pure-Go
- `github.com/google/nftables v0.3.0` is the nftables library (already in `go.mod` as indirect; promote to direct in Task 1)
- Tests run unprivileged in CI (`make build/cover.out` on `ubuntu-latest`); integration tests touching the kernel must be guarded with `testing.Short()` skip or a root-check skip
- Both repos must land together to avoid duplicate NFLOG rules during rollout

---

### Task 1: Add `google/nftables` as a direct dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `github.com/google/nftables v0.3.0` available as a direct (non-`// indirect`) import

- [ ] **Step 1: Promote nftables to direct dependency**

```bash
go get github.com/google/nftables@v0.3.0
go mod tidy
```

- [ ] **Step 2: Verify `go.mod` entry is not marked indirect**

```bash
grep nftables go.mod
# Expected: github.com/google/nftables v0.3.0   (no "// indirect")
```

- [ ] **Step 3: Build to confirm no compilation errors**

```bash
go build ./...
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add github.com/google/nftables as direct dependency"
```

---

### Task 2: `internal/firewall` package — sysctl writer

**Files:**
- Create: `internal/firewall/sysctl.go`
- Create: `internal/firewall/sysctl_test.go`

**Interfaces:**
- Produces: `writeSysctl(fsRoot, path string, value int) error` — writes `value` as a decimal string to `fsRoot+path`. Used by `Manager.Setup` (Task 3).

The sysctl paths for the rule are:
- `net/ipv4/conf/all/rp_filter`
- `net/ipv4/conf/<iface>/rp_filter` for each `cfg.InterfaceNames`

- [ ] **Step 1: Write failing test**

Create `internal/firewall/sysctl_test.go`:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteSysctl(t *testing.T) {
	root := t.TempDir()
	path := "net/ipv4/conf/all/rp_filter"
	full := filepath.Join(root, filepath.FromSlash(path))

	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("1\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := writeSysctl(root, path, 0); err != nil {
		t.Fatalf("writeSysctl: %v", err)
	}

	got, err := os.ReadFile(full)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "0" {
		t.Errorf("got %q, want %q", string(got), "0")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/firewall/ -run TestWriteSysctl -v
# Expected: FAIL — package does not exist yet
```

- [ ] **Step 3: Implement `writeSysctl`**

Create `internal/firewall/sysctl.go`:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeSysctl writes value to fsRoot/path (a /proc/sys-style path, forward-slash separated).
// fsRoot is "/" in production; injectable for tests.
func writeSysctl(fsRoot, path string, value int) error {
	full := filepath.Join(fsRoot, filepath.FromSlash(path))
	return os.WriteFile(full, []byte(fmt.Sprintf("%d", value)), 0644)
}
```

- [ ] **Step 4: Run test to verify it passes**

```bash
go test ./internal/firewall/ -run TestWriteSysctl -v
# Expected: PASS
```

- [ ] **Step 5: Commit**

```bash
git add internal/firewall/sysctl.go internal/firewall/sysctl_test.go
git commit -m "feat(firewall): add sysctl writer with injectable fs root"
```

---

### Task 3: `internal/firewall` package — `Manager` struct, `buildRule`, and unit tests

This task creates the `Manager` type and a pure (kernel-free) `buildRule` helper that constructs the nftables objects. Testing the construction lets CI verify rule correctness without root.

**Files:**
- Create: `internal/firewall/manager.go`
- Create: `internal/firewall/rule.go`
- Create: `internal/firewall/rule_test.go`

**Interfaces:**
- Consumes: `writeSysctl` from Task 2
- Produces:
  - `firewall.New(cfg *config.Config, log logr.Logger) *Manager`
  - `(*Manager).Setup() error`
  - `(*Manager).Teardown() error`
  - `buildNFTObjects(iifname string, nfGroup uint16) (*nftables.Table, *nftables.Chain, *nftables.Rule)` — pure constructor, no kernel I/O; used by `Setup` and tested in `rule_test.go`

- [ ] **Step 1: Write failing unit test for `buildRule`**

Create `internal/firewall/rule_test.go`:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"testing"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

func TestBuildNFTObjects(t *testing.T) {
	table, chain, rule := buildNFTObjects("eth0", 33)

	// table
	if table.Name != "pmtud" {
		t.Errorf("table name: got %q, want %q", table.Name, "pmtud")
	}
	if table.Family != nftables.TableFamilyIPv4 {
		t.Errorf("table family: got %v, want TableFamilyIPv4", table.Family)
	}

	// chain
	if chain.Name != "prerouting" {
		t.Errorf("chain name: got %q, want %q", chain.Name, "prerouting")
	}
	if chain.Type != nftables.ChainTypeFilter {
		t.Errorf("chain type: got %v, want ChainTypeFilter", chain.Type)
	}
	if *chain.Hooknum != *nftables.ChainHookPrerouting {
		t.Errorf("chain hook: got %v, want ChainHookPrerouting", *chain.Hooknum)
	}
	if *chain.Priority != *nftables.ChainPriorityRaw {
		t.Errorf("chain priority: got %v, want ChainPriorityRaw (-300)", *chain.Priority)
	}

	// rule expressions: [meta iifname, cmp iifname, meta l4proto, cmp icmp, payload type, cmp 3, payload code, cmp 4, log]
	if len(rule.Exprs) != 9 {
		t.Fatalf("rule expr count: got %d, want 9", len(rule.Exprs))
	}

	// meta iifname => reg 1
	metaIface, ok := rule.Exprs[0].(*expr.Meta)
	if !ok || metaIface.Key != expr.MetaKeyIIFNAME || metaIface.Register != 1 {
		t.Errorf("expr[0]: want Meta{Key:IIFNAME, Register:1}, got %+v", rule.Exprs[0])
	}

	// cmp eq reg 1 "eth0"
	cmpIface, ok := rule.Exprs[1].(*expr.Cmp)
	if !ok || cmpIface.Op != expr.CmpOpEq || cmpIface.Register != 1 {
		t.Errorf("expr[1]: want Cmp{Op:Eq, Register:1}, got %+v", rule.Exprs[1])
	}
	wantIFName := ifnamePad("eth0")
	for i, b := range wantIFName {
		if cmpIface.Data[i] != b {
			t.Errorf("expr[1].Data[%d]: got %x, want %x", i, cmpIface.Data[i], b)
		}
	}

	// meta l4proto => reg 1
	metaL4, ok := rule.Exprs[2].(*expr.Meta)
	if !ok || metaL4.Key != expr.MetaKeyL4PROTO || metaL4.Register != 1 {
		t.Errorf("expr[2]: want Meta{Key:L4PROTO, Register:1}, got %+v", rule.Exprs[2])
	}

	// cmp eq reg 1 IPPROTO_ICMP
	cmpL4, ok := rule.Exprs[3].(*expr.Cmp)
	if !ok || cmpL4.Op != expr.CmpOpEq || cmpL4.Data[0] != unix.IPPROTO_ICMP {
		t.Errorf("expr[3]: want Cmp{Op:Eq, Data:[1]}, got %+v", rule.Exprs[3])
	}

	// payload network header offset 9 len 1 (ip protocol field) — wait, we already
	// used l4proto for that; exprs[4] is icmp type from transport header offset 0
	payloadType, ok := rule.Exprs[4].(*expr.Payload)
	if !ok || payloadType.Base != expr.PayloadBaseTransportHeader ||
		payloadType.Offset != 0 || payloadType.Len != 1 || payloadType.DestRegister != 1 {
		t.Errorf("expr[4]: want Payload{transport,off=0,len=1,reg=1}, got %+v", rule.Exprs[4])
	}

	// cmp eq reg 1 3 (ICMP type destination-unreachable)
	cmpType, ok := rule.Exprs[5].(*expr.Cmp)
	if !ok || cmpType.Op != expr.CmpOpEq || cmpType.Data[0] != 3 {
		t.Errorf("expr[5]: want Cmp{Op:Eq, Data:[3]}, got %+v", rule.Exprs[5])
	}

	// payload transport header offset 1 len 1 (icmp code)
	payloadCode, ok := rule.Exprs[6].(*expr.Payload)
	if !ok || payloadCode.Base != expr.PayloadBaseTransportHeader ||
		payloadCode.Offset != 1 || payloadCode.Len != 1 || payloadCode.DestRegister != 1 {
		t.Errorf("expr[6]: want Payload{transport,off=1,len=1,reg=1}, got %+v", rule.Exprs[6])
	}

	// cmp eq reg 1 4 (ICMP code frag-needed)
	cmpCode, ok := rule.Exprs[7].(*expr.Cmp)
	if !ok || cmpCode.Op != expr.CmpOpEq || cmpCode.Data[0] != 4 {
		t.Errorf("expr[7]: want Cmp{Op:Eq, Data:[4]}, got %+v", rule.Exprs[7])
	}

	// log group 33
	logExpr, ok := rule.Exprs[8].(*expr.Log)
	if !ok {
		t.Fatalf("expr[8]: want *expr.Log, got %T", rule.Exprs[8])
	}
	if logExpr.Group != 33 {
		t.Errorf("log group: got %d, want 33", logExpr.Group)
	}
	wantKey := uint32(1 << unix.NFTA_LOG_GROUP)
	if logExpr.Key != wantKey {
		t.Errorf("log key: got %d, want %d", logExpr.Key, wantKey)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/firewall/ -run TestBuildNFTObjects -v
# Expected: FAIL — buildNFTObjects not defined
```

- [ ] **Step 3: Implement `rule.go` (pure construction, no kernel I/O)**

Create `internal/firewall/rule.go`:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

const tableName = "pmtud"

// ifnamePad pads a network interface name to 16 bytes (null-terminated), matching
// the kernel's IFNAMSIZ representation used by nftables meta iifname comparisons.
func ifnamePad(name string) []byte {
	b := make([]byte, 16)
	copy(b, name+"\x00")
	return b
}

// buildNFTObjects constructs the nftables table, chain, and rule for the PMTUD
// NFLOG rule. No kernel I/O; safe to call in tests.
//
// Equivalent shell: iptables-nft -t raw -I PREROUTING -i <iifname> -p icmp
//
//	--icmp-type 3/4 -j NFLOG --nflog-group <nfGroup>
func buildNFTObjects(iifname string, nfGroup uint16) (*nftables.Table, *nftables.Chain, *nftables.Rule) {
	table := &nftables.Table{
		Family: nftables.TableFamilyIPv4,
		Name:   tableName,
	}
	chain := &nftables.Chain{
		Name:     "prerouting",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityRaw,
	}
	rule := &nftables.Rule{
		Table: table,
		Chain: chain,
		Exprs: []expr.Any{
			// meta load iifname => reg 1
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			// cmp eq reg 1 <iifname padded to 16 bytes>
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ifnamePad(iifname)},
			// meta load l4proto => reg 1
			&expr.Meta{Key: expr.MetaKeyL4PROTO, Register: 1},
			// cmp eq reg 1 IPPROTO_ICMP (1)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.IPPROTO_ICMP}},
			// payload load 1b @ transport header + 0 => reg 1  (ICMP type)
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
			// cmp eq reg 1 3  (destination-unreachable)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{3}},
			// payload load 1b @ transport header + 1 => reg 1  (ICMP code)
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 1, Len: 1},
			// cmp eq reg 1 4  (fragmentation needed)
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{4}},
			// log group <nfGroup>  (non-terminating NFLOG)
			&expr.Log{
				Key:   uint32(1 << unix.NFTA_LOG_GROUP),
				Group: nfGroup,
			},
		},
	}
	return table, chain, rule
}
```

- [ ] **Step 4: Implement `manager.go`**

Create `internal/firewall/manager.go`:

```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package firewall

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/nftables"

	"github.com/sapcc/go-pmtud/internal/config"
)

// Manager owns the host-level firewall state required by go-pmtud:
// rp_filter=0 on the relevant interfaces, and the NFLOG nftables rule.
type Manager struct {
	cfg    *config.Config
	log    logr.Logger
	fsRoot string // injectable for tests; "/" in production
}

// New returns a Manager. In production pass cfg and log; fsRoot is set to "/".
func New(cfg *config.Config, log logr.Logger) *Manager {
	return &Manager{cfg: cfg, log: log, fsRoot: "/"}
}

// Setup sets rp_filter=0 and installs the NFLOG nftables rule.
// Must be called after cfg.DefaultInterface and cfg.InterfaceNames are populated
// (i.e. after preRunRootCmd).
func (m *Manager) Setup() error {
	if err := m.setupSysctl(); err != nil {
		return fmt.Errorf("firewall sysctl setup: %w", err)
	}
	if err := m.setupNFT(); err != nil {
		return fmt.Errorf("firewall nft setup: %w", err)
	}
	m.log.Info("firewall ready", "iifname", m.cfg.DefaultInterface, "nflog_group", m.cfg.NfGroup)
	return nil
}

// Teardown deletes the pmtud nftables table. rp_filter is not restored.
// Safe to call even if Setup was never called or failed partway through.
func (m *Manager) Teardown() error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("firewall teardown: nftables.New: %w", err)
	}
	table, _, _ := buildNFTObjects(m.cfg.DefaultInterface, m.cfg.NfGroup)
	conn.DelTable(table)
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("firewall teardown: flush: %w", err)
	}
	m.log.Info("firewall torn down")
	return nil
}

func (m *Manager) setupSysctl() error {
	paths := []string{"net/ipv4/conf/all/rp_filter"}
	for _, iface := range m.cfg.InterfaceNames {
		paths = append(paths, fmt.Sprintf("net/ipv4/conf/%s/rp_filter", iface))
	}
	for _, p := range paths {
		m.log.Info("setting sysctl", "path", p, "value", 0)
		if err := writeSysctl(m.fsRoot, p, 0); err != nil {
			return fmt.Errorf("write %s: %w", p, err)
		}
	}
	return nil
}

func (m *Manager) setupNFT() error {
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("nftables.New: %w", err)
	}
	table, chain, rule := buildNFTObjects(m.cfg.DefaultInterface, m.cfg.NfGroup)
	// Delete any existing pmtud table first to make Setup idempotent across restarts.
	conn.DelTable(table)
	if err := conn.Flush(); err != nil {
		// Ignore "no such table" — it just means we're starting fresh.
		m.log.V(1).Info("pre-cleanup flush (ignore if table didn't exist)", "err", err)
	}
	// Fresh connection after the delete flush.
	conn, err = nftables.New()
	if err != nil {
		return fmt.Errorf("nftables.New (post-cleanup): %w", err)
	}
	conn.AddTable(table)
	conn.AddChain(chain)
	conn.AddRule(rule)
	if err := conn.Flush(); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	return nil
}
```

- [ ] **Step 5: Run tests**

```bash
go test ./internal/firewall/ -v
# Expected: all PASS (sysctl + buildNFTObjects tests; no kernel needed)
```

- [ ] **Step 6: Build the whole module**

```bash
go build ./...
```

- [ ] **Step 7: Commit**

```bash
git add internal/firewall/
git commit -m "feat(firewall): Manager with sysctl setup and nftables NFLOG rule lifecycle"
```

---

### Task 4: Wire `Manager` into `runRootCmd`

**Files:**
- Modify: `internal/cmd/command.go`

**Interfaces:**
- Consumes: `firewall.New`, `(*Manager).Setup`, `(*Manager).Teardown` from Task 3

The existing `runRootCmd` calls `mgr.Start(signals.SetupSignalHandler())` which blocks until SIGTERM. The deferred `Teardown` runs immediately after `mgr.Start` returns.

- [ ] **Step 1: Edit `internal/cmd/command.go`**

Add the import and the setup/teardown block. The diff below shows exactly what changes:

Add to the import block:
```go
"github.com/sapcc/go-pmtud/internal/firewall"
```

In `runRootCmd`, insert after the `ctrl.SetLogger(log)` line and before `managerOpts := ...`:

```go
fw := firewall.New(&cfg, log.WithName("firewall"))
if err := fw.Setup(); err != nil {
    log.Error(err, "firewall setup failed")
    return err
}
defer func() {
    if err := fw.Teardown(); err != nil {
        log.Error(err, "firewall teardown failed")
    }
}()
```

The full updated `runRootCmd` beginning should look like:

```go
func runRootCmd(cmd *cobra.Command, args []string) error {
	log := zap.New(func(o *zap.Options) {
		o.Development = true
	}).WithName("runRoot")
	ctrl.SetLogger(log)

	fw := firewall.New(&cfg, log.WithName("firewall"))
	if err := fw.Setup(); err != nil {
		log.Error(err, "firewall setup failed")
		return err
	}
	defer func() {
		if err := fw.Teardown(); err != nil {
			log.Error(err, "firewall teardown failed")
		}
	}()

	managerOpts := manager.Options{
	// ... rest unchanged
```

- [ ] **Step 2: Build to verify it compiles**

```bash
go build ./...
```

- [ ] **Step 3: Run all tests**

```bash
go test ./...
```

- [ ] **Step 4: Commit**

```bash
git add internal/cmd/command.go
git commit -m "feat(cmd): wire firewall.Manager Setup/Teardown around mgr.Start"
```

---

### Task 5: Helm chart — remove init container, preStop, configmap, and iptables image

Both files live in `/system/go-pmtud/`.

**Files:**
- Modify: `templates/go-pmtud-daemonset.yaml`
- Delete: `templates/go-pmtud-configmap.yaml`
- Delete: `templates/etc/_iptables_init.tpl`
- Delete: `templates/etc/_iptables_stop.tpl`
- Modify: `values.yaml`

**Interfaces:**
- Consumes: the daemonset and values files as they exist in the repo
- Produces: a daemonset with no initContainers, no lifecycle.preStop, no configmap volume, and no iptables image reference

- [ ] **Step 1: Remove the init container block from `templates/go-pmtud-daemonset.yaml`**

Delete these lines entirely from the `spec.template.spec` section:

```yaml
      initContainers:
        - name: iptables-init
          image: "{{ required ".Values.images.iptables.image is missing" .Values.images.iptables.image }}:{{ required ".Values.images.iptables.image is missing" .Values.images.iptables.tag }}"
          command:
            - /scripts/pmtud/iptables-init.sh
          securityContext:
            privileged: true
          volumeMounts:
            - name: pmtud
              mountPath: /scripts/pmtud
```

- [ ] **Step 2: Remove the `preStop` hook and `volumeMounts` from the pmtud container**

In the `containers[0]` (pmtud) section, delete the `lifecycle` block:

```yaml
        lifecycle:
          preStop:
            exec:
              command: ["/scripts/pmtud/iptables-stop.sh"]
```

And delete its `volumeMounts` block:

```yaml
          volumeMounts:
            - name: pmtud
              mountPath: /scripts/pmtud
```

- [ ] **Step 3: Remove the configmap volume from `spec.template.spec.volumes`**

Delete:

```yaml
        - name: pmtud
          configMap:
            name: pmtud
            defaultMode: 0744
```

- [ ] **Step 4: Delete template files no longer needed**

```bash
rm /system/go-pmtud/templates/go-pmtud-configmap.yaml
rm /system/go-pmtud/templates/etc/_iptables_init.tpl
rm /system/go-pmtud/templates/etc/_iptables_stop.tpl
rmdir /system/go-pmtud/templates/etc
```

- [ ] **Step 5: Remove `images.iptables` from `values.yaml`**

Delete the `iptables` entry under `images:`:

```yaml
  iptables:
    tag: v20241210113345
```

After deletion the `images` block should contain only:

```yaml
images:
  pmtud:
    tag: sha-2d470b4e0140b4b6ac7ab33eb727af1cec7e4907
```

- [ ] **Step 6: Verify the daemonset template is valid YAML (no helm rendering needed)**

```bash
python3 -c "import yaml, open; yaml.safe_load(open('/system/go-pmtud/templates/go-pmtud-daemonset.yaml').read())" 2>&1 || echo "Note: helm template expressions cause YAML parse errors; check manually for structural issues instead"
# For a structural sanity check without helm:
grep -n 'initContainer\|preStop\|iptables-init\|iptables-stop\|pmtud-configmap\|images.iptables' \
  /system/go-pmtud/templates/go-pmtud-daemonset.yaml
# Expected: no output
```

- [ ] **Step 7: Commit in the helm-charts repo**

```bash
cd
git add system/go-pmtud/
git commit -m "feat(go-pmtud): remove init container and preStop hook (rule lifecycle now in binary)"
```

---

### Task 6: Add REUSE/license header to new Go files and run `go mod tidy`

CI runs `check-license-headers` and `check-dependency-licenses`; this task ensures the new files pass.

**Files:**
- Verify: `internal/firewall/sysctl.go`, `internal/firewall/sysctl_test.go`, `internal/firewall/manager.go`, `internal/firewall/rule.go`, `internal/firewall/rule_test.go`

- [ ] **Step 1: Verify all new files have correct SPDX headers**

```bash
grep -L 'SPDX-FileCopyrightText' internal/firewall/*.go
# Expected: no output (all files have the header)
```

- [ ] **Step 2: Run `go mod tidy` to remove any unused indirect entries**

```bash
go mod tidy
```

- [ ] **Step 3: Build and test one final time**

```bash
go build ./...
go test ./...
```

- [ ] **Step 4: Commit if go.mod/go.sum changed**

```bash
git diff --quiet go.mod go.sum || git commit -m "chore: go mod tidy" go.mod go.sum
```
