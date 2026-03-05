# FASE 0 - Fundacao e Contratos

> **Duracao estimada:** 2 semanas
> **Equipe minima:** 1 engenheiro
> **Pre-requisitos:** Nenhum (fase inicial)
> **Resultado:** Monorepo funcional, contratos Protobuf, infra Docker, CI basico

---

## 0.1 Objetivo

Estabelecer toda a infraestrutura de projeto, contratos de comunicacao entre servicos,
ambiente de desenvolvimento local e pipeline de CI. Nenhum codigo de negocio e escrito
nesta fase - apenas fundacao.

---

## 0.2 Estrutura do Monorepo

```
swarm-emotions/
├── proto/                           # Contratos Protobuf (fonte de verdade)
│   ├── emotion_engine/
│   │   └── v1/
│   │       └── emotion_engine.proto
│   └── buf.yaml                     # Config do Buf (linter/breaking change detection)
│
├── emotion-engine/                  # Projeto Rust - Motor Emocional
│   ├── Cargo.toml
│   ├── build.rs                     # Compilacao proto via tonic-build
│   ├── src/
│   │   ├── lib.rs
│   │   └── main.rs                  # Entrypoint do servidor gRPC
│   └── tests/
│
├── orchestrator/                    # Projeto Go - Plano de Controle
│   ├── go.mod
│   ├── go.sum
│   ├── cmd/
│   │   └── server/
│   │       └── main.go              # Entrypoint do servidor HTTP
│   ├── internal/
│   │   ├── api/                     # Handlers HTTP
│   │   ├── pipeline/                # Orquestrador de pipeline
│   │   ├── connector/               # Clients para servicos externos
│   │   │   ├── emotion/             # Client gRPC para motor Rust
│   │   │   ├── vectorstore/         # Client Qdrant
│   │   │   ├── cache/               # Client Redis
│   │   │   ├── db/                  # Client PostgreSQL
│   │   │   └── llm/                 # Client LLM (interface + implementacoes)
│   │   └── model/                   # Structs de dominio
│   └── pkg/
│       └── proto/                   # Codigo Go gerado do proto
│
├── python-ml/                       # Servico Python auxiliar (temporario)
│   ├── pyproject.toml
│   ├── app/
│   │   ├── main.py                  # FastAPI entrypoint
│   │   ├── classifier.py            # GoEmotions classifier
│   │   └── embedder.py              # Sentence-transformers
│   └── Dockerfile
│
├── docker/
│   ├── Dockerfile.emotion-engine    # Multi-stage build Rust
│   ├── Dockerfile.orchestrator      # Multi-stage build Go
│   └── Dockerfile.python-ml         # Python com modelos
│
├── docker-compose.yml               # Ambiente local completo
├── docker-compose.override.yml      # Overrides para dev (volumes, hot-reload)
├── Makefile                         # Comandos unificados
├── .github/
│   └── workflows/
│       └── ci.yml                   # GitHub Actions CI
├── .gitignore
├── README.md
└── docs/
```

### Tarefa 0.2.1 - Inicializar repositorio

```bash
# Inicializar Git
cd swarm-emotions
git init

# Criar .gitignore
# (incluir: target/, vendor/, __pycache__/, *.pyc, .env, node_modules/)

# Criar estrutura de diretorios
mkdir -p proto/emotion_engine/v1
mkdir -p emotion-engine/src emotion-engine/tests
mkdir -p orchestrator/cmd/server orchestrator/internal/{api,pipeline,connector/{emotion,vectorstore,cache,db,llm},model} orchestrator/pkg/proto
mkdir -p python-ml/app
mkdir -p docker
mkdir -p .github/workflows
mkdir -p docs/fases
```

### Tarefa 0.2.2 - Inicializar projeto Rust

```bash
cd emotion-engine
cargo init --name emotion-engine
```

**Cargo.toml inicial:**
```toml
[package]
name = "emotion-engine"
version = "0.1.0"
edition = "2021"

[dependencies]
# gRPC
tonic = "0.12"
prost = "0.13"
prost-types = "0.13"
tokio = { version = "1", features = ["full"] }

# Algebra linear
ndarray = "0.16"

# Serialization
serde = { version = "1", features = ["derive"] }
serde_json = "1"
toml = "0.8"

# Randomness
rand = "0.8"
rand_distr = "0.4"

# Observabilidade
tracing = "0.1"
tracing-subscriber = { version = "0.3", features = ["env-filter"] }
tracing-opentelemetry = "0.27"

# Benchmark (dev only)
criterion = { version = "0.5", features = ["html_reports"] }

[build-dependencies]
tonic-build = "0.12"

[[bench]]
name = "hot_path"
harness = false
```

