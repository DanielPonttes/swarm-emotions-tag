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

Tambem existe o caminho validado com `ollama-native`, sem camada OpenAI
compatível intermediaria:

```bash
OLLAMA_HOST=0.0.0.0:11434 ollama serve
ollama pull qwen3.5:27b
make orchestrator-local-ollama
```

Esse alvo sobe o `orchestrator` no host, em `:8080`, usando:

```bash
LLM_PROVIDER=ollama-native
LLM_BASE_URL=http://127.0.0.1:11434
LLM_MODEL=Qwen/Qwen3.5-27B
```

Para validar o caminho completo `HTTP -> Go -> Python -> Rust -> LLM` com
dependencias reais, existe um smoke automatizado:

```bash
make phase3-smoke-qwen-local
```

Esse alvo roda o `orchestrator` localmente em `:18080`, faz warmup do
`qwen3.5:27b`, executa um `POST /api/v1/interact` real, verifica `trace_id`
nos logs do `python-ml` e do `emotion-engine`, e confirma persistencia
deterministica em Redis e Postgres.

Tambem existe uma suite multi-turno com 20 interacoes no mesmo agente:

```bash
make phase3-multiturn-qwen-local
```

Esse alvo valida a evolucao de estado `curious -> frustrated -> empathetic ->
joyful -> worried ...`, respeita os `min_duration_ms` da FSM com atrasos
automaticos entre turnos, confere o `/state`, compara o `/history` inteiro e
verifica a persistencia final em Redis e Postgres.

Para fechar a regressao deterministica de estados, existe um alvo separado:

```bash
make phase3-determinism-qwen-local
```

Esse alvo executa o mesmo roteiro de 20 turnos duas vezes, em agentes limpos e
com exatamente os mesmos inputs, e falha se a sequencia de estados diferir entre
as duas execucoes.

Tambem existe uma matriz comportamental para cobrir rotas adicionais da FSM:

```bash
make phase3-behavioral-qwen-local
```

Esse alvo valida cenarios isolados como `urgency -> anxious`,
`mild_criticism -> empathetic`, `severe_criticism -> worried`,
`curious -> joyful` via `success`, `worried -> empathetic` via `empathy`,
`frustrated -> calm` via `resolution`, `calm -> neutral` via `boredom` e
`empathetic -> anxious` via `user_frustration`.

Para validar o mesmo pipeline usando `Qwen/Qwen3.5-27B` tambem como
classificador de emocao via Ollama local:

```bash
make phase3-qwen-emotions-local
```

Esse alvo rebuilda o `python-ml`, sobe o servico em `CLASSIFIER_MODE=ollama`,
valida `POST /classify-emotion` diretamente contra o Qwen local e depois roda a
matriz comportamental completa com Qwen tanto na geracao quanto na classificacao
emocional.

Para validar o mesmo pipeline com o classifier Python real em modo
`transformers`, existe um alvo dedicado:

```bash
make phase3-transformers-qwen-local
```

Esse alvo rebuilda o `python-ml` com `PYTHON_ML_INSTALL_EXTRAS=ml`,
aguarda `model_loaded=true`, valida `POST /classify-emotion` com textos variados
e depois roda a matriz comportamental completa com Qwen local.

Para fechar a meta de latencia sem LLM da Fase 3, existe um alvo separado em
modo totalmente mockado:

```bash
make phase3-latency-mock-local
```

Esse alvo sobe apenas o `orchestrator` local em `USE_MOCK_CONNECTORS=true` com
`LLM_PROVIDER=mock`, roda um smoke request e depois executa o `cmd/loadtest`
contra a API HTTP validando `avg`, `p95` e `p99` abaixo dos thresholds locais.
Ele nao depende de Docker nem de Ollama e serve para medir o overhead do plano
de controle sem o custo de Python, Rust, Qdrant ou LLM real.

Observacao importante: neste host, o caminho validado foi `Ollama no host` +
`orchestrator no host` + dependencias reais em Docker (`emotion-engine`,
`python-ml`, Redis, Postgres e Qdrant). Rodar o `orchestrator` no compose e o
Ollama no host ainda depende de conectividade da bridge Docker ate a porta
`11434`.

Observacoes praticas para a Fase 3:

- Comece com respostas curtas (`LLM_MAX_TOKENS=256`) para reduzir latencia.
- Mantenha `LLM_ENABLE_THINKING=false` neste pipeline inicial.
- O provider real pode ser tanto `openai-compatible` quanto `ollama-native`.

## Classificador Python (Fase 3)

O `python-ml` agora suporta dois modos:

