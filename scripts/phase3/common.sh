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

phase3_wait_for_classifier_model() {
  local timeout_sec="${1:-120}"
  local started_at response validation_output validation_status=0

  started_at="$(date +%s)"
  while true; do
    response="$(curl -fsS "$PYTHON_ML_URL/health")"
    if validation_output="$(
      python3 - \
        "$response" \
        "${CLASSIFIER_MODE:-}" \
        "${CLASSIFIER_MODEL_NAME:-}" \
        "${CLASSIFIER_BATCH_SIZE:-}" \
        "${CLASSIFIER_OLLAMA_MAX_CONCURRENCY:-}" 2>&1 <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_mode = sys.argv[2].strip()
expected_model = sys.argv[3].strip()
expected_batch_size = sys.argv[4].strip()
expected_ollama_concurrency = sys.argv[5].strip()

if not payload.get("model_loaded"):
    raise SystemExit(2)

if expected_mode and payload.get("classifier_mode") != expected_mode:
    raise SystemExit(
        f"expected classifier_mode={expected_mode!r}, got {payload.get('classifier_mode')!r}"
    )
if expected_model and payload.get("model_name") != expected_model:
    raise SystemExit(
        f"expected model_name={expected_model!r}, got {payload.get('model_name')!r}"
    )
if expected_batch_size and str(payload.get("classifier_batch_size")) != expected_batch_size:
    raise SystemExit(
        f"expected classifier_batch_size={expected_batch_size!r}, got {payload.get('classifier_batch_size')!r}"
    )
if (
    expected_mode == "ollama"
    and expected_ollama_concurrency
    and str(payload.get("classifier_ollama_max_concurrency")) != expected_ollama_concurrency
):
    raise SystemExit(
        "expected classifier_ollama_max_concurrency="
        f"{expected_ollama_concurrency!r}, got {payload.get('classifier_ollama_max_concurrency')!r}"
    )
PY
    )"; then
      return 0
    fi
    validation_status=$?
    if [ "$validation_status" -ne 2 ]; then
      echo "$validation_output" >&2
      echo "$response" >&2
      return 1
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for python-ml model to load" >&2
      echo "$response" >&2
      return 1
    fi
    sleep 2
  done
}

phase3_init_python_ml_runtime_defaults() {
  CLASSIFIER_MODE="${CLASSIFIER_MODE:-heuristic}"
  CLASSIFIER_MODEL_NAME="${CLASSIFIER_MODEL_NAME:-monologg/bert-base-cased-goemotions-original}"
  CLASSIFIER_TOP_K="${CLASSIFIER_TOP_K:-5}"
  CLASSIFIER_BATCH_SIZE="${CLASSIFIER_BATCH_SIZE:-8}"
  CLASSIFIER_OLLAMA_BASE_URL="${CLASSIFIER_OLLAMA_BASE_URL:-http://host.docker.internal:11434}"
  CLASSIFIER_REQUEST_TIMEOUT_SEC="${CLASSIFIER_REQUEST_TIMEOUT_SEC:-120}"
  CLASSIFIER_OLLAMA_MAX_CONCURRENCY="${CLASSIFIER_OLLAMA_MAX_CONCURRENCY:-16}"
  PHASE3_QWEN_PYTHON_ML_HOST_MODE="${PHASE3_QWEN_PYTHON_ML_HOST_MODE:-auto}"
  PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST="${PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST:-127.0.0.1}"
  PHASE3_QWEN_PYTHON_ML_HOST_PORT="${PHASE3_QWEN_PYTHON_ML_HOST_PORT:-8091}"
  PHASE3_PYTHON_ML_HOST_CONTAINER_NAME="${PHASE3_PYTHON_ML_HOST_CONTAINER_NAME:-phase3-python-ml-host-$$}"
  PHASE3_PYTHON_ML_RUNTIME_MODE="${PHASE3_PYTHON_ML_RUNTIME_MODE:-container}"
  PHASE3_PYTHON_ML_RUNTIME_OWNER_PID="${PHASE3_PYTHON_ML_RUNTIME_OWNER_PID:-}"
  export CLASSIFIER_MODE CLASSIFIER_MODEL_NAME CLASSIFIER_TOP_K CLASSIFIER_BATCH_SIZE
  export CLASSIFIER_OLLAMA_BASE_URL
  export CLASSIFIER_REQUEST_TIMEOUT_SEC CLASSIFIER_OLLAMA_MAX_CONCURRENCY
  export PHASE3_QWEN_PYTHON_ML_HOST_MODE PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST
  export PHASE3_QWEN_PYTHON_ML_HOST_PORT PHASE3_PYTHON_ML_HOST_CONTAINER_NAME
  export PHASE3_PYTHON_ML_RUNTIME_MODE PHASE3_PYTHON_ML_RUNTIME_OWNER_PID
}

phase3_ensure_python_ml_image() {
  if phase2_docker image inspect swarm-emotions-tag-python-ml >/dev/null 2>&1; then
    return 0
  fi
  phase2_docker_compose build python-ml
}

phase3_probe_containerized_ollama_access() {
  local ollama_url="$1"

  phase2_docker run --rm \
    --add-host host.docker.internal:host-gateway \
    swarm-emotions-tag-python-ml \
    python -c "import urllib.request; urllib.request.urlopen('${ollama_url}', timeout=10).read()" \
    >/dev/null 2>&1
}