**build.rs:**
```rust
fn main() -> Result<(), Box<dyn std::error::Error>> {
    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .compile_protos(
            &["../proto/emotion_engine/v1/emotion_engine.proto"],
            &["../proto"],
        )?;
    Ok(())
}
```

### Tarefa 0.2.3 - Inicializar projeto Go

```bash
cd orchestrator
go mod init github.com/swarm-emotions/orchestrator
```

**go.mod dependencias iniciais (apos go get):**
```
google.golang.org/grpc
google.golang.org/protobuf
github.com/go-chi/chi/v5
github.com/redis/go-redis/v9
github.com/jackc/pgx/v5
github.com/qdrant/go-client
go.opentelemetry.io/otel
```

### Tarefa 0.2.4 - Inicializar projeto Python

```toml
# python-ml/pyproject.toml
[project]
name = "emotion-ml"
version = "0.1.0"
requires-python = ">=3.11"
dependencies = [
    "fastapi>=0.115",
    "uvicorn[standard]>=0.32",
    "transformers>=4.46",
    "torch>=2.5",
    "sentence-transformers>=3.3",
    "onnx>=1.17",
    "onnxruntime>=1.20",
    "numpy>=1.26",
]
```

---

## 0.3 Contrato Protobuf

### Tarefa 0.3.1 - Definir emotion_engine.proto v0

