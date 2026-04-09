# Butler CLI Gap Analysis

**Date**: 2026-04-08
**Scope**: butler-cli repository (butlerctl + butleradm)
**Codebase**: 9,535 LOC across ~30 Go files, zero test files
**Current tagged version**: v0.5.0 (MEMORY.md), hardcoded as "v0.1.0-dev" in source

---

## Executive Summary

butler-cli has solid foundational architecture -- the dual-CLI pattern (ADR-001), CRDs-as-API (ADR-002), and the bootstrap orchestrator are well-designed. The tenant cluster lifecycle commands (create, list, get, scale, export, kubeconfig, destroy) and the ImageSync commands are feature-complete for their scope. The bootstrap process supports all 5 providers (Harvester, Nutanix, GCP, AWS, Azure) with credential flag overrides.

However, the CLI covers only 3 of 14 Butler CRDs in scope (TenantCluster, ProviderConfig, ImageSync). The console -- which talks to butler-server -- can manage Teams, Users, IdentityProviders, Addons, NetworkPools, Certificates, GitOps, Observability, and Audit logs. The CLI cannot do any of these. There are zero test files, no auth mechanism, a missing version package, and duplicated utility code across packages. (Workspace/WorkspaceTemplate are out of scope -- they belong to Butler Portal.)

The most critical gap is authentication and authorization. The CLI uses raw kubeconfig with whatever RBAC the kubeconfig carries (typically cluster-admin). There is no Butler identity, no team scoping, and no audit trail that ties operations to Butler users.

---

## Findings

### Category 1: Command Completeness

| ID | Resource | CRD Exists | butlerctl | butleradm | Console | What CLI Is Missing |
|----|----------|------------|-----------|-----------|---------|---------------------|
| G1 | TenantCluster | Yes | create, list, get, scale, export, kubeconfig, destroy | - | Full CRUD + edit + events + nodes + machines | cluster edit, cluster upgrade, cluster events, cluster nodes |
| G2 | ProviderConfig | Yes | - | list, validate | Full CRUD + test + images + networks | create, get, delete, update (butleradm); list (butlerctl) |
| G3 | Team | Yes | - | - | Full CRUD + members + groups + clusters + audit | Entire resource missing from both CLIs |
| G4 | User | Yes | - | - | Full CRUD + invite + disable/enable | Entire resource missing from both CLIs |
| G5 | IdentityProvider | Yes | - | - | Full CRUD + test + validate | Entire resource missing from both CLIs |
| G6 | ButlerConfig | Yes | - | - | get + update | Entire resource missing from both CLIs |
| G7 | TenantAddon | Yes | - | - | Full CRUD per cluster | Entire resource missing from both CLIs |
| G8 | AddonDefinition | Yes | - | - | Full CRUD (catalog) | Entire resource missing from both CLIs |
| G9 | ManagementAddon | Yes | - | - | Full CRUD | Entire resource missing from both CLIs |
| G10 | NetworkPool | Yes | - | - | Full CRUD + allocations | Entire resource missing from both CLIs |
| G11 | IPAllocation | Yes | - | - | list + release | Entire resource missing from both CLIs |
| G12 | ImageSync | Yes | list, sync, status, delete, catalog | - | Full CRUD | update (minor) |
| G13 | MachineRequest | Yes | - | - | list per cluster | list/get per cluster |
| G14 | LoadBalancerRequest | Yes | - | - | list per cluster | list per cluster |
| G15 | ClusterBootstrap | Yes | - | bootstrap (creates CB) | - | status of existing bootstraps |
| G18 | Certificate management | N/A | - | - | get + rotate + rotation-status | Entire capability missing |
| G19 | GitOps | N/A | - | - | Full config + enable/disable + discover + export + migrate | Entire capability missing |
| G20 | Observability | N/A | - | - | config + status + pipeline setup/teardown | Entire capability missing |
| G21 | Audit log | N/A | - | - | list all + list by team | Entire capability missing |

**Severity**: G1 (Medium), G2 (High), G3-G6 (High), G7-G11 (Medium), G12-G14 (Low), G15 (Low), G18-G21 (Medium)

---

### Category 2: Command Quality & UX

