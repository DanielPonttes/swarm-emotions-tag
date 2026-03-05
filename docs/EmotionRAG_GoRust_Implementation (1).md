# EmotionRAG: Arquitetura de Implementação em Go + Rust

> **Documento técnico de viabilidade e design** — Proposta de implementação da arquitetura EmotionRAG usando Go como camada de orquestração e Rust como motor computacional, com análise detalhada de integração, trade-offs, dependências e estratégia de deployment.

---

## 1. Tese Central: Por que Go + Rust?

A arquitetura EmotionRAG exige duas capacidades simultâneas que raramente coexistem bem em uma única linguagem: **computação numérica de alta performance** (operações vetoriais, FSM, ranking) e **orquestração concorrente de I/O intensivo** (chamadas a LLMs, stores vetoriais, comunicação entre agentes, APIs HTTP/gRPC).

Go e Rust ocupam lados complementares desse espectro. Rust oferece controle de memória em nível de hardware, zero-cost abstractions e SIMD para o hot-path numérico. Go oferece goroutines, channels, e um ecossistema maduro de rede/API para o plano de controle. Juntas, formam uma stack onde nenhuma camada está "forçada" a fazer algo fora do seu ponto ótimo.

A alternativa mais óbvia — Python — seria mais rápida de prototipar, mas colapsaria em performance no hot-path (cálculos vetoriais por interação, FSM em cada turno) e exigiria workarounds pesados (Cython, multiprocessing) para escalar. A alternativa Rust-only seria viável mas ergonomicamente cara na camada de rede (async Rust com Tokio funciona, mas a complexidade de lifetime + async + networking é substancialmente maior que Go para o mesmo resultado). Go-only funcionaria na orquestração mas sofreria no motor numérico — `math` nativo do Go não compete com `ndarray` ou SIMD manual em Rust para operações vetoriais em batch.

---

## 2. Mapa de Responsabilidades

### 2.1 Fronteira de Responsabilidade

