# Task 9: Add kubebuilder RBAC marker

## Status: ✅ Completed

## Work done

1. **Added kubebuilder RBAC marker** — Created `internal/relay/doc.go` with the required marker:
   ```
   // +kubebuilder:rbac:groups=pmtud.cloud.sap,resources=pmtunoderelays,verbs=get;list;watch;create;delete
   ```

2. **Ran `make generate`** — Executed controller-gen to process RBAC markers and generate the ClusterRole.

3. **Verified role includes pmtunoderelays** — Checked `config/rbac/role.yaml` and confirmed it contains:
   ```yaml
   - apiGroups:
     - pmtud.cloud.sap
     resources:
     - pmtunoderelays
     verbs:
     - create
     - delete
     - get
     - list
     - watch
   ```

4. **Committed changes**:
   - Commit: `afe2ca6 feat(relay): add kubebuilder RBAC marker for pmtunoderelays resource`
   - Files: `internal/relay/doc.go` (new), `config/rbac/role.yaml` (generated)

## Verification

- ✅ doc.go file created with RBAC marker
- ✅ `make generate` successful
- ✅ ClusterRole includes pmtunoderelays resource with all required verbs (get, list, watch, create, delete)
- ✅ Commit created

## Commits

- `afe2ca6` feat(relay): add kubebuilder RBAC marker for pmtunoderelays resource
