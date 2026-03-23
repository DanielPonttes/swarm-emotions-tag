#!/usr/bin/env bash

phase2_root_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")/../.." >/dev/null 2>&1 && pwd
}

phase2_source_profile() {
  if [ -f "$HOME/.profile" ]; then
    # shellcheck disable=SC1090
    . "$HOME/.profile"
  fi
}

phase2_require_cmd() {
  local cmd="$1"
  if ! command -v "$cmd" >/dev/null 2>&1; then
    echo "missing required command: $cmd" >&2
    exit 1
  fi
}

phase2_shell_join() {
  local joined=""
  local part
  for part in "$@"; do
    printf -v part '%q' "$part"
    joined+="$part "
  done
  printf '%s\n' "${joined% }"
}

phase2_docker() {
  if docker info >/dev/null 2>&1; then
    docker "$@"
    return
  fi

  if command -v sg >/dev/null 2>&1; then
    sg docker -c "$(phase2_shell_join docker "$@")"
    return
  fi

  echo "docker daemon is not accessible from this shell" >&2
  exit 1
}

phase2_docker_compose() {
  local compose_args=("-f" "$PHASE2_COMPOSE_FILE")
  if [ -f "${PHASE2_COMPOSE_OVERRIDE_FILE:-}" ]; then
    compose_args+=("-f" "$PHASE2_COMPOSE_OVERRIDE_FILE")
  fi
  phase2_docker compose "${compose_args[@]}" "$@"
}

phase2_prepare_env() {
  PHASE2_ROOT_DIR="$(phase2_root_dir)"
  PHASE2_COMPOSE_FILE="$PHASE2_ROOT_DIR/docker-compose.yml"
  PHASE2_COMPOSE_OVERRIDE_FILE="$PHASE2_ROOT_DIR/docker-compose.override.yml"

  HTTP_PORT="${HTTP_PORT:-8080}"
  ORCH_URL="${ORCH_URL:-http://127.0.0.1:${HTTP_PORT}}"
  REDIS_ADDR="${REDIS_ADDR:-127.0.0.1:6379}"
  POSTGRES_DSN="${POSTGRES_DSN:-postgres://emotionrag:dev_password_change_me@127.0.0.1:5433/emotionrag?sslmode=disable}"
  QDRANT_ADDR="${QDRANT_ADDR:-127.0.0.1:6333}"
  QDRANT_COLLECTION="${QDRANT_COLLECTION:-agent_memories}"
  EMOTION_ENGINE_ADDR="${EMOTION_ENGINE_ADDR:-127.0.0.1:50051}"
  PYTHON_ML_URL="${PYTHON_ML_URL:-http://127.0.0.1:8090}"
  PHASE2_KEEP_STACK_UP="${PHASE2_KEEP_STACK_UP:-false}"
  PHASE2_RESET_STACK="${PHASE2_RESET_STACK:-false}"
  PHASE2_WAIT_REDIS_SEC="${PHASE2_WAIT_REDIS_SEC:-60}"
  PHASE2_WAIT_POSTGRES_SEC="${PHASE2_WAIT_POSTGRES_SEC:-60}"
  PHASE2_WAIT_QDRANT_SEC="${PHASE2_WAIT_QDRANT_SEC:-60}"
  PHASE2_WAIT_EMOTION_SEC="${PHASE2_WAIT_EMOTION_SEC:-90}"
  PHASE2_WAIT_PYTHON_SEC="${PHASE2_WAIT_PYTHON_SEC:-90}"
  PHASE2_SUPPORT_SERVICES="${PHASE2_SUPPORT_SERVICES:-redis postgresql qdrant emotion-engine python-ml}"

  export PHASE2_ROOT_DIR PHASE2_COMPOSE_FILE PHASE2_COMPOSE_OVERRIDE_FILE
  export HTTP_PORT ORCH_URL REDIS_ADDR POSTGRES_DSN QDRANT_ADDR QDRANT_COLLECTION
  export EMOTION_ENGINE_ADDR PYTHON_ML_URL PHASE2_KEEP_STACK_UP PHASE2_RESET_STACK
  export PHASE2_WAIT_REDIS_SEC PHASE2_WAIT_POSTGRES_SEC PHASE2_WAIT_QDRANT_SEC
  export PHASE2_WAIT_EMOTION_SEC PHASE2_WAIT_PYTHON_SEC PHASE2_SUPPORT_SERVICES
}