```
┌─────────────────────────────────────────────────────────────────────┐
│                      PLANO DE CONTROLE (Go)                         │
│                                                                     │
│  ┌──────────┐  ┌──────────────┐  ┌────────────┐  ┌─────────────┐  │
│  │ API      │  │ Orquestrador │  │ Agent      │  │ Observabi-  │  │
│  │ Gateway  │  │ de Pipeline  │  │ Manager    │  │ lidade      │  │
│  │ (HTTP/   │  │ (sequencia   │  │ (ciclo de  │  │ (métricas,  │  │
│  │  gRPC)   │  │  os 8 steps) │  │  vida dos  │  │  logs,      │  │
│  │          │  │              │  │  agentes)  │  │  tracing)   │  │
│  └────┬─────┘  └──────┬───────┘  └─────┬──────┘  └─────────────┘  │
│       │               │                │                            │
│       └───────────────┼────────────────┘                            │
│                       │                                             │
│              ┌────────▼────────┐                                    │
│              │  Connector Hub  │ ← chamadas a LLMs, stores,        │
│              │  (goroutines    │   serviços externos em paralelo    │
│              │   paralelas)    │                                    │
│              └────────┬────────┘                                    │
│                       │                                             │
├───────────────────────┼─ ─ ─ ─ FFI (cgo) ou gRPC ─ ─ ─ ─ ─────────┤
│                       │                                             │
│              ┌────────▼────────┐                                    │
│              │  MOTOR EMOCIONAL│                                    │
│              │  (Rust Library) │                                    │
│              │                 │                                    │
│  ┌───────────┴───────────────────────────────────────────────┐     │
│  │                                                           │     │
│  │  ┌─────────────┐  ┌──────────────┐  ┌─────────────────┐  │     │
│  │  │ FSM/HFSM    │  │ VectorEngine │  │ MemoryPromoter  │  │     │
│  │  │ Engine      │  │ (sim cosseno,│  │ (promoção,      │  │     │
│  │  │ (transições,│  │  fusão de    │  │  decaimento,    │  │     │
│  │  │  restrições,│  │  scores,     │  │  garbage        │  │     │
│  │  │  HFSM)      │  │  ranking)    │  │  collection)    │  │     │
│  │  └─────────────┘  └──────────────┘  └─────────────────┘  │     │
│  │                                                           │     │
│  │  ┌─────────────────────┐  ┌────────────────────────────┐  │     │
│  │  │ SusceptibilityMatrix│  │ EmotionClassifier          │  │     │
│  │  │ (W × g(t) + ε,     │  │ (ONNX Runtime inference    │  │     │
│  │  │  personalidade)     │  │  para extração emocional)  │  │     │
│  │  └─────────────────────┘  └────────────────────────────┘  │     │
│  │                                                           │     │
│  │              PLANO COMPUTACIONAL (Rust)                    │     │
│  └───────────────────────────────────────────────────────────┘     │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.2 Atribuição Detalhada por Componente EmotionRAG

| Componente (do doc. arquitetural) | Linguagem | Justificativa |
|----------------------------------|-----------|---------------|
| **Percepção — parsing de input** | Go | I/O bound: receber HTTP, deserializar JSON, extrair texto |
| **Percepção — extração de emoção** | Rust (ONNX) | CPU bound: inference de modelo classificador |
| **FSM/HFSM Engine** | Rust | Hot-path determinístico; enums + pattern matching nativos |
| **Cálculo de $\vec{e}(t+1) = \vec{e}(t) + \mathbf{W} \times \vec{g}(t) + \epsilon$** | Rust | Álgebra linear pura; SIMD-friendly |
| **Decaimento temporal exponencial** | Rust | Batch vectorizável sobre todas as memórias |
| **Queries aos stores vetoriais** | Go | I/O bound: chamadas gRPC/HTTP a Qdrant/Milvus |
| **Fusão de rankings (score triplo)** | Rust | CPU bound: sorting + weighted combination |
| **Construção do prompt cognitivo** | Go | String manipulation + template rendering |
| **Chamada ao LLM** | Go | I/O bound: HTTP streaming para API do LLM |
| **Pós-processamento + promoção de memória** | Rust (lógica) + Go (persistência) | Decisão em Rust; escrita no store via Go |
| **Comunicação entre agentes** | Go | Channels nativos, gRPC, pub/sub |
| **Gerenciamento de ciclo de vida** | Go | Configuração, health checks, graceful shutdown |
| **Métricas e observabilidade** | Go | OpenTelemetry, Prometheus, logging estruturado |

---

## 3. Estratégias de Integração Go ↔ Rust

Esta é a decisão arquitetural mais consequente da implementação. Existem três caminhos viáveis, cada um com trade-offs distintos.

### 3.1 Opção A — FFI via C ABI (cgo)

Rust compila como biblioteca estática (`.a`) ou dinâmica (`.so`), exportando funções com `extern "C"` e `#[no_mangle]`. Go chama via `cgo` com `// #cgo LDFLAGS` directives.

**Interface típica:**

```
Rust expõe:
  emotion_fsm_transition(state, stimulus, env) → new_state
  emotion_vector_compute(e_prev, W, trigger, noise) → e_new
  memory_score_fuse(semantic[], emotional[], cognitive[], weights) → ranked[]
  emotion_classify_onnx(text_embedding, model_ptr) → emotion_vector
  memory_promote_evaluate(memories[], threshold) → promoted_ids[]

Go chama:
  resultado := C.emotion_fsm_transition(...)
```

**Vantagens:** Latência mínima (~200ns por crossing), sem serialização, deployment como binário único.

**Desvantagens:** `cgo` quebra otimizações do scheduler Go (goroutines que chamam C ficam presas a threads OS), complica cross-compilation, debugging entre linguagens é doloroso, e segfaults no lado Rust se propagam como crashes no processo Go inteiro.

**Mitigação:** Agrupar operações em chamadas batch (ex: passar um array de 50 memórias para scoring de uma vez, não uma por uma) para amortizar o overhead do crossing.

**Quando escolher:** Quando latência sub-milissegundo no hot-path é requisito absoluto e o time domina ambas as linguagens.

