<!--
SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company

SPDX-License-Identifier: Apache-2.0
-->

# Pluggable Relay Backends (UDP + CRD) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the inter-node ICMP relay transport pluggable behind a `Relay` interface, refactor the existing UDP path into one backend, and add a Kubernetes CRD backend that needs no new ports.

**Architecture:** Packet capture (NFLOG) and packet injection (TUN `pmtud0`) become shared, transport-agnostic code. A `Relay` interface abstracts the transport with `Send` (capture side) and `Start`/receive (injection side). One backend is active per process, chosen by `--relay-backend`. The CRD backend relays via namespaced `PMTUNodeRelay` objects (broadcast, dedup-by-name, TTL-swept in-daemon).

**Tech Stack:** Go 1.26, controller-runtime, cobra/viper, NFLOG (`github.com/florianl/go-nflog/v2`), controller-gen (via `make generate`), envtest (`setup-envtest`), Kind (`lab/`).

## Global Constraints

- Module path: `github.com/sapcc/go-pmtud`. Copy for new files.
- Every source file starts with the SPDX header:
  `// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company` and
  `// SPDX-License-Identifier: Apache-2.0`.
- Linux-only code (TUN device, syscalls) is gated behind `//go:build linux` with a
  non-linux stub of identical signature. UDP and CRD backends are OS-agnostic (use
  `net` / controller-runtime only) — no build tag.
- Do NOT hand-write CRD YAML or deepcopy. Generate via `make generate`
  (controller-gen scans `./...`, CRDs → `crd/`, RBAC roleName `go-pmtud`).
- Do NOT edit the generated `Makefile`. If build tooling must change, edit
  `Makefile.maker.yaml` and re-run go-makefile-maker.
- CRD: group `pmtud.cloud.sap`, version `v1alpha1`, kind `PMTUNodeRelay`, scope
  Namespaced.
- Preserve existing loop prevention: NFLOG rule `! -i pmtud0`; the capturing node
  never injects its own capture; CRD watchers skip `sourceNode == own node`.
- `--relay-backend` default `udp` (existing behavior unchanged).
- Tests: `go test ./...` must pass on the dev platform. TUN-dependent tests are
  linux+privileged (CI linux runner); guard them so `go test ./...` stays green on
  macOS.
- Commit frequently, one task per commit, Conventional Commit messages.

---

## File Structure

**Create:**
- `api/v1alpha1/groupversion_info.go` — scheme/group-version registration.
- `api/v1alpha1/pmtunoderelay_types.go` — `PMTUNodeRelay` type + kubebuilder markers.
- `internal/relay/relay.go` — `Relay` interface, `RelayPacket`, backend constants, `New` factory, `Deps`.
- `internal/relay/udp.go` — UDP backend (`Send` + `Start`), peer validation (moved from `internal/receiver/peer.go`).
- `internal/relay/crd.go` — CRD backend (`Send` + `Start` + GC).
- `internal/relay/inject_linux.go` — `Injector` (TUN `pmtud0`), moved from `receiver.go`.
- `internal/relay/inject_other.go` — non-linux `Injector` stub.
- `internal/relay/runnable.go` — manager `Runnable`: owns `Injector`, calls `backend.Start`.
- `internal/relay/udp_test.go`, `internal/relay/crd_test.go`, `internal/relay/relay_test.go`.

**Modify:**
- `internal/config/config.go` — add `RelayBackend`, `RelayNamespace`, `RelayGCInterval` (and keep `ReplicationPort` for UDP).
- `internal/cmd/command.go` — new flags, scheme registration, build backend via factory, wire into nflog + add runnable.
- `internal/nflog/controller.go` — drop UDP send loop; call `Relay.Send`.

**Delete (after moves):**
- `internal/receiver/receiver.go`, `receiver_other.go`, `peer.go`, `tun_linux.go`, `receiver_test.go` — content moved into `internal/relay`.

---

### Task 1: PMTUNodeRelay API types + generated artifacts

**Files:**
- Create: `api/v1alpha1/groupversion_info.go`
- Create: `api/v1alpha1/pmtunoderelay_types.go`
- Generated (by `make generate`): `api/v1alpha1/zz_generated.deepcopy.go`, `crd/pmtud.cloud.sap_pmtunoderelays.yaml`

**Interfaces:**
- Produces: `v1alpha1.PMTUNodeRelay{ Spec: PMTUNodeRelaySpec{ SourceNode string, Payload string, ExpiresAt metav1.Time } }`, `v1alpha1.PMTUNodeRelayList`, `v1alpha1.GroupVersion`, `v1alpha1.AddToScheme`, `v1alpha1.SchemeBuilder`.

- [ ] **Step 1: Write the type definitions**

`api/v1alpha1/groupversion_info.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

// +kubebuilder:object:generate=true
// +groupName=pmtud.cloud.sap
package v1alpha1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	GroupVersion  = schema.GroupVersion{Group: "pmtud.cloud.sap", Version: "v1alpha1"}
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}
	AddToScheme   = SchemeBuilder.AddToScheme
)
```