| ID | Issue | Severity | File:Line | Description | Proposed Fix | Est. LOC |
|----|-------|----------|-----------|-------------|--------------|----------|
| G22 | `cluster get` YAML/JSON output unimplemented | Medium | `internal/ctl/cluster/cluster.go:142` | Prints "not yet implemented" for -o yaml/json | Use the output.Printer pattern already used by `cluster list` | 15 |
| G23 | Hardcoded version string | High | `internal/adm/cmd/root.go:131`, `internal/ctl/cmd/root.go:100` | Both print "v0.1.0-dev"; goreleaser ldflags set `internal/common/version.Version` but the package does not exist | Create `internal/common/version/version.go` with `Version`, `Commit`, `Date` vars; reference in version commands | 30 |
| G24 | Makefile ldflags path mismatch | High | `Makefile:15` | Makefile uses `internal/version.Version` but goreleaser uses `internal/common/version.Version`; neither package exists | Align both to `internal/common/version` and create the package | 10 |
| G25 | No `--kubeconfig` on create command | Medium | `internal/ctl/cluster/create.go:200-304` | The `create` command calls `client.NewFromDefault()` with no kubeconfig flag, unlike `list`, `get`, `kubeconfig` which have `--kubeconfig` | Add `--kubeconfig` flag to `create` | 5 |
| G26 | No `--kubeconfig` on scale command | Medium | `internal/ctl/cluster/scale.go:66-108` | Same issue as create | Add `--kubeconfig` flag to `scale` | 5 |
| G27 | No `--kubeconfig` on destroy command | Medium | `internal/ctl/cluster/destroy.go:67-117` | Same issue as create | Add `--kubeconfig` flag to `destroy` | 5 |
| G28 | No `--kubeconfig` on export command | Medium | `internal/ctl/cluster/export.go:60-119` | Same issue as create | Add `--kubeconfig` flag to `export` | 5 |
| G29 | Workers hardcoded max 10 | Low | `internal/ctl/cluster/create.go:135`, `internal/ctl/cluster/scale.go:59` | Validation caps workers at 10; production clusters may need more | Make configurable or raise to 100 with ButlerConfig integration | 5 |
| G30 | No shell completion generation command | Medium | Both CLIs | Cobra supports `completion` subcommand but neither CLI registers it | Add `butlerctl completion bash/zsh/fish/powershell` and same for butleradm | 10 per CLI |
| G31 | Duplicated `getNestedString` helper | Low | `internal/ctl/cluster/helpers.go:76`, `internal/ctl/image/image.go:773`, `internal/adm/provider/provider.go:531`, `internal/adm/status/status.go` (imports unstructured directly) | Four separate implementations of the same helper | Move to `internal/common/client/helpers.go` and import from one place | 20 |
| G32 | Duplicated `getClient` helper | Low | `internal/ctl/image/image.go:757`, `internal/adm/provider/provider.go:524` | Two identical implementations | Move to `internal/common/client/` | 10 |
| G33 | Custom `contains`, `splitLines`, `trimSpace` reimplementations | Low | `internal/ctl/cluster/helpers.go:332-379` | Reimplements standard library functions (`strings.Contains`, `strings.Split`, `strings.TrimSpace`) | Replace with stdlib calls | -20 (removal) |
| G34 | No `--context` flag | Low | All commands | kubectl supports `--context` for selecting kubeconfig context; butler-cli does not | Add `--context` persistent flag to both root commands | 15 per CLI |
| G35 | `cluster create` defaults to `butler-tenants` namespace | Medium | `internal/ctl/cluster/create.go:105` | In multi-tenant setups, namespace should be `team-{name}`; hardcoded default may confuse users | Document that `--namespace` should be team namespace; auto-detect from ButlerConfig if possible | 20 |
| G36 | No interactive mode for cluster create | Low | `internal/ctl/cluster/create.go` | No interactive prompts for required fields; contrast with console's guided wizard | Add `--interactive` flag with prompts using bubbletea (already a dependency via lipgloss) | 150 |
| G37 | Provider validate missing AWS and Azure | Medium | `internal/adm/provider/provider.go:211-224` | Switch statement handles nutanix, harvester, proxmox, gcp but not aws or azure | Add `validateAWS` and `validateAzure` functions | 80 |
| G110 | No `--output wide` or custom column support | Low | `internal/ctl/cluster/list.go`, `internal/adm/provider/provider.go` | List commands support table/json/yaml but not `--output wide` (extra columns) or custom column selection | Add `wide` format variant to output.Printer with additional columns | 15 per list cmd |

---

### Category 3: Bootstrap Completeness