### 3.2 Opção B — Processos Separados com gRPC (Recomendada)

Rust roda como um serviço independente (microserviço) expondo gRPC. Go chama como qualquer outro serviço de rede.

**Interface típica:**

```protobuf
service EmotionEngine {
  rpc TransitionState(TransitionRequest) returns (EmotionState);
  rpc ComputeEmotionVector(VectorRequest) returns (EmotionVector);
  rpc FuseScores(FuseRequest) returns (RankedResults);
  rpc ClassifyEmotion(TextEmbedding) returns (EmotionVector);
  rpc EvaluatePromotion(MemoryBatch) returns (PromotionDecisions);
  
  // Batch endpoint para pipeline completo
  rpc ProcessInteraction(InteractionRequest) returns (InteractionResult);
}
```

**Vantagens:** Isolamento de falhas (crash no Rust não derruba o Go), deployment independente (escalar o motor Rust separadamente), debugging e profiling independentes, cross-compilation trivial, e o Go mantém todas as otimizações de goroutines sem `cgo`.

**Desvantagens:** Latência de rede (~0.5-2ms por chamada local via Unix socket, ~1-5ms via TCP localhost), overhead de serialização Protobuf. Para um pipeline que faz 3-5 chamadas ao motor Rust por interação, isso soma 5-15ms — geralmente aceitável quando a chamada ao LLM leva 500-3000ms.

**Mitigação:** Usar Unix domain sockets em vez de TCP, e projetar um endpoint batch `ProcessInteraction` que execute todo o pipeline Rust (FSM + vetor + scoring + promoção) em uma única chamada, reduzindo roundtrips.

**Quando escolher:** Para a maioria dos casos. A perda de latência é irrelevante frente ao bottleneck do LLM, e os ganhos de operabilidade são enormes.

### 3.3 Opção C — WebAssembly (WASM) como Intermediário

Rust compila para WASM, Go executa via runtime WASM (ex: `wazero`, que é puro Go — sem cgo).

**Vantagens:** Sem cgo, sem processos separados, sandbox de segurança, portabilidade.

**Desvantagens:** Performance WASM é ~60-80% de nativo (sem SIMD em muitos runtimes), limitações de memória, ecossistema ONNX em WASM é imaturo, e a complexidade de debugging é alta.

**Quando escolher:** Quando o deployment exige binário único sem dependências e a performance ~70% do nativo é aceitável. Viável para prova de conceito, menos ideal para produção com carga pesada.

### 3.4 Decisão Recomendada

**Opção B (gRPC)** para produção, com **Opção A (FFI)** como otimização futura do hot-path se profiling indicar que a latência gRPC é relevante. Na prática, com o LLM como bottleneck dominante (ordens de magnitude mais lento que qualquer cálculo vetorial), a Opção B oferece o melhor equilíbrio entre performance, operabilidade e velocidade de desenvolvimento.

---

## 4. Dependências e Ecossistema por Linguagem

### 4.1 Rust — Motor Emocional

| Categoria | Crate | Propósito |
|-----------|-------|-----------|
| **Álgebra linear** | `ndarray` + `ndarray-linalg` | Operações vetoriais, multiplicação $\mathbf{W} \times \vec{g}$, similaridade cosseno |
| **SIMD** | `packed_simd2` ou `std::simd` (nightly) | Aceleração de operações vetoriais em batch |
| **Aleatoriedade** | `rand` + `rand_distr` | Geração de $\epsilon$ para modo estocástico, amostragem de distribuições |
| **FSM** | Implementação custom com `enum` + `match` | Transições determinísticas e hierárquicas |
| **Serialização** | `serde` + `serde_json` / `prost` (protobuf) | Serialização de estados, comunicação |
| **ONNX Inference** | `ort` (ONNX Runtime bindings) | Classificação emocional de texto |
| **gRPC** | `tonic` | Servidor gRPC para exposição do motor |
| **Logging** | `tracing` + `tracing-opentelemetry` | Observabilidade distribuída |
| **Benchmarking** | `criterion` | Microbenchmarks do hot-path |