`api/v1alpha1/pmtunoderelay_types.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// PMTUNodeRelaySpec carries one captured ICMP frag-needed packet to be injected
// by peer nodes.
type PMTUNodeRelaySpec struct {
	// SourceNode is the name of the node that captured the packet.
	SourceNode string `json:"sourceNode"`
	// Payload is the base64-encoded raw IP packet (ICMP type 3 code 4).
	Payload string `json:"payload"`
	// ExpiresAt is when this object may be garbage-collected if not yet consumed.
	ExpiresAt metav1.Time `json:"expiresAt"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Namespaced,shortName=pnr

// PMTUNodeRelay relays an ICMP frag-needed packet between nodes via the API server.
type PMTUNodeRelay struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PMTUNodeRelaySpec `json:"spec"`
}

// +kubebuilder:object:root=true

// PMTUNodeRelayList is a list of PMTUNodeRelay.
type PMTUNodeRelayList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PMTUNodeRelay `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PMTUNodeRelay{}, &PMTUNodeRelayList{})
}
```

- [ ] **Step 2: Generate deepcopy + CRD**

Run: `make generate`
Expected: creates `api/v1alpha1/zz_generated.deepcopy.go` and `crd/pmtud.cloud.sap_pmtunoderelays.yaml`. If `make generate` errors that the target is missing, add a `controllerGen` block to `Makefile.maker.yaml` and re-run go-makefile-maker, then `make generate`.

- [ ] **Step 3: Verify it compiles**

Run: `go build ./api/...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add api/ crd/
git commit -m "feat(api): add PMTUNodeRelay v1alpha1 CRD types"
```

---

### Task 2: Relay interface, RelayPacket, factory

**Files:**
- Create: `internal/relay/relay.go`
- Test: `internal/relay/relay_test.go`

**Interfaces:**
- Produces:
  - `relay.RelayPacket{ Payload []byte, SrcNode string }`
  - `relay.Relay interface { Send(context.Context, RelayPacket) error; Start(context.Context, func([]byte) error) error }`
  - `relay.BackendUDP = "udp"`, `relay.BackendCRD = "crd"`
  - `relay.Deps{ Cfg *config.Config, Log logr.Logger, Client client.Client, Cache cache.Cache }`
  - `relay.New(backend string, d Deps) (Relay, error)`

- [ ] **Step 1: Write the failing test**

`internal/relay/relay_test.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"testing"

	"github.com/sapcc/go-pmtud/internal/config"
)

func TestNewUnknownBackend(t *testing.T) {
	_, err := New("bogus", Deps{Cfg: &config.Config{}})
	if err == nil {
		t.Fatal("expected error for unknown backend, got nil")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/relay/... -run TestNewUnknownBackend`
Expected: FAIL (package/`New` not defined).

- [ ] **Step 3: Write the interface + factory**

`internal/relay/relay.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/go-pmtud/internal/config"
)

const (
	BackendUDP = "udp"
	BackendCRD = "crd"
)

// RelayPacket is a captured ICMP 3/4 packet to be replicated to peer nodes.
type RelayPacket struct {
	Payload []byte // raw IP packet as captured by NFLOG
	SrcNode string // capturing node's name (provenance / dedup)
}

// Relay abstracts the inter-node transport. One implementation is active per
// process. Capture (NFLOG) and injection (TUN) live outside this interface.
type Relay interface {
	// Send relays a captured packet to peer node(s). Called from the nflog
	// callback hot path — must be fast / non-blocking.
	Send(ctx context.Context, pkt RelayPacket) error
	// Start runs the receive loop until ctx is done. For every packet relayed
	// from a peer it calls inject(payload).
	Start(ctx context.Context, inject func(payload []byte) error) error
}

// Deps carries everything any backend might need. UDP ignores Client/Cache.
type Deps struct {
	Cfg    *config.Config
	Log    logr.Logger
	Client client.Client
	Cache  cache.Cache
}

// New builds the selected backend.
func New(backend string, d Deps) (Relay, error) {
	switch backend {
	case BackendUDP:
		return newUDPBackend(d)
	case BackendCRD:
		return newCRDBackend(d)
	default:
		return nil, fmt.Errorf("unknown relay backend %q (want %q or %q)", backend, BackendUDP, BackendCRD)
	}
}
```

Note: `newUDPBackend` and `newCRDBackend` are added in Tasks 4 and 6. To keep this task compiling on its own, add temporary stubs at the bottom of `relay.go` returning `nil, fmt.Errorf("not implemented")`, and delete each stub when its real constructor lands.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/relay/... -run TestNewUnknownBackend`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/relay.go internal/relay/relay_test.go
git commit -m "feat(relay): add Relay interface and backend factory"
```

---

### Task 3: Shared TUN Injector + manager Runnable

