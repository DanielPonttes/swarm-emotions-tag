# Swarm Emotions

Monorepo multi-servico do EmotionRAG. A fundacao da Fase 0 esta concluida e o
`emotion-engine` em Rust ja entrou na Fase 1 com FSM, calculo vetorial, score
fusion, promocao de memoria e servidor gRPC funcionais.

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

## Endpoints

- Orchestrator: `GET /health`, `POST /api/v1/interact`
- Python ML: `GET /health`, `POST /classify-emotion`
- Emotion Engine: gRPC `emotion_engine.v1.EmotionEngineService`
  (`TransitionState`, `ComputeEmotionVector`, `FuseScores`, `EvaluatePromotion`,
  `ProcessInteraction`)

## Observacoes

- Os contratos Protobuf estao versionados em `v1` e ja reservam faixas de campos para evolucao.
- O codigo Go gerado a partir dos protos e versionado em `orchestrator/pkg/proto`.
- O `emotion-engine` Rust ja possui implementacao real e cobertura via `cargo test`,
  `cargo clippy` e `cargo bench`.