- `CLASSIFIER_MODE=heuristic`: leve, sem dependencias extras, bom para smoke e desenvolvimento.
- `CLASSIFIER_MODE=transformers`: carrega `monologg/bert-base-cased-goemotions-original` ou outro modelo compatível.
- `CLASSIFIER_MODE=ollama`: usa um modelo servido por Ollama local, como `Qwen/Qwen3.5-27B`, e converte os labels retornados para o vetor emocional 6D atual.

Para ativar o modo com modelo real em ambiente local:

```bash
cd python-ml
.venv/bin/python -m pip install -e '.[ml]'
CLASSIFIER_MODE=transformers CLASSIFIER_DEVICE=cuda:0 .venv/bin/uvicorn app.main:app --host 0.0.0.0 --port 8090
```

Para ativar o classificador por Ollama/Qwen no host:

```bash
cd python-ml
CLASSIFIER_MODE=ollama \
CLASSIFIER_MODEL_NAME=Qwen/Qwen3.5-27B \
CLASSIFIER_OLLAMA_BASE_URL=http://127.0.0.1:11434 \
.venv/bin/uvicorn app.main:app --host 0.0.0.0 --port 8090
```

No `docker compose`, o `python-ml` passa a usar
`CLASSIFIER_OLLAMA_BASE_URL=http://host.docker.internal:11434` por padrao,
permitindo que o container fale com o Ollama rodando no host.

No orquestrador:

- `CLASSIFIER_CACHE_ENABLED=true` ativa cache Redis das classificacoes.
- `CLASSIFIER_FALLBACK_NEUTRAL=true` faz fallback para vetor neutro se o Python falhar em runtime.

## Preparacao para GPU / RTX 5090

O host atual ja expõe a GPU via `nvidia-smi`, mas a venv local do `python-ml`
nao vem com `torch` e `transformers` por padrao. Para preparar a venv com os
extras de ML:

```bash
make python-ml-setup-venv
```

Esse alvo cria ou reutiliza `python-ml/.venv`, instala `.[ml]` e imprime o
runtime detectado (`torch_version`, `cuda_available`, `cuda_devices`). Se voce
precisar usar um indice especifico para as wheels de GPU, exporte antes:

```bash
export PIP_INDEX_URL=...
export PIP_EXTRA_INDEX_URL=...
make python-ml-setup-venv
```

O `/health` do `python-ml` agora tambem expõe `classifier_device`,
`classifier_batch_size` e o bloco `runtime`, o que facilita confirmar se o
servico realmente subiu em `cuda:0`.

Para uma rodada longa de benchmark/soak do classifier direto na GPU:

```bash
CLASSIFIER_DEVICE=cuda:0 \
CLASSIFIER_BATCH_SIZE=64 \
PYTHON_ML_BENCH_DURATION_SEC=1800 \
make python-ml-gpu-long-run
```

Por padrao esse fluxo:

- prepara a venv do `python-ml`
- carrega `monologg/bert-base-cased-goemotions-original` em `transformers`
- exige uma GPU contendo `RTX 5090`
- roda benchmark em lote por 30 minutos
- grava um JSON em `artifacts/python-ml-gpu/` com throughput, `p95/p99`,
  latencia media por batch/item e estatisticas de memoria CUDA

Se quiser usar um corpus proprio em vez do conjunto embutido:

```bash
PYTHON_ML_BENCH_TEXTS_FILE=/caminho/textos.txt make python-ml-gpu-long-run
```

Cada linha nao vazia do arquivo vira uma amostra do benchmark.

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
- No `docker compose`, `orchestrator` e `emotion-engine` agora se conectam por Unix domain socket compartilhado em `/var/run/emotion-engine/engine.sock`; o TCP `50051` foi mantido para desenvolvimento no host.
- O `request_id` HTTP do orquestrador agora aparece como `trace_id` nos logs HTTP do proprio servico Go e e propagado ao `emotion-engine` via metadata gRPC `x-trace-id`; os logs do servico Rust passam a incluir `trace_id` e `traceparent` quando presentes.
- O mesmo `request_id` tambem e propagado ao `python-ml` via header HTTP `x-trace-id`, e o log HTTP do FastAPI passa a registrar esse valor em `/classify-emotion`, fechando a correlacao basica Go -> Rust -> Python.
- O `emotion-engine` Rust ja possui implementacao real e cobertura via `cargo test`,
  `cargo clippy` e `cargo bench`.
- O `orchestrator` Go ja possui API funcional com mocks, cobertura via `go test`
  e `golangci-lint`, e esta pronto para a integracao real da Fase 3.