**Files:**
- Create: `internal/relay/inject_linux.go` (move `createTUN` + `configureTUNNetlink` from `internal/receiver/receiver.go` and `tun_linux.go`)
- Create: `internal/relay/inject_other.go`
- Create: `internal/relay/runnable.go`

**Interfaces:**
- Produces:
  - `relay.newInjector(name string) (*Injector, error)`
  - `(*relay.Injector).Inject(payload []byte) error`, `(*relay.Injector).Close() error`
  - `relay.TUNDeviceName = "pmtud0"`
  - `relay.Runnable{ Backend Relay, Log logr.Logger }` implementing `Start(ctx) error` (satisfies `manager.Runnable`)

- [ ] **Step 1: Write the linux Injector**

`internal/relay/inject_linux.go` — move the TUN creation from `receiver.go` (`createTUN`, `ifnamsiz`, `maxPacketSize`) and `configureTUNNetlink` from `receiver/tun_linux.go`. Wrap the fd in an `Injector`:
```go
//go:build linux

// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	TUNDeviceName = "pmtud0"
	ifnamsiz      = 16
	maxPacketSize = 1500
)

// Injector owns the pmtud0 TUN device and writes packets into the kernel
// receive path (ip_input -> icmp_rcv -> icmp_unreach -> PMTU cache update).
type Injector struct {
	f *os.File
}

func newInjector(name string) (*Injector, error) {
	fd, err := createTUN(name)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), "/dev/net/tun") //#nosec G115
	if err := configureTUNNetlink(name); err != nil {
		f.Close()
		return nil, err
	}
	return &Injector{f: f}, nil
}

func (i *Injector) Inject(payload []byte) error {
	if _, err := i.f.Write(payload); err != nil {
		if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
			return fmt.Errorf("tun write backpressure: %w", err)
		}
		return fmt.Errorf("tun write: %w", err)
	}
	return nil
}

func (i *Injector) Close() error { return i.f.Close() }

// createTUN — moved verbatim from internal/receiver/receiver.go.
func createTUN(name string) (int, error) {
	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open /dev/net/tun: %w", err)
	}
	var ifr [ifnamsiz + 64]byte
	copy(ifr[:ifnamsiz], name)
	flags := uint16(unix.IFF_TUN | unix.IFF_NO_PI)
	ifr[ifnamsiz] = byte(flags & 0xff)          //#nosec G115
	ifr[ifnamsiz+1] = byte((flags >> 8) & 0xff) //#nosec G115
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr[0]))) //#nosec G115
	if errno != 0 {
		unix.Close(fd)
		return -1, fmt.Errorf("ioctl TUNSETIFF: %w", errno)
	}
	return fd, nil
}
```
Also move `configureTUNNetlink` into this file (or a `inject_netlink_linux.go`) from `receiver/tun_linux.go` unchanged.

- [ ] **Step 2: Write the non-linux stub**

`internal/relay/inject_other.go`:
```go
//go:build !linux

// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import "errors"

const TUNDeviceName = "pmtud0"

type Injector struct{}

func newInjector(string) (*Injector, error) {
	return nil, errors.New("TUN injection is only supported on Linux")
}

func (i *Injector) Inject([]byte) error { return errors.New("not supported") }
func (i *Injector) Close() error        { return nil }
```

- [ ] **Step 3: Write the Runnable**

`internal/relay/runnable.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"

	"github.com/go-logr/logr"
)

// Runnable owns the shared TUN injector and drives the active backend's receive
// loop. It satisfies sigs.k8s.io/controller-runtime manager.Runnable.
type Runnable struct {
	Backend Relay
	Log     logr.Logger
}

func (r *Runnable) Start(ctx context.Context) error {
	inj, err := newInjector(TUNDeviceName)
	if err != nil {
		r.Log.Error(err, "creating TUN injector")
		return err
	}
	defer inj.Close()
	r.Log.Info("TUN injector ready", "device", TUNDeviceName)
	r.Log.Info("IMPORTANT: NFLOG rule must exclude the TUN device to prevent loops",
		"rule", "iptables -t raw -A PREROUTING -p icmp -m icmp --icmp-type 3/4 ! -i "+TUNDeviceName+" -j NFLOG --nflog-group <group>")
	return r.Backend.Start(ctx, inj.Inject)
}
```

- [ ] **Step 4: Verify cross-platform compile**

Run: `go build ./internal/relay/...` and `GOOS=linux go build ./internal/relay/...`
Expected: both PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/inject_linux.go internal/relay/inject_other.go internal/relay/runnable.go
git commit -m "feat(relay): add shared TUN injector and backend runnable"
```

---

### Task 4: UDP backend

**Files:**
- Create: `internal/relay/udp.go` (move send loop from `nflog/controller.go`; move listener + `isKnownPeer` from `receiver/receiver.go` + `receiver/peer.go`)
- Test: `internal/relay/udp_test.go` (move cases from `receiver/receiver_test.go`)

**Interfaces:**
- Consumes: `RelayPacket`, `Deps`, `config.Config{ReplicationPort, PeerList, PeerMutex, NodeName}`, `packet.ParseICMPFragNeeded`.
- Produces: `newUDPBackend(d Deps) (Relay, error)`; `udpBackend` implements `Relay`.

- [ ] **Step 1: Write the failing tests**

`internal/relay/udp_test.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"net"
	"testing"

	"github.com/sapcc/go-pmtud/internal/config"
)

