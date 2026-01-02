# ADR-001: Dual CLI Pattern

## Status

Accepted

## Context

Butler needs command-line tools for two distinct user groups with different needs:

1. **Platform Operators**: Manage the Butler platform itself (bootstrap, upgrade, backup)
2. **Platform Users**: Consume the platform to run workloads (create clusters, manage addons)

We needed to decide how to structure the CLI tooling:

1. **Single CLI with subcommands**: `butler adm bootstrap`, `butler ctl cluster create`
2. **Single CLI with role flags**: `butler --admin bootstrap`, `butler cluster create`
3. **Two separate CLIs**: `butleradm`, `butlerctl`
4. **kubectl plugins**: `kubectl butler-adm`, `kubectl butler-ctl`

## Decision

We implement two separate CLI binaries: `butleradm` and `butlerctl`.

Each binary:
- Has its own entry point and command tree
- Can be distributed independently
- Has clear scope and purpose
- Follows the kubeadm/kubectl pattern familiar to Kubernetes users

Shared code (logging, Kubernetes client utilities) lives in `internal/common/` and is compiled into both binaries.

## Consequences

### Positive

- Clear mental model for users about which tool to use
- Platform users do not need admin tooling installed
- RBAC can be configured per-tool based on intended use
- Smaller binary size for users who only need one tool
- Independent release cycles possible if needed
- Familiar pattern from Kubernetes ecosystem (kubeadm, kubectl, kubelet)

### Negative

- Two binaries to build, test, and release
- Some code duplication in command structure boilerplate
- Users must install both tools for full functionality
- Version synchronization between tools must be maintained

### Neutral

- Both tools share the same underlying CRD-based architecture
- Both tools can be built from the same repository
- Installation can bundle both tools together for convenience