| ID | Issue | Severity | File:Line | Description | Proposed Fix | Est. LOC |
|----|-------|----------|-----------|-------------|--------------|----------|
| G38 | No Proxmox bootstrap command | Medium | `internal/adm/bootstrap/bootstrap.go:53-58` | Bootstrap registers harvester, nutanix, gcp, aws, azure but not proxmox. ProxmoxProviderConfig exists in config.go | Add `NewProxmoxCmd` following the pattern of other provider commands | 80 |
| G39 | No partial failure recovery | Medium | `internal/adm/bootstrap/orchestrator/orchestrator.go:116-246` | If bootstrap fails mid-way, `--skip-cleanup` preserves KIND but there is no `butleradm bootstrap resume` command | Add `resume` subcommand that reconnects to existing KIND cluster | 200 |
| G40 | No bootstrap status command | Medium | N/A | After bootstrap starts, users must rely on kubectl or TUI; no `butleradm bootstrap status` to check an in-progress bootstrap | Add `bootstrap status` that watches ClusterBootstrap CR phase | 80 |
| G41 | Bootstrap TUI in progress on feature branch | Low | `CLAUDE.md:140-168` | CLAUDE.md documents a Bubbletea TUI; files are on `feat/bootstrap-tui` branch (WIP, not merged). CLAUDE.md accurately describes the design intent but main branch does not have the files yet. | No action needed until branch is merged; CLAUDE.md is aspirational documentation for the in-progress feature | 0 |
| G42 | Signal handling duplication | Low | `internal/adm/bootstrap/harvester.go:68-79`, `nutanix.go:71-79`, `aws.go:71-79`, `azure.go:74-79`, `gcp.go:70-79` | Identical signal handling code in all 5 provider commands | Extract to shared function in bootstrap package | -50 (net reduction) |
| G43 | No `--no-tui` flag | Low | All bootstrap commands | CLAUDE.md references `--no-tui` flag but it doesn't exist (TUI doesn't exist either) | Defer until TUI is implemented | 0 |

---

### Category 4: Distribution & Packaging

| ID | Issue | Severity | File:Line | Description | Proposed Fix | Est. LOC |
|----|-------|----------|-----------|-------------|--------------|----------|
| G44 | Missing version package | High | N/A | Goreleaser ldflags inject `internal/common/version.Version`, `Commit`, `Date` but the package does not exist; Makefile uses different path `internal/version.*` | Create `internal/common/version/version.go` with exported vars | 15 |
| G45 | Homebrew uses deprecated `homebrew_casks` key | Low | `.goreleaser.yaml:69` | GoReleaser v2 renamed this; current config may fail | Verify against goreleaser docs and update | 2 |
| G46 | No shell completion in release | Low | `.goreleaser.yaml` | Shell completions not generated or bundled in release archives | Add `nfpms` or post-hooks to generate completions | 20 |
| G47 | Archives bundle both binaries together | Low | `.goreleaser.yaml:46-56` | Single archive contains both butleradm and butlerctl; users wanting only butlerctl must download both | Create separate archives per binary (one for each) plus a combined one | 20 |
| G48 | No Docker image for CLI | Low | N/A | Makefile has `docker-build` but no Dockerfile exists in the repo | Add multi-stage Dockerfile | 20 |
| G49 | `docs` Makefile target calls nonexistent `generate docs` subcommand | Low | `Makefile:128-131` | `butleradm generate docs` and `butlerctl generate docs` don't exist | Add cobra doc generation command or remove target | 30 |

---

### Category 5: Code Quality & Patterns