func newTestUDP(t *testing.T, peers map[string]string) *udpBackend {
	t.Helper()
	cfg := &config.Config{NodeName: "self", ReplicationPort: 4390, PeerList: peers}
	b, err := newUDPBackend(Deps{Cfg: cfg})
	if err != nil {
		t.Fatalf("newUDPBackend: %v", err)
	}
	return b.(*udpBackend)
}

func TestIsKnownPeer(t *testing.T) {
	b := newTestUDP(t, map[string]string{"n1": "10.0.1.2"})
	if !b.isKnownPeer(net.ParseIP("10.0.1.2")) {
		t.Fatal("expected 10.0.1.2 to be a known peer")
	}
	if b.isKnownPeer(net.ParseIP("203.0.113.9")) {
		t.Fatal("expected 203.0.113.9 to be rejected")
	}
}
```
(Also move the invalid-payload-rejection and known/unknown-source cases from `receiver/receiver_test.go` here, adapted to `udpBackend`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/relay/... -run TestIsKnownPeer`
Expected: FAIL (`newUDPBackend`/`udpBackend` not defined — replace the Task 2 stub).

- [ ] **Step 3: Implement the UDP backend**

`internal/relay/udp.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"fmt"
	"net"

	"github.com/go-logr/logr"

	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
	"github.com/sapcc/go-pmtud/internal/packet"
)

type udpBackend struct {
	cfg      *config.Config
	log      logr.Logger
	sendConn *net.UDPConn
}

func newUDPBackend(d Deps) (Relay, error) {
	// Persistent unconnected socket for sends (avoids per-packet FD churn).
	sendConn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return nil, fmt.Errorf("udp send socket: %w", err)
	}
	return &udpBackend{cfg: d.Cfg, log: d.Log, sendConn: sendConn}, nil
}

func (u *udpBackend) peers() []string {
	u.cfg.PeerMutex.Lock()
	defer u.cfg.PeerMutex.Unlock()
	out := make([]string, 0, len(u.cfg.PeerList))
	for _, ip := range u.cfg.PeerList {
		out = append(out, ip)
	}
	return out
}

func (u *udpBackend) isKnownPeer(ip net.IP) bool {
	u.cfg.PeerMutex.Lock()
	defer u.cfg.PeerMutex.Unlock()
	for _, p := range u.cfg.PeerList {
		if ip.Equal(net.ParseIP(p)) {
			return true
		}
	}
	return false
}

func (u *udpBackend) Send(_ context.Context, pkt RelayPacket) error {
	for _, peerIP := range u.peers() {
		addr := &net.UDPAddr{IP: net.ParseIP(peerIP), Port: u.cfg.ReplicationPort}
		if _, err := u.sendConn.WriteTo(pkt.Payload, addr); err != nil {
			metrics.Error.WithLabelValues(u.cfg.NodeName).Inc()
			metrics.SentError.WithLabelValues(u.cfg.NodeName, peerIP).Inc()
			u.log.Error(err, "error writing packet to peer", "peer", peerIP)
			continue
		}
		metrics.SentPackets.WithLabelValues(u.cfg.NodeName).Inc()
		metrics.SentPacketsPeer.WithLabelValues(u.cfg.NodeName, peerIP).Inc()
	}
	return nil
}

func (u *udpBackend) Start(ctx context.Context, inject func([]byte) error) error {
	addr, err := net.ResolveUDPAddr("udp4", fmt.Sprintf(":%d", u.cfg.ReplicationPort))
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	go func() {
		<-ctx.Done()
		conn.Close()
		u.sendConn.Close()
	}()

	buf := make([]byte, maxPacketSizeUDP)
	for {
		n, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				metrics.Error.WithLabelValues(u.cfg.NodeName).Inc()
				u.log.Error(err, "error reading from UDP")
				continue
			}
		}
		payload := append([]byte(nil), buf[:n]...)
		if !u.isKnownPeer(remote.IP) {
			metrics.Error.WithLabelValues(u.cfg.NodeName).Inc()
			u.log.Info("rejected packet from unknown source", "remote", remote.IP.String())
			continue
		}
		if _, err := packet.ParseICMPFragNeeded(payload); err != nil {
			metrics.Error.WithLabelValues(u.cfg.NodeName).Inc()
			u.log.Info("invalid packet, discarding", "remote", remote.IP.String(), "error", err.Error())
			continue
		}
		if err := inject(payload); err != nil {
			metrics.Error.WithLabelValues(u.cfg.NodeName).Inc()
			u.log.Error(err, "error injecting packet")
			continue
		}
		metrics.RecvPackets.WithLabelValues(u.cfg.NodeName, remote.IP.String()).Inc()
	}
}

const maxPacketSizeUDP = 1500
```

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/relay/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/relay/udp.go internal/relay/udp_test.go
git commit -m "feat(relay): add UDP backend behind Relay interface"
```

---

### Task 5: Refactor nflog controller to call Relay.Send

**Files:**
- Modify: `internal/nflog/controller.go`
- Test: `internal/nflog/controller_test.go` (adapt if it exercised the UDP loop)

**Interfaces:**
- Consumes: `relay.Relay`, `relay.RelayPacket`.
- Produces: `nflog.Controller{ Log, Cfg, Relay relay.Relay }`; callback calls `Relay.Send`.

- [ ] **Step 1: Add the Relay field + failing expectation**

Add `Relay relay.Relay` to `nflog.Controller`. In `controller_test.go`, add a fake relay recording `Send` calls and assert the callback forwards the captured payload. (Fake:)
```go
type fakeRelay struct{ sent [][]byte }