phase2_host_port() {
  local raw_addr="$1"
  local default_port="$2"

  python3 - "$raw_addr" "$default_port" <<'PY'
import sys
import urllib.parse

raw_addr = sys.argv[1].strip()
default_port = sys.argv[2].strip()

if "://" not in raw_addr:
    raw_addr = "tcp://" + raw_addr

parsed = urllib.parse.urlparse(raw_addr)
host = parsed.hostname
port = parsed.port or int(default_port)

if not host:
    raise SystemExit(f"unable to parse host from {sys.argv[1]!r}")

print(host)
print(port)
PY
}

phase2_postgres_host_port() {
  local dsn="$1"

  python3 - "$dsn" <<'PY'
import sys
import urllib.parse

dsn = sys.argv[1].strip()
parsed = urllib.parse.urlparse(dsn)

host = parsed.hostname
port = parsed.port or 5432

if not host:
    raise SystemExit(f"unable to parse postgres host from {sys.argv[1]!r}")

print(host)
print(port)
PY
}

phase2_wait_for_tcp() {
  local label="$1"
  local host="$2"
  local port="$3"
  local timeout_sec="$4"
  local started_at

  started_at="$(date +%s)"
  while true; do
    if (echo >"/dev/tcp/$host/$port") >/dev/null 2>&1; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for $label on $host:$port" >&2
      return 1
    fi
    sleep 1
  done
}

phase2_wait_for_http() {
  local label="$1"
  local url="$2"
  local timeout_sec="$3"
  local started_at

  started_at="$(date +%s)"
  while true; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge "$timeout_sec" ]; then
      echo "timed out waiting for $label at $url" >&2
      return 1
    fi
    sleep 1
  done
}

phase2_compose_up_support() {
  local services=()

  if [ "$PHASE2_RESET_STACK" = "true" ]; then
    phase2_docker_compose down --remove-orphans -v >/dev/null 2>&1 || true
  fi

  # shellcheck disable=SC2206
  services=($PHASE2_SUPPORT_SERVICES)
  phase2_docker_compose up -d "${services[@]}"
}

phase2_wait_for_support() {
  local lines
  local redis_host redis_port
  local postgres_host postgres_port
  local qdrant_host qdrant_port qdrant_http_port
  local emotion_host emotion_port
  local python_host python_port

  mapfile -t lines < <(phase2_host_port "$REDIS_ADDR" "6379")
  redis_host="${lines[0]}"
  redis_port="${lines[1]}"

  mapfile -t lines < <(phase2_postgres_host_port "$POSTGRES_DSN")
  postgres_host="${lines[0]}"
  postgres_port="${lines[1]}"

  mapfile -t lines < <(phase2_host_port "$QDRANT_ADDR" "6333")
  qdrant_host="${lines[0]}"
  qdrant_port="${lines[1]}"
  qdrant_http_port="$qdrant_port"
  if [ "$qdrant_http_port" = "6334" ]; then
    qdrant_http_port="6333"
  fi

  mapfile -t lines < <(phase2_host_port "$EMOTION_ENGINE_ADDR" "50051")
  emotion_host="${lines[0]}"
  emotion_port="${lines[1]}"

  mapfile -t lines < <(phase2_host_port "$PYTHON_ML_URL" "8090")
  python_host="${lines[0]}"
  python_port="${lines[1]}"

  phase2_wait_for_tcp "redis" "$redis_host" "$redis_port" "$PHASE2_WAIT_REDIS_SEC"
  phase2_wait_for_tcp "postgres" "$postgres_host" "$postgres_port" "$PHASE2_WAIT_POSTGRES_SEC"
  phase2_wait_for_http "qdrant" "http://${qdrant_host}:${qdrant_http_port}/collections" "$PHASE2_WAIT_QDRANT_SEC"
  phase2_wait_for_tcp "emotion-engine" "$emotion_host" "$emotion_port" "$PHASE2_WAIT_EMOTION_SEC"
  phase2_wait_for_http "python-ml" "http://${python_host}:${python_port}/health" "$PHASE2_WAIT_PYTHON_SEC"
}