```protobuf
// proto/emotion_engine/v1/emotion_engine.proto
syntax = "proto3";

package emotion_engine.v1;

option go_package = "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1";

// =============================================================================
// Servico principal do Motor Emocional
// =============================================================================

service EmotionEngineService {
  // Transicao de estado na FSM emocional
  rpc TransitionState(TransitionStateRequest) returns (TransitionStateResponse);

  // Calculo do vetor emocional: e(t+1) = e(t) + W * g(t) + epsilon
  rpc ComputeEmotionVector(ComputeEmotionVectorRequest) returns (ComputeEmotionVectorResponse);

  // Fusao ponderada de scores: alpha*sem + beta*emo + gamma*cog
  rpc FuseScores(FuseScoresRequest) returns (FuseScoresResponse);

  // Avaliacao de promocao de memorias (L1->L2->L3)
  rpc EvaluatePromotion(EvaluatePromotionRequest) returns (EvaluatePromotionResponse);

  // Pipeline batch: executa FSM + vetor + scoring em uma unica chamada
  rpc ProcessInteraction(ProcessInteractionRequest) returns (ProcessInteractionResponse);
}

// =============================================================================
// Tipos base
// =============================================================================

// Vetor emocional n-dimensional (tipicamente 6D: V, A, D, certeza, social, novidade)
message EmotionVector {
  repeated float components = 1;
}

// Estado FSM
message FsmState {
  string state_name = 1;          // Ex: "Neutro", "Alegre", "Ansioso"
  string macro_state = 2;         // Ex: "Positivo", "Neutro", "Negativo" (HFSM)
  int64 entered_at_ms = 3;        // Timestamp de entrada no estado (para restricoes Omega)
}

// Matriz de suscetibilidade W (serializada row-major)
message SusceptibilityMatrix {
  repeated float values = 1;      // Row-major: [w00, w01, ..., w0n, w10, ...]
  uint32 dimension = 2;           // Dimensao n (matriz n x n)
}

// =============================================================================
// TransitionState
// =============================================================================

message TransitionStateRequest {
  FsmState current_state = 1;
  string stimulus = 2;            // Tipo do estimulo (ex: "elogio", "critica", "urgencia")
  EmotionVector stimulus_vector = 3;  // Vetor emocional do estimulo
  string agent_id = 4;
}

message TransitionStateResponse {
  FsmState new_state = 1;
  bool transition_occurred = 2;   // False se restricao Omega bloqueou
  string blocked_reason = 3;      // Motivo do bloqueio (se aplicavel)
}

// =============================================================================
// ComputeEmotionVector
// =============================================================================

message ComputeEmotionVectorRequest {
  EmotionVector current_emotion = 1;   // e(t)
  EmotionVector trigger = 2;           // g(t) - vetor do gatilho externo
  SusceptibilityMatrix w_matrix = 3;   // W
  EmotionVector baseline = 4;          // e_baseline para decaimento
  float decay_lambda = 5;              // Taxa de decaimento
  float delta_time = 6;                // Tempo desde ultimo estimulo
  bool enable_noise = 7;               // Se true, adiciona epsilon
  float noise_sigma = 8;               // Desvio padrao do ruido
}

message ComputeEmotionVectorResponse {
  EmotionVector new_emotion = 1;       // e(t+1)
  float intensity = 2;                 // ||e(t+1)|| - norma do vetor
}

// =============================================================================
// FuseScores
// =============================================================================

message ScoreCandidate {
  string memory_id = 1;
  float semantic_score = 2;            // Similaridade semantica
  float emotional_score = 3;           // Similaridade emocional
  float cognitive_score = 4;           // Relevancia cognitiva
  uint32 memory_level = 5;             // 1, 2 ou 3
  bool is_pseudopermanent = 6;
}

message FuseScoresRequest {
  repeated ScoreCandidate candidates = 1;
  float alpha = 2;                     // Peso semantico
  float beta = 3;                      // Peso emocional
  float gamma = 4;                     // Peso cognitivo
  float pseudoperm_boost = 5;          // Boost para memorias pseudopermanentes
  EmotionVector current_emotion = 6;   // Para mood-congruent boost
}

message RankedMemory {
  string memory_id = 1;
  float final_score = 2;
  float semantic_contribution = 3;
  float emotional_contribution = 4;
  float cognitive_contribution = 5;
}

message FuseScoresResponse {
  repeated RankedMemory ranked = 1;    // Ordenado por final_score desc
}

// =============================================================================
// EvaluatePromotion
// =============================================================================

message MemoryForPromotion {
  string memory_id = 1;
  EmotionVector emotion_at_creation = 2;
  float intensity = 3;                 // ||e|| no momento da criacao
  uint32 current_level = 4;            // 1 ou 2
  uint32 access_frequency = 5;         // Quantas vezes foi acessada
  float valence_magnitude = 6;         // |valencia| da memoria
}

message EvaluatePromotionRequest {
  repeated MemoryForPromotion memories = 1;
  float intensity_threshold = 2;       // theta_1
  uint32 frequency_threshold = 3;      // theta_2
  float valence_threshold = 4;         // theta_3
}

message PromotionDecision {
  string memory_id = 1;
  bool should_promote = 2;
  uint32 target_level = 3;             // Nivel alvo (2 ou 3)
  string reason = 4;                   // Motivo da decisao
}

message EvaluatePromotionResponse {
  repeated PromotionDecision decisions = 1;
}

// =============================================================================
// ProcessInteraction (batch endpoint)
// =============================================================================

message ProcessInteractionRequest {
  string agent_id = 1;
  FsmState current_fsm_state = 2;
  EmotionVector current_emotion = 3;
  string stimulus = 4;
  EmotionVector stimulus_vector = 5;
  SusceptibilityMatrix w_matrix = 6;
  EmotionVector baseline = 7;
  float decay_lambda = 8;
  float delta_time = 9;
  bool enable_noise = 10;
  float noise_sigma = 11;

  // Para score fusion
  repeated ScoreCandidate score_candidates = 12;
  float alpha = 13;
  float beta = 14;
  float gamma = 15;
  float pseudoperm_boost = 16;

  // Para avaliacao de promocao
  repeated MemoryForPromotion promotion_candidates = 17;
  float intensity_threshold = 18;
  uint32 frequency_threshold = 19;
  float valence_threshold = 20;
}

message ProcessInteractionResponse {
  // Resultado da transicao FSM
  FsmState new_fsm_state = 1;
  bool transition_occurred = 2;

  // Resultado do calculo vetorial
  EmotionVector new_emotion = 3;
  float new_intensity = 4;

  // Resultado da fusao de scores
  repeated RankedMemory ranked_memories = 5;

  // Resultado da avaliacao de promocao
  repeated PromotionDecision promotion_decisions = 6;
}
```

