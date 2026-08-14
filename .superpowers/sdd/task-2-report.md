# Task 2: Relay Interface and Backend Factory — Report

## Status
**DONE**

## Summary
Created the core `Relay` interface, `RelayPacket` struct, and `New` factory function that backends will implement.

## Files Created
- `internal/relay/relay.go` — Interface, constants, factory, and temporary stubs
- `internal/relay/relay_test.go` — Test for unknown backend case

## Implementation Details

### relay.go
- **Interface**: `Relay` with methods `Send(ctx context.Context, pkt RelayPacket) error` and `Start(ctx context.Context, inject func(payload []byte) error) error`
- **Struct**: `RelayPacket` with `Payload []byte` and `SrcNode string`
- **Constants**: `BackendUDP = "udp"` and `BackendCRD = "crd"`
- **Dependencies**: `Deps` struct containing `Cfg`, `Log`, `Client`, and `Cache` (using real `sigs.k8s.io/controller-runtime/pkg/cache.Cache`)
- **Factory**: `New(backend string, d Deps) (Relay, error)` that routes to backend constructors with spec-compliant error message
- **Stubs**: `newUDPBackend` and `newCRDBackend` return `nil, fmt.Errorf("not implemented")` for Tasks 4 and 6

### relay_test.go
- **Test**: `TestNewUnknownBackend` verifies that requesting an unknown backend returns the spec-compliant error message

## Verification

| Check | Result |
|-------|--------|
| Test `TestNewUnknownBackend` | ✅ PASS |
| `go build ./internal/relay/...` | ✅ Clean |
| `go vet ./internal/relay/...` | ✅ Clean |

## Commit
- **Hash**: `103de16`
- **Message**: `feat(relay): add Relay interface and backend factory`
- **Files**: 2 created, 102 insertions

## Fixes (Review Round 1)

### CRITICAL: Cache interface
- **Issue**: Custom `Cache interface` with `Get/Set` methods conflicted with Task 6's requirement for `cache.Cache.GetInformer()`
- **Fix**: Deleted custom Cache interface; imported `sigs.k8s.io/controller-runtime/pkg/cache` and use `cache.Cache` directly in Deps struct
- **Verification**: Test still passes, build clean

### IMPORTANT: Error message
- **Issue**: Error message was `"unknown backend: %s"` instead of spec-required `"unknown relay backend %q (want %q or %q)"`
- **Fix**: Updated error to exactly: `fmt.Errorf("unknown relay backend %q (want %q or %q)", backend, BackendUDP, BackendCRD)`
- **Verification**: Test validates error format

## Fix Commit
- **Hash**: (pending)
- **Message**: `fix(relay): use real cache.Cache and spec-compliant error message`
