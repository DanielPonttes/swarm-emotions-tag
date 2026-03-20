# Swarm Emotions

Monorepo multi-servico do EmotionRAG. A fundacao da Fase 0 esta concluida e o
`emotion-engine` em Rust ja entrou na Fase 1 com FSM, calculo vetorial, score
fusion, promocao de memoria e servidor gRPC funcionais. O `orchestrator` em Go
ja entrou na Fase 2 com API REST, pipeline de 8 steps e connectors mockados.

## Estrutura

- `proto/`: fonte de verdade dos contratos Protobuf.
- `emotion-engine/`: servico Rust gRPC do motor emocional.
- `orchestrator/`: servico Go HTTP do plano de controle.
- `python-ml/`: servico Python FastAPI auxiliar.
- `docker/`: Dockerfiles e schema SQL inicial.
- `.github/workflows/ci.yml`: pipeline basico de lint, teste e validacao de proto.

## Pre-requisitos

- Go 1.23+
- Rust toolchain estavel com `clippy` e `rustfmt`
- GNU Make
- `protoc`
- `protoc-gen-go`
- `protoc-gen-go-grpc`
- Docker com Docker Compose
- Python 3.11+

## Setup rapido

```bash
cp .env.example .env
make proto-gen
make build
make test
```

Para subir apenas a infraestrutura:

```bash
make docker-infra
```

Para subir tudo:

```bash
make docker-up
```

## Docker no Ubuntu 24.04

Se o host ainda nao tiver Docker, o repositório agora inclui um bootstrap para
Ubuntu:

```bash
./scripts/setup_docker_ubuntu.sh
```

Esse script instala o Docker Engine oficial, o plugin `docker compose`,
habilita o servico e adiciona o usuario atual ao grupo `docker`.

Depois, abra um shell novo ou rode `newgrp docker` e valide:

```bash
docker --version
docker compose version
docker ps
```

## Qdrant local

Para subir apenas o Qdrant e aguardar o endpoint HTTP ficar pronto:

```bash
./scripts/up_qdrant.sh
```

Ou via `make`:

```bash
make qdrant-up
```

O script usa `docker compose up -d qdrant` e espera resposta em
`http://127.0.0.1:6333/collections`.

## LLM real local (Qwen3.5-27B)

O orquestrador agora aceita provider `openai-compatible`, adequado para servir
o Qwen localmente por um endpoint estilo OpenAI (`/v1/models`,
`/v1/chat/completions`).

Configuracao recomendada no host:

```bash
export LLM_PROVIDER=openai-compatible
export LLM_BASE_URL=http://127.0.0.1:8000/v1
export LLM_MODEL=Qwen/Qwen3.5-27B
export LLM_ENABLE_THINKING=false
```

Se voce rodar o orquestrador via `docker compose`, o compose ja aponta para
`http://host.docker.internal:8000/v1` por padrao quando `LLM_PROVIDER` sair de
`mock`.

Observacoes praticas para a Fase 3:

- Comece com respostas curtas (`LLM_MAX_TOKENS=256`) para reduzir latencia.
- Mantenha `LLM_ENABLE_THINKING=false` neste pipeline inicial.
- O provider real so depende de uma API OpenAI-compatible; o servidor do modelo
  pode ser trocado sem alterar o codigo Go.

## Classificador Python (Fase 3)

O `python-ml` agora suporta dois modos:

- `CLASSIFIER_MODE=heuristic`: leve, sem dependencias extras, bom para smoke e desenvolvimento.
- `CLASSIFIER_MODE=transformers`: carrega `monologg/bert-base-cased-goemotions-original` ou outro modelo compatível.

Para ativar o modo com modelo real em ambiente local:

```bash
cd python-ml
.venv/bin/python -m pip install -e '.[ml]'
CLASSIFIER_MODE=transformers CLASSIFIER_DEVICE=cuda:0 .venv/bin/uvicorn app.main:app --host 0.0.0.0 --port 8090
```

No orquestrador:

- `CLASSIFIER_CACHE_ENABLED=true` ativa cache Redis das classificacoes.
- `CLASSIFIER_FALLBACK_NEUTRAL=true` faz fallback para vetor neutro se o Python falhar em runtime.

## Endpoints

- Orchestrator: `GET /health`, `GET /ready`, `POST /api/v1/interact`,
  `POST /api/v1/interact/stream`,
  `POST|GET /api/v1/agents/`, `GET|PUT|DELETE /api/v1/agents/{agentID}/`,
  `GET /api/v1/agents/{agentID}/state`, `GET /api/v1/agents/{agentID}/history`
- Python ML: `GET /health`, `POST /classify-emotion`
- Emotion Engine: gRPC `emotion_engine.v1.EmotionEngineService`
  (`TransitionState`, `ComputeEmotionVector`, `FuseScores`, `EvaluatePromotion`,
  `ProcessInteraction`)

## Observacoes

- Os contratos Protobuf estao versionados em `v1` e ja reservam faixas de campos para evolucao.
- O codigo Go gerado a partir dos protos e versionado em `orchestrator/pkg/proto`.
- O `emotion-engine` Rust ja possui implementacao real e cobertura via `cargo test`,
  `cargo clippy` e `cargo bench`.
- O `orchestrator` Go ja possui API funcional com mocks, cobertura via `go test`
  e `golangci-lint`, e esta pronto para a integracao real da Fase 3.