**Nota sobre `ort` (ONNX Runtime):** Este crate faz binding com a biblioteca C++ do ONNX Runtime. A qualidade dos bindings melhorou significativamente, mas exige que o ONNX Runtime esteja disponível no sistema. Para deployment containerizado, isso significa incluir a shared library no Docker image. A alternativa é `tract` (inference engine puro Rust, sem dependências C++), que suporta um subconjunto dos operadores ONNX — suficiente para modelos de classificação de sentimento baseados em BERT/DistilBERT.

### 4.2 Go — Plano de Controle

| Categoria | Pacote | Propósito |
|-----------|--------|-----------|
| **HTTP/API** | `net/http` stdlib ou `chi` / `fiber` | API gateway, endpoints REST |
| **gRPC** | `google.golang.org/grpc` + `protoc-gen-go` | Comunicação com motor Rust e stores |
| **Store vetorial** | SDK do Qdrant (`qdrant-go`) ou Milvus (`milvus-sdk-go`) | Queries semânticas e emocionais |
| **LLM client** | `sashabaranov/go-openai` ou HTTP direto | Chamadas ao LLM (OpenAI, Anthropic, local) |
| **Concorrência** | `errgroup`, `context`, channels (stdlib) | Paralelização de queries, timeouts |
| **Configuração** | `viper` ou `koanf` | Configuração de agentes, FSM, pesos |
| **Observabilidade** | `go.opentelemetry.io/otel` | Tracing distribuído, métricas |
| **Logging** | `slog` (stdlib Go 1.21+) | Logging estruturado |
| **Scheduler** | `robfig/cron` ou goroutines com timers | Batch jobs de decaimento, garbage collection de memória |
| **Protobuf** | `google.golang.org/protobuf` | Definição de contratos com motor Rust |

### 4.3 Dependência Crítica: Serviço Python Auxiliar (Fase Inicial)

Na fase inicial, antes de consolidar o pipeline ONNX em Rust, um microserviço Python fino é pragmaticamente necessário para:

- **Exportação de modelos** de classificação emocional (GoEmotions, NRCLex) para formato ONNX,
- **Geração de embeddings** semânticos (se usando modelos locais em vez de APIs),
- **Validação** de que os resultados ONNX em Rust são equivalentes aos resultados Python.

Este serviço pode ser exposto via FastAPI com endpoint simples (`POST /classify-emotion → EmotionVector`) e eliminado progressivamente conforme o pipeline ONNX em Rust amadurece. O objetivo é que em produção estável, Python não exista no runtime — apenas no toolchain de desenvolvimento.

---

## 5. Arquitetura de Dados e Stores

### 5.1 Topologia de Armazenamento

```
┌─────────────────────────────────────────────────────────────┐
│                    CAMADA DE STORES                          │
│                                                             │
│  ┌─────────────────────┐    ┌─────────────────────┐        │
│  │  QDRANT (ou Milvus)  │    │  QDRANT (instância  │        │
│  │  Collection:          │    │  ou collection      │        │
│  │  semantic_memories    │    │  separada):          │        │
│  │                      │    │  emotional_memories   │        │
│  │  • Vector: 768-3072d │    │                      │        │
│  │    (embedding semânt.)│    │  • Vector: 3-8d      │        │
│  │  • Payload:           │    │    (vetor emocional) │        │
│  │    - agent_id         │    │  • Payload:           │        │
│  │    - memory_level     │    │    - intensity (I)    │        │
│  │    - timestamp        │    │    - agent_id         │        │
│  │    - content_hash     │    │    - memory_level     │        │
│  │    - emotion_ref_id   │    │    - is_pseudoperm    │        │
│  └──────────┬───────────┘    └──────────┬───────────┘        │
│             │                           │                    │
│             └─────────┬─────────────────┘                    │
│                       │ (linked por content_hash             │
│                       │  ou shared ID)                       │
│                       │                                      │
│  ┌────────────────────▼──────────────────────────────┐      │
│  │  POSTGRESQL (ou Redis para cache)                  │      │
│  │                                                    │      │
│  │  • agent_configs: FSM definitions, W matrices,     │      │
│  │    baselines, weight configs (α,β,γ)               │      │
│  │  • cognitive_contexts: active goals, beliefs,      │      │
│  │    norms por agent_id                              │      │
│  │  • interaction_log: audit trail de transições FSM  │      │
│  │  • emotion_history: série temporal de ē(t)         │      │
│  │    por agente (para cálculo de entropia emocional) │      │
│  └────────────────────────────────────────────────────┘      │
│                                                             │
│  ┌────────────────────────────────────────────────────┐      │
│  │  REDIS (opcional)                                  │      │
│  │                                                    │      │
│  │  • working_memory:{agent_id}: buffer L1 (volátil)  │      │
│  │  • emotion_state:{agent_id}: ē(t) corrente         │      │
│  │  • fsm_state:{agent_id}: estado FSM atual          │      │
│  │  • TTL automático para decaimento de memórias L1   │      │
│  └────────────────────────────────────────────────────┘      │
└─────────────────────────────────────────────────────────────┘
```

