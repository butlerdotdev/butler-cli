# ADR-011: butler-cli Impersonation Auth for apiserver Mutations

## Status

Proposed

## Date

2026-04-21

## Context

ADR-010 (butler-server/docs/architecture/ADR-010-impersonation-auth.md) established the impersonation pattern for butler-server: on webhook-gated apiserver mutations, forward the authenticated end-user's identity via `rest.Config.Impersonate` so the ADR-009 Team and TenantCluster admission webhooks see the real caller. That ADR closed the spoof hole on the server path.

butler-cli has the parallel problem one layer down. The CLI follows ADR-002 (CRDs as API): after device-flow login, CLI commands write CRDs directly against the Kubernetes API via the scoped ServiceAccount kubeconfig in `~/.butler/credentials.json`. The SA identity is `system:serviceaccount:butler-system:butler-cli-<hash>`. When `butlerctl cluster create --environment=<env>` runs against a team with `MaxClustersPerMember` set, the TC's `butler.butlerlabs.dev/creator-email` annotation carries the post-login user email, but the outgoing apiserver call presents the SA as `UserInfo.Username`. The webhook compares email to SA name, they do not match, the create is rejected.

Without a CLI-side fix, the per-member cap feature only works on raw-kubeconfig paths where the kubeconfig identity is the user's email directly (client-cert CN = email). Every device-flow-logged-in user hits the mismatch.

Raw-kubeconfig paths do not have this problem. The webhook's SAR fallback correctly identifies cluster-admin-equivalent callers, and for TC creates without MaxClustersPerMember enforcement the creator-email annotation is not identity-matched at all. The CLI-side fix must not regress this path.

## Decision

butler-cli performs client-side Kubernetes impersonation when the active credential set carries a non-empty `User.Email`. The outgoing REST config's `Impersonate` field is set from `credentials.ActiveCredential().User.Email`. The CLI's client factory wraps today's `client.New` with a conditional:

```
if cred := creds.ActiveCredential(); cred != nil && cred.User.Email != "" {
    cfg.Impersonate = rest.ImpersonationConfig{
        UserName: strings.ToLower(strings.TrimSpace(cred.User.Email)),
        Groups:   []string{"butler-api-users", "system:authenticated"},
    }
}
```

Same payload as ADR-010 butler-server: fixed `butler-api-users` group plus `system:authenticated`. Email is lowercased to match the canonicalization applied at session creation in butler-server (ADR-010 §Email canonicalization) and the `strings.EqualFold` comparison convention the admission webhook uses. OIDC groups are not forwarded for the same reasons ADR-010 cited.

**Raw-kubeconfig short-circuit:** when `User.Email` is empty (no active credential, or operator using a kubeconfig that was not issued by the device flow), no impersonation is applied. The raw kubeconfig identity reaches the apiserver unchanged and the webhook's existing SAR-fallback path handles it (see ADR-009 §Mutation authority split).

### RBAC for the device-flow SA

The device-flow SA's backing ClusterRoles (`butler-cli-admin`, `butler-cli-operator`, `butler-cli-viewer`, `butler-cli-platform-admin`) all gain the `impersonate` verb on `users` and `groups`. Grant is uniform across roles. Viewers do not mutate through normal flows, but the `impersonate` verb is cheap and its absence produces confusing `cannot impersonate users` errors the moment a viewer hits a read path that the apiserver routes through admission (or a future mutation path the viewer should be rejected from by the webhook, not by missing-RBAC). The webhook is the authoritative gate on who can do what; RBAC here governs identity forwarding only.

The grant is added in a butler-charts PR against `butler-crds/templates/cli-clusterroles.yaml` (the authoritative location for CLI-facing ClusterRoles, gated by `.Values.cliAuth.enabled`).

Scope of the grant: cluster-wide on users/groups, same shape as the butler-server grant in butler-charts#58. The CLI only ever sends `Impersonate-User = credentials.User.Email` (the SA's own mapped user), so in practice the grant is used for self-impersonation, but the RBAC verb itself is not `resourceNames`-scoped. A tighter binding with `resourceNames: [<user email>]` would be per-user and would force RoleBinding churn on every device-flow login; the ClusterRole grant trades that churn for a slightly wider RBAC surface.

## Alternatives Considered

### Skip creator-email enforcement for CLI-direct writes

Leave butler-cli's outgoing calls as-is. When the user creates a TC via the CLI in an env with `MaxClustersPerMember`, the webhook rejects. Operator runs `kubectl --as=<email>` manually or switches to the console.

Rejected: creates a per-env feature gap where an operator configuring an env with per-member caps is surprised when their team cannot use the CLI to create clusters in that env. Feature documentation would need a "if per-member cap is set, CLI does not work" carve-out. Feature presence should not vary with operator configuration.

### Manual `kubectl --as` per invocation

Document that operators must run `butlerctl cluster create --as <email> ...` when creating in capped envs. butler-cli does not add impersonation logic.

Rejected: operator-hostile. The whole point of the CLI is that it abstracts raw kubectl mechanics. Requiring `--as` on every invocation negates the CLI's value and forces each operator to remember the exact email format the webhook expects (mixed-case drift, etc.). Also fails on pre-ADR-016 users who have no email context to pass.

### Per-user `RoleBinding` with resourceNames

Create a `RoleBinding` per device-flow user that grants `impersonate` only on the user's own email as a `resourceName`. Tightest possible grant.

Rejected: adds one RoleBinding per user per login. Butler-cli's device flow already creates SA + RoleBinding per user per team (ADR-016); this would multiply that by the set of teams the user belongs to and add another rotation concern when the user's email changes. The ClusterRole-scoped grant matches the ADR-010 precedent and does not create new lifecycle pressure.