func (f *fakeRelay) Send(_ context.Context, p relay.RelayPacket) error {
	f.sent = append(f.sent, p.Payload)
	return nil
}
func (f *fakeRelay) Start(context.Context, func([]byte) error) error { return nil }
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/nflog/...`
Expected: FAIL (field/behavior not present).

- [ ] **Step 3: Replace the UDP send loop with Relay.Send**

In `controller.go`: delete the `sendConn` creation and the `for _, peerIP := range peerIPs { WriteTo }` block. Keep NFLOG open/config, ignore-network + peer-IP loop guards, `RecvPackets` capture metric, and `CallbackDuration`. Replace the send with:
```go
if err := nfc.Relay.Send(ctx, relay.RelayPacket{Payload: b, SrcNode: cfg.NodeName}); err != nil {
	metrics.Error.WithLabelValues(cfg.NodeName).Inc()
	log.Error(err, "relay send failed")
}
```
Remove the now-unused `net` import if nothing else uses it.

- [ ] **Step 4: Run tests to verify pass**

Run: `go test ./internal/nflog/...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/nflog/controller.go internal/nflog/controller_test.go
git commit -m "refactor(nflog): send captured packets via Relay interface"
```

---

### Task 6: CRD backend

**Files:**
- Create: `internal/relay/crd.go`
- Test: `internal/relay/crd_test.go` (envtest)

**Interfaces:**
- Consumes: `Deps{Cfg, Log, Client, Cache}`, `v1alpha1.PMTUNodeRelay`, `config.Config{NodeName, RelayNamespace, RelayGCInterval, TimeToLive}`.
- Produces: `newCRDBackend(d Deps) (Relay, error)`; `crdBackend` implements `Relay`; `relayObjectName(srcNode string, payload []byte) string`.

- [ ] **Step 1: Write the failing name-derivation test (no cluster needed)**

`internal/relay/crd_test.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import "testing"