### 5.2 Justificativa da Separação de Stores Vetoriais

Conforme discutido no documento arquitetural (Seção 4.3, Opção B — fusão tardia), manter collections separadas para vetores semânticos (768-3072 dimensões) e vetores emocionais (3-8 dimensões) é a abordagem recomendada. A razão prática adicional na implementação: Qdrant e Milvus usam índices HNSW cujo desempenho depende da dimensionalidade. Um índice HNSW otimizado para 768d opera com parâmetros completamente diferentes de um índice para 6d. Misturá-los degradaria ambos.

Go gerencia as queries paralelas:

```
goroutine 1 → query semantic_memories (768d, top-K)  ─┐
goroutine 2 → query emotional_memories (6d, top-K)    ├→ merge → Rust (fuse scores)
goroutine 3 → query cognitive_context (PostgreSQL)     ─┘
```

O merge dos resultados é enviado ao motor Rust para a fusão ponderada ($\alpha \cdot \text{sem} + \beta \cdot \text{emo} + \gamma \cdot \text{cog}$), retornando o ranking final ao Go para construção do prompt.

### 5.3 Redis como Memória de Trabalho (Nível 1)

O buffer de memória de trabalho (Nível 1 na hierarquia) é naturalmente modelado em Redis: estruturas voláteis com TTL automático, acesso sub-milissegundo, e estruturas de dados nativas (sorted sets para ranking por recência, hashes para estado emocional corrente). O TTL do Redis implementa o decaimento do Nível 1 de forma trivial — sem necessidade de batch jobs de limpeza.

---

## 6. Fluxo de Execução Detalhado com Ownership

```
 TEMPO   │  GO (Orquestrador)                RUST (Motor)
─────────┼─────────────────────────────────────────────────────
  t₀     │  Recebe HTTP request
         │  Deserializa input
         │  Busca estado corrente do agente
         │    (Redis: emotion_state, fsm_state)
         │
  t₁     │  ──── envia texto + estado ────▶  Classifica emoção do
         │                                    input via ONNX
         │                                    → ē_input = [v,a,d,...]
         │
  t₂     │                                   Executa transição FSM:
         │                                    estado_anterior + estímulo
         │                                    → novo_estado
         │                                   Calcula ē(t+1) =
         │                                    ē(t) + W × g(t) + ε
         │  ◀── retorna ē(t+1), estado ────
         │
  t₃     │  Atualiza Redis com novo estado
         │  Inicia 3 goroutines paralelas:
         │    g1: query Qdrant semântico
         │    g2: query Qdrant emocional
         │    g3: query PostgreSQL cognitivo
         │  Aguarda todas completarem
         │
  t₄     │  ──── envia candidatos ─────────▶  Calcula score triplo:
         │       + pesos (α,β,γ)               α·sem + β·emo + γ·cog
         │       + ē(t) corrente              Aplica boost pseudoperm.
         │                                    Retorna top-N rankeados
         │  ◀── retorna ranking final ──────
         │
  t₅     │  Constrói prompt cognitivo:
         │    - Documentos rankeados
         │    - Estado emocional → diretriz
         │    - Contexto cognitivo (goals)
         │  Chama LLM (streaming)
         │    ⏳ ~500-3000ms (bottleneck)
         │
  t₆     │  Recebe resposta do LLM
         │  ──── envia resposta gerada ────▶  Extrai vetor emocional
         │                                    da resposta
         │                                   Avalia intensidade I
         │                                   Decide: promover memória?
         │                                    L1 → L2 → L3?
         │  ◀── retorna decisão ────────────
         │
  t₇     │  Persiste memória no nível
         │    adequado (Qdrant + Postgres)
         │  Atualiza Redis (working memory)
         │  Retorna resposta ao cliente
         │
─────────┼─────────────────────────────────────────────────────
 TOTAL   │  ~50-100ms (sem LLM)
         │  ~550-3100ms (com LLM) ← LLM domina >90% do tempo
```