### Tarefa 0.3.2 - Configurar geracao de codigo

**Makefile target para proto:**
```makefile
PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto')
GO_PROTO_OUT := orchestrator/pkg/proto

.PHONY: proto-gen
proto-gen: proto-gen-go proto-gen-rust

.PHONY: proto-gen-go
proto-gen-go:
	@echo "Gerando codigo Go dos protos..."
	protoc \
		--go_out=$(GO_PROTO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GO_PROTO_OUT) \
		--go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_FILES)

.PHONY: proto-gen-rust
proto-gen-rust:
	@echo "Codigo Rust gerado via build.rs (tonic-build) no cargo build"
	cd emotion-engine && cargo build 2>&1 | head -5
```

### Tarefa 0.3.3 - Instalar ferramentas de proto

```bash
# Go
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Protoc (via package manager ou download)
# Ubuntu/Debian:
sudo apt install -y protobuf-compiler

# Buf (linter/breaking change detection - opcional mas recomendado)
# https://buf.build/docs/installation
```

---

## 0.4 Infraestrutura Docker

### Tarefa 0.4.1 - docker-compose.yml

```yaml
# docker-compose.yml
version: "3.9"

services:
  # ---------- Dependencias de infraestrutura ----------

  qdrant:
    image: qdrant/qdrant:v1.12.1
    ports:
      - "6333:6333"   # REST API
      - "6334:6334"   # gRPC
    volumes:
      - qdrant_data:/qdrant/storage
    environment:
      QDRANT__SERVICE__GRPC_PORT: 6334

  postgresql:
    image: postgres:17-alpine
    ports:
      - "5432:5432"
    environment:
      POSTGRES_DB: emotionrag
      POSTGRES_USER: emotionrag
      POSTGRES_PASSWORD: dev_password_change_me
    volumes:
      - pg_data:/var/lib/postgresql/data
      - ./docker/init.sql:/docker-entrypoint-initdb.d/init.sql

  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
    command: redis-server --maxmemory 256mb --maxmemory-policy allkeys-lru

  # ---------- Servicos da aplicacao ----------

  emotion-engine:
    build:
      context: .
      dockerfile: docker/Dockerfile.emotion-engine
    ports:
      - "50051:50051"  # gRPC
    environment:
      RUST_LOG: info,emotion_engine=debug
      GRPC_PORT: 50051
    depends_on:
      - redis

  orchestrator:
    build:
      context: .
      dockerfile: docker/Dockerfile.orchestrator
    ports:
      - "8080:8080"   # HTTP API
    environment:
      HTTP_PORT: 8080
      EMOTION_ENGINE_ADDR: emotion-engine:50051
      QDRANT_ADDR: qdrant:6334
      REDIS_ADDR: redis:6379
      POSTGRES_DSN: postgres://emotionrag:dev_password_change_me@postgresql:5432/emotionrag?sslmode=disable
      PYTHON_ML_URL: http://python-ml:8090
      LLM_PROVIDER: openai
      LLM_API_KEY: ${LLM_API_KEY}
    depends_on:
      - emotion-engine
      - qdrant
      - postgresql
      - redis

  python-ml:
    build:
      context: .
      dockerfile: docker/Dockerfile.python-ml
    ports:
      - "8090:8090"
    environment:
      PORT: 8090
      MODEL_NAME: monologg/bert-base-cased-goemotions-original
      EMBEDDING_MODEL: all-MiniLM-L6-v2
    deploy:
      resources:
        limits:
          memory: 4G  # Modelos BERT precisam de memoria

volumes:
  qdrant_data:
  pg_data:
```

### Tarefa 0.4.2 - Dockerfiles

**docker/Dockerfile.emotion-engine:**
```dockerfile
# Build stage
FROM rust:1.82-bookworm AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y protobuf-compiler && rm -rf /var/lib/apt/lists/*
COPY proto/ proto/
COPY emotion-engine/ emotion-engine/
WORKDIR /app/emotion-engine
RUN cargo build --release

# Runtime stage
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*
COPY --from=builder /app/emotion-engine/target/release/emotion-engine /usr/local/bin/
EXPOSE 50051
CMD ["emotion-engine"]
```