func TestRelayObjectNameStable(t *testing.T) {
	p := []byte{1, 2, 3, 4}
	a := relayObjectName("node-a", p)
	b := relayObjectName("node-a", p)
	if a != b {
		t.Fatalf("name not deterministic: %q vs %q", a, b)
	}
	if relayObjectName("node-b", p) == a {
		t.Fatal("different source nodes must yield different names")
	}
	// <node>--<8 hex chars>
	if len(a) != len("node-a")+2+8 {
		t.Fatalf("unexpected name shape: %q", a)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/relay/... -run TestRelayObjectNameStable`
Expected: FAIL (`relayObjectName` not defined — replace the Task 2 CRD stub).

- [ ] **Step 3: Implement the CRD backend**

`internal/relay/crd.go`:
```go
// SPDX-FileCopyrightText: 2026 SAP SE or an SAP affiliate company
// SPDX-License-Identifier: Apache-2.0

package relay

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/sapcc/go-pmtud/api/v1alpha1"
	"github.com/sapcc/go-pmtud/internal/config"
	"github.com/sapcc/go-pmtud/internal/metrics"
)

type crdBackend struct {
	cfg    *config.Config
	log    logr.Logger
	client client.Client
	cache  cache.Cache
	ns     string
	ttl    time.Duration
	gcTick time.Duration
}

func newCRDBackend(d Deps) (Relay, error) {
	if d.Client == nil || d.Cache == nil {
		return nil, fmt.Errorf("crd backend requires a kube client and cache")
	}
	if d.Cfg.RelayNamespace == "" {
		return nil, fmt.Errorf("crd backend requires a namespace (--relay-namespace or POD_NAMESPACE)")
	}
	return &crdBackend{
		cfg: d.Cfg, log: d.Log, client: d.Client, cache: d.Cache,
		ns:     d.Cfg.RelayNamespace,
		ttl:    time.Duration(d.Cfg.TimeToLive) * time.Second,
		gcTick: d.Cfg.RelayGCInterval,
	}, nil
}

func relayObjectName(srcNode string, payload []byte) string {
	sum := sha256.Sum256(payload)
	return fmt.Sprintf("%s--%x", srcNode, sum[:4]) // 4 bytes = 8 hex chars
}

func (c *crdBackend) Send(ctx context.Context, pkt RelayPacket) error {
	obj := &v1alpha1.PMTUNodeRelay{
		ObjectMeta: metav1.ObjectMeta{Name: relayObjectName(pkt.SrcNode, pkt.Payload), Namespace: c.ns},
		Spec: v1alpha1.PMTUNodeRelaySpec{
			SourceNode: pkt.SrcNode,
			Payload:    base64.StdEncoding.EncodeToString(pkt.Payload),
			ExpiresAt:  metav1.NewTime(time.Now().Add(c.ttl)),
		},
	}
	err := c.client.Create(ctx, obj)
	if apierrors.IsAlreadyExists(err) {
		return nil // dedup: same event already relayed
	}
	if err != nil {
		metrics.Error.WithLabelValues(c.cfg.NodeName).Inc()
		return err
	}
	metrics.SentPackets.WithLabelValues(c.cfg.NodeName).Inc()
	return nil
}

func (c *crdBackend) Start(ctx context.Context, inject func([]byte) error) error {
	inf, err := c.cache.GetInformer(ctx, &v1alpha1.PMTUNodeRelay{})
	if err != nil {
		return fmt.Errorf("get informer: %w", err)
	}
	handle := func(obj any) {
		r, ok := obj.(*v1alpha1.PMTUNodeRelay)
		if !ok || r.Namespace != c.ns || r.Spec.SourceNode == c.cfg.NodeName {
			return // skip foreign namespace / own captures (loop guard)
		}
		raw, err := base64.StdEncoding.DecodeString(r.Spec.Payload)
		if err != nil {
			c.log.Error(err, "decode relay payload", "obj", r.Name)
			return
		}
		if err := inject(raw); err != nil {
			metrics.Error.WithLabelValues(c.cfg.NodeName).Inc()
			c.log.Error(err, "inject relayed packet", "obj", r.Name)
			return
		}
		metrics.RecvPackets.WithLabelValues(c.cfg.NodeName, r.Spec.SourceNode).Inc()
		if err := c.client.Delete(ctx, r); err != nil && !apierrors.IsNotFound(err) {
			c.log.Error(err, "delete relay object after inject", "obj", r.Name)
		}
	}
	if _, err := inf.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		AddFunc:    handle,
		UpdateFunc: func(_, obj any) { handle(obj) },
	}); err != nil {
		return fmt.Errorf("add event handler: %w", err)
	}
	go c.gcLoop(ctx)
	<-ctx.Done()
	return nil
}

func (c *crdBackend) gcLoop(ctx context.Context) {
	t := time.NewTicker(c.gcTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.gcExpired(ctx)
		}
	}
}

