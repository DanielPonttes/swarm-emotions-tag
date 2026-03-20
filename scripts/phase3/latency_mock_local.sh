#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_validate_mock_latency_summary() {
  local summary="$1"

  python3 - "$summary" \
    "$PHASE3_LATENCY_AVG_THRESHOLD_MS" \
    "$PHASE3_LATENCY_P95_THRESHOLD_MS" \
    "$PHASE3_LATENCY_P99_THRESHOLD_MS" \
    "$PHASE3_LATENCY_MAX_THRESHOLD_MS" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
avg_threshold_ms = float(sys.argv[2])
p95_threshold_ms = float(sys.argv[3])
p99_threshold_ms = float(sys.argv[4])
max_threshold_ms = float(sys.argv[5])

checks = [
    ("total", payload.get("total", 0) > 0, "expected total > 0"),
    ("failure", int(payload.get("failure", 0)) == 0, "expected zero failures"),
    ("avg_latency_ms", float(payload.get("avg_latency_ms", 0)) < avg_threshold_ms, f"avg latency exceeded {avg_threshold_ms}ms"),
    ("p95_latency_ms", float(payload.get("p95_latency_ms", 0)) < p95_threshold_ms, f"p95 latency exceeded {p95_threshold_ms}ms"),
    ("p99_latency_ms", float(payload.get("p99_latency_ms", 0)) < p99_threshold_ms, f"p99 latency exceeded {p99_threshold_ms}ms"),
    ("max_latency_ms", float(payload.get("max_latency_ms", 0)) < max_threshold_ms, f"max latency exceeded {max_threshold_ms}ms"),
]

for field, ok, message in checks:
    if not ok:
        raise SystemExit(f"{message}: {field}={payload.get(field)!r}")

print(json.dumps({
    "total": payload["total"],
    "success": payload["success"],
    "avg_latency_ms": payload["avg_latency_ms"],
    "p95_latency_ms": payload["p95_latency_ms"],
    "p99_latency_ms": payload["p99_latency_ms"],
    "max_latency_ms": payload["max_latency_ms"],
}, ensure_ascii=True))
PY
}

phase2_source_profile

HTTP_PORT="${HTTP_PORT:-18083}"
ORCH_URL="${ORCH_URL:-http://127.0.0.1:${HTTP_PORT}}"
USE_MOCK_CONNECTORS="${USE_MOCK_CONNECTORS:-true}"
LLM_PROVIDER="${LLM_PROVIDER:-mock}"
PHASE3_LATENCY_DURATION_SEC="${PHASE3_LATENCY_DURATION_SEC:-15}"
PHASE3_LATENCY_RPS="${PHASE3_LATENCY_RPS:-100}"
PHASE3_LATENCY_AGENT_POOL="${PHASE3_LATENCY_AGENT_POOL:-1000}"
PHASE3_LATENCY_MAX_INFLIGHT="${PHASE3_LATENCY_MAX_INFLIGHT:-256}"
PHASE3_LATENCY_TIMEOUT_MS="${PHASE3_LATENCY_TIMEOUT_MS:-500}"
PHASE3_LATENCY_AVG_THRESHOLD_MS="${PHASE3_LATENCY_AVG_THRESHOLD_MS:-50}"
PHASE3_LATENCY_P95_THRESHOLD_MS="${PHASE3_LATENCY_P95_THRESHOLD_MS:-100}"
PHASE3_LATENCY_P99_THRESHOLD_MS="${PHASE3_LATENCY_P99_THRESHOLD_MS:-100}"
PHASE3_LATENCY_MAX_THRESHOLD_MS="${PHASE3_LATENCY_MAX_THRESHOLD_MS:-250}"

export HTTP_PORT ORCH_URL USE_MOCK_CONNECTORS LLM_PROVIDER
export PHASE3_LATENCY_DURATION_SEC PHASE3_LATENCY_RPS PHASE3_LATENCY_AGENT_POOL
export PHASE3_LATENCY_MAX_INFLIGHT PHASE3_LATENCY_TIMEOUT_MS
export PHASE3_LATENCY_AVG_THRESHOLD_MS PHASE3_LATENCY_P95_THRESHOLD_MS
export PHASE3_LATENCY_P99_THRESHOLD_MS PHASE3_LATENCY_MAX_THRESHOLD_MS

phase2_prepare_env

phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3

trap 'phase2_cleanup_orchestrator' EXIT

echo "Starting local orchestrator with mock connectors and mock LLM..."
phase2_start_orchestrator

echo "Running smoke request without real dependencies..."
phase2_smoke_request >/dev/null

echo "Running latency loadtest without LLM..."
summary="$(
  cd "$PHASE2_ROOT_DIR/orchestrator"
  go run ./cmd/loadtest \
    -base-url "$ORCH_URL" \
    -duration "${PHASE3_LATENCY_DURATION_SEC}s" \
    -rps "$PHASE3_LATENCY_RPS" \
    -agents "$PHASE3_LATENCY_AGENT_POOL" \
    -max-inflight "$PHASE3_LATENCY_MAX_INFLIGHT" \
    -timeout "${PHASE3_LATENCY_TIMEOUT_MS}ms"
)"
echo "$summary"

echo "Validating latency thresholds..."
phase3_validate_mock_latency_summary "$summary"

echo "Phase 3 mock latency validation completed successfully."