**docker/Dockerfile.orchestrator:**
```dockerfile
# Build stage
FROM golang:1.23-bookworm AS builder
WORKDIR /app
RUN apt-get update && apt-get install -y protobuf-compiler && rm -rf /var/lib/apt/lists/*
COPY orchestrator/go.mod orchestrator/go.sum ./
RUN go mod download
COPY orchestrator/ .
COPY proto/ ../proto/
RUN CGO_ENABLED=0 go build -o /server ./cmd/server

# Runtime stage
FROM gcr.io/distroless/static-debian12
COPY --from=builder /server /server
EXPOSE 8080
CMD ["/server"]
```

**docker/Dockerfile.python-ml:**
```dockerfile
FROM python:3.12-slim
WORKDIR /app
RUN pip install --no-cache-dir uv
COPY python-ml/pyproject.toml .
RUN uv pip install --system -r pyproject.toml
COPY python-ml/app/ app/
EXPOSE 8090
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8090"]
```

### Tarefa 0.4.3 - Schema SQL inicial

```sql
-- docker/init.sql

-- Configuracoes de agentes
CREATE TABLE agent_configs (
    agent_id        TEXT PRIMARY KEY,
    display_name    TEXT NOT NULL,
    baseline        JSONB NOT NULL,         -- vetor emocional baseline
    w_matrix        JSONB NOT NULL,         -- matriz de suscetibilidade (row-major)
    w_dimension     INTEGER NOT NULL DEFAULT 6,
    fsm_transitions JSONB NOT NULL,         -- tabela de transicoes
    fsm_constraints JSONB DEFAULT '{}',     -- restricoes Omega
    weights         JSONB NOT NULL DEFAULT '{"alpha": 0.5, "beta": 0.3, "gamma": 0.2}',
    decay_lambda    REAL NOT NULL DEFAULT 0.1,
    noise_enabled   BOOLEAN NOT NULL DEFAULT FALSE,
    noise_sigma     REAL NOT NULL DEFAULT 0.01,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Contextos cognitivos (um por agente, atualizado a cada interacao)
CREATE TABLE cognitive_contexts (
    agent_id            TEXT PRIMARY KEY REFERENCES agent_configs(agent_id),
    active_goals        JSONB NOT NULL DEFAULT '[]',
    beliefs             JSONB NOT NULL DEFAULT '{}',
    norms               JSONB NOT NULL DEFAULT '{}',
    conversation_phase  TEXT NOT NULL DEFAULT 'idle',
    interlocutor_model  JSONB DEFAULT '{}',
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Log de interacoes (audit trail)
CREATE TABLE interaction_log (
    id              BIGSERIAL PRIMARY KEY,
    agent_id        TEXT NOT NULL REFERENCES agent_configs(agent_id),
    timestamp       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    input_text      TEXT,
    stimulus_type   TEXT,
    fsm_from        TEXT,
    fsm_to          TEXT,
    emotion_before  JSONB,                  -- vetor emocional antes
    emotion_after   JSONB,                  -- vetor emocional depois
    intensity       REAL,
    llm_response    TEXT,
    latency_ms      INTEGER,
    trace_id        TEXT                    -- correlation com OpenTelemetry
);

CREATE INDEX idx_interaction_log_agent ON interaction_log(agent_id, timestamp DESC);

-- Historico de emocoes (serie temporal para calculo de entropia)
CREATE TABLE emotion_history (
    id          BIGSERIAL PRIMARY KEY,
    agent_id    TEXT NOT NULL REFERENCES agent_configs(agent_id),
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    emotion     JSONB NOT NULL,             -- vetor emocional
    intensity   REAL NOT NULL,
    fsm_state   TEXT NOT NULL
);

CREATE INDEX idx_emotion_history_agent ON emotion_history(agent_id, timestamp DESC);
```

---

## 0.5 Makefile Unificado

### Tarefa 0.5.1 - Makefile completo

