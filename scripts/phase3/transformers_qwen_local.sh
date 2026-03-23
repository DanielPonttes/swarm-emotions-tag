#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck disable=SC1091
. "$SCRIPT_DIR/common.sh"

phase3_validate_transformers_classifier() {
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
  python3 - \
    "$health_response" \
    "$CLASSIFIER_MODEL_NAME" \
    "$CLASSIFIER_DEVICE" \
    "$CLASSIFIER_BATCH_SIZE" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
expected_model = sys.argv[2]
expected_device = sys.argv[3]
expected_batch_size = sys.argv[4]
if payload.get("classifier_mode") != "transformers":
    raise SystemExit(f"expected classifier_mode=transformers, got {payload.get('classifier_mode')!r}")
if payload.get("model_name") != expected_model:
    raise SystemExit(f"expected model_name={expected_model!r}, got {payload.get('model_name')!r}")
if payload.get("classifier_device") != expected_device:
    raise SystemExit(f"expected classifier_device={expected_device!r}, got {payload.get('classifier_device')!r}")
if str(payload.get("classifier_batch_size")) != expected_batch_size:
    raise SystemExit(
        f"expected classifier_batch_size={expected_batch_size!r}, got {payload.get('classifier_batch_size')!r}"
    )
if not payload.get("model_loaded"):
    raise SystemExit("transformers classifier is not loaded")
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
PHASE2_WAIT_PYTHON_SEC="${PHASE2_WAIT_PYTHON_SEC:-900}"
PHASE3_CLASSIFIER_READY_TIMEOUT_SEC="${PHASE3_CLASSIFIER_READY_TIMEOUT_SEC:-900}"
PYTHON_ML_INSTALL_EXTRAS="${PYTHON_ML_INSTALL_EXTRAS:-ml}"
CLASSIFIER_MODE="${CLASSIFIER_MODE:-transformers}"
CLASSIFIER_DEVICE="${CLASSIFIER_DEVICE:-cpu}"
CLASSIFIER_MODEL_NAME="${CLASSIFIER_MODEL_NAME:-monologg/bert-base-cased-goemotions-original}"
CLASSIFIER_TOP_K="${CLASSIFIER_TOP_K:-5}"
CLASSIFIER_BATCH_SIZE="${CLASSIFIER_BATCH_SIZE:-8}"
LLM_PROVIDER="${LLM_PROVIDER:-ollama-native}"
LLM_BASE_URL="${LLM_BASE_URL:-http://127.0.0.1:11434}"
LLM_MODEL="${LLM_MODEL:-Qwen/Qwen3.5-27B}"
LLM_ENABLE_THINKING="${LLM_ENABLE_THINKING:-false}"
OLLAMA_MODEL_TAG="${OLLAMA_MODEL_TAG:-qwen3.5:27b}"
PHASE3_MULTI_MAX_LATENCY_MS="${PHASE3_MULTI_MAX_LATENCY_MS:-5000}"

export PHASE2_KEEP_STACK_UP PHASE2_WAIT_PYTHON_SEC PHASE3_CLASSIFIER_READY_TIMEOUT_SEC
export PYTHON_ML_INSTALL_EXTRAS CLASSIFIER_MODE CLASSIFIER_DEVICE CLASSIFIER_MODEL_NAME
export CLASSIFIER_TOP_K CLASSIFIER_BATCH_SIZE
export LLM_PROVIDER LLM_BASE_URL LLM_MODEL LLM_ENABLE_THINKING OLLAMA_MODEL_TAG PHASE3_MULTI_MAX_LATENCY_MS

phase2_prepare_env

phase2_require_cmd docker
phase2_require_cmd curl
phase2_require_cmd go
phase2_require_cmd python3
phase2_require_cmd ollama

classifier_results_file="$(mktemp -t phase3-transformers-classifier.XXXXXX.jsonl)"
cleanup_files() {
  rm -f "$classifier_results_file"
}

trap 'phase2_cleanup_stack; cleanup_files' EXIT

echo "Building python-ml with transformers dependencies..."
phase2_docker_compose build python-ml

echo "Starting real dependencies for Phase 3 transformers validation..."
phase2_compose_up_support
phase2_wait_for_support
phase3_wait_for_classifier_model "$PHASE3_CLASSIFIER_READY_TIMEOUT_SEC"

echo "Validating direct transformers classifier responses..."
phase3_validate_transformers_classifier "$classifier_results_file"

echo "Running behavioral matrix with transformers classifier and Qwen local..."
"$SCRIPT_DIR/behavioral_qwen_local.sh"

echo "Phase 3 transformers + Qwen validation completed successfully."
