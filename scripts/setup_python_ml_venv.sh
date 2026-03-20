#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PYTHON_ML_DIR="$ROOT_DIR/python-ml"
PYTHON_ML_VENV="${PYTHON_ML_VENV:-$PYTHON_ML_DIR/.venv}"
PYTHON_ML_PYTHON_BIN="${PYTHON_ML_PYTHON_BIN:-python3}"
PYTHON_ML_INSTALL_EXTRAS="${PYTHON_ML_INSTALL_EXTRAS:-ml}"

if ! command -v "$PYTHON_ML_PYTHON_BIN" >/dev/null 2>&1; then
  echo "python interpreter not found: $PYTHON_ML_PYTHON_BIN" >&2
  exit 1
fi

if command -v uv >/dev/null 2>&1; then
  uv venv --allow-existing --seed --python "$PYTHON_ML_PYTHON_BIN" "$PYTHON_ML_VENV"
fi

if [ ! -x "$PYTHON_ML_VENV/bin/python" ]; then
  "$PYTHON_ML_PYTHON_BIN" -m venv "$PYTHON_ML_VENV"
fi

install_target="."
if [ -n "$PYTHON_ML_INSTALL_EXTRAS" ]; then
  install_target=".[${PYTHON_ML_INSTALL_EXTRAS}]"
fi

if command -v uv >/dev/null 2>&1; then
  (
    cd "$PYTHON_ML_DIR"
    uv pip install --python "$PYTHON_ML_VENV/bin/python" -e "$install_target"
  )
else
  "$PYTHON_ML_VENV/bin/python" -m ensurepip --upgrade >/dev/null 2>&1 || true
  if ! "$PYTHON_ML_VENV/bin/python" -m pip --version >/dev/null 2>&1; then
    echo "pip is unavailable in $PYTHON_ML_VENV and uv was not found" >&2
    exit 1
  fi
  "$PYTHON_ML_VENV/bin/python" -m pip install --upgrade pip setuptools wheel
  (
    cd "$PYTHON_ML_DIR"
    "$PYTHON_ML_VENV/bin/python" -m pip install -e "$install_target"
  )
fi

(
  cd "$PYTHON_ML_DIR"
  "$PYTHON_ML_VENV/bin/python" - <<'PY'
import json

from app.runtime import collect_runtime_info, runtime_info_dict

print(json.dumps(runtime_info_dict(collect_runtime_info()), ensure_ascii=True))
PY
)
