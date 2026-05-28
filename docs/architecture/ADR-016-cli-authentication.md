# ADR-016: CLI Authentication

## Status

Proposed

## Date

2026-04-09

## Decision Makers

- Alex Bagan (Principal Architect, Butler Labs)

## Context

butler-cli (`butlerctl` + `butleradm`) talks directly to the Kubernetes API via kubeconfig, bypassing butler-server entirely. This creates four concrete gaps:

1. **No Butler identity mapping.** CLI operations run as whatever Kubernetes ServiceAccount or client certificate the kubeconfig provides. There is no link between a Butler User CRD and the identity performing the operation.

2. **No Butler RBAC enforcement.** The admin/operator/viewer roles defined in Team CRD `spec.access` do not apply to CLI operations. A user with a cluster-admin kubeconfig can create or delete any TenantCluster regardless of their team role.

3. **No audit trail.** butler-server records login events via its `audit.Emitter` and every request passes through `SessionMiddleware`. CLI operations are invisible to this audit path. Kubernetes audit logs exist but record the raw ServiceAccount identity, not the Butler user.

4. **No team scoping.** The CLI defaults to `butler-tenants` namespace or requires explicit `--namespace` flags. There is no concept of a current team context.

butler-console authenticates through butler-server using Google OAuth SSO, internal users, and session cookies. Every console request passes through `SessionMiddleware` which validates the JWT and re-resolves Team membership on every request (see `butler-server/internal/auth/middleware.go`). The JWT contains email, name, teams with roles, groups, and an `isPlatformAdmin` flag.

ADR-002 (CRDs as the API Contract) establishes that the CLI creates, reads, updates, and deletes Butler CRDs directly on the Kubernetes API. This is a deliberate architectural choice that enables GitOps compatibility, resumable operations, and kubectl interoperability. Any auth solution must preserve this property.

## Options Considered

### Option A: OAuth Device Flow via butler-server

`butlerctl login --server https://butler.example.com` initiates an OAuth 2.0 Device Authorization Grant (RFC 8628). butler-server returns a device code and a verification URL. The user opens the URL in a browser, authenticates with their IdP (Google, Microsoft, Okta) or enters internal credentials, and approves the device. The CLI polls butler-server for completion. On success, butler-server creates a Kubernetes ServiceAccount scoped to the user's teams and returns a kubeconfig containing the SA token. The CLI stores this in `~/.butler/credentials.json`.

Subsequent commands use the scoped kubeconfig. The SA name encodes the Butler user identity, making Kubernetes audit logs attributable. RBAC is enforced through Roles and RoleBindings matching the Butler role (admin, operator, viewer) scoped to team namespaces.

### Option B: Scoped ServiceAccount kubeconfigs per user/team (admin provisioned)

An administrator creates a User CRD, then a ServiceAccount and RoleBinding in each team namespace. The CLI discovers kubeconfigs from `~/.butler/`. The SA token restricts operations to the team namespace with role-appropriate permissions.

This is purely Kubernetes-native with no butler-server dependency. However, it requires manual provisioning of SA + RoleBinding for every user/team combination. No self-service. No SSO integration. No automatic role propagation when team membership changes.

### Option C: CLI routes through butler-server as API proxy

All CLI operations go through butler-server's REST API instead of the Kubernetes API. butler-server has full auth infrastructure and applies the same authorization as the console. This provides complete auth/audit parity with the console.

This contradicts ADR-002. It adds a hard dependency on butler-server availability, eliminates GitOps compatibility for CLI-created resources, and means `kubectl apply` and `butlerctl` produce different results. It also doubles the API surface area since every CRD operation needs a corresponding REST endpoint.

## Competitor Analysis

| Tool | Auth Mechanism | Team/Org Scoping |
|------|---------------|------------------|
| oc login (OpenShift) | OAuth token via API server | Project context switching |
| rancher login (Rancher) | API key/token via server | Cluster/project context |
| argocd login (Argo CD) | SSO/password via server | Project RBAC |
| kubectl | kubeconfig (raw) | Namespace only |
| helm | kubeconfig (raw) | Namespace only |

Every Kubernetes platform that adds identity-aware CLI access follows the pattern of authenticating through a server component and receiving a scoped credential. `oc login` and `rancher login` are the closest analogues.

## Decision

We adopt Option A: OAuth Device Flow via butler-server.

