# ADR-002: CRDs as the API Contract

## Status

Accepted

## Context

Butler needs an API layer that supports multiple clients: CLI tools, web console, and direct kubectl usage. We needed to decide how to structure this API:

1. **REST API**: Traditional HTTP endpoints with OpenAPI spec
2. **gRPC API**: Protocol buffer definitions with generated clients
3. **Kubernetes CRDs**: Custom Resources with controller reconciliation
4. **Hybrid**: REST/gRPC for synchronous operations, CRDs for async

## Decision

All Butler operations are expressed as Kubernetes Custom Resources. The CLIs and Console are thin clients that create CRs and watch their status. Controllers perform the actual work.

Core CRDs:
- `ClusterBootstrap`: Management cluster lifecycle
- `MachineRequest`: VM provisioning requests (provider-agnostic)
- `ProviderConfig`: Infrastructure provider credentials and settings
- `TenantCluster`: Tenant cluster lifecycle
- `PlatformAddon`: Addon installation requests
- `ClusterAccess`: RBAC and access grants

## Consequences

### Positive

- Single source of truth in Kubernetes API
- Standard Kubernetes RBAC for authorization
- Built-in audit logging via Kubernetes audit
- Watch-based updates without polling
- GitOps-compatible (store CRs in Git, apply via Flux/ArgoCD)
- kubectl works out of the box for power users
- Resumable operations (controllers reconcile to desired state)
- Familiar patterns for Kubernetes users

### Negative

- Requires Kubernetes cluster to exist (solved with KIND for bootstrap)
- Learning curve for users unfamiliar with CRDs
- Status reporting limited to what fits in CR status fields
- No true synchronous operations (must poll/watch status)

### Neutral

- CRD definitions live in separate repository (butler-api) for reuse
- Controllers must be deployed before operations work
- Schema evolution follows Kubernetes API versioning conventions