---

## 7. Análise de Dificuldade por Módulo

### 7.1 Matriz de Dificuldade

| Módulo | Dificuldade | Esforço Estimado | Risco Principal |
|--------|-------------|------------------|-----------------|
| **API Gateway (Go)** | Baixa | 1-2 semanas | Nenhum relevante — é Go padrão |
| **Orquestrador de Pipeline (Go)** | Média | 2-3 semanas | Coordenação de erros entre goroutines paralelas; circuit breaking para stores indisponíveis |
| **FSM/HFSM Engine (Rust)** | Média | 2-4 semanas | Design correto da hierarquia de estados; sistema de restrições ($\Omega$); testing de todas as transições |
| **Motor Vetorial (Rust)** | Média | 2-3 semanas | Otimização SIMD; correctness de similaridade cosseno normalizada; numerical stability |
| **Classificador Emocional ONNX (Rust)** | Alta | 3-5 semanas | Exportação do modelo Python → ONNX; tokenização em Rust (sem HuggingFace); validação de paridade de resultados |
| **Integração Go ↔ Rust (gRPC)** | Média | 1-2 semanas | Definição de contratos Protobuf; serialização eficiente de arrays vetoriais; error propagation |
| **Stores vetoriais (Go)** | Média-Baixa | 2-3 semanas | Schema design nas collections; tuning de HNSW params por dimensionalidade; consistency entre stores semântico e emocional |
| **Prompt Cognitivo (Go)** | Baixa | 1 semana | Template engineering; tradução vetor → diretriz textual |
| **Memória Hierárquica + Promoção** | Alta | 3-4 semanas | Lógica distribuída entre Rust (decisão) e Go (persistência); race conditions na promoção; batch jobs de decaimento |
| **Agent Manager Multiagente (Go)** | Alta | 3-5 semanas | Contágio emocional entre agentes; ciclo de vida; isolamento de estado entre agentes; deadlock prevention |
| **Observabilidade (Go)** | Média | 2-3 semanas | Tracing distribuído cross-language (Go ↔ Rust); métricas custom (entropia emocional, latência por componente) |

### 7.2 Estimativa Total

Para um time de 2-3 engenheiros com experiência em ambas as linguagens: **3-5 meses** para um MVP funcional com um único agente, FSM básica (sem hierarquia), um store vetorial e integração com um LLM. **6-9 meses** para a arquitetura completa com multiagentes, HFSM, modo estocástico, três níveis de memória e observabilidade plena.

O fator multiplicador mais perigoso não é técnico — é a **escassez de engenheiros fluentes simultaneamente em Go e Rust**. Na maioria dos mercados, o time terá especialistas em uma ou outra, com a integração caindo sobre o engenheiro mais sênior.

---

## 8. O Bottleneck Real: O LLM, Não a Linguagem

Um ponto que merece destaque explícito: em qualquer implementação do EmotionRAG, **a chamada ao LLM domina >90% da latência total**. Uma chamada típica à API da OpenAI/Anthropic leva 500-3000ms. Todo o pipeline Rust (FSM + vetores + scoring) completa em <5ms. As queries aos stores vetoriais levam 5-20ms. A construção do prompt em Go leva <1ms.

