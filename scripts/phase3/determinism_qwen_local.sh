#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_run_determinism_pass() {
  local pass_label="$1"
  local agent_id="$2"
  local run_token="$3"
  local history_file="$4"
  local latency_file="$5"
  local turn_texts expected_states
  local max_latency_ms=0

  phase3_build_multiturn_fixture "$run_token" turn_texts expected_states

  echo "Creating agent for deterministic pass ${pass_label}..."
  phase3_create_agent "$agent_id"

  local turn_count="${#turn_texts[@]}"
  local turn_no summary current_state current_latency
  for i in "${!turn_texts[@]}"; do
    turn_no=$((i + 1))
    summary="$(phase3_run_turn "$agent_id" "${turn_texts[$i]}" "${expected_states[$i]}")"
    echo "pass ${pass_label} turn ${turn_no}/${turn_count}: ${summary}"

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

  local expected_states_csv
  local last_expected_state
  expected_states_csv="$(IFS=,; echo "${expected_states[*]}")"
  last_expected_state="${expected_states[$((turn_count - 1))]}"

  phase3_wait_for_state "$agent_id" "$last_expected_state"
  phase3_wait_for_history "$agent_id" "$expected_states_csv"
  phase3_wait_for_redis_window "$agent_id" 12
  phase3_wait_for_postgres_count "$agent_id" interaction_log "$turn_count"
  phase3_wait_for_postgres_count "$agent_id" emotion_history "$turn_count"

  phase3_fetch_history_states_json "$agent_id" >"$history_file"
  printf '%s\n' "$max_latency_ms" >"$latency_file"
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
PHASE3_MULTI_MAX_LATENCY_MS="${PHASE3_MULTI_MAX_LATENCY_MS:-5000}"

export LLM_PROVIDER LLM_BASE_URL LLM_MODEL LLM_ENABLE_THINKING
export OLLAMA_MODEL_TAG PHASE3_MULTI_MAX_LATENCY_MS

phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3
phase2_require_cmd ollama

history_run_1="$(mktemp -t phase3-determinism-history-1.XXXXXX.json)"
history_run_2="$(mktemp -t phase3-determinism-history-2.XXXXXX.json)"
latency_run_1="$(mktemp -t phase3-determinism-latency-1.XXXXXX.txt)"
latency_run_2="$(mktemp -t phase3-determinism-latency-2.XXXXXX.txt)"
cleanup_files() {
  rm -f "$history_run_1" "$history_run_2" "$latency_run_1" "$latency_run_2"
}

trap 'phase2_cleanup_orchestrator; phase2_cleanup_stack; cleanup_files' EXIT

echo "Starting real dependencies for Phase 3 deterministic regression..."
phase2_compose_up_support
phase2_wait_for_support

echo "Waiting for local Ollama..."
phase3_wait_for_ollama
phase3_require_ollama_model

echo "Warming Qwen local model..."
phase3_warm_model

echo "Starting local orchestrator against real dependencies and Ollama..."
phase2_start_orchestrator

scenario_token="phase3-determinism-token-$(date +%s)-$$"
agent_run_1="phase3-determinism-a-$(date +%s)-$$"
agent_run_2="phase3-determinism-b-$(date +%s)-$$"

phase3_run_determinism_pass "A" "$agent_run_1" "$scenario_token" "$history_run_1" "$latency_run_1"
phase3_run_determinism_pass "B" "$agent_run_2" "$scenario_token" "$history_run_2" "$latency_run_2"

comparison_summary="$(python3 - "$history_run_1" "$history_run_2" "$latency_run_1" "$latency_run_2" <<'PY'
import json
import pathlib
import sys

history_a = json.loads(pathlib.Path(sys.argv[1]).read_text(encoding="utf-8"))
history_b = json.loads(pathlib.Path(sys.argv[2]).read_text(encoding="utf-8"))
latency_a = int(pathlib.Path(sys.argv[3]).read_text(encoding="utf-8").strip())
latency_b = int(pathlib.Path(sys.argv[4]).read_text(encoding="utf-8").strip())

if history_a != history_b:
    raise SystemExit(
        "determinism regression failed: state history differs between repeated runs\n"
        f"run_a={history_a}\nrun_b={history_b}"
    )

print(json.dumps({
    "states": history_a,
    "max_latency_ms": max(latency_a, latency_b),
}, ensure_ascii=True))
PY
)"

echo "Phase 3 Qwen local determinism regression completed successfully."
echo "$comparison_summary"
