# Swarm Emotions

Fundacao inicial do monorepo multi-servico descrito em `docs/fases/FASE_0_FUNDACAO.md`.
Nesta fase, os servicos existem como stubs operacionais com contratos versionados,
infra local via Docker e automacao basica de build, teste e CI.

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

## Endpoints de stubs

- Orchestrator: `GET /health`, `POST /api/v1/interact`
- Python ML: `GET /health`, `POST /classify-emotion`
- Emotion Engine: gRPC `emotion_engine.v1.EmotionEngineService`

## Observacoes

- Os contratos Protobuf estao versionados em `v1` e ja reservam faixas de campos para evolucao.
- O codigo Go gerado a partir dos protos e versionado em `orchestrator/pkg/proto`.
- A logica real dos servicos sera implementada a partir das proximas fases.
