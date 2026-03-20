#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if ! command -v docker >/dev/null 2>&1; then
  echo "docker nao encontrado. Rode ./scripts/setup_docker_ubuntu.sh primeiro."
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "docker compose plugin nao encontrado."
  exit 1
fi

echo "Subindo Qdrant..."
docker compose up -d qdrant

echo "Aguardando Qdrant responder em http://127.0.0.1:6333/collections ..."
for _ in $(seq 1 60); do
  if curl -fsS http://127.0.0.1:6333/collections >/dev/null 2>&1; then
    echo "Qdrant pronto."
    docker compose ps qdrant
    exit 0
  fi
  sleep 1
done

echo "Qdrant nao respondeu dentro do tempo esperado."
docker compose logs --tail=100 qdrant || true
exit 1