phase2_start_orchestrator() {
  local use_mock_connectors="${USE_MOCK_CONNECTORS:-false}"

  PHASE2_ORCH_LOG="$(mktemp -t phase2-orchestrator.XXXXXX.log)"
  PHASE2_ORCH_BIN="$(mktemp -t phase2-orchestrator.XXXXXX.bin)"
  export PHASE2_ORCH_LOG
  export PHASE2_ORCH_BIN

  (
    cd "$PHASE2_ROOT_DIR/orchestrator"
    go build -o "$PHASE2_ORCH_BIN" ./cmd/server
  ) >>"$PHASE2_ORCH_LOG" 2>&1

  (
    cd "$PHASE2_ROOT_DIR/orchestrator"
    USE_MOCK_CONNECTORS="$use_mock_connectors" \
      HTTP_PORT="$HTTP_PORT" \
      REDIS_ADDR="$REDIS_ADDR" \
      POSTGRES_DSN="$POSTGRES_DSN" \
      QDRANT_ADDR="$QDRANT_ADDR" \
      QDRANT_COLLECTION="$QDRANT_COLLECTION" \
      EMOTION_ENGINE_ADDR="$EMOTION_ENGINE_ADDR" \
      PYTHON_ML_URL="$PYTHON_ML_URL" \
      "$PHASE2_ORCH_BIN"
  ) >"$PHASE2_ORCH_LOG" 2>&1 &

  PHASE2_ORCH_PID=$!
  export PHASE2_ORCH_PID

  phase2_wait_for_orchestrator_ready
}

phase2_wait_for_orchestrator_ready() {
  local started_at

  started_at="$(date +%s)"
  while true; do
    if curl -fsS "$ORCH_URL/ready" >/dev/null 2>&1; then
      return 0
    fi

    if ! kill -0 "$PHASE2_ORCH_PID" >/dev/null 2>&1; then
      echo "orchestrator exited before becoming ready; log: $PHASE2_ORCH_LOG" >&2
      cat "$PHASE2_ORCH_LOG" >&2
      return 1
    fi

    if [ "$(( $(date +%s) - started_at ))" -ge 90 ]; then
      echo "timed out waiting for orchestrator readiness; log: $PHASE2_ORCH_LOG" >&2
      cat "$PHASE2_ORCH_LOG" >&2
      return 1
    fi
    sleep 1
  done
}

phase2_cleanup_orchestrator() {
  if [ -n "${PHASE2_ORCH_PID:-}" ] && kill -0 "$PHASE2_ORCH_PID" >/dev/null 2>&1; then
    kill "$PHASE2_ORCH_PID" >/dev/null 2>&1 || true
    wait "$PHASE2_ORCH_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "${PHASE2_ORCH_BIN:-}" ] && [ -f "${PHASE2_ORCH_BIN:-}" ]; then
    rm -f "$PHASE2_ORCH_BIN"
  fi
}

phase2_cleanup_stack() {
  if [ "${PHASE2_KEEP_STACK_UP:-false}" != "true" ]; then
    phase2_docker_compose down --remove-orphans >/dev/null 2>&1 || true
  fi
}

phase2_smoke_request() {
  local agent_id payload response

  agent_id="phase2-smoke-$(date +%s)-$$"
  payload="$(python3 - "$agent_id" <<'PY'
import json
import sys

print(json.dumps({
    "agent_id": sys.argv[1],
    "text": "phase2 smoke request",
}))
PY
)"

  response="$(curl -fsS \
    -H "Content-Type: application/json" \
    -d "$payload" \
    "$ORCH_URL/api/v1/interact")"

  python3 - "$response" <<'PY'
import json
import sys

payload = json.loads(sys.argv[1])
if not payload.get("response"):
    raise SystemExit("missing response in /interact payload")
if not payload.get("emotion_state"):
    raise SystemExit("missing emotion_state in /interact payload")
if not payload.get("fsm_state"):
    raise SystemExit("missing fsm_state in /interact payload")

print(json.dumps({
    "response": payload["response"],
    "fsm_state": payload["fsm_state"],
    "latency_ms": payload.get("latency_ms"),
}, ensure_ascii=True))
PY
}

phase2_capture_goroutines() {
  local output_file="$1"
  curl -fsS "$ORCH_URL/debug/pprof/goroutine?debug=1" -o "$output_file"
}

phase2_goroutine_total() {
  local input_file="$1"
  python3 - "$input_file" <<'PY'
import pathlib
import re
import sys

content = pathlib.Path(sys.argv[1]).read_text(encoding="utf-8")
match = re.search(r"goroutine profile: total (\d+)", content)
if not match:
    raise SystemExit("unable to parse goroutine count")
print(match.group(1))
PY
}
