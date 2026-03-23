#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_probe_containerized_ollama_access() {
  local ollama_url="$1"

  phase2_docker run --rm \
    --add-host host.docker.internal:host-gateway \
    swarm-emotions-tag-python-ml \
    python -c "import urllib.request; urllib.request.urlopen('${ollama_url}', timeout=10).read()" \
    >/dev/null 2>&1
}

phase3_start_python_ml_host_container() {
  local image_name="$1"
  local bind_host="$2"
  local bind_port="$3"
  local ollama_base_url="$4"

  phase2_docker rm -f "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" >/dev/null 2>&1 || true

  phase2_docker run -d --rm \
    --name "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" \
    --network host \
    -e PORT="$bind_port" \
    -e CLASSIFIER_MODE="$CLASSIFIER_MODE" \
    -e CLASSIFIER_MODEL_NAME="$CLASSIFIER_MODEL_NAME" \
    -e CLASSIFIER_OLLAMA_BASE_URL="$ollama_base_url" \
    -e CLASSIFIER_REQUEST_TIMEOUT_SEC="$CLASSIFIER_REQUEST_TIMEOUT_SEC" \
    "$image_name" \
    uvicorn app.main:app --host "$bind_host" --port "$bind_port" \
    >/dev/null
}

phase3_cleanup_python_ml_host_container() {
  if [ -n "${PHASE3_PYTHON_ML_HOST_CONTAINER_NAME:-}" ]; then
    phase2_docker rm -f "$PHASE3_PYTHON_ML_HOST_CONTAINER_NAME" >/dev/null 2>&1 || true
  fi
}

phase3_prepare_python_ml_runtime() {
  local image_name="swarm-emotions-tag-python-ml"
  local ollama_probe_url
  local host_mode_enabled="${PHASE3_QWEN_PYTHON_ML_HOST_MODE:-auto}"

  ollama_probe_url="${CLASSIFIER_OLLAMA_BASE_URL%/}/api/version"

  case "$host_mode_enabled" in
    true)
      ;;
    false)
      return 0
      ;;
    auto)
      if phase3_probe_containerized_ollama_access "$ollama_probe_url"; then
        return 0
      fi
      ;;
    *)
      echo "invalid PHASE3_QWEN_PYTHON_ML_HOST_MODE: $host_mode_enabled" >&2
      return 1
      ;;
  esac

  echo "Containerized python-ml cannot reach Ollama at $CLASSIFIER_OLLAMA_BASE_URL; using host-network python-ml fallback..."
  PHASE2_SUPPORT_SERVICES="redis postgresql qdrant emotion-engine"
  PYTHON_ML_URL="http://${PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST}:${PHASE3_QWEN_PYTHON_ML_HOST_PORT}"
  CLASSIFIER_OLLAMA_BASE_URL="$LLM_BASE_URL"
  export PHASE2_SUPPORT_SERVICES PYTHON_ML_URL CLASSIFIER_OLLAMA_BASE_URL

  phase3_start_python_ml_host_container \
    "$image_name" \
    "$PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST" \
    "$PHASE3_QWEN_PYTHON_ML_HOST_PORT" \
    "$CLASSIFIER_OLLAMA_BASE_URL"

  phase2_wait_for_http "python-ml-host" "$PYTHON_ML_URL/health" "$PHASE3_CLASSIFIER_READY_TIMEOUT_SEC"
}

phase3_validate_ollama_classifier() {
  local responses_file="$1"
  local health_response
  local texts=(
    "I am grateful for your help on this project."
    "This is urgent and I am worried about the deadline."
    "I am disappointed and upset because this failed."
    "Tell me more, I am curious about this design."
    "This is awesome and I am very happy with the result."
  )

  health_response="$(curl -fsS "$PYTHON_ML_URL/health")"
  python3 - "$health_response" "$CLASSIFIER_MODE" "$CLASSIFIER_MODEL_NAME" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_mode = sys.argv[2]
expected_model = sys.argv[3]

if payload.get("classifier_mode") != expected_mode:
    raise SystemExit(
        f"expected classifier_mode={expected_mode!r}, got {payload.get('classifier_mode')!r}"
    )
if payload.get("model_name") != expected_model:
    raise SystemExit(
        f"expected model_name={expected_model!r}, got {payload.get('model_name')!r}"
    )
if not payload.get("model_loaded"):
    raise SystemExit("Qwen emotion classifier is not loaded")
PY

  : >"$responses_file"
  for text in "${texts[@]}"; do
    local payload response
    payload="$(python3 - "$text" <<'PY'
import json
import sys

print(json.dumps({"text": sys.argv[1]}, ensure_ascii=True))
PY
)"
    response="$(curl -fsS \
      -H "Content-Type: application/json" \
      -d "$payload" \
      "$PYTHON_ML_URL/classify-emotion")"
    printf '%s\n' "$response" >>"$responses_file"

    python3 - "$response" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
vector = payload.get("emotion_vector", [])
label = payload.get("label", "")
confidence = payload.get("confidence", -1)
if len(vector) != 6:
    raise SystemExit(f"expected 6D vector, got {len(vector)}")
if not isinstance(label, str) or not label.strip():
    raise SystemExit("classifier returned empty label")
if not isinstance(confidence, (int, float)) or confidence < 0 or confidence > 1:
    raise SystemExit(f"invalid confidence: {confidence!r}")