| ID | Issue | Severity | File:Line | Description | Proposed Fix | Est. LOC |
|----|-------|----------|-----------|-------------|--------------|----------|
| G50 | Zero test files | Critical | Entire repo | `find . -name "*_test.go"` returns nothing; 0% coverage | Add unit tests for: validation logic, helpers, output formatting, client construction. Add integration tests for command execution. | 1500+ |
| G51 | Shell-outs to kubectl and docker in orchestrator | Low | `orchestrator.go:598,607` | `exec.Command("kubectl", ...)` for CoreDNS patch instead of using the Go client already created | Replace kubectl shell-out with clientset ConfigMap patch | 20 |
| G52 | `buildTenantCluster` is 83 lines | Low | `internal/ctl/cluster/create.go:456-540` | Large function but well-structured; acceptable | No change needed | 0 |
| G53 | `runStatus` is 120+ lines | Low | `internal/adm/status/status.go:109-226` | Long function with many sequential checks; could be decomposed | Extract component checks into a registry pattern | 40 |
| G54 | Error wrapping inconsistency | Low | Various | Most errors use `fmt.Errorf("doing X: %w", err)` consistently but some use bare `fmt.Errorf` without wrapping (e.g., `orchestrator.go` validation errors) | Audit and ensure all error returns from external calls use `%w` | 20 |
| G55 | Package-level `var` for flag state in create.go | Low | `internal/ctl/cluster/create.go:307-311` | `memoryFlag`, `diskFlag`, `lbPoolFlag` are package-level vars; can cause issues if command is reused in tests | Move into CreateOptions or use cobra flag retrieval | 15 |
| G56 | `SetVerbose` does not propagate to existing logger | Low | `internal/common/log/logger.go:73-76` | `SetVerbose` sets `l.level` but the prettyHandler was created with the old level; changing `l.level` has no effect because the handler has its own copy | Share the level via pointer or atomic; or recreate the handler | 15 |
| G57 | No linting in CI | Medium | `.github/workflows/release.yaml` | Only a release workflow exists; no CI for PRs with linting, vetting, or testing | Add CI workflow with `go vet`, `golangci-lint`, `go test` | 30 (workflow) |

---

### Category 6: Feature Parity with Console

The console (butler-console via butler-server) provides these capabilities that the CLI lacks entirely:

| ID | Console Capability | Console Pages/Components | Server Endpoints | CLI Gap | Severity |
|----|-------------------|--------------------------|------------------|---------|----------|
| G58 | Cluster spec editing | EditClusterModal | `PUT /clusters/{ns}/{name}` | No `butlerctl cluster edit` command | Medium |
| G59 | Kubernetes version upgrade | EditClusterModal (k8sVersion field) | `PUT /clusters/{ns}/{name}` | No `butlerctl cluster upgrade` command | Medium |
| G60 | Certificate management | CertificatesTab, RotationModals, CertificateHealthOverview | `GET/POST /clusters/{ns}/{name}/certificates/*` | No certificate visibility or rotation | Medium |
| G61 | GitOps integration | GitOpsTab, GitProviderSetup, EnableGitOpsModal, ExportModal, MigrateAllModal | `GET/POST/DELETE /clusters/{ns}/{name}/gitops/*` + `/git/config` | No GitOps management | Medium |
| G62 | Tenant addon management | AddonsTab | `GET/POST/PUT/DELETE /clusters/{ns}/{name}/addons/*` | No addon install/uninstall/update | High |
| G63 | Addon catalog | AddonCatalogPage | `GET /addons/catalog`, `POST/PUT/DELETE /addons/catalog/{name}` | No catalog browsing or management | Medium |
| G64 | Management addon management | ManagementAddonsTab | `GET/POST/PUT/DELETE /management/addons/*` | No management addon lifecycle | Medium |
| G65 | Team management | AdminTeamsPage, TeamMembersPage, TeamSettingsPage | `GET/POST/PUT/DELETE /teams/*` | No team CRUD, member management, or group sync | High |
| G66 | User management | UsersPage | `GET/POST/DELETE /users/*` + invite + disable/enable | No user lifecycle management | High |
| G67 | Identity provider management | IdentityProvidersPage, CreateIdentityProviderPage | `GET/POST/PUT/DELETE /identity-providers/*` | No IdP configuration | High |
| G68 | Network pool management | NetworkPoolsPage, NetworkPoolDetailPage, CreateNetworkPoolModal | `GET/POST/PUT/DELETE /networks/*` | No IPAM management | Medium |
| G69 | Cluster events | ClusterDetailPage (events tab) | `GET /clusters/{ns}/{name}/events` | No event streaming or listing | Low |
| G70 | Cluster nodes view | ClusterDetailPage (nodes tab) | `GET /clusters/{ns}/{name}/nodes` | No node listing | Low |
| G71 | Dashboard/overview | DashboardPage, OverviewPage | Multiple aggregation endpoints | No summary view (butleradm status is partial) | Low |
| G72 | Real-time updates | WebSocketProvider | `WS /ws/clusters` | No watch/stream capability | Low |
| G73 | Observability pipeline | ObservabilityPage, ObservabilityTab, EnableMetricsModal | `GET/PUT /observability/*` | No observability management | Low |
| G74 | Provider create/delete | CreateProviderPage, ProvidersPage | `POST/DELETE /providers/*` | butleradm can list/validate but not create/delete providers | High |
| G76 | ButlerConfig management | SettingsPage | `GET/PUT /config` | No platform config viewing or editing | Medium |
| G77 | Audit log viewing | AuditLogPage | `GET /audit` | No audit log access | Low |
| G78 | Terminal access | TerminalPage, ClusterTerminal | `WS /terminal/*` | No in-CLI terminal proxy | Low |
| G79 | Steward TCP viewing | ManagementPage | `GET /management/tenantcontrolplanes/*` | No TCP inspection | Low |
| G80 | Cluster machine request listing | ClusterDetailPage | `GET /clusters/{ns}/{name}/machines` | No MachineRequest visibility | Low |

