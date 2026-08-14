# Task 7 Report: Add Relay Backend Config Fields and CLI Flags

## Status: COMPLETE

## Summary
Added three CLI flags and configuration validation to support pluggable relay backends (UDP and CRD).

## Changes Made

### Files Modified
- `/Users/D053727/SAPDevelop/git/go-pmtud/internal/cmd/command.go`

### What Was Done

#### 1. Config Fields (Already Existed)
The three config fields were already defined in `internal/config/config.go`:
- `RelayBackend` (string) — "udp" (default) or "crd"
- `RelayNamespace` (string) — namespace for CRD relay objects
- `RelayGCInterval` (time.Duration) — CRD GC sweep interval

#### 2. CLI Flags Added
Added three new persistent flags to `init()`:
```go
rootCmd.PersistentFlags().StringVar(&cfg.RelayBackend, "relay-backend", "udp", "Relay backend: udp or crd")
rootCmd.PersistentFlags().StringVar(&cfg.RelayNamespace, "relay-namespace", "", "Namespace for CRD relay objects (defaults to POD_NAMESPACE env var)")
rootCmd.PersistentFlags().DurationVar(&cfg.RelayGCInterval, "relay-gc-interval", 60*time.Second, "CRD relay garbage collection interval")
```

#### 3. Validation in preRunRootCmd
Added validation logic covering:
- **Backend validation**: Rejects values other than "udp" or "crd" with clear error
- **Namespace resolution**: Falls back to `POD_NAMESPACE` env var if `--relay-namespace` not provided
- **CRD fail-fast**: If backend is "crd" but no namespace resolved, fails at startup with clear error

```go
// Validate relay backend
if cfg.RelayBackend != "udp" && cfg.RelayBackend != "crd" {
    return fmt.Errorf("invalid relay backend %q: must be 'udp' or 'crd'", cfg.RelayBackend)
}
// Resolve relay namespace from flag or POD_NAMESPACE env var
if cfg.RelayBackend == "crd" {
    if cfg.RelayNamespace == "" {
        cfg.RelayNamespace = os.Getenv("POD_NAMESPACE")
    }
    if cfg.RelayNamespace == "" {
        return fmt.Errorf("relay backend is 'crd' but no namespace could be resolved: set --relay-namespace or POD_NAMESPACE env var")
    }
}
```

## Spec Alignment

### Requirement: Runtime backend selection
✅ `--relay-backend` flag with "udp" (default) or "crd" values
✅ Invalid backend rejected at startup
✅ Scenario: Default backend — "udp" is default
✅ Scenario: CRD backend selected — accepted and stored
✅ Scenario: Invalid backend rejected — error thrown

### Requirement: CRD backend namespace resolution
✅ `--relay-namespace` flag for explicit namespace
✅ Falls back to `POD_NAMESPACE` env var
✅ Scenario: Namespace from downward API — POD_NAMESPACE used when flag empty
✅ Scenario: No namespace resolvable — fail fast if CRD backend selected

## Verification

- Code compiles without errors: ✅
- Default values match spec: ✅
  - `--relay-backend` defaults to "udp"
  - `--relay-gc-interval` defaults to 60s (1 minute)
  - `--relay-namespace` defaults to empty (falls back to env var)
- Validation logic correct: ✅
- Error messages clear: ✅

## Commits

```
08b2550 feat(relay): add CLI flags and validation for relay backend config
```

## Concerns

None. Implementation is complete and straightforward per spec.