phase3_start_python_ml_host_container() {
  local bind_host="$1"
  local bind_port="$2"
  local ollama_base_url="$3"

  phase2_docker rm -f "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" >/dev/null 2>&1 || true
  phase2_docker run -d --rm \
    --name "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" \
    --network host \
    -e PORT="$bind_port" \
    -e CLASSIFIER_MODE="$CLASSIFIER_MODE" \
    -e CLASSIFIER_MODEL_NAME="$CLASSIFIER_MODEL_NAME" \
    -e CLASSIFIER_TOP_K="$CLASSIFIER_TOP_K" \
    -e CLASSIFIER_BATCH_SIZE="$CLASSIFIER_BATCH_SIZE" \
    -e CLASSIFIER_OLLAMA_BASE_URL="$ollama_base_url" \
    -e CLASSIFIER_REQUEST_TIMEOUT_SEC="$CLASSIFIER_REQUEST_TIMEOUT_SEC" \
    -e CLASSIFIER_OLLAMA_MAX_CONCURRENCY="$CLASSIFIER_OLLAMA_MAX_CONCURRENCY" \
    swarm-emotions-tag-python-ml \
    uvicorn app.main:app --host "$bind_host" --port "$bind_port" \
    >/dev/null

  PHASE3_PYTHON_ML_RUNTIME_MODE="host"
  PHASE3_PYTHON_ML_RUNTIME_OWNER_PID="$$"
  export PHASE3_PYTHON_ML_RUNTIME_MODE PHASE3_PYTHON_ML_RUNTIME_OWNER_PID
}

phase3_cleanup_python_ml_runtime() {
  if [ "${PHASE3_PYTHON_ML_RUNTIME_MODE:-}" != "host" ]; then
    return 0
  fi
  if [ "${PHASE3_PYTHON_ML_RUNTIME_OWNER_PID:-}" != "$$" ]; then
    return 0
  fi
  phase2_docker rm -f "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" >/dev/null 2>&1 || true
}

phase3_exclude_python_ml_from_support_services() {
  local service
  local filtered=()
  local services=()

  # shellcheck disable=SC2206
  services=($PHASE2_SUPPORT_SERVICES)
  for service in "${services[@]}"; do
    if [ "$service" != "python-ml" ]; then
      filtered+=("$service")
    fi
  done

  PHASE2_SUPPORT_SERVICES="${filtered[*]}"
  export PHASE2_SUPPORT_SERVICES
}

phase3_prepare_python_ml_runtime() {
  local host_mode
  local ollama_probe_url
  local host_ollama_base_url

  phase3_init_python_ml_runtime_defaults

  if [ "$CLASSIFIER_MODE" != "ollama" ]; then
    return 0
  fi

  if [ "${PHASE3_PYTHON_ML_RUNTIME_MODE:-}" = "host" ]; then
    phase3_exclude_python_ml_from_support_services
    if curl -fsS "$PYTHON_ML_URL/health" >/dev/null 2>&1; then
      return 0
    fi
  fi

  host_mode="$PHASE3_QWEN_PYTHON_ML_HOST_MODE"
  ollama_probe_url="${CLASSIFIER_OLLAMA_BASE_URL%/}/api/version"

  case "$host_mode" in
    false)
      PHASE3_PYTHON_ML_RUNTIME_MODE="container"
      export PHASE3_PYTHON_ML_RUNTIME_MODE
      return 0
      ;;
    auto|true)
      phase3_ensure_python_ml_image
      if [ "$host_mode" = "auto" ] && phase3_probe_containerized_ollama_access "$ollama_probe_url"; then
        PHASE3_PYTHON_ML_RUNTIME_MODE="container"
        export PHASE3_PYTHON_ML_RUNTIME_MODE
        return 0
      fi
      ;;
    *)
      echo "invalid PHASE3_QWEN_PYTHON_ML_HOST_MODE: $host_mode" >&2
      return 1
      ;;
  esac

  echo "Containerized python-ml cannot reach Ollama at $CLASSIFIER_OLLAMA_BASE_URL; using host-network python-ml fallback..."
  host_ollama_base_url="$(phase3_normalize_ollama_base_url "$LLM_BASE_URL")"
  phase3_exclude_python_ml_from_support_services
  PYTHON_ML_URL="http://${PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST}:${PHASE3_QWEN_PYTHON_ML_HOST_PORT}"
  CLASSIFIER_OLLAMA_BASE_URL="$host_ollama_base_url"
  export PYTHON_ML_URL CLASSIFIER_OLLAMA_BASE_URL

  phase3_start_python_ml_host_container \
    "$PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST" \
    "$PHASE3_QWEN_PYTHON_ML_HOST_PORT" \
    "$CLASSIFIER_OLLAMA_BASE_URL"
  phase2_wait_for_http "python-ml-host" "$PYTHON_ML_URL/health" "${PHASE3_CLASSIFIER_READY_TIMEOUT_SEC:-120}"
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
    if phase3_service_logs_since "$service" "2m" | grep -F "$trace_id" >/dev/null 2>&1; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for trace $trace_id in logs for service $service" >&2
      phase3_service_logs_since "$service" "2m" | tail -n 80 >&2 || true
      return 1
    fi
    sleep 1
  done
}

phase3_service_logs_since() {
  local service="$1"
  local since="${2:-2m}"

  if [ "$service" = "python-ml" ] && [ "${PHASE3_PYTHON_ML_RUNTIME_MODE:-}" = "host" ]; then
    phase2_docker logs --since "$since" "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" 2>&1
    return
  fi

  phase2_docker_compose logs --since "$since" "$service" 2>&1
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
