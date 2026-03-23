#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_ML_DIR="$ROOT_DIR/python-ml"
PYTHON_ML_VENV="${PYTHON_ML_VENV:-$PYTHON_ML_DIR/.venv}"

CLASSIFIER_MODE="${CLASSIFIER_MODE:-transformers}"
CLASSIFIER_MODEL_NAME="${CLASSIFIER_MODEL_NAME:-monologg/bert-base-cased-goemotions-original}"
CLASSIFIER_DEVICE="${CLASSIFIER_DEVICE:-cuda:0}"
CLASSIFIER_TOP_K="${CLASSIFIER_TOP_K:-5}"
CLASSIFIER_BATCH_SIZE="${CLASSIFIER_BATCH_SIZE:-64}"
CLASSIFIER_OLLAMA_MAX_CONCURRENCY="${CLASSIFIER_OLLAMA_MAX_CONCURRENCY:-8}"
PYTHON_ML_BENCH_WARMUP_BATCHES="${PYTHON_ML_BENCH_WARMUP_BATCHES:-10}"
PYTHON_ML_BENCH_DURATION_SEC="${PYTHON_ML_BENCH_DURATION_SEC:-1800}"
PYTHON_ML_BENCH_TEXTS_FILE="${PYTHON_ML_BENCH_TEXTS_FILE:-}"
PYTHON_ML_EXPECTED_GPU_NAME="${PYTHON_ML_EXPECTED_GPU_NAME:-RTX 5090}"
PYTHON_ML_BENCH_OUT_DIR="${PYTHON_ML_BENCH_OUT_DIR:-$ROOT_DIR/artifacts/python-ml-gpu}"
PYTHON_ML_SKIP_SETUP="${PYTHON_ML_SKIP_SETUP:-false}"

mkdir -p "$PYTHON_ML_BENCH_OUT_DIR"

if [ "$PYTHON_ML_SKIP_SETUP" != "true" ]; then
  "$ROOT_DIR/scripts/setup_python_ml_venv.sh"
fi

if [ ! -x "$PYTHON_ML_VENV/bin/python" ]; then
  echo "python-ml virtualenv is missing: $PYTHON_ML_VENV" >&2
  exit 1
fi

timestamp="$(date +%Y%m%d-%H%M%S)"
output_file="$PYTHON_ML_BENCH_OUT_DIR/classifier-benchmark-${timestamp}.json"

cmd=(
  "$PYTHON_ML_VENV/bin/python"
  tools/benchmark_classifier.py
  --mode "$CLASSIFIER_MODE"
  --model-name "$CLASSIFIER_MODEL_NAME"
  --device "$CLASSIFIER_DEVICE"
  --top-k "$CLASSIFIER_TOP_K"
  --batch-size "$CLASSIFIER_BATCH_SIZE"
  --ollama-max-concurrency "$CLASSIFIER_OLLAMA_MAX_CONCURRENCY"
  --warmup-batches "$PYTHON_ML_BENCH_WARMUP_BATCHES"
  --duration-sec "$PYTHON_ML_BENCH_DURATION_SEC"
  --expected-gpu-substring "$PYTHON_ML_EXPECTED_GPU_NAME"
  --output "$output_file"
)

if [ -n "$PYTHON_ML_BENCH_TEXTS_FILE" ]; then
  cmd+=(--texts-file "$PYTHON_ML_BENCH_TEXTS_FILE")
fi

(
  cd "$PYTHON_ML_DIR"
  "${cmd[@]}"
)

echo "benchmark report saved to $output_file"