Isso significa que a escolha Go + Rust (vs. Python puro, por exemplo) **não se justifica primariamente por latência** — se justifica por:

1. **Throughput**: Go com goroutines escala para milhares de agentes concorrentes com fração da memória que Python consumiria (sem GIL, sem multiprocessing overhead).
2. **Previsibilidade**: latências P99 em Go/Rust são ordens de magnitude mais estáveis que em Python (sem stop-the-world GC do Python, sem overhead do asyncio).
3. **Custo de infraestrutura**: o mesmo hardware suporta 5-10x mais agentes simultâneos em Go+Rust comparado a Python.
4. **Robustez**: type safety de ambas as linguagens previne classes inteiras de bugs que em Python só aparecem em runtime.

Para um protótipo ou prova de conceito com poucos agentes, Python é a escolha correta. Para produção com centenas ou milhares de agentes simultâneos, Go + Rust se paga.

---

## 9. Estratégia de Deployment

### 9.1 Containerização

```
┌─────────────────────────────────────────────────┐
│               docker-compose / k8s               │
│                                                   │
│  ┌─────────────────┐   ┌──────────────────────┐  │
│  │  go-orchestrator │   │  rust-emotion-engine │  │
│  │  (container 1)   │   │  (container 2)       │  │
│  │                  │   │                      │  │
│  │  Port: 8080      │──▶│  Port: 50051 (gRPC)  │  │
│  │  (HTTP API)      │   │                      │  │
│  └────────┬─────────┘   └──────────────────────┘  │
│           │                                       │
│  ┌────────▼─────────┐   ┌──────────────────────┐  │
│  │  qdrant           │   │  postgresql           │  │
│  │  (container 3)    │   │  (container 4)        │  │
│  │  Port: 6333/6334  │   │  Port: 5432           │  │
│  └──────────────────┘   └──────────────────────┘  │
│                                                   │
│  ┌──────────────────┐   ┌──────────────────────┐  │
│  │  redis             │   │  python-ml (opcional)│  │
│  │  (container 5)     │   │  (container 6)       │  │
│  │  Port: 6379        │   │  Port: 8090           │  │
│  │                    │   │  FastAPI + modelos    │  │
│  └──────────────────┘   └──────────────────────┘  │
└─────────────────────────────────────────────────────┘
```

### 9.2 Scaling

- **Go orchestrator**: escala horizontalmente (stateless; estado em Redis/Postgres). Load balancer na frente.
- **Rust emotion engine**: escala horizontalmente se necessário, mas uma instância suporta alta carga (operações são CPU-bound e rápidas). Escalar verticalmente primeiro (mais cores → mais threads no Tokio runtime).
- **Qdrant**: scaling nativo com sharding por agent_id.
- **Redis**: cluster mode se necessário; na maioria dos casos, uma instância basta.
- **Python ML**: eliminar progressivamente; quando eliminado, reduz complexidade operacional significativamente.

### 9.3 Evolução: Eliminação Progressiva do Python

```
FASE 1 (MVP):       Go ←→ Rust ←→ Python (ML)
                     Python faz classificação emocional + embeddings

FASE 2 (Maturação):  Go ←→ Rust (com ONNX via ort/tract)
                     Modelos exportados para ONNX, Python eliminado do runtime
                     Python mantido apenas no pipeline de treinamento/exportação

FASE 3 (Otimização): Go ←→ Rust (FFI direto para hot-path, se necessário)
                     gRPC mantido para operações batch
                     FFI apenas para single-interaction fast path
```

---

## 10. Riscos Específicos da Implementação Go + Rust

### 10.1 Riscos Técnicos