The CLI authenticates through butler-server to establish a Butler identity, then receives a scoped kubeconfig for direct Kubernetes API access. This preserves ADR-002 (CRDs as the API contract) while adding identity, RBAC, audit, and team scoping.

Key properties:

1. **Preserves ADR-002.** After login, the CLI still creates CRDs directly via the Kubernetes API. butler-server is only involved during login, not during normal operations.

2. **Reuses existing auth infrastructure.** butler-server already has JWT issuance, OIDC integration, internal user authentication, team membership resolution, and group sync with IdPs. The device flow adds two endpoints, not a new auth system.

3. **Follows established patterns.** `oc login`, `rancher login`, and `argocd login` all use the same approach: authenticate against a server, receive a credential, use the credential against the underlying API.

4. **Automatic team scoping via RBAC.** The scoped ServiceAccount has RoleBindings only in the team namespaces the user belongs to. The Kubernetes API enforces namespace access, so the CLI cannot read or modify resources outside the user's teams.

5. **Audit through Kubernetes audit logs.** The ServiceAccount name follows the pattern `butler-cli-{user-hash}`. Kubernetes audit logs identify this SA on every API call, linking operations to the Butler user.

6. **Self-service.** Users run `butlerctl login`, authenticate in their browser, and are ready. No administrator intervention required for initial setup or team changes.

7. **Graceful degradation.** If butler-server is unavailable, users with an existing valid credential continue operating. The `--kubeconfig` flag still works for break-glass access with a raw kubeconfig.

## Implementation Plan

### Phase 1: butler-server endpoints

#### New endpoints

**POST /api/auth/cli/device** (public, no session required)

Initiates the device authorization flow. Returns a device code for the CLI to poll and a verification URL for the user to open.

```
Request:
  POST /api/auth/cli/device
  Content-Type: application/json
  {"client_id": "butlerctl"}

Response (200):
  {
    "device_code": "GmRh...xyzA",
    "user_code": "ABCD-1234",
    "verification_uri": "https://butler.example.com/auth/device",
    "expires_in": 900,
    "interval": 5
  }
```

The server generates a random device code and user code, stores them in a TTL map (15 minutes), and returns them to the CLI. The verification URI points to a page in butler-console that accepts the user code.

**POST /api/auth/cli/token** (public, no session required)

The CLI polls this endpoint with the device code. Returns "authorization_pending" until the user completes browser authentication, then returns a scoped kubeconfig.

```
Request:
  POST /api/auth/cli/token
  Content-Type: application/json
  {"device_code": "GmRh...xyzA", "client_id": "butlerctl"}

Response (400, pending):
  {"error": "authorization_pending"}

Response (200, approved):
  {
    "user": {
      "email": "alice@example.com",
      "name": "Alice",
      "teams": [{"name": "platform", "role": "admin"}],
      "isPlatformAdmin": false
    },
    "kubeconfig": "<base64-encoded kubeconfig>",
    "expires_at": "2026-04-10T00:00:00Z"
  }

Response (400, denied):
  {"error": "access_denied"}

Response (400, expired):
  {"error": "expired_token"}
```

When the user approves the device in the browser, the server:
1. Resolves the user's team memberships via `TeamResolver.ResolveTeams()`
2. Creates or updates a ServiceAccount named `butler-cli-{sha256(email)[:12]}` in `butler-system`
3. Creates or updates RoleBindings in each team namespace matching the user's role
4. Creates a short-lived token for the SA (24h, configurable via `BUTLER_CLI_TOKEN_EXPIRY`)
5. Builds a kubeconfig with the SA token, cluster CA, and API server URL
6. Returns the kubeconfig to the polling CLI

**GET /auth/device** (butler-console route)

The verification page is a butler-console route at `/auth/device`, not a server-rendered page. butler-server does not serve HTML anywhere else and this should not be the exception. The console page is a simple React component with a code input field. Flow:

1. User opens `https://butler.example.com/auth/device` (from CLI output)
2. Console renders a code input form (no auth required to view the form)
3. User enters the user code (e.g., ABCD-1234)
4. Console calls `POST /api/auth/cli/verify` with the code
5. If the user is not already authenticated, the console redirects to the standard login flow (SSO or internal), then returns to the device page
6. On successful auth, console calls `POST /api/auth/cli/approve` with the device code and the authenticated session
7. The device code is marked as approved; the CLI's next poll picks it up

