# Butler CLI Open Items

## Gap Analysis
Full gap analysis: `.secrets/cli-gap-analysis.md` (2026-04-08)

## Immediate Action Items (Phase A)

- [ ] Create `internal/common/version/version.go` with Version, Commit, Date vars
- [ ] Fix version commands in both root.go files to use the version package
- [ ] Fix Makefile ldflags to match goreleaser path (`internal/common/version`)
- [ ] Implement `cluster get -o yaml/json` (currently prints "not yet implemented")
- [ ] TUI is WIP on `feat/bootstrap-tui` branch -- CLAUDE.md docs are aspirational, no action needed
- [ ] Add CI workflow for PRs (go vet, golangci-lint, go test)
- [ ] Add unit tests for validation logic (create.go Validate, parseMemoryToMB, parseDiskToGB, parseLBPool, isValidClusterName, isValidIP)

## Architecture Decisions Needed

- [ ] Auth mechanism: OAuth device flow vs scoped SA kubeconfigs vs server proxy
- [ ] Should CLI commands go through butler-server or stay direct-to-K8s-API?
- [ ] TUI: WIP on `feat/bootstrap-tui` branch -- finish and merge when ready

## Key Patterns Discovered

- All commands use dynamic client (unstructured) -- no typed butler-api import
- Kubeconfig discovery: KUBECONFIG env > ~/.butler/*-kubeconfig > ~/.kube/config
- Output: `internal/common/output` has Table, JSON, YAML with Printer pattern
- Helpers are duplicated: getNestedString in 3 packages, getClient in 2 packages
- Bootstrap uses KIND for chicken-and-egg: creates temp cluster, deploys controllers, watches CR
- Provider validate covers nutanix, harvester, proxmox, gcp; missing aws and azure
- Bootstrap commands have identical signal handling code (5 copies)
- go.mod module is `github.com/butlerdotdev/butler` (not butler-cli)

## Version History
- v0.5.0: cloud bootstrap CLI, credential flags (current release per MEMORY.md)
- v0.6.0: pending PR #16 merge
- Source hardcodes "v0.1.0-dev" -- version package never created