## Consequences

### Positive

- `butlerctl cluster create --environment=<env>` works for device-flow-logged-in users regardless of whether the env has `MaxClustersPerMember`. Feature parity with butler-console.
- Audit logs show the impersonated user's email on CLI-originated mutations, matching the butler-server-originated mutation pattern. Operations forensics converges on a single identity key (user email) across all ingress paths.
- Raw-kubeconfig paths continue to work unchanged via the empty-email short-circuit. Operators running platform-admin kubeconfigs are not re-routed through an impersonation path they do not need.
- Pattern is identical to ADR-010 (same payload, same lowercasing, same group fixture). A reviewer already familiar with ADR-010 can audit ADR-011's implementation in minutes.

### Negative

- Device-flow SA's backing ClusterRoles gain a privileged primitive (`impersonate`). A compromised CLI token can act as any email under the `butler-api-users` group's RBAC. Mitigation: butler-cli device-flow tokens have a 7-day refresh window (ADR-016); compromise blast radius is bounded by SA revocation through butler-server. The SA-to-user mapping is maintained server-side and cannot be forged client-side.
- The impersonation payload is the same as butler-server's, so a compromised CLI token has the same blast-radius ceiling as a compromised butler-server deployment. Operators should treat both at the same trust level for incident response.
- **`butler-api-users` group coupling** (inherited from ADR-010, restated for ADR-011 readers): every authenticated butler-cli and butler-console user maps to the same `butler-api-users` baseline group in K8s RBAC. Butler's per-user authorization (platform-admin gates, team-admin gates, per-member cluster caps) is enforced by the admission webhook, not by K8s RBAC. A webhook bypass (webhook pod unavailable, admission config stripped, CRD changes not replayed through admission) collapses every Butler user down to `butler-api-users` permissions uniformly. Not a new risk introduced by this ADR, but any reader evaluating ADR-011 in isolation should see it stated: the webhook is the authorization layer; RBAC is the identity-forwarding layer.

### Rollout sequence

Order matters, same as ADR-010. Deploying butler-cli with impersonation against a cluster whose device-flow SAs lack the `impersonate` verb produces `cannot impersonate users` on every CLI mutation.

1. **butler-charts RBAC PR merges first.** Adds `impersonate` on users/groups to all four CLI ClusterRoles (`butler-cli-admin`, `butler-cli-operator`, `butler-cli-viewer`, `butler-cli-platform-admin`) in `butler-crds/templates/cli-clusterroles.yaml`. No chart version bump (release discipline).
2. **butler-cli feat/team-environments PR merges after.** Implementation depends on the RBAC.
3. **Live validation on butler-beta.** Exercises device-flow-logged-in create in a capped env, raw-kubeconfig create, and the raw-kubeconfig short-circuit.
4. **Coordinated chart tags cut across all team-environments artifacts.** butler-crds, butler-controller, butler-console/butler-server, and butler-cli release together.

### Test harness implications

- Unit tests pin the impersonation payload shape (user lowercased, groups fixed) identically to butler-server's `asuser_test.go`.
- Empty-email short-circuit test verifies raw-kubeconfig users see an unmodified `rest.Config`.
- Live validation needs both a device-flow-logged-in identity and a raw-admin kubeconfig against the same cluster to exercise both paths. Same butler-beta dev loop the server side used.

## Open Questions

None blocking. Items flagged for follow-on:

- **CLI identity check against User CRD**: the webhook already validates the caller is a real Butler user (User CRD lookup or SAR fallback). The CLI does not pre-validate before impersonating; the apiserver rejects invalid impersonation requests with 403. An optional client-side pre-flight (check the email matches a User CRD) would give a nicer error message but requires the device-flow SA to list User CRDs, which it does not need today. Defer.
- **Viewer-role impersonation**: decided. The `impersonate` grant applies uniformly to `butler-cli-viewer` alongside admin/operator/platform-admin. Authorization is enforced by the webhook (including viewer-read-only gating on any future mutation path); RBAC absence of `impersonate` on the viewer role would produce `cannot impersonate users` errors that mask the intended authorization failure. Uniform grant gives viewers consistent webhook-sourced error messages and removes a future-feature migration step if a read-time impersonation path (e.g., audit-logging viewer lookups) lands later.
- **`Impersonate-Uid` / `Impersonate-Extra` headers**: client-go supports these for advanced identity forwarding. Butler does not use them today. If a future feature needs them, extend the payload in a new ADR.

## References

- ADR-010 (butler-server `docs/architecture/ADR-010-impersonation-auth.md`) - canonical decision framework; this ADR adopts the same shape at the CLI layer.
- ADR-009 (butler-controller `docs/architecture/ADR-009-team-environments.md`) - the admission webhooks this ADR enables for the CLI path.
- [ADR-002: CRDs as the API Contract](./ADR-002-crds-as-api.md) - the architectural constraint this ADR preserves. Client-side impersonation keeps the CLI-to-apiserver contract untouched.
- [ADR-016: CLI Authentication](./ADR-016-cli-authentication.md) - the device-flow auth path whose SA identity this ADR augments with impersonation.
- `internal/common/auth/credentials.go:49-56` - `UserInfo.Email` source for the impersonation user field.
- `internal/common/client/client.go` - client factory that will gain the impersonation wrap.
- butler-charts `charts/butler-crds/templates/cli-clusterroles.yaml` - authoritative CLI ClusterRole definitions; impersonate verb lands here.
- butler-charts#58 - butler-server impersonate grant; pattern this ADR mirrors for the CLI ClusterRoles.