New butler-console files:
- `src/pages/DeviceAuthPage.tsx` (code input form, minimal UI)
- Route: `/auth/device` added to `App.tsx` (public, outside RequireAuth)

New butler-server endpoints:
- `POST /api/auth/cli/verify` (validates user code, returns device info)
- `POST /api/auth/cli/approve` (requires authenticated session, marks device as approved)

#### Server-side files

| File | Purpose |
|------|---------|
| `butler-server/internal/api/handlers/auth_device.go` | Device flow handlers: `DeviceAuthorize`, `DeviceToken`, `DeviceVerify`, `DeviceApprove`, `DeviceRefresh` |
| `butler-server/internal/auth/device.go` | `DeviceStore` (TTL map of device codes), `DeviceCode` struct, code generation, refresh token generation |
| `butler-server/internal/auth/serviceaccount.go` | SA creation, RoleBinding management, token generation, kubeconfig building, SA cleanup goroutine |
| `butler-console/src/pages/DeviceAuthPage.tsx` | Verification page: code input form, redirects to login if unauthenticated |

#### ServiceAccount naming and lifecycle

ServiceAccount name: `butler-cli-{sha256(email)[:12]}` in namespace `butler-system`.

The SA is annotated with the Butler user identity:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: butler-cli-a1b2c3d4e5f6
  namespace: butler-system
  labels:
    app.kubernetes.io/managed-by: butler
    butler.butlerlabs.dev/cli-user: "true"
  annotations:
    butler.butlerlabs.dev/user-email: "alice@example.com"
    butler.butlerlabs.dev/last-login: "2026-04-09T12:00:00Z"
```

SAs are reused across logins. RoleBindings are updated on each login (and each refresh) to reflect current team membership.

Stale SA cleanup runs as a butler-server background goroutine (not a controller, since butler-server owns SA creation). On a 24-hour ticker (the same maintenance loop used for other cleanup), butler-server lists all ServiceAccounts in `butler-system` with the label `butler.butlerlabs.dev/cli-user: "true"` and deletes any where `last-login` is older than `BUTLER_CLI_SA_TTL` (default 30 days). Associated RoleBindings are cleaned up by owner references on the SA.

### Phase 2: RBAC mapping

Three ClusterRoles define the Butler permission model for CLI access:

**butler-cli-admin** (team namespace scope via RoleBinding)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: butler-cli-admin
rules:
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["tenantclusters", "tenantaddons", "providerconfigs", "ipallocations", "networkpools", "imagesyncs", "workspaces"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
    # Needed for kubeconfig retrieval
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
```