---

### Category 7: Missing Commands for Day-2 Operations

| ID | Command | Description | Severity | Est. LOC |
|----|---------|-------------|----------|----------|
| G81 | `butlerctl cluster upgrade` | Patch kubernetesVersion in TenantCluster spec; optionally wait for rolling update | High | 100 |
| G82 | `butlerctl cluster edit` | Open TenantCluster spec in $EDITOR (like `kubectl edit`) or accept field-level patches | Medium | 120 |
| G83 | `butlerctl cluster conditions` | Show conditions array from TenantCluster status in table format | Low | 40 |
| G84 | `butlerctl cluster events` | List Kubernetes Events related to the TenantCluster and its child resources | Medium | 80 |
| G85 | `butlerctl cluster nodes` | List nodes of a tenant cluster (fetches from tenant kubeconfig) | Medium | 80 |
| G86 | `butlerctl addon list` | List available addons from AddonDefinition catalog | Medium | 60 |
| G87 | `butlerctl addon install` | Install an addon on a tenant cluster (create TenantAddon CR) | Medium | 80 |
| G88 | `butlerctl addon status` | Show addon installation status on a cluster | Low | 40 |
| G89 | `butlerctl addon uninstall` | Remove an addon from a tenant cluster | Low | 40 |
| G90 | `butleradm config get` | Display current ButlerConfig | Medium | 40 |
| G91 | `butleradm config set` | Patch individual ButlerConfig fields | Medium | 60 |
| G92 | `butleradm team list/create/delete` | Team lifecycle management | High | 200 |
| G93 | `butleradm team add-member/remove-member` | Team membership management | Medium | 100 |
| G94 | `butleradm user list/create/delete/invite` | User lifecycle management | High | 200 |
| G95 | `butleradm idp list/create/delete/test` | IdentityProvider management | Medium | 200 |
| G96 | `butleradm network list/create/delete` | NetworkPool management | Medium | 150 |
| G97 | `butleradm provider create/delete` | Full provider lifecycle | High | 150 |
| G98 | `butleradm addon catalog list/add/remove` | AddonDefinition catalog management | Medium | 120 |
| G99 | `butleradm addon install/uninstall` | ManagementAddon lifecycle on management cluster | Medium | 100 |
| G100 | `butleradm upgrade` | Upgrade Butler platform components (referenced in root.go TODO at line 97) | Medium | 200 |
| G101 | `butlerctl cluster watch` | Watch cluster status changes in real-time (like `kubectl get -w`) | Low | 60 |
| G102 | `butleradm doctor` | Diagnostics command that checks all system health in one pass | Medium | 150 |

---

### Category 8: Authentication & Authorization (CRITICAL)

#### Current State

The CLI has **zero authentication or authorization**. Both CLIs operate directly against the Kubernetes API using whatever kubeconfig is available. There is no:

1. **Butler identity mapping**: The CLI does not know who the Butler user is. Kubernetes RBAC sees the kubeconfig's identity (typically a certificate CN), not a Butler User/Team.

2. **Butler RBAC enforcement**: The console enforces admin/operator/viewer roles through butler-server's `SessionMiddleware` which checks Team membership on every request. The CLI bypasses this entirely.

3. **Audit trail**: butler-server logs every API request with the authenticated Butler user. CLI operations appear in Kubernetes audit logs under the kubeconfig identity, which is typically the same admin cert for all users.

4. **Team scoping**: The console automatically scopes operations to the user's current team. The CLI defaults to `butler-tenants` namespace and requires manual `-n` flag usage.

#### Specific Findings

