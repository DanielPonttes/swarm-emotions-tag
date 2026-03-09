#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase2_source_profile
phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3

trap 'phase2_cleanup_orchestrator; phase2_cleanup_stack' EXIT

echo "Starting real dependencies for Phase 2 smoke test..."
phase2_compose_up_support
phase2_wait_for_support

echo "Starting local orchestrator against real dependencies..."
phase2_start_orchestrator

echo "Running /api/v1/interact smoke request..."
phase2_smoke_request

echo "Phase 2 smoke test completed successfully."