print(json.dumps({
    "label": label,
    "confidence": confidence,
}, ensure_ascii=True))
PY
  done

  python3 - "$responses_file" <<'PY'
import json
import pathlib
import sys

lines = [
    line.strip()
    for line in pathlib.Path(sys.argv[1]).read_text(encoding="utf-8").splitlines()
    if line.strip()
]
if len(lines) < 5:
    raise SystemExit(f"expected 5 classifier responses, got {len(lines)}")

labels = []
for line in lines:
    payload = json.loads(line)
    labels.append(payload["label"].strip().lower())

unique_labels = sorted(set(labels))
if len(unique_labels) < 3:
    raise SystemExit(f"expected at least 3 distinct labels, got {unique_labels}")

print(json.dumps({
    "responses": len(lines),
    "unique_labels": unique_labels,
}, ensure_ascii=True))
PY
}

phase2_source_profile

HTTP_PORT="${HTTP_PORT:-18080}"
POSTGRES_DSN="${POSTGRES_DSN:-postgres://emotionrag:dev_password_change_me@127.0.0.1:5433/emotionrag?sslmode=disable}"
QDRANT_COLLECTION="${QDRANT_COLLECTION:-agent_memories}"
PHASE2_KEEP_STACK_UP="${PHASE2_KEEP_STACK_UP:-true}"
PHASE2_WAIT_PYTHON_SEC="${PHASE2_WAIT_PYTHON_SEC:-180}"
PHASE3_CLASSIFIER_READY_TIMEOUT_SEC="${PHASE3_CLASSIFIER_READY_TIMEOUT_SEC:-240}"
CLASSIFIER_MODE="${CLASSIFIER_MODE:-ollama}"
CLASSIFIER_MODEL_NAME="${CLASSIFIER_MODEL_NAME:-Qwen/Qwen3.5-27B}"
CLASSIFIER_OLLAMA_BASE_URL="${CLASSIFIER_OLLAMA_BASE_URL:-http://host.docker.internal:11434}"
CLASSIFIER_REQUEST_TIMEOUT_SEC="${CLASSIFIER_REQUEST_TIMEOUT_SEC:-120}"
PHASE3_QWEN_PYTHON_ML_HOST_MODE="${PHASE3_QWEN_PYTHON_ML_HOST_MODE:-auto}"
PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST="${PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST:-127.0.0.1}"
PHASE3_QWEN_PYTHON_ML_HOST_PORT="${PHASE3_QWEN_PYTHON_ML_HOST_PORT:-8091}"
PHASE3_PYTHON_ML_HOST_CONTAINER_NAME="${PHASE3_PYTHON_ML_HOST_CONTAINER_NAME:-phase3-qwen-python-ml-host}"
LLM_PROVIDER="${LLM_PROVIDER:-ollama-native}"
LLM_BASE_URL="${LLM_BASE_URL:-http://127.0.0.1:11434}"
LLM_MODEL="${LLM_MODEL:-Qwen/Qwen3.5-27B}"
LLM_ENABLE_THINKING="${LLM_ENABLE_THINKING:-false}"
OLLAMA_MODEL_TAG="${OLLAMA_MODEL_TAG:-qwen3.5:27b}"
PHASE3_MULTI_MAX_LATENCY_MS="${PHASE3_MULTI_MAX_LATENCY_MS:-5000}"

export PHASE2_KEEP_STACK_UP PHASE2_WAIT_PYTHON_SEC PHASE3_CLASSIFIER_READY_TIMEOUT_SEC
export CLASSIFIER_MODE CLASSIFIER_MODEL_NAME CLASSIFIER_OLLAMA_BASE_URL CLASSIFIER_REQUEST_TIMEOUT_SEC
export PHASE3_QWEN_PYTHON_ML_HOST_MODE PHASE3_QWEN_PYTHON_ML_HOST_BIND_HOST
export PHASE3_QWEN_PYTHON_ML_HOST_PORT PHASE3_PYTHON_ML_HOST_CONTAINER_NAME
export LLM_PROVIDER LLM_BASE_URL LLM_MODEL LLM_ENABLE_THINKING OLLAMA_MODEL_TAG PHASE3_MULTI_MAX_LATENCY_MS

phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3
phase2_require_cmd ollama

classifier_results_file="$(mktemp -t phase3-qwen-emotions-classifier.XXXXXX.jsonl)"
cleanup_files() {
  rm -f "$classifier_results_file"
}

trap 'phase3_cleanup_python_ml_host_container; phase2_cleanup_stack; cleanup_files' EXIT

echo "Waiting for local Ollama..."
phase3_wait_for_ollama
phase3_require_ollama_model

echo "Warming Qwen local model..."
phase3_warm_model

echo "Building python-ml image for Qwen emotion classifier..."
phase2_docker_compose build python-ml

phase3_prepare_python_ml_runtime

echo "Starting real dependencies for Phase 3 Qwen emotion validation..."
phase2_compose_up_support
phase2_wait_for_support
phase3_wait_for_classifier_model "$PHASE3_CLASSIFIER_READY_TIMEOUT_SEC"

echo "Validating direct Qwen classifier responses..."
phase3_validate_ollama_classifier "$classifier_results_file"

echo "Running behavioral matrix with Qwen local for LLM and emotion classifier..."
"$SCRIPT_DIR/behavioral_qwen_local.sh"

echo "Phase 3 Qwen emotion classifier validation completed successfully."