| ID | Issue | Severity | File:Line | Description |
|----|-------|----------|-----------|-------------|
| G103 | No auth mechanism exists | Critical | All commands | Every command calls `client.NewFromDefault()` which uses raw kubeconfig; no Butler auth layer | 
| G104 | Destroy has RBAC TODO but no implementation | High | `internal/ctl/cluster/destroy.go:39-51` | Comments document future RBAC fields (`Team`, `RequireRole`) and a permission check placeholder at line 144-149, but nothing is implemented |
| G105 | No `butlerctl login` command | Critical | N/A | No way to authenticate as a Butler user; contrast with `oc login`, `rancher login`, `argocd login` |
| G106 | No `butlerctl context` command | High | N/A | No way to switch between Butler contexts (management clusters, teams) |
| G107 | Default namespace bypasses team scoping | High | `internal/ctl/cluster/helpers.go:34` | `DefaultTenantNamespace = "butler-tenants"` means commands default to a shared namespace rather than a team-scoped one |
| G108 | No token/session management | Critical | N/A | No `~/.butler/credentials` or token cache; no JWT handling |
| G109 | No RBAC-aware error messages | Medium | All commands | When a user gets a Kubernetes 403, the error is raw API server text; should explain Butler RBAC |

#### Analysis: What Auth Should Look Like

**Option A: OAuth Device Flow via butler-server**

```
butlerctl login --server https://butler.example.com
> Opening browser for authentication...
> Alternatively, visit: https://butler.example.com/device?code=ABCD-1234
> Waiting for authentication...
> Authenticated as alice@example.com
> Teams: platform-team (admin), dev-team (operator)
> Current team: platform-team
> Kubeconfig context: butler-platform-team
```

The flow:
1. CLI initiates OAuth device flow with butler-server
2. User authenticates in browser (SSO or internal auth)
3. butler-server returns a scoped kubeconfig (SA token per user/team)
4. CLI stores the token in `~/.butler/credentials`
5. Subsequent commands use the scoped token
6. butler-server can audit all CLI operations through the SA

**Option B: Scoped ServiceAccount kubeconfigs per user/team**

```
butleradm user create alice --email alice@co.com --team dev-team --role operator
> ServiceAccount butler-system/alice-dev-team created
> RBAC: operator role on namespace team-dev-team
> Kubeconfig: ~/.butler/alice-dev-team-kubeconfig
```

The flow:
1. Admin creates User CRD + ServiceAccount + RoleBinding
2. CLI auto-discovers kubeconfig from `~/.butler/` directory
3. SA token restricts operations to the team namespace
4. Kubernetes audit log identifies the SA (and thus the user)

**Option C: butler-server as API proxy**

Break ADR-002 and route CLI through butler-server instead of direct K8s API access. This would give full auth/audit but contradicts the architectural decision.

#### Recommendation

**Option A (OAuth device flow)** is the right approach because:

1. It aligns with ADR-002 -- the CLI still creates CRDs directly, but with a scoped kubeconfig obtained through butler-server auth
2. butler-server already has full auth infrastructure (JWT, OIDC, internal users)
3. It mirrors what competitors do (`oc login`, `rancher login`)
4. It enables team scoping automatically
5. It enables audit through Kubernetes audit logs (SA identity = Butler user)
6. The scoped kubeconfig can have RBAC that matches the Butler role (admin/operator/viewer)

**Implementation path**: butler-server adds a `/auth/device-flow` endpoint that returns a scoped kubeconfig. The CLI adds `login`, `logout`, `context switch`, and `context list` commands. All existing commands gain auth by using the scoped kubeconfig from `~/.butler/credentials` instead of the raw management cluster kubeconfig.

#### Competitor Analysis

| Tool | Auth Mechanism | Team/Org Scoping |
|------|---------------|------------------|
| `oc login` (OpenShift) | OAuth token via server | Project context switching |
| `rancher login` (Rancher) | API key/token via server | Cluster/project context |
| `argocd login` | SSO/password via server | Project RBAC |
| `kubectl` | kubeconfig (raw) | Namespace only |
| `helm` | kubeconfig (raw) | Namespace only |
| `butlerctl` (current) | kubeconfig (raw) | Namespace only |

---

## Summary Table

