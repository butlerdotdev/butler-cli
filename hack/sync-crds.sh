#!/bin/bash
# hack/sync-crds.sh - Sync CRDs from butler-api into the embedded bootstrap manifests.
#
# butleradm bootstrap embeds these CRD YAMLs (see manifests/embed.go) and applies them
# when standing up a management cluster. butler-api is the single source of truth for the
# CRD schemas; this script is the only supported way to refresh the embedded copies.
# Do NOT hand-edit files under internal/adm/bootstrap/manifests/crds/ - they are
# overwritten here and any manual change is reverted (and caught by the crd-drift CI job).
#
# Usage:
#   ./hack/sync-crds.sh [BUTLER_API_PATH]
#   BUTLER_API_PATH=/path/to/butler-api ./hack/sync-crds.sh
# Defaults to the sibling checkout ../butler-api.
#
# Copyright 2026 The Butler Authors.
# SPDX-License-Identifier: Apache-2.0
set -uo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
BUTLER_API_PATH="${1:-${BUTLER_API_PATH:-$REPO_ROOT/../butler-api}}"
CRD_SOURCE="$BUTLER_API_PATH/config/crd/bases"
CRD_DEST="$REPO_ROOT/internal/adm/bootstrap/manifests/crds"

if [[ ! -d "$CRD_SOURCE" ]]; then
    echo "ERROR: CRD source not found: $CRD_SOURCE" >&2
    echo "Pass the butler-api checkout path as \$1 or set BUTLER_API_PATH." >&2
    exit 1
fi

mkdir -p "$CRD_DEST"
echo "Syncing CRDs from: $CRD_SOURCE"

# The embedded copies are raw controller-gen output, applied verbatim by the bootstrap
# orchestrator, so this is a straight copy (no Helm templating, unlike butler-charts).
synced=0
for src in "$CRD_SOURCE"/*.yaml; do
    [[ -e "$src" ]] || continue
    b="$(basename "$src")"
    cp "$src" "$CRD_DEST/$b"
    echo "SYNC: $b"
    synced=$((synced + 1))
done

# Prune embedded CRDs that no longer exist upstream so the embedded set stays exactly
# equal to butler-api's (a CRD deleted in butler-api must not linger here).
for dst in "$CRD_DEST"/*.yaml; do
    [[ -e "$dst" ]] || continue
    b="$(basename "$dst")"
    if [[ ! -f "$CRD_SOURCE/$b" ]]; then
        echo "PRUNE: $b (no longer in butler-api)"
        rm -f "$dst"
    fi
done

echo "Done: $synced CRDs synced to ${CRD_DEST#"$REPO_ROOT"/}"