| Risco | Probabilidade | Impacto | Mitigação |
|-------|---------------|---------|-----------|
| **cgo performance penalty** (se Opção A) | Alta | Médio | Usar Opção B (gRPC) como default; FFI só se profiling justificar |
| **Divergência de resultados ONNX vs Python** | Média | Alto | Suite de testes de paridade com tolerância numérica; CI que roda ambos e compara |
| **Tokenização inconsistente em Rust** | Média | Alto | Usar `tokenizers` crate (bindings do HuggingFace Tokenizers em Rust) — mesmo tokenizador que Python |
| **Memory leak no motor Rust** (se usando FFI) | Baixa | Alto | Ownership model do Rust previne maioria; atenção especial a objetos passados pelo C ABI |
| **Serialização de vetores via Protobuf** | Baixa | Baixo | Usar `repeated float` em Protobuf; para vetores grandes, considerar encoding binário custom |
| **Debugging cross-language** | Alta | Médio | OpenTelemetry com trace IDs propagados entre Go e Rust; logging estruturado com correlation IDs |

### 10.2 Riscos Organizacionais

| Risco | Mitigação |
|-------|-----------|
| **Escassez de devs Go+Rust** | Especializar: devs Go não precisam entender Rust internamente (e vice-versa); a interface gRPC é o contrato |
| **Complexidade de onboarding** | Documentação exaustiva dos contratos Protobuf; docker-compose que sobe tudo com um comando |
| **Tentação de reescrever tudo em uma linguagem** | Manter disciplina arquitetural; a separação de responsabilidades é a força do design |

---

## 11. Benchmarks Esperados e Metas de Performance

| Operação | Meta | Justificativa |
|----------|------|---------------|
| Transição FSM (Rust) | < 1μs | Lookup em hashmap + pattern match |
| Cálculo $\vec{e}(t+1)$ (Rust) | < 10μs | Multiplicação matriz-vetor 8×8 + adição |
| Classificação emocional ONNX (Rust) | < 5ms | Inference de DistilBERT quantizado |
| Score fusion de 100 candidatos (Rust) | < 100μs | 100 × (3 multiplicações + sort) |
| Query Qdrant semântico top-50 | < 10ms | HNSW approximate NN |
| Query Qdrant emocional top-50 | < 5ms | HNSW em 6 dimensões (muito rápido) |
| Pipeline completo sem LLM | < 50ms | Soma de todas as etapas |
| Pipeline completo com LLM | < 3500ms | Dominado pela latência do LLM |
| Throughput de agentes simultâneos | > 1000/instância | Go goroutines + Rust thread pool |

---

## 12. Conclusão: Veredicto de Viabilidade

A implementação do EmotionRAG em Go + Rust é **plenamente viável** e representa uma das combinações mais adequadas para este tipo de sistema. A divisão natural — Go para orquestração e I/O, Rust para computação e FSM — alinha cada linguagem com seu ponto forte sem forçar nenhuma delas a operar fora da sua zona de excelência.

A dificuldade real não está nas linguagens, mas na **complexidade inerente da arquitetura**: são muitos componentes interagindo (FSM, stores vetoriais, hierarquia de memória, classificador emocional, camada cognitiva, LLM). Go + Rust não reduz essa complexidade — mas garante que cada componente opera com performance previsível e type safety rigorosa, reduzindo a classe de bugs que emergem em produção.

A recomendação final é: **começar com a Opção B (gRPC entre processos)**, um **único agente**, **FSM plana (sem hierarquia)**, e **Python auxiliar para ML**. Iterar adicionando hierarquia, modo estocástico, multiagentes e eliminação do Python progressivamente. A arquitetura modular permite essa evolução incremental sem reescritas — e essa é talvez a maior vantagem do design proposto.

---

## Apêndice — Referência Rápida de Comandos

### Build do motor Rust
```bash
# Compilar como serviço gRPC standalone
cd emotion-engine/
cargo build --release

# Compilar como biblioteca para FFI (se Opção A)
cargo build --release --lib
# Gera: target/release/libemotion_engine.a
```

### Build do orquestrador Go
```bash
cd orchestrator/
# Gerar código Protobuf
protoc --go_out=. --go-grpc_out=. proto/emotion_engine.proto

go build -o emotionrag-server ./cmd/server
```

### Docker Compose (desenvolvimento)
```bash
docker compose up -d    # Sobe todos os serviços
docker compose logs -f  # Acompanha logs unificados
```

### Teste de paridade ONNX
```bash
# Roda inference em Python e Rust, compara resultados
make test-onnx-parity TOLERANCE=1e-5
```