| ID | Category | Severity | Description |
|----|----------|----------|-------------|
| G1 | 1 | Medium | TenantCluster missing edit, upgrade, events, nodes |
| G2 | 1 | High | ProviderConfig missing create, get, delete, update |
| G3 | 1 | High | Team -- zero CLI support |
| G4 | 1 | High | User -- zero CLI support |
| G5 | 1 | High | IdentityProvider -- zero CLI support |
| G6 | 1 | High | ButlerConfig -- zero CLI support |
| G7 | 1 | Medium | TenantAddon -- zero CLI support |
| G8 | 1 | Medium | AddonDefinition -- zero CLI support |
| G9 | 1 | Medium | ManagementAddon -- zero CLI support |
| G10 | 1 | Medium | NetworkPool -- zero CLI support |
| G11 | 1 | Low | IPAllocation -- zero CLI support |
| G12 | 1 | Low | ImageSync missing update |
| G13 | 1 | Low | MachineRequest not visible via CLI |
| G14 | 1 | Low | LoadBalancerRequest not visible via CLI |
| G15 | 1 | Low | ClusterBootstrap status not available post-bootstrap |
| G18 | 1 | Medium | Certificate management missing |
| G19 | 1 | Medium | GitOps management missing |
| G20 | 1 | Medium | Observability management missing |
| G21 | 1 | Low | Audit log viewing missing |
| G22 | 2 | Medium | cluster get -o yaml/json unimplemented |
| G23 | 2 | High | Hardcoded version "v0.1.0-dev" |
| G24 | 2 | High | Makefile ldflags path mismatch with goreleaser |
| G25-G28 | 2 | Medium | Missing --kubeconfig on create, scale, destroy, export |
| G29 | 2 | Low | Workers hardcoded max 10 |
| G30 | 2 | Medium | No shell completion generation command |
| G31-G32 | 2 | Low | Duplicated helper functions |
| G33 | 2 | Low | Reimplemented stdlib string functions |
| G34 | 2 | Low | No --context flag |
| G35 | 2 | Medium | Default namespace ignores team context |
| G36 | 2 | Low | No interactive mode for cluster create |
| G37 | 2 | Medium | Provider validate missing AWS/Azure |
| G38 | 3 | Medium | No Proxmox bootstrap command |
| G39 | 3 | Medium | No bootstrap resume/recovery |
| G40 | 3 | Medium | No bootstrap status command |
| G41 | 3 | Low | TUI in progress on feat/bootstrap-tui branch; CLAUDE.md docs are aspirational |
| G42 | 3 | Low | Signal handling duplication in bootstrap commands |
| G43 | 3 | Low | No --no-tui flag (TUI doesn't exist) |
| G44 | 4 | High | Version package missing entirely |
| G45 | 4 | Low | GoReleaser homebrew_casks deprecation |
| G46 | 4 | Low | Shell completions not in release |
| G47 | 4 | Low | Archives bundle both binaries together |
| G48 | 4 | Low | No Dockerfile |
| G49 | 4 | Low | docs Makefile target broken |
| G50 | 5 | Critical | Zero test files, 0% coverage |
| G51 | 5 | Low | Shell-outs to kubectl in orchestrator |
| G52-G53 | 5 | Low | Long functions (acceptable) |
| G54 | 5 | Low | Error wrapping inconsistency |
| G55 | 5 | Low | Package-level flag vars |
| G56 | 5 | Low | SetVerbose doesn't propagate |
| G57 | 5 | Medium | No CI workflow for PRs |
| G58-G74,G76-G80 | 6 | Various | Console feature parity gaps (see Category 6 table; G75 removed -- Workspace belongs to Butler Portal) |
| G81-G102 | 7 | Various | Day-2 operation commands (see Category 7 table) |
| G103-G109 | 8 | Critical | Authentication & authorization gaps |
| G110 | 2 | Low | No --output wide or custom column support on list commands |

---

## Recommended Execution Order

### ADR-016: CLI Authentication (design dependency)

Not a code phase. This is an architecture decision that must be written and reviewed before Phase C items that require team scoping. Phases A and B execute independently and do not wait on this ADR.

**Est. LOC** (once ADR is approved): 500 (CLI) + 300 (butler-server device-flow endpoint)

Scope of the ADR:

1. Design doc: OAuth device flow via butler-server
2. butler-server: `/auth/device-flow` endpoint returning scoped kubeconfig
3. butler-cli: `butlerctl login`, `butlerctl logout`, `butlerctl context list/use`
4. butler-cli: Credential storage in `~/.butler/credentials`
5. butler-cli: Modify `client.NewFromDefault()` to prefer Butler credentials
6. butler-cli: Team-scoped default namespace from Butler context

Findings addressed: G103, G104, G105, G106, G107, G108, G109

### Phase A: Critical Gaps
**Est. LOC**: ~575

1. **G44/G23/G24**: Create `internal/common/version/version.go`, fix both version commands and Makefile ldflags (30 LOC)
2. **G50**: Add unit tests for validation logic, helpers, output formatting (target: 60% coverage on utility code) (500 LOC)
3. **G22**: Implement `cluster get -o yaml/json` output (15 LOC)
4. **G57**: Add CI workflow with go vet + golangci-lint + go test (30 LOC workflow)

### Phase B: Quality Improvements
**Est. LOC**: ~100 (net)

1. **G25-G28**: Add `--kubeconfig` flag to create, scale, destroy, export (20 LOC)
2. **G30**: Add `completion` subcommand to both CLIs (20 LOC)
3. **G31-G33**: Deduplicate helpers and remove stdlib reimplementations (net -30 LOC)
4. **G37**: Add AWS and Azure provider validate (80 LOC)
5. **G42**: Extract shared signal handling in bootstrap commands (-50 LOC net)
6. **G55-G56**: Fix package-level vars and SetVerbose propagation (30 LOC)
7. **G34**: Add `--context` persistent flag (30 LOC)

### Phase C: Feature Parity with Console
**Est. LOC**: ~2,200

Requires ADR-016 for items that need team scoping (G65, G66, G92-G94). Items without team dependency can proceed independently.

Priority order based on user impact:

1. **G62/G86-G89**: `butlerctl addon list/install/status/uninstall` (220 LOC)
2. **G65/G92-G93**: `butleradm team list/create/delete/add-member/remove-member` (300 LOC) -- depends on ADR-016
3. **G74/G97**: `butleradm provider create/delete` (150 LOC)
4. **G66/G94**: `butleradm user list/create/delete/invite` (200 LOC) -- depends on ADR-016
5. **G76/G90-G91**: `butleradm config get/set` (100 LOC)
6. **G67/G95**: `butleradm idp list/create/delete/test` (200 LOC)
7. **G58/G82**: `butlerctl cluster edit` -- supports both `$EDITOR` mode (like `kubectl edit`) and inline field patches via flags (`butlerctl cluster edit my-cluster --workers-replicas 5 --workers-cpu 8`); flag mode enables scripting and CI pipelines (150 LOC)
8. **G59/G81**: `butlerctl cluster upgrade` (100 LOC)
9. **G68/G96**: `butleradm network list/create/delete` (150 LOC)
10. **G63/G98-G99**: `butleradm addon catalog list/add/remove` + management addon install (220 LOC)
11. **G60**: Certificate management commands (100 LOC)
12. **G61/G19**: GitOps management commands (200 LOC)

### Phase D: Day-2 Operational Commands
**Est. LOC**: ~970

1. **G84**: `butlerctl cluster events` (80 LOC)
2. **G85**: `butlerctl cluster nodes` (80 LOC)
3. **G83**: `butlerctl cluster conditions` (40 LOC)
4. **G101**: `butlerctl cluster watch` (60 LOC)
5. **G102**: `butleradm doctor` (150 LOC)
6. **G100**: `butleradm upgrade` (200 LOC)
7. **G38**: Proxmox bootstrap command (80 LOC)
8. **G39-G40**: Bootstrap resume and status (280 LOC)

### Phase E: Polish
**Est. LOC**: ~530

1. **G36**: Interactive cluster creation mode (150 LOC)
2. **G47**: Separate release archives per binary (20 LOC config)
3. **G46**: Shell completions in release (20 LOC config)
4. **G48**: Dockerfile (20 LOC)
5. **G49**: Fix docs Makefile target (30 LOC)
6. **G29**: Configurable worker max (5 LOC)
7. **G35**: Team-aware default namespace (20 LOC, depends on ADR-016)
8. **G45**: GoReleaser config update (2 LOC)
9. **G72**: Watch/stream capability for cluster status (100 LOC)
10. **G69-G71**: Dashboard/overview improvements (150 LOC)
11. **G110**: `--output wide` and custom column support on list commands (15 LOC per list command)

---

## LOC Estimates by Phase

| Phase | Description | Estimated LOC |
|-------|-------------|---------------|
| ADR-016 | Auth architecture (design only, code after approval) | 800 |
| A | Critical gaps | 575 |
| B | Quality improvements | 100 (net) |
| C | Console feature parity | 2,200 |
| D | Day-2 operations | 970 |
| E | Polish | 530 |
| **Total** | | **~5,175** |
