#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_run_behavioral_scenario() {
  local name="$1"
  local -n turn_texts_ref="$2"
  local -n expected_states_ref="$3"
  local agent_id="phase3-behavior-${name}-$(date +%s)-$$"
  local turn_count="${#turn_texts_ref[@]}"
  local scenario_max_latency_ms=0

  echo "Creating agent for scenario ${name}..."
  phase3_create_agent "$agent_id"

  for i in "${!turn_texts_ref[@]}"; do
    local turn_no=$((i + 1))
    local summary
    summary="$(phase3_run_turn "$agent_id" "${turn_texts_ref[$i]}" "${expected_states_ref[$i]}")"
    echo "scenario ${name} turn ${turn_no}/${turn_count}: ${summary}"

    local current_state
    local current_latency
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

    if [ "$current_latency" -gt "$scenario_max_latency_ms" ]; then
      scenario_max_latency_ms="$current_latency"
    fi
    if [ "$current_latency" -gt "$max_latency_ms" ]; then
      max_latency_ms="$current_latency"
    fi

    if [ "$turn_no" -lt "$turn_count" ]; then
      phase3_sleep_for_state_constraints "$current_state"
    fi
  done

  local expected_states_csv
  local last_expected_state
  local expected_working_memory_entries=$((turn_count * 2))
  expected_states_csv="$(IFS=,; echo "${expected_states_ref[*]}")"
  last_expected_state="${expected_states_ref[$((turn_count - 1))]}"

  phase3_wait_for_state "$agent_id" "$last_expected_state"
  phase3_wait_for_history "$agent_id" "$expected_states_csv"
  phase3_wait_for_redis_window "$agent_id" "$expected_working_memory_entries"
  phase3_wait_for_postgres_count "$agent_id" interaction_log "$turn_count"
  phase3_wait_for_postgres_count "$agent_id" emotion_history "$turn_count"

  scenario_count=$((scenario_count + 1))
  echo "scenario ${name} completed: final_state=${last_expected_state} max_latency_ms=${scenario_max_latency_ms}"
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

trap 'phase2_cleanup_orchestrator; phase3_cleanup_python_ml_runtime; phase2_cleanup_stack' EXIT

phase3_prepare_python_ml_runtime

echo "Starting real dependencies for Phase 3 behavioral matrix..."
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

scenario_count=0
max_latency_ms=0
run_token="phase3-behavior-token-$(date +%s)-$$"

scenario_urgency_turns=(
  "This is urgent, please handle asap ${run_token}-u1"
)
scenario_urgency_states=(
  anxious
)
phase3_run_behavioral_scenario "urgency" scenario_urgency_turns scenario_urgency_states

scenario_mild_criticism_turns=(
  "I am disappointed with this answer ${run_token}-mc1"
)
scenario_mild_criticism_states=(
  empathetic
)
phase3_run_behavioral_scenario "mild-criticism" scenario_mild_criticism_turns scenario_mild_criticism_states

scenario_severe_criticism_turns=(
  "This rollout is unacceptable and terrible ${run_token}-sc1"
)
scenario_severe_criticism_states=(
  worried
)
phase3_run_behavioral_scenario "severe-criticism" scenario_severe_criticism_turns scenario_severe_criticism_states

scenario_curious_success_turns=(
  "Tell me more about this architecture ${run_token}-cs1"
  "It worked, success confirmed ${run_token}-cs2"
)
scenario_curious_success_states=(
  curious
  joyful
)
phase3_run_behavioral_scenario "curious-success" scenario_curious_success_turns scenario_curious_success_states

scenario_worried_empathy_turns=(
  "This rollout is unacceptable and terrible ${run_token}-we1"
  "I understand this is hard, sorry this happened ${run_token}-we2"
)
scenario_worried_empathy_states=(
  worried
  empathetic
)
phase3_run_behavioral_scenario "worried-empathy" scenario_worried_empathy_turns scenario_worried_empathy_states

scenario_frustrated_resolution_turns=(
  "Tell me more about this architecture ${run_token}-fr1"
  "This problem is wrong ${run_token}-fr2"
  "The issue is fixed now and resolved ${run_token}-fr3"
)
scenario_frustrated_resolution_states=(
  curious
  frustrated
  calm
)
phase3_run_behavioral_scenario "frustrated-resolution" scenario_frustrated_resolution_turns scenario_frustrated_resolution_states

scenario_calm_boredom_turns=(
  "Tell me more about this architecture ${run_token}-cb1"
  "This problem is wrong ${run_token}-cb2"
  "The issue is fixed now and resolved ${run_token}-cb3"
  "This is getting boring and repetitive ${run_token}-cb4"
)
scenario_calm_boredom_states=(
  curious
  frustrated
  calm
  neutral
)
phase3_run_behavioral_scenario "calm-boredom" scenario_calm_boredom_turns scenario_calm_boredom_states

scenario_empathetic_frustration_turns=(
  "I am disappointed with this answer ${run_token}-ef1"
  "I'm frustrated and stuck again ${run_token}-ef2"
)
scenario_empathetic_frustration_states=(
  empathetic
  anxious
)
phase3_run_behavioral_scenario "empathetic-frustration" scenario_empathetic_frustration_turns scenario_empathetic_frustration_states

scenario_anxious_resolution_turns=(
  "This is urgent, please handle asap ${run_token}-ar1"
  "The issue is fixed now and resolved ${run_token}-ar2"
)
scenario_anxious_resolution_states=(
  anxious
  calm
)
phase3_run_behavioral_scenario "anxious-resolution" scenario_anxious_resolution_turns scenario_anxious_resolution_states

echo "Phase 3 Qwen local behavioral matrix completed successfully."
echo "scenarios=${scenario_count}"
echo "max_latency_ms=${max_latency_ms}"