**butler-cli-operator** (team namespace scope via RoleBinding)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: butler-cli-operator
rules:
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["tenantclusters", "tenantaddons", "imagesyncs", "workspaces"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["providerconfigs", "ipallocations", "networkpools"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
```

**butler-cli-viewer** (team namespace scope via RoleBinding)

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: butler-cli-viewer
rules:
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["tenantclusters", "tenantaddons", "providerconfigs", "ipallocations", "networkpools", "imagesyncs", "workspaces"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list"]
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
```

**butler-cli-platform-admin** (ClusterRoleBinding for platform admins)

Platform admins get a ClusterRoleBinding to `butler-cli-admin` rather than per-namespace RoleBindings. This grants access to all namespaces, matching the console's `IsPlatformAdmin` behavior.

Additionally, platform admins get access to cluster-scoped resources:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: butler-cli-platform-admin
rules:
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["butlerconfigs", "teams", "users", "identityproviders", "addondefinitions", "managementaddons"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["butler.butlerlabs.dev"]
    resources: ["tenantclusters", "tenantaddons", "providerconfigs", "ipallocations", "networkpools", "imagesyncs", "workspaces"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["namespaces", "secrets", "events"]
    verbs: ["get", "list", "watch"]
```

RoleBindings are created per user per team namespace:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: butler-cli-a1b2c3d4e5f6
  namespace: acme  # team namespace
  labels:
    app.kubernetes.io/managed-by: butler
    butler.butlerlabs.dev/cli-user: "true"
  annotations:
    butler.butlerlabs.dev/user-email: "alice@example.com"
subjects:
  - kind: ServiceAccount
    name: butler-cli-a1b2c3d4e5f6
    namespace: butler-system
roleRef:
  kind: ClusterRole
  name: butler-cli-operator  # matches Team role
  apiGroup: rbac.authorization.k8s.io
```

### Phase 3: butler-cli changes

#### New commands

**butlerctl login**

```
butlerctl login --server https://butler.example.com

Opening browser for authentication...
If the browser does not open, visit: https://butler.example.com/auth/device
Enter code: ABCD-1234

Waiting for approval... done.

Logged in as alice@example.com
Teams:
  platform (admin)
  staging  (operator)

Context saved to ~/.butler/credentials.json
Current team: platform
```

Flags:
- `--server URL` (required on first login, remembered after)
- `--no-browser` (print URL instead of opening browser)

**butlerctl logout**

```
butlerctl logout

Logged out. Credentials removed from ~/.butler/credentials.json
```

Removes the stored credential. Does not revoke the SA token server-side (SA tokens are short-lived and expire naturally).

**butlerctl context list**

```
butlerctl context list

SERVER                          TEAM       ROLE      EXPIRES
https://butler.example.com      platform   admin     23h remaining
https://butler.example.com      staging    operator  23h remaining
```

Lists all available team contexts from the stored credential.

**butlerctl context use**

```
butlerctl context use staging

Switched to team: staging (operator)
Default namespace: staging
```

Sets the active team. Subsequent commands default to the team's namespace.

**Multi-server usage:** The credential file supports multiple servers (the `servers` map). The server is selected at login time and stored as the active server. For users operating against multiple Butler installations (e.g., staging and production):

```
butlerctl login --server https://staging.butler.example.com
butlerctl login --server https://prod.butler.example.com

butlerctl context list
SERVER                                    TEAM       ROLE      EXPIRES
https://staging.butler.example.com  *     dev        operator  23h remaining
https://prod.butler.example.com           platform   admin     22h remaining

butlerctl context use platform --server https://prod.butler.example.com
```

The `--server` flag on `context use` and `context list` selects which server's teams to show/switch. Without `--server`, the most recently logged-in server is used. The credential file tracks `activeServer` at the top level:

```json
{
  "activeServer": "https://staging.butler.example.com",
  "servers": { ... }
}
```

#### Credential storage

File: `~/.butler/credentials.json`

```json
{
  "activeServer": "https://butler.example.com",
  "servers": {
    "https://butler.example.com": {
      "user": {
        "email": "alice@example.com",
        "name": "Alice",
        "teams": [
          {"name": "platform", "role": "admin"},
          {"name": "staging", "role": "operator"}
        ],
        "isPlatformAdmin": false
      },
      "kubeconfig": "<base64-encoded kubeconfig>",
      "expiresAt": "2026-04-10T00:00:00Z",
      "refreshToken": "<opaque-refresh-token>",
      "refreshExpiresAt": "2026-04-16T00:00:00Z",
      "activeTeam": "platform"
    }
  }
}
```

File permissions: `0600` (read/write owner only).

#### CLI client changes

File: `butler-cli/internal/common/client/client.go`

The `New()` function gains a new resolution step. Before falling through to kubeconfig discovery, it checks for a valid Butler credential:

```
Resolution order:
1. --kubeconfig flag (explicit path, unchanged)
2. --context flag (kubeconfig context, unchanged)
3. KUBECONFIG env var (unchanged)
4. ~/.butler/credentials.json (NEW: Butler auth credential)
5. ~/.butler/*-kubeconfig files (existing Butler kubeconfig discovery)
6. ~/.kube/config (standard fallback)
```

When a Butler credential is found and not expired, the CLI uses the embedded kubeconfig from the credential. The active team from the credential sets the default namespace.

The `--kubeconfig` flag and `KUBECONFIG` env var always take precedence. This ensures break-glass access works without a butler-server dependency.

#### New CLI packages

| File | Purpose |
|------|---------|
| `butler-cli/internal/common/auth/credentials.go` | Load, save, validate `~/.butler/credentials.json` |
| `butler-cli/internal/common/auth/device.go` | Device flow client: initiate, poll, open browser |
| `butler-cli/internal/ctl/login/login.go` | `butlerctl login` command |
| `butler-cli/internal/ctl/logout/logout.go` | `butlerctl logout` command |
| `butler-cli/internal/ctl/context/context.go` | `butlerctl context list` and `butlerctl context use` commands |

#### butleradm considerations

`butleradm` operates with cluster-admin privileges (bootstrap, upgrade, platform health). The device flow is not required for `butleradm` because:

- Bootstrap runs before butler-server exists (KIND cluster phase)
- Platform operations require cluster-admin, which the scoped SA model cannot safely provide through a web login flow

`butleradm` continues to use raw kubeconfigs. A future ADR may address `butleradm` auth if the need arises.

### Phase 4: Helm chart changes

The `butler-controller` and `butler-server` Helm charts need updates:

**butler-crds chart:** Add the four ClusterRoles (`butler-cli-admin`, `butler-cli-operator`, `butler-cli-viewer`, `butler-cli-platform-admin`).

**butler-server chart:** Add RBAC granting butler-server permission to create and manage ServiceAccounts, Secrets (for SA tokens), RoleBindings, and ClusterRoleBindings in team namespaces and `butler-system`.

**butler-controller chart:** No changes. RoleBindings are created by butler-server, not the controller.

### Token Lifecycle

- **Access token expiry.** SA tokens are created with a 24-hour lifetime by default, configurable via `BUTLER_CLI_TOKEN_EXPIRY`. The CLI checks expiry before each command.

- **Refresh token.** The credential file stores a refresh token (7-day lifetime, configurable via `BUTLER_CLI_REFRESH_EXPIRY`). When the access token expires, the CLI silently calls `POST /api/auth/cli/refresh` with the refresh token. The server re-resolves team memberships, rotates the SA token, updates RoleBindings to reflect current roles, and returns a new access token. No browser interaction required. When the refresh token expires, the user must run `butlerctl login` again (full device flow). This matches the `oc login` and standard OAuth CLI refresh pattern.

- **Refresh endpoint.**

```
POST /api/auth/cli/refresh
Content-Type: application/json
{"refresh_token": "..."}

Response (200):
  {
    "user": { ... },
    "kubeconfig": "<base64>",
    "expires_at": "...",
    "refresh_token": "<new-refresh-token>",
    "refresh_expires_at": "..."
  }

Response (401):
  {"error": "refresh_token_expired"}
```

The refresh call re-resolves team memberships via `TeamResolver.ResolveTeams()`, so role changes propagate within 24 hours (at the next token refresh) without requiring a full browser re-login.

- **Revocation.** Disabling a user via the User CRD does not immediately revoke the SA token. For immediate revocation: `butleradm user revoke-cli alice@example.com` deletes the ServiceAccount and all associated RoleBindings. This is a planned MVP command (see Consequences).

- **Stale roles.** If a user's team role changes (e.g., operator to viewer), the change takes effect at the next token refresh (up to 24 hours). Planned mitigations for enterprise deployments (post-MVP):
  1. `butleradm user revoke-cli EMAIL` for immediate SA deletion
  2. A butler-server background watcher on Team CRD membership changes that patches RoleBindings in real-time when members are added or removed

## Consequences

### Positive

- CLI operations are attributable to a specific Butler user
- Butler RBAC roles (admin/operator/viewer) are enforced at the Kubernetes API level
- Kubernetes audit logs capture the Butler user identity via the SA name and annotations
- SSO users get CLI access without manual kubeconfig distribution
- Team scoping works automatically through namespace RBAC
- ADR-002 is preserved: the CLI still creates CRDs directly on the Kubernetes API
- The device flow works in headless environments (SSH sessions, CI) via the `--no-browser` flag and manual URL entry
- Graceful degradation: existing kubeconfigs continue to work via `--kubeconfig` or `KUBECONFIG`

### Negative

- butler-server must be reachable for initial login and token refresh (not for normal operations between refreshes)
- ServiceAccount proliferation: one SA per CLI user in `butler-system`, plus RoleBindings in each team namespace. Mitigated by the 30-day SA TTL cleanup.
- Disabling a user does not immediately revoke CLI access (up to 24-hour window until next refresh). Mitigated by planned `butleradm user revoke-cli` command for immediate SA deletion, and a planned Team CRD membership watcher for real-time RoleBinding updates.
- `butleradm` is not covered by this design and continues to use raw kubeconfigs
- Users unfamiliar with device flow may find the browser-based approval step unfamiliar

### Neutral

- The four ClusterRoles are static and do not change per deployment
- The credential file at `~/.butler/credentials.json` coexists with existing `*-kubeconfig` files in `~/.butler/`
- CI/CD pipelines that use Butler CLI can authenticate via the device flow (with `--no-browser`) or continue using raw kubeconfigs