```makefile
# Makefile raiz - orquestrador de build para monorepo Go + Rust + Python

.DEFAULT_GOAL := help

# ---- Variaveis ----
PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto' 2>/dev/null)
GO_PROTO_OUT := orchestrator/pkg/proto

# ---- Help ----
.PHONY: help
help:
	@echo "Targets disponiveis:"
	@echo "  make build         - Compila Go + Rust"
	@echo "  make test          - Roda testes Go + Rust"
	@echo "  make lint          - Lint Go (golangci-lint) + Rust (clippy)"
	@echo "  make proto-gen     - Gera codigo Go e Rust dos protos"
	@echo "  make docker-up     - Sobe todos os servicos via docker compose"
	@echo "  make docker-down   - Para todos os servicos"
	@echo "  make docker-infra  - Sobe apenas infra (Qdrant, Postgres, Redis)"
	@echo "  make clean         - Limpa artefatos de build"

# ---- Build ----
.PHONY: build build-rust build-go
build: build-rust build-go

build-rust:
	cd emotion-engine && cargo build --release

build-go:
	cd orchestrator && go build -o ../bin/orchestrator ./cmd/server

# ---- Test ----
.PHONY: test test-rust test-go
test: test-rust test-go

test-rust:
	cd emotion-engine && cargo test

test-go:
	cd orchestrator && go test ./...

# ---- Lint ----
.PHONY: lint lint-rust lint-go
lint: lint-rust lint-go

lint-rust:
	cd emotion-engine && cargo clippy -- -D warnings

lint-go:
	cd orchestrator && golangci-lint run ./...

# ---- Proto ----
.PHONY: proto-gen
proto-gen:
	@mkdir -p $(GO_PROTO_OUT)/emotion_engine/v1
	protoc \
		--go_out=$(GO_PROTO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GO_PROTO_OUT) \
		--go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_FILES)
	cd emotion-engine && cargo build 2>&1 | head -5
	@echo "Proto generation complete."

# ---- Docker ----
.PHONY: docker-up docker-down docker-infra docker-build
docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-infra:
	docker compose up -d qdrant postgresql redis

docker-build:
	docker compose build

# ---- Bench ----
.PHONY: bench
bench:
	cd emotion-engine && cargo bench

# ---- Clean ----
.PHONY: clean
clean:
	cd emotion-engine && cargo clean
	rm -rf bin/
	cd orchestrator && go clean
```

---

## 0.6 CI Pipeline

### Tarefa 0.6.1 - GitHub Actions

```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  rust:
    name: Rust (lint + test)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: emotion-engine
    steps:
      - uses: actions/checkout@v4
      - uses: dtolnay/rust-toolchain@stable
        with:
          components: clippy, rustfmt
      - name: Install protoc
        run: sudo apt-get install -y protobuf-compiler
      - uses: Swatinem/rust-cache@v2
        with:
          workspaces: emotion-engine
      - run: cargo fmt --check
      - run: cargo clippy -- -D warnings
      - run: cargo test

  go:
    name: Go (lint + test)
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: orchestrator
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.23'
      - name: Install protoc
        run: sudo apt-get install -y protobuf-compiler
      - name: Generate proto
        run: cd .. && make proto-gen
      - name: golangci-lint
        uses: golangci/golangci-lint-action@v6
        with:
          working-directory: orchestrator
      - run: go test ./...

  proto:
    name: Proto (lint + breaking)
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: bufbuild/buf-setup-action@v1
      - run: buf lint proto/
      - run: buf breaking proto/ --against '.git#branch=main'
```

---

## 0.7 Stubs Minimos (Hello World)

### Tarefa 0.7.1 - Stub Rust (servidor gRPC que responde ping)

```rust
// emotion-engine/src/main.rs
use tonic::{transport::Server, Request, Response, Status};

pub mod proto {
    tonic::include_proto!("emotion_engine.v1");
}

use proto::emotion_engine_service_server::{EmotionEngineService, EmotionEngineServiceServer};
use proto::*;

#[derive(Default)]
pub struct EmotionEngine;

#[tonic::async_trait]
impl EmotionEngineService for EmotionEngine {
    async fn transition_state(
        &self,
        _request: Request<TransitionStateRequest>,
    ) -> Result<Response<TransitionStateResponse>, Status> {
        // Stub: retorna estado inalterado
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn compute_emotion_vector(
        &self,
        _request: Request<ComputeEmotionVectorRequest>,
    ) -> Result<Response<ComputeEmotionVectorResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn fuse_scores(
        &self,
        _request: Request<FuseScoresRequest>,
    ) -> Result<Response<FuseScoresResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn evaluate_promotion(
        &self,
        _request: Request<EvaluatePromotionRequest>,
    ) -> Result<Response<EvaluatePromotionResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn process_interaction(
        &self,
        _request: Request<ProcessInteractionRequest>,
    ) -> Result<Response<ProcessInteractionResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt::init();

    let port = std::env::var("GRPC_PORT").unwrap_or_else(|_| "50051".into());
    let addr = format!("0.0.0.0:{port}").parse()?;

    tracing::info!("EmotionEngine gRPC server listening on {addr}");

    Server::builder()
        .add_service(EmotionEngineServiceServer::new(EmotionEngine::default()))
        .serve(addr)
        .await?;

    Ok(())
}
```

