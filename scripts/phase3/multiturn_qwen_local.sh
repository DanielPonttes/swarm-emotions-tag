#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

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
PHASE3_MULTI_MAX_LATENCY_MS="${PHASE3_MULTI_MAX_LATENCY_MS:-5000}"

export LLM_PROVIDER LLM_BASE_URL LLM_MODEL LLM_ENABLE_THINKING
export OLLAMA_MODEL_TAG PHASE3_MULTI_MAX_LATENCY_MS

phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3
phase2_require_cmd ollama

trap 'phase2_cleanup_orchestrator; phase3_cleanup_python_ml_runtime; phase2_cleanup_stack' EXIT

phase3_prepare_python_ml_runtime

echo "Starting real dependencies for Phase 3 multi-turn test..."
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

agent_id="phase3-dialogue-$(date +%s)-$$"
run_token="phase3-dialogue-token-$(date +%s)-$$"
phase3_build_multiturn_fixture "$run_token" turn_texts expected_states

echo "Creating agent for multi-turn dialogue..."
phase3_create_agent "$agent_id"

max_latency_ms=0
turn_count="${#turn_texts[@]}"
for i in "${!turn_texts[@]}"; do
  turn_no=$((i + 1))
  summary="$(phase3_run_turn "$agent_id" "${turn_texts[$i]}" "${expected_states[$i]}")"
  echo "turn ${turn_no}/${turn_count}: ${summary}"

  current_state="$(python3 - "$summary" <<'PY'
import json
import sys
print(json.loads(sys.argv[1])["fsm_state"])
PY
)"
  current_latency="$(python3 - "$summary" <<'PY'
import json
import sys
print(json.loads(sys.argv[1])["latency_ms"])
PY
)"

  if [ "$current_latency" -gt "$max_latency_ms" ]; then
    max_latency_ms="$current_latency"
  fi

  if [ "$turn_no" -lt "$turn_count" ]; then
    phase3_sleep_for_state_constraints "$current_state"
  fi
done

expected_states_csv="$(IFS=,; echo "${expected_states[*]}")"
last_expected_state="${expected_states[$((turn_count - 1))]}"

echo "Verifying final state and history..."
phase3_wait_for_state "$agent_id" "$last_expected_state"
phase3_wait_for_history "$agent_id" "$expected_states_csv"

echo "Verifying deterministic persistence after multi-turn run..."
phase3_wait_for_redis_window "$agent_id" 12
phase3_wait_for_postgres_count "$agent_id" interaction_log "$turn_count"
phase3_wait_for_postgres_count "$agent_id" emotion_history "$turn_count"

echo "Phase 3 Qwen local multi-turn test completed successfully."
echo "max_latency_ms=${max_latency_ms}"