func (c *crdBackend) gcExpired(ctx context.Context) {
	var list v1alpha1.PMTUNodeRelayList
	if err := c.client.List(ctx, &list, client.InNamespace(c.ns)); err != nil {
		c.log.Error(err, "gc list")
		return
	}
	now := time.Now()
	for i := range list.Items {
		if list.Items[i].Spec.ExpiresAt.Time.After(now) {
			continue
		}
		if err := c.client.Delete(ctx, &list.Items[i]); err != nil && !apierrors.IsNotFound(err) {
			c.log.Error(err, "gc delete", "obj", list.Items[i].Name)
		}
	}
}
```

- [ ] **Step 4: Write the envtest integration test**

`internal/relay/crd_test.go` (add; guarded so it skips when `KUBEBUILDER_ASSETS` is unset):
```go
func TestCRDSendCreatesAndDedups(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("envtest assets not available")
	}
	// envtest.Environment{CRDDirectoryPaths: []string{"../../crd"}}, Start,
	// build client with v1alpha1 scheme, newCRDBackend with a test namespace,
	// call Send twice with same payload, assert exactly one object exists and
	// its Spec fields match; then set ExpiresAt in the past and assert gcExpired
	// deletes it.
}
```
(Fill in the standard envtest boilerplate — `sigs.k8s.io/controller-runtime/pkg/envtest`, register `v1alpha1.AddToScheme`.)

- [ ] **Step 5: Run tests**

Run: `make build/cover.out` (sets `KUBEBUILDER_ASSETS` via setup-envtest) or `go test ./internal/relay/...` locally.
Expected: PASS (envtest case runs under `make`, skips under plain `go test` without assets).

- [ ] **Step 6: Commit**

```bash
git add internal/relay/crd.go internal/relay/crd_test.go
git commit -m "feat(relay): add CRD backend with dedup, watch-inject-delete, and TTL GC"
```

---

### Task 7: Config fields, CLI flags, namespace resolution

**Files:**
- Modify: `internal/config/config.go`
- Modify: `internal/cmd/command.go` (flag registration + validation only; wiring in Task 8)

**Interfaces:**
- Produces: `config.Config{ RelayBackend string, RelayNamespace string, RelayGCInterval time.Duration }`; flags `--relay-backend`, `--relay-namespace`, `--relay-gc-interval`.

- [ ] **Step 1: Add config fields**

In `config.go`, add to `Config`:
```go
RelayBackend    string        // "udp" (default) or "crd"
RelayNamespace  string        // namespace for CRD relay objects
RelayGCInterval time.Duration // CRD GC sweep interval
```
(Add `"time"` import.)

- [ ] **Step 2: Register flags + validation**

In `command.go` `init()`:
```go
rootCmd.PersistentFlags().StringVar(&cfg.RelayBackend, "relay-backend", "udp", "Relay transport: udp|crd")
rootCmd.PersistentFlags().StringVar(&cfg.RelayNamespace, "relay-namespace", "", "Namespace for CRD relay objects (default: $POD_NAMESPACE)")
rootCmd.PersistentFlags().DurationVar(&cfg.RelayGCInterval, "relay-gc-interval", 60*time.Second, "CRD relay GC sweep interval")
```
In `preRunRootCmd`, resolve namespace + validate backend:
```go
if cfg.RelayNamespace == "" {
	cfg.RelayNamespace = os.Getenv("POD_NAMESPACE")
}
switch cfg.RelayBackend {
case "udp":
case "crd":
	if cfg.RelayNamespace == "" {
		return fmt.Errorf("--relay-backend=crd requires --relay-namespace or POD_NAMESPACE")
	}
default:
	return fmt.Errorf("invalid --relay-backend %q (want udp|crd)", cfg.RelayBackend)
}
```

- [ ] **Step 3: Verify build**

Run: `go build ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add internal/config/config.go internal/cmd/command.go
git commit -m "feat(cmd): add --relay-backend/--relay-namespace/--relay-gc-interval flags"
```

---

### Task 8: Wire the backend in command.go; remove old receiver package

**Files:**
- Modify: `internal/cmd/command.go`
- Delete: `internal/receiver/` (receiver.go, receiver_other.go, peer.go, tun_linux.go, receiver_test.go)

**Interfaces:**
- Consumes: `relay.New`, `relay.Runnable`, `v1alpha1.AddToScheme`.

- [ ] **Step 1: Register the CRD scheme**

In `runRootCmd`, before building the manager, register the API types on the scheme controller-runtime uses. Build a scheme and pass via `manager.Options{Scheme: scheme}`:
```go
scheme := runtime.NewScheme()
utilruntime.Must(clientgoscheme.AddToScheme(scheme))
utilruntime.Must(v1alpha1.AddToScheme(scheme))
managerOpts := manager.Options{
	Scheme:                 scheme,
	Metrics:                metricsserver.Options{BindAddress: cfg.MetricsPort},
	HealthProbeBindAddress: cfg.HealthPort,
}
```

- [ ] **Step 2: Build backend + wire**

Replace the `receiver.Controller` block with:
```go
backend, err := relay.New(cfg.RelayBackend, relay.Deps{
	Cfg:    &cfg,
	Log:    log.WithName("relay-" + cfg.RelayBackend),
	Client: mgr.GetClient(),
	Cache:  mgr.GetCache(),
})
if err != nil {
	log.Error(err, "error building relay backend")
	return err
}
nfc.Relay = backend // set on the nflog controller before mgr.Add(&nfc)

if err := mgr.Add(&relay.Runnable{Backend: backend, Log: log.WithName("relay-runnable")}); err != nil {
	log.Error(err, "error adding relay runnable")
	return err
}
```
Ensure `nfc.Relay` is set before `mgr.Add(&nfc)`. Remove the `receiver` import.

- [ ] **Step 3: Delete the old receiver package**

```bash
git rm -r internal/receiver
```
(All its logic now lives in `internal/relay`.)

- [ ] **Step 4: Verify build + tests + vet**

Run: `go build ./... && go test ./... && go vet ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add -A
git commit -m "feat(cmd): wire pluggable relay backend and remove receiver package"
```

---

### Task 9: RBAC marker + regenerate

**Files:**
- Modify: `internal/relay/crd.go` (add kubebuilder RBAC marker) or a new `internal/relay/doc.go`
- Generated: `crd/`, RBAC output from `make generate`

- [ ] **Step 1: Add the RBAC marker**

Above `crdBackend` (or in `doc.go`):
```go
// +kubebuilder:rbac:groups=pmtud.cloud.sap,resources=pmtunoderelays,verbs=get;list;watch;create;delete
```

- [ ] **Step 2: Regenerate**

Run: `make generate`
Expected: RBAC role (roleName `go-pmtud`) includes `pmtunoderelays` verbs; `crd/` unchanged from Task 1. Commit any diffs.

- [ ] **Step 3: Verify build**

Run: `go build ./... && go vet ./...`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore(relay): add RBAC markers for pmtunoderelays"
```

---

### Task 10: Local integration — netns doc + Kind CRD parameter

