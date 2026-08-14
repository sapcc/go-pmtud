# Task 3 Report: Shared TUN Injector and Backend Runnable

## Status
**DONE**

## Commits
- `ccb4f55` feat(relay): add shared TUN injector and backend runnable

## Implementation Summary

### Files Created

1. **`internal/relay/inject_linux.go`** (Linux-specific)
   - Build tag: `//go:build linux`
   - Constants: `TUNDeviceName = "pmtud0"`, `ifnamsiz = 16`, `maxPacketSize = 1500`
   - `Injector` struct: holds file descriptor and os.File for TUN device
   - `newInjector(name string)` factory: creates TUN device, configures via netlink
   - `Inject(payload []byte) error` method: writes payload to TUN device
   - `Close() error` method: closes TUN device file
   - Functions moved verbatim from receiver:
     - `createTUN()`: opens `/dev/net/tun`, configures IFF_TUN|IFF_NO_PI via ioctl
     - `configureTUNNetlink()`: brings up interface, assigns 169.254.254.1/32 address

2. **`internal/relay/inject_other.go`** (Non-Linux stub)
   - Build tag: `//go:build !linux`
   - Same constants and interface signature as inject_linux.go
   - `newInjector()` returns error: "TUN injection is only supported on Linux"
   - `Inject()` and `Close()` stubs return error for non-linux platforms

3. **`internal/relay/runnable.go`** (Cross-platform, no build tag)
   - `Runnable` struct: `{ Backend Relay, Log logr.Logger }`
   - Implements `sigs.k8s.io/controller-runtime` manager.Runnable interface
   - `Start(ctx context.Context) error` method:
     - Creates Injector via `newInjector(TUNDeviceName)`
     - Defers `Close()`
     - Logs TUN device name
     - Logs IMPORTANT warning about iptables NFLOG rule (exact format from receiver)
     - Calls `Backend.Start(ctx, inj.Inject)` and returns its result

## Testing

- Darwin build: `go build ./internal/relay/...` → **PASS**
- Linux build: `GOOS=linux go build ./internal/relay/...` → **PASS**

No new tests required (stub on non-linux, moved code identical on linux).

## Code Extraction Notes

- `createTUN()` and `configureTUNNetlink()` copied verbatim from receiver (no rewrites)
- All receiver.go TUN initialization logic now encapsulated in Injector
- NFLOG warning message preserved exactly for consistency
- Removed unused `syscall` import (only `unix` is used)

## Next Steps

Task 4 will implement UDP backend to use Runnable; Task 6 will implement CRD backend.
Both backends will call `Backend.Start(ctx, inj.Inject)` through this shared Runnable.