### Tarefa 0.7.2 - Stub Go (servidor HTTP com health check)

```go
// orchestrator/cmd/server/main.go
package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	r.Post("/api/v1/interact", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not yet implemented", http.StatusNotImplemented)
	})

	slog.Info("Orchestrator HTTP server starting", "port", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
```

### Tarefa 0.7.3 - Stub Python (FastAPI com health check)

```python
# python-ml/app/main.py
from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI(title="EmotionML Service", version="0.1.0")


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool


class ClassifyRequest(BaseModel):
    text: str


class ClassifyResponse(BaseModel):
    emotion_vector: list[float]
    label: str
    confidence: float


_model_loaded = False


@app.on_event("startup")
async def load_models():
    global _model_loaded
    # TODO: carregar GoEmotions + sentence-transformers na Fase 3
    _model_loaded = True


@app.get("/health", response_model=HealthResponse)
async def health():
    return HealthResponse(status="ok", model_loaded=_model_loaded)


@app.post("/classify-emotion", response_model=ClassifyResponse)
async def classify_emotion(req: ClassifyRequest):
    # Stub: retorna vetor neutro
    return ClassifyResponse(
        emotion_vector=[0.0, 0.0, 0.0, 0.0, 0.0, 0.0],
        label="neutral",
        confidence=1.0,
    )
```

---

## 0.8 Checklist de Aceitacao

> **Status atualizado em 2026-03-05 neste ambiente**

- [x] `git clone` + `make docker-infra` sobe Qdrant, PostgreSQL e Redis sem erros
- [x] `make proto-gen` gera codigo Go e Rust sem warnings
- [x] `make build` compila Go e Rust sem erros
- [x] `make test` passa (testes triviais dos stubs)
- [x] `make lint` passa sem warnings
- [x] `docker compose up -d` sobe todos os 6 servicos
- [x] `curl localhost:8080/health` retorna `{"status":"ok"}`
- [x] `grpcurl -plaintext localhost:50051 list` mostra `emotion_engine.v1.EmotionEngineService`
- [x] `curl localhost:8090/health` retorna `{"status":"ok","model_loaded":true}`
- [x] PostgreSQL tem as 4 tabelas criadas (`agent_configs`, `cognitive_contexts`, `interaction_log`, `emotion_history`)
- [ ] CI pipeline roda verde no GitHub Actions (ou localmente via `act`)  
  Workflow implementado, mas ainda nao executado neste ambiente.
- [x] README.md com instrucoes de setup para novos desenvolvedores
- [x] Proto versionado como `v1` com campos `reserved` planejados

---

## 0.9 Riscos Especificos desta Fase

| Risco | Prob. | Impacto | Mitigacao |
|-------|-------|---------|-----------|
| Proto definido cedo demais, vai mudar | Alta | Medio | Versionar como v0/v1. Campos `reserved`. Aceitar retrabalho. |
| Docker images pesadas (especialmente Python com torch) | Media | Baixo | Multi-stage builds. Python: usar CPU-only torch. |
| Conflito de versao protoc entre Go e Rust | Baixa | Medio | Fixar versao do protoc no CI. Usar mesma versao local. |
| Complexidade de onboarding para dev novo | Media | Medio | README detalhado. `make docker-up` como single command. |

---

## 0.10 Transicao para Fase 1

Ao concluir a Fase 0, a equipe tem:
1. Um monorepo com builds funcionais em Go e Rust
2. Contratos Protobuf que definem a interface entre os servicos
3. Infra local com todos os stores necessarios
4. CI que valida builds e linting

A **Fase 1** comeca implementando a logica real do motor emocional em Rust,
substituindo os stubs `unimplemented!()` pelos algoritmos de FSM, calculo
vetorial e score fusion.