**Files:**
- Modify: `lab/scripts/deploy-pmtud.sh` (accept `RELAY_BACKEND`, install CRD + RBAC when `crd`)
- Modify: `lab/manifests/pmtud-daemonset.yaml` (pass `--relay-backend`, `POD_NAMESPACE` via downward API)
- Modify: `lab/manifests/rbac.yaml` (add namespaced Role/RoleBinding for `pmtunoderelays`)
- Create: `lab/manifests/crd.yaml` (symlink or copy of generated `crd/pmtud.cloud.sap_pmtunoderelays.yaml`)
- Modify: `lab/README.md` (document netns fast-path + Kind CRD run)

- [ ] **Step 1: Parameterize the daemonset**

Add to the container args `--relay-backend=$(RELAY_BACKEND)` (templated by the deploy script) and an env var:
```yaml
- name: POD_NAMESPACE
  valueFrom:
    fieldRef:
      fieldPath: metadata.namespace
```

- [ ] **Step 2: Deploy script installs CRD/RBAC for crd backend**

In `deploy-pmtud.sh`, when `RELAY_BACKEND=crd`: `kubectl apply -f lab/manifests/crd.yaml` and the namespaced Role/RoleBinding before applying the daemonset; substitute the backend into the daemonset manifest.

- [ ] **Step 3: Document the netns fast-path**

In `lab/README.md`, add the 4-namespace `ip netns` recipe from the spec (node-a, node-b, router, destination; destination veth MTU 1400; static routes forcing ICMP to node-b), noting it exercises capture+inject and that the CRD backend additionally needs a local API server.

- [ ] **Step 4: Run the Kind lab with each backend (manual)**

Run: `make -C lab up && RELAY_BACKEND=udp make -C lab test-e2e` then `RELAY_BACKEND=crd make -C lab test-e2e`
Expected: `verify-pmtu.sh` reports `mtu 1400` on the node that did not receive the ICMP natively, for both backends.

- [ ] **Step 5: Commit**

```bash
git add lab/
git commit -m "test(lab): parameterize relay backend and add CRD manifests/RBAC"
```

---

### Task 11: E2E automation — backend-parameterized script + CI gate

**Files:**
- Modify: `lab/scripts/test-e2e.sh` (loop over `RELAY_BACKEND` values, assert positive + negative)
- Modify: `lab/Makefile` (target that runs both backends)
- Modify: `Makefile.maker.yaml` (add a label-gated E2E GitHub workflow) then `go-makefile-maker`

- [ ] **Step 1: Make test-e2e assert both directions**

In `test-e2e.sh`: after traffic, assert `ip route get <dst>` on the target node shows `mtu 1400` (positive) and `tcpdump` shows the ICMP arriving only on the peer and being injected on the target (negative). Parameterize by `RELAY_BACKEND` (default `udp`); a wrapper target runs `udp` then `crd`.

- [ ] **Step 2: Add a label-gated CI workflow**

In `Makefile.maker.yaml`, add a `customWorkflow` (or extend `githubWorkflow`) that runs the Kind E2E on a `linux` runner, triggered by a PR label (e.g. `e2e`) or nightly `schedule` — not on every PR (needs Docker + privileged). Re-run `go-makefile-maker`. If maker cannot express this, add a standalone `.github/workflows/e2e.yaml` and record in `REUSE.toml` as needed. Do NOT silently skip: the job must clearly report skipped-vs-run.

- [ ] **Step 3: Validate the script locally**

Run: `make -C lab up && make -C lab e2e-all`
Expected: both backends pass; skip cleanly if Docker/Kind unavailable with a clear message.

- [ ] **Step 4: Commit**

```bash
git add lab/ .github/ Makefile.maker.yaml
git commit -m "test(lab): automate E2E across udp and crd backends (label-gated CI)"
```

---

### Task 12: Final validation + openspec completion

**Files:**
- Modify: `openspec/changes/pluggable-relay-crd/tasks.md` (check off completed items)

- [ ] **Step 1: Full build/test/vet/generate clean**

Run: `go build ./... && go vet ./... && make generate && git diff --exit-code`
Expected: builds, vets, and `make generate` produces no uncommitted diff.

- [ ] **Step 2: Full test suite (with envtest)**

Run: `make check`
Expected: lint + tests pass, including the CRD envtest case.

- [ ] **Step 3: Static checks**

Run: `make static-check`
Expected: golangci-lint, shellcheck (lab scripts), typos, license checks pass.

- [ ] **Step 4: Commit**

```bash
git add openspec/changes/pluggable-relay-crd/tasks.md
git commit -m "chore(relay): mark pluggable-relay-crd tasks complete"
```

---

## Follow-up (out of scope — separate repo)

`sapcc/helm-charts` (`system/go-pmtud`): install the `PMTUNodeRelay` CRD, add the
namespaced Role/RoleBinding for `pmtunoderelays`, pass `--relay-backend` +
`POD_NAMESPACE`, and drop container port 4390 / its firewall annotations when the
CRD backend is used. Tracked separately; not part of this plan.
