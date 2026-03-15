#!/usr/bin/env bash
set -euo pipefail

cd ~/swarm-emotions-tag

# Mata sessões antigas, se existirem
tmux kill-session -t swarm-go 2>/dev/null || true
tmux kill-session -t swarm-rust 2>/dev/null || true
tmux kill-session -t swarm-py 2>/dev/null || true
tmux kill-session -t swarm-orch 2>/dev/null || true
tmux kill-session -t swarm-logs 2>/dev/null || true

# Watcher do Go / orchestrator
tmux new-session -d -s swarm-go \
'cd ~/swarm-emotions-tag && \
find orchestrator -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) | \
entr -r sh -c "docker compose up -d --build orchestrator"'

# Watcher do Rust / emotion-engine
tmux new-session -d -s swarm-rust \
'cd ~/swarm-emotions-tag && \
find emotion-engine proto -type f \( -name "*.rs" -o -name "*.toml" -o -name "*.proto" \) | \
entr -r sh -c "docker compose up -d --build emotion-engine"'

# Watcher do Python / python-ml
tmux new-session -d -s swarm-py \
'cd ~/swarm-emotions-tag && \
find python-ml -type f \( -name "*.py" -o -name "*.toml" -o -name "*.txt" \) | \
entr -r sh -c "docker compose up -d --build python-ml"'

# Watcher de arquivos compartilhados / compose
# Quando mudar docker-compose.yml, override, .env.example, proto ou docs/configs compartilhados,
# reinicia o orchestrator para reabsorver configuração e dependências de integração.
tmux new-session -d -s swarm-orch \
'cd ~/swarm-emotions-tag && \
find . -type f \( \
  -name "docker-compose.yml" -o \
  -name "docker-compose.override.yml" -o \
  -name ".env.example" -o \
  -path "./proto/*" -o \
  -path "./docs/*" -o \
  -path "./scripts/*" -o \
  -name "*.yml" -o \
  -name "*.yaml" \
\) | entr -r sh -c "docker compose up -d --build orchestrator"'

# Sessão só de logs
tmux new-session -d -s swarm-logs \
'cd ~/swarm-emotions-tag && docker compose logs -f --tail=100'

tmux ls



