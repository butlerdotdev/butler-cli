# Butler CLI

Two separate CLI binaries: `butleradm` for platform administrators and `butlerctl` for platform users. This follows the kubeadm/kubectl pattern (see ADR-001-dual-cli-pattern.md).

**Go module**: `github.com/butlerdotdev/butler-cli`
**CLI framework**: Cobra
**Entry points**: `cmd/butleradm/main.go`, `cmd/butlerctl/main.go`

## Directory Structure

```
cmd/
  butleradm/main.go            — Admin CLI entry point
  butlerctl/main.go             — User CLI entry point
configs/examples/
  bootstrap-harvester.yaml      — Example ClusterBootstrap for Harvester
  bootstrap-nutanix.yaml        — Example ClusterBootstrap for Nutanix
  bootstrap-single-node.yaml    — Example single-node bootstrap
  bootstrap-test.yaml           — Test bootstrap config
docs/architecture/
  ADR-001-dual-cli-pattern.md   — Why two CLIs
  ADR-002-crds-as-api.md        — Why CRDs are the API
  DESIGN.md                     — Overall design document
internal/
  adm/                          — butleradm implementation
    cmd/root.go                 — Root cobra command for butleradm
    bootstrap/
      bootstrap.go              — Bootstrap orchestration
      harvester.go              — Harvester-specific bootstrap
      nutanix.go                — Nutanix-specific bootstrap
      manifests/
        controllers/            — Embedded controller manifests (bootstrap, provider)
        crds/                   — Embedded CRD YAML (all Butler CRDs)
        deployer.go             — Manifest deployment to cluster
        embed.go                — Go embed directives
      orchestrator/
        config.go               — Bootstrap config parsing
        orchestrator.go         — Bootstrap step orchestration
    provider/provider.go        — Provider management commands
    status/status.go            — Platform status commands
  common/                       — Shared between adm and ctl
    client/client.go            — Kubernetes client setup
    log/logger.go               — Structured logging (slog)
    output/
      format.go                 — Output formatting (table, JSON, YAML)
      help.go                   — Custom help templates
  ctl/                          — butlerctl implementation
    cmd/root.go                 — Root cobra command for butlerctl
    cluster/
      cluster.go                — Cluster parent command
      create.go                 — Create TenantCluster
      destroy.go                — Delete TenantCluster
      export.go                 — Export cluster config
      helpers.go                — Shared cluster helpers
      kubeconfig.go             — Get kubeconfig for cluster
      list.go                   — List TenantClusters
      scale.go                  — Scale worker count
```

## butleradm Commands

`butleradm` is for platform operators who manage the Butler platform itself.

```
butleradm bootstrap             — Bootstrap a new Butler management cluster
  --config <path>               — Path to ClusterBootstrap YAML
  --provider harvester|nutanix  — Infrastructure provider
  --dry-run                     — Show what would happen

butleradm provider              — Manage infrastructure providers
  list                          — List registered providers
  add                           — Register a new provider
  test                          — Test provider connectivity

butleradm status                — Platform health status
```

The bootstrap process:
1. Parses ClusterBootstrap YAML config
2. Provisions VMs via the provider (Harvester API / Nutanix Prism)
3. Generates and applies Talos configs to each node
4. Bootstraps the first control plane node
5. Gets kubeconfig
6. Installs core addons: kube-vip, Cilium, MetalLB, cert-manager, Longhorn
7. Installs Butler CRDs and controllers
8. Installs Steward (Kamaji fork) for hosted control planes
9. Creates initial ButlerConfig and ProviderConfig resources
10. Optionally pivots (for HA: moves management to itself)

Embedded manifests: The bootstrap process embeds all CRD YAML and controller deployment manifests directly into the binary via `go:embed`. This means CRD changes in butler-api must be synced to `internal/adm/bootstrap/manifests/crds/`.

## butlerctl Commands

`butlerctl` is for platform users who consume tenant clusters.

```
butlerctl cluster               — Manage tenant clusters
  list                          — List clusters (filtered by team context)
  create                        — Create a new TenantCluster
    --name <n>
    --kubernetes-version <ver>
    --workers <count>
    --team <team>
    --provider <provider>
  destroy <namespace/name>      — Delete a cluster
  scale <namespace/name>        — Scale workers
    --workers <count>
  kubeconfig <namespace/name>   — Get kubeconfig
    --output <path>
  export <namespace/name>       — Export cluster YAML
```

Both CLIs operate directly against the Kubernetes API of the management cluster using the current kubeconfig context. They create/read/update/delete Butler CRDs.

## Architecture Decision: CRDs as API (ADR-002)

The CLIs do NOT talk to butler-server. They talk directly to the Kubernetes API. The CRDs are the API contract. This means:
- Users need kubeconfig access to the management cluster
- RBAC is enforced at the K8s API level
- The CLI and web console have identical capabilities (both create the same CRDs)
- Offline/disconnected operation is possible (queue CRDs, sync later)

## Key Patterns

### Adding a new butlerctl command
1. Create `internal/ctl/{resource}/{action}.go`
2. Define cobra command with flags
3. Use `common/client` to get K8s client
4. Create/read/update/delete Butler CRDs using dynamic client
5. Format output using `common/output/format.go`
6. Wire command into parent in `internal/ctl/cmd/root.go`

### Adding a new butleradm command
1. Create `internal/adm/{resource}/{action}.go`
2. Define cobra command
3. Wire into `internal/adm/cmd/root.go`

### Output formatting
`common/output/format.go` supports table, JSON, and YAML output. Default is table. Users can switch with `--output json|yaml|table`.

## Build

- `make build` — Builds both `butleradm` and `butlerctl` to `bin/`
- GoReleaser handles cross-platform builds (see `.goreleaser.yaml`)
- GitHub Actions CI builds on push, creates releases with goreleaser

## What NOT to do

- Do not duplicate CRD type definitions — import from butler-api
- Do not add server-side logic — CLIs are purely client-side K8s API consumers
- Do not add web UI concerns — that's butler-console
- Do not hardcode namespace assumptions — respect team context and kubeconfig
