#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_smoke_request() {
  local agent_id unique_token payload response

  agent_id="phase3-qwen-$(date +%s)-$$"
  unique_token="phase3-token-$(date +%s)-$$"
  payload="$(python3 - "$agent_id" "$unique_token" <<'PY'
import json
import sys

print(json.dumps({
    "agent_id": sys.argv[1],
    "text": f"Responda em portugues, em uma frase curta: confirme que voce esta rodando localmente no pipeline EmotionRAG com Qwen 3.5 27B e inclua o token {sys.argv[2]}.",
}))
PY
)"

  response="$(curl -fsS \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$ORCH_URL/api/v1/interact")"

  python3 - "$response" "$agent_id" "$PHASE3_SMOKE_MAX_LATENCY_MS" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
agent_id = sys.argv[2]
max_latency_ms = int(sys.argv[3])

response_text = payload.get("response", "")
latency_ms = int(payload.get("latency_ms", 0))
trace_id = payload.get("trace_id", "")

if not response_text:
    raise SystemExit("missing response in /interact payload")
if response_text.startswith("Mock response based on prompt:"):
    raise SystemExit("received mock response instead of real LLM output")
if not payload.get("emotion_state"):
    raise SystemExit("missing emotion_state in /interact payload")
if not payload.get("fsm_state"):
    raise SystemExit("missing fsm_state in /interact payload")
if not trace_id:
    raise SystemExit("missing trace_id in /interact payload")
if latency_ms <= 0:
    raise SystemExit("missing or invalid latency_ms in /interact payload")
if latency_ms > max_latency_ms:
    raise SystemExit(f"latency {latency_ms}ms exceeded threshold {max_latency_ms}ms")

print(json.dumps({
    "agent_id": agent_id,
    "trace_id": trace_id,
    "latency_ms": latency_ms,
    "fsm_state": payload["fsm_state"],
    "response": response_text,
}, ensure_ascii=True))
PY
}

phase2_source_profile

HTTP_PORT="${HTTP_PORT:-18080}"
POSTGRES_DSN="${POSTGRES_DSN:-postgres://emotionrag:dev_password_change_me@127.0.0.1:5433/emotionrag?sslmode=disable}"
QDRANT_COLLECTION="${QDRANT_COLLECTION:-agent_memories}"
PHASE2_KEEP_STACK_UP="${PHASE2_KEEP_STACK_UP:-true}"
LLM_PROVIDER="${LLM_PROVIDER:-ollama-native}"
LLM_BASE_URL="${LLM_BASE_URL:-http://127.0.0.1:11434}"
LLM_MODEL="${LLM_MODEL:-Qwen/Qwen3.5-27B}"
LLM_ENABLE_THINKING="${LLM_ENABLE_THINKING:-false}"
OLLAMA_MODEL_TAG="${OLLAMA_MODEL_TAG:-qwen3.5:27b}"
PHASE3_SMOKE_MAX_LATENCY_MS="${PHASE3_SMOKE_MAX_LATENCY_MS:-5000}"

export LLM_PROVIDER LLM_BASE_URL LLM_MODEL LLM_ENABLE_THINKING
export OLLAMA_MODEL_TAG PHASE3_SMOKE_MAX_LATENCY_MS

phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3
phase2_require_cmd ollama

trap 'phase2_cleanup_orchestrator; phase3_cleanup_python_ml_runtime; phase2_cleanup_stack' EXIT

phase3_prepare_python_ml_runtime

echo "Starting real dependencies for Phase 3 smoke test..."
phase2_compose_up_support
phase2_wait_for_support
phase3_wait_for_classifier_model "${PHASE3_CLASSIFIER_READY_TIMEOUT_SEC:-120}"

echo "Waiting for local Ollama..."
phase3_wait_for_ollama
phase3_require_ollama_model

echo "Warming Qwen local model..."
phase3_warm_model

echo "Starting local orchestrator against real dependencies and Ollama..."
phase2_start_orchestrator

echo "Running end-to-end smoke request with Qwen local..."
summary="$(phase3_smoke_request)"
echo "$summary"

trace_id="$(python3 - "$summary" <<'PY'
import json
import sys
print(json.loads(sys.argv[1])["trace_id"])
PY
)"

agent_id="$(python3 - "$summary" <<'PY'
import json
import sys
print(json.loads(sys.argv[1])["agent_id"])
PY
)"

echo "Verifying correlated trace in Python and Rust logs..."
phase3_wait_for_service_trace "python-ml" "$trace_id"
phase3_wait_for_service_trace "emotion-engine" "$trace_id"

echo "Verifying deterministic persistence in Redis and Postgres..."
phase3_wait_for_redis_min_entries "$agent_id" 2
phase3_wait_for_postgres_count "$agent_id" interaction_log 1

echo "Phase 3 Qwen local smoke test completed successfully."
