#!/usr/bin/env bash

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

phase3_create_agent() {
  local agent_id="$1"
  curl -fsS \
    -H "Content-Type: application/json" \
    -d "{\"agent_id\":\"${agent_id}\"}" \
    "$ORCH_URL/api/v1/agents/" >/dev/null
}

phase3_run_turn() {
  local agent_id="$1"
  local text="$2"
  local expected_state="$3"
  local response

  response="$(curl -fsS \
    -H "Content-Type: application/json" \
    -d "{\"agent_id\":\"${agent_id}\",\"text\":\"${text}\"}" \
    "$ORCH_URL/api/v1/interact")"

  python3 - "$response" "$expected_state" "$PHASE3_MULTI_MAX_LATENCY_MS" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_state = sys.argv[2]
max_latency_ms = int(sys.argv[3])

response_text = payload.get("response", "")
fsm_state = payload.get("fsm_state", "")
latency_ms = int(payload.get("latency_ms", 0))
trace_id = payload.get("trace_id", "")

if not response_text:
    raise SystemExit("missing response in /interact payload")
if response_text.startswith("Mock response based on prompt:"):
    raise SystemExit("received mock response instead of real LLM output")
if fsm_state != expected_state:
    raise SystemExit(f"expected fsm_state {expected_state!r}, got {fsm_state!r}")
if latency_ms <= 0:
    raise SystemExit("missing or invalid latency_ms in /interact payload")
if latency_ms > max_latency_ms:
    raise SystemExit(f"latency {latency_ms}ms exceeded threshold {max_latency_ms}ms")
if not trace_id:
    raise SystemExit("missing trace_id in /interact payload")

print(json.dumps({
    "trace_id": trace_id,
    "latency_ms": latency_ms,
    "fsm_state": fsm_state,
    "response": response_text,
}, ensure_ascii=True))
PY
}

phase3_wait_for_state() {
  local agent_id="$1"
  local expected_state="$2"
  local timeout_sec="${3:-20}"
  local started_at response

  started_at="$(date +%s)"
  while true; do
    response="$(curl -fsS "$ORCH_URL/api/v1/agents/${agent_id}/state")"
    if python3 - "$response" "$expected_state" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_state = sys.argv[2]
state_name = payload.get("current_fsm_state", {}).get("state_name", "")
raise SystemExit(0 if state_name == expected_state else 1)
PY
    then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for final state ${expected_state} on agent ${agent_id}" >&2
      echo "$response" >&2
      return 1
    fi
    sleep 1
  done
}

phase3_wait_for_history() {
  local agent_id="$1"
  local expected_states_csv="$2"
  local timeout_sec="${3:-20}"
  local started_at response

  started_at="$(date +%s)"
  while true; do
    response="$(curl -fsS "$ORCH_URL/api/v1/agents/${agent_id}/history")"
    if python3 - "$response" "$expected_states_csv" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_states = sys.argv[2].split(",")
history = payload.get("history", [])
states = [entry.get("fsm_state", {}).get("state_name", "") for entry in history]

if len(states) != len(expected_states):
    raise SystemExit(1)
if states != expected_states:
    raise SystemExit(1)
PY
    then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for expected history on agent ${agent_id}" >&2
      echo "$response" >&2
      return 1
    fi
    sleep 1
  done
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

phase3_wait_for_redis_min_entries() {
  local agent_id="$1"
  local expected_entries="$2"
  local timeout_sec="${3:-20}"
  local started_at count

  started_at="$(date +%s)"
  while true; do
    count="$(phase2_docker_compose exec -T redis redis-cli ZCARD "working_memory:${agent_id}" | tr -d '\r[:space:]')"
    if [ -n "$count" ] && [ "$count" -ge "$expected_entries" ]; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for at least ${expected_entries} Redis working memory entries for agent ${agent_id}" >&2
      phase2_docker_compose exec -T redis redis-cli ZRANGE "working_memory:${agent_id}" 0 -1 >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_wait_for_redis_window() {
  local agent_id="$1"
  local expected_entries="$2"
  local timeout_sec="${3:-20}"
  local started_at count

  started_at="$(date +%s)"
  while true; do
    count="$(phase2_docker_compose exec -T redis redis-cli ZCARD "working_memory:${agent_id}" | tr -d '\r[:space:]')"
    if [ -n "$count" ] && [ "$count" -eq "$expected_entries" ]; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for Redis working memory window ${expected_entries} for agent ${agent_id}" >&2
      phase2_docker_compose exec -T redis redis-cli ZRANGE "working_memory:${agent_id}" 0 -1 >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_wait_for_postgres_count() {
  local agent_id="$1"
  local table_name="$2"
  local expected_count="$3"
  local timeout_sec="${4:-20}"
  local started_at count

  started_at="$(date +%s)"
  while true; do
    count="$(phase2_docker_compose exec -T postgresql \
      psql -U emotionrag -d emotionrag -tAc "SELECT COUNT(*) FROM ${table_name} WHERE agent_id = '${agent_id}'" | tr -d '\r[:space:]')"
    if [ -n "$count" ] && [ "$count" -eq "$expected_count" ]; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for ${expected_count} rows in ${table_name} for agent ${agent_id}" >&2
      phase2_docker_compose exec -T postgresql \
        psql -U emotionrag -d emotionrag -tAc "SELECT agent_id, COUNT(*) FROM ${table_name} GROUP BY agent_id ORDER BY COUNT(*) DESC LIMIT 5" >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_sleep_for_state_constraints() {
  local state="$1"
  case "$state" in
    anxious)
      sleep 1.1
      ;;
    frustrated)
      sleep 1.6
      ;;
    joyful)
      sleep 2.1
      ;;
  esac
}

phase3_build_multiturn_fixture() {
  local run_token="$1"
  local -n out_turn_texts="$2"
  local -n out_expected_states="$3"

  out_turn_texts=(
    "Tell me more about this architecture ${run_token}-01"
    "This problem is wrong ${run_token}-02"
    "thanks, great help ${run_token}-03"
    "thanks, great guidance ${run_token}-04"
    "This problem is wrong ${run_token}-05"
    "This problem is wrong ${run_token}-06"
    "thanks, great help ${run_token}-07"
    "thanks, great guidance ${run_token}-08"
    "This problem is wrong ${run_token}-09"
    "This problem is wrong ${run_token}-10"
    "thanks, great help ${run_token}-11"
    "thanks, great guidance ${run_token}-12"
    "This problem is wrong ${run_token}-13"
    "This problem is wrong ${run_token}-14"
    "thanks, great help ${run_token}-15"
    "thanks, great guidance ${run_token}-16"
    "This problem is wrong ${run_token}-17"
    "This problem is wrong ${run_token}-18"
    "thanks, great help ${run_token}-19"
    "thanks, great guidance ${run_token}-20"
  )

  out_expected_states=(
    curious
    frustrated
    empathetic
    joyful
    worried
    frustrated
    empathetic
    joyful
    worried
    frustrated
    empathetic
    joyful
    worried
    frustrated
    empathetic
    joyful
    worried
    frustrated
    empathetic
    joyful
  )
}

phase3_fetch_history_states_json() {
  local agent_id="$1"
  local response

  response="$(curl -fsS "$ORCH_URL/api/v1/agents/${agent_id}/history")"
  python3 - "$response" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
states = [entry.get("fsm_state", {}).get("state_name", "") for entry in payload.get("history", [])]
print(json.dumps(states, ensure_ascii=True))
PY
}
