# Task 4: UDP Backend Implementation — Report

## Status
✅ Complete (after fixes)

## Files Created
- `internal/relay/udp.go` — UDP relay backend (246 lines)
- `internal/relay/udp_test.go` — Tests for backend (247 lines)

## Implementation Summary

### UDP Backend (`udp.go`)
- **Type**: `udpBackend` struct with `cfg`, `log`, `sendConn` fields
- **Constructor**: `newUDPBackend(d Deps)` creates unconnected UDP send socket
- **Send()**: Broadcasts RelayPacket payload to all peers in PeerList
  - Increments `metrics.Error` + `metrics.SentError` on send failures
  - Increments `metrics.SentPackets` + `metrics.SentPacketsPeer` per peer on success
  - Logs errors, returns nil (best-effort broadcast)
- **Start()**: Listens on ReplicationPort for peer packets
  - Validates sender via isKnownPeer() (known peer IPs only)
  - Validates payload via packet.ParseICMPFragNeeded() (ICMP type 3 code 4)
  - Injects validated packets via callback function
  - Records RecvPackets metric per source IP
  - Handles ctx.Done() for graceful shutdown
- **isKnownPeer()**: Thread-safe lookup in cfg.PeerList
- **peers()**: Returns snapshot of peer IPs with PeerMutex lock
- **Constants**: maxPacketSizeUDP = 1500

### Tests (`udp_test.go`)
- **Helper**: `newTestUDP(t *testing.T, peers map[string]string)` creates backend with dynamic port
- **TestIsKnownPeer**: 4 subtests
  - known peer 1 → true
  - known peer 2 → true
  - unknown peer → false
  - nil IP → false
- **TestSendToUnknownPeer**: Verifies packet from unknown source rejected
- **TestInvalidPayload**: Verifies malformed ICMP rejected without calling inject
- **TestValidPayload**: Verifies valid ICMP from known peer injected correctly

## Test Results
```
TestIsKnownPeer (4 subtests) PASS
TestSendToUnknownPeer PASS
TestInvalidPayload PASS
TestValidPayload PASS
```

## Build & Vet
- `go build ./internal/relay/...` ✅
- `go vet ./internal/relay/...` ✅

## Commits
```
d7421f7 feat(relay): add UDP backend behind Relay interface
86cb91c fix(relay): add Error metrics to Send, complete test coverage
```

## Fixes Applied (Code Review Round 1)
1. ✅ Test helper signature: `newTestUDP(t *testing.T, peers map[string]string) *udpBackend` with dynamic port allocation
2. ✅ Error metrics: Send() now increments both `metrics.Error` + `metrics.SentError` on failures
3. ✅ Test coverage: Added TestSendToUnknownPeer, TestInvalidPayload, TestValidPayload

## Integration
- Replaces stub `newUDPBackend()` in relay.go
- Implements Relay interface contract from relay.go
- Ready for integration with receiver injection callback
- Metrics: SentError, SentPackets, SentPacketsPeer, RecvPackets, Error
- Thread-safe peer list access via PeerMutex

## Status
✅ All CRITICAL findings addressed. All tests passing. Clean build.
