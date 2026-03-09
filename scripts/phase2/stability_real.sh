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

RPS="${RPS:-10}"
DURATION_SEC="${DURATION_SEC:-300}"
AGENT_POOL="${AGENT_POOL:-8}"
MAX_INFLIGHT="${MAX_INFLIGHT:-64}"
MAX_GOROUTINE_DELTA="${MAX_GOROUTINE_DELTA:-25}"
PPROF_OUT_DIR="${PPROF_OUT_DIR:-$(mktemp -d -t phase2-stability.XXXXXX)}"

mkdir -p "$PPROF_OUT_DIR"

trap 'phase2_cleanup_orchestrator; phase2_cleanup_stack' EXIT

echo "Starting real dependencies for Phase 2 stability test..."
phase2_compose_up_support
phase2_wait_for_support

echo "Starting local orchestrator against real dependencies..."
phase2_start_orchestrator

echo "Running smoke request before sustained load..."
phase2_smoke_request >/dev/null

before_file="$PPROF_OUT_DIR/goroutines-before.txt"
after_file="$PPROF_OUT_DIR/goroutines-after.txt"
summary_file="$PPROF_OUT_DIR/loadtest-summary.json"
metrics_file="$PPROF_OUT_DIR/metrics.txt"

phase2_capture_goroutines "$before_file"
before_total="$(phase2_goroutine_total "$before_file")"

echo "Running sustained load: ${RPS} RPS for ${DURATION_SEC}s..."
(
  cd "$PHASE2_ROOT_DIR/orchestrator"
  go run ./cmd/loadtest \
    -base-url "$ORCH_URL" \
    -duration "${DURATION_SEC}s" \
    -rps "$RPS" \
    -agents "$AGENT_POOL" \
    -max-inflight "$MAX_INFLIGHT"
) | tee "$summary_file"

phase2_capture_goroutines "$after_file"
after_total="$(phase2_goroutine_total "$after_file")"
delta="$((after_total - before_total))"

curl -fsS "$ORCH_URL/metrics" -o "$metrics_file"

echo "Goroutines before: $before_total"
echo "Goroutines after:  $after_total"
echo "Goroutine delta:   $delta"
echo "Artifacts saved to: $PPROF_OUT_DIR"

if [ "$delta" -gt "$MAX_GOROUTINE_DELTA" ]; then
  echo "goroutine delta $delta exceeded threshold $MAX_GOROUTINE_DELTA" >&2
  exit 1
fi

echo "Phase 2 stability test completed successfully."
