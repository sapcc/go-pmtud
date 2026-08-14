# Task 5 Report: Refactor nflog Controller to Use Relay Interface

## Status
COMPLETE

## Summary
Refactored `internal/nflog/controller.go` to decouple packet transport: nflog now sends captured ICMP 3/4 packets via the `Relay` interface instead of embedding UDP send logic.

## Changes Made

### `internal/nflog/controller.go`
- **Updated SPDX header:** 2024 → 2026 (per spec)
- **Added imports:** `relay` package
- **Removed imports:** Removed `net` import (re-added for helper functions that still use `net.IP`/`net.IPNet`)
- **Modified `Controller` struct:** Added `Relay relay.Relay` field
- **Removed:** UDP socket creation (`sendConn`) and associated defer
- **Removed:** For loop that sent packets to each peer via UDP (`WriteTo`)
- **Removed:** Per-peer metrics (`SentError`, `SentPackets`, `SentPacketsPeer`)
- **Added:** Single `Relay.Send()` call with exact error handling from spec:
  ```go
  if err := nfc.Relay.Send(ctx, relay.RelayPacket{Payload: b, SrcNode: cfg.NodeName}); err != nil {
      metrics.Error.WithLabelValues(cfg.NodeName).Inc()
      log.Error(err, "relay send failed")
  }
  ```
- **Preserved:** All NFLOG setup, ignore-network guards, peer-IP loop prevention, `RecvPackets` metric, `CallbackDuration` metric

### `internal/nflog/controller_test.go`
- **Updated SPDX header:** 2024 → 2026
- **Added imports:** `context`, `relay` packages
- **Added fake relay implementation:**
  ```go
  type fakeRelay struct {
      sent [][]byte
  }
  
  func (f *fakeRelay) Send(_ context.Context, p relay.RelayPacket) error {
      f.sent = append(f.sent, p.Payload)
      return nil
  }
  
  func (f *fakeRelay) Start(context.Context, func([]byte) error) error {
      return nil
  }
  ```
- **Added test:** `TestControllerSendViaRelay` verifies fake relay records Send calls and payload matches

## Test Results
```
=== RUN   TestControllerSendViaRelay
--- PASS: TestControllerSendViaRelay (0.00s)
=== RUN   TestUDPSendToAllPeers
--- PASS: TestUDPSendToAllPeers (0.00s)
=== RUN   TestIsIgnoredNetwork
--- PASS: TestIsIgnoredNetwork (0.00s)
=== RUN   TestIsPeerIP
--- PASS: TestIsPeerIP (0.00s)
PASS
ok  	github.com/sapcc/go-pmtud/internal/nflog	0.700s
```

## Build & Vet
- `go build ./internal/nflog/...` — PASS (clean, no output)
- `go vet ./internal/nflog/...` — PASS (clean, no output)

## Commit
```
d184535 refactor(nflog): send captured packets via Relay interface
```

## Concerns
None. Task complete:
- Decoupling achieved: nflog transport-agnostic, delegates all Send to Relay
- Tests verify fake relay integrates and captures payloads
- All existing metrics and guards preserved (except per-peer UDP metrics, now Relay's responsibility)
- Code change minimal and surgical
