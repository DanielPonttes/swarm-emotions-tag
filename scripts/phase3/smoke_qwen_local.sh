#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/../phase2/common.sh"

phase3_normalize_ollama_base_url() {
  local base_url="${1%/}"
  base_url="${base_url%/v1}"
  printf '%s\n' "$base_url"
}

phase3_wait_for_ollama() {
  local base_url
  base_url="$(phase3_normalize_ollama_base_url "$LLM_BASE_URL")"
  phase2_wait_for_http "ollama" "$base_url/api/tags" 60
}

phase3_require_ollama_model() {
  if ! ollama list | awk 'NR > 1 { print $1 }' | grep -Fx "$OLLAMA_MODEL_TAG" >/dev/null 2>&1; then
    echo "missing Ollama model: $OLLAMA_MODEL_TAG" >&2
    echo "run: ollama pull $OLLAMA_MODEL_TAG" >&2
    exit 1
  fi
}

phase3_warm_model() {
  local base_url response
  base_url="$(phase3_normalize_ollama_base_url "$LLM_BASE_URL")"

  response="$(curl -fsS \
    -H "Content-Type: application/json" \
    -d "{\"model\":\"${OLLAMA_MODEL_TAG}\",\"stream\":false,\"think\":false,\"messages\":[{\"role\":\"user\",\"content\":\"Reply with exactly: OK\"}],\"options\":{\"num_predict\":8,\"temperature\":0}}" \
    "$base_url/api/chat")"

  python3 - "$response" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
content = payload.get("message", {}).get("content", "").strip()
if content != "OK":
    raise SystemExit(f"unexpected warmup response: {content!r}")
PY
}

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

phase3_wait_for_service_trace() {
  local service="$1"
  local trace_id="$2"
  local timeout_sec="${3:-20}"
  local started_at

  started_at="$(date +%s)"
  while true; do
    if phase2_docker_compose logs --since 2m "$service" 2>&1 | grep -F "$trace_id" >/dev/null 2>&1; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for trace $trace_id in logs for service $service" >&2
      phase2_docker_compose logs --since 2m "$service" 2>&1 | tail -n 80 >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_wait_for_redis_working_memory() {
  local agent_id="$1"
  local timeout_sec="${2:-20}"
  local started_at count

  started_at="$(date +%s)"
  while true; do
    count="$(phase2_docker_compose exec -T redis redis-cli ZCARD "working_memory:${agent_id}" | tr -d '\r[:space:]')"
    if [ -n "$count" ] && [ "$count" -ge 2 ]; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for Redis working memory entries for agent $agent_id" >&2
      phase2_docker_compose exec -T redis redis-cli ZRANGE "working_memory:${agent_id}" 0 -1 >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_wait_for_postgres_interaction() {
  local agent_id="$1"
  local timeout_sec="${2:-20}"
  local started_at count

  started_at="$(date +%s)"
  while true; do
    count="$(phase2_docker_compose exec -T postgresql \
      psql -U emotionrag -d emotionrag -tAc "SELECT COUNT(*) FROM interaction_log WHERE agent_id = '${agent_id}'" | tr -d '\r[:space:]')"
    if [ -n "$count" ] && [ "$count" -ge 1 ]; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for Postgres interaction_log row for agent $agent_id" >&2
      phase2_docker_compose exec -T postgresql \
        psql -U emotionrag -d emotionrag -tAc "SELECT agent_id, input_text, fsm_to FROM interaction_log ORDER BY timestamp DESC LIMIT 5" >&2 || true
      return 1
    fi
    sleep 1
  done
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

trap 'phase2_cleanup_orchestrator; phase2_cleanup_stack' EXIT

echo "Starting real dependencies for Phase 3 smoke test..."
phase2_compose_up_support
phase2_wait_for_support

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
phase3_wait_for_redis_working_memory "$agent_id"
phase3_wait_for_postgres_interaction "$agent_id"

echo "Phase 3 Qwen local smoke test completed successfully."
