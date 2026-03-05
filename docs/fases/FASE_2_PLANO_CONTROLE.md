# FASE 2 - Plano de Controle em Go

> **Duracao estimada:** 5 semanas (Semana 5-9)
> **Equipe minima:** 1 engenheiro Go
> **Pre-requisitos:** Fase 0 concluida (proto, infra Docker)
> **Resultado:** Orquestrador Go funcional com API, pipeline, connectors (usando mocks)
> **Paralelizavel com:** Fase 1 (Rust) - ambas convergem na Fase 3

---

## 2.1 Objetivo

Implementar o **plano de controle** do EmotionRAG em Go:
- API Gateway HTTP com endpoints REST
- Orquestrador de pipeline (8 steps sequenciais/paralelos)
- Connector Hub (clients para Rust engine, Qdrant, Redis, PostgreSQL, LLM)
- Concorrencia via goroutines + errgroup com timeout budget

Ao final, o orquestrador opera com mocks e esta pronto para conectar ao motor Rust (Fase 3).

---

## 2.2 Estrutura de Pacotes Go

```
orchestrator/
├── cmd/
│   └── server/
│       └── main.go                      # Entrypoint: wiring de dependencias
├── internal/
│   ├── api/
│   │   ├── router.go                    # Chi router setup
│   │   ├── handler_interact.go          # POST /api/v1/interact
│   │   ├── handler_agent.go             # CRUD /api/v1/agents
│   │   ├── handler_state.go             # GET /api/v1/agents/{id}/state
│   │   ├── middleware.go                # Request ID, logging, timeout
│   │   └── response.go                  # Helpers de resposta JSON
│   ├── pipeline/
│   │   ├── orchestrator.go              # Pipeline de 8 steps
│   │   ├── step_perceive.go             # Step 1: parsing + classificacao emocional
│   │   ├── step_fsm.go                  # Step 2: transicao FSM
│   │   ├── step_cognitive.go            # Step 3: atualizacao cognitiva
│   │   ├── step_retrieve.go             # Step 4: queries paralelas aos stores
│   │   ├── step_fuse.go                 # Step 5: fusao de scores (via Rust)
│   │   ├── step_prompt.go               # Step 6: construcao do prompt
│   │   ├── step_generate.go             # Step 7: chamada ao LLM
│   │   └── step_postprocess.go          # Step 8: pos-processamento + promocao
│   ├── connector/
│   │   ├── emotion/
│   │   │   ├── client.go               # Client gRPC para motor Rust
│   │   │   └── mock.go                 # Mock para desenvolvimento isolado
│   │   ├── vectorstore/
│   │   │   ├── client.go               # Client Qdrant (queries semantica + emocional)
│   │   │   └── mock.go
│   │   ├── cache/
│   │   │   ├── client.go               # Client Redis (working memory, estado)
│   │   │   └── mock.go
│   │   ├── db/
│   │   │   ├── client.go               # Client PostgreSQL (configs, cognitivo, log)
│   │   │   └── mock.go
│   │   ├── llm/
│   │   │   ├── provider.go             # Interface LLMProvider
│   │   │   ├── openai.go              # Implementacao OpenAI
│   │   │   ├── anthropic.go           # Implementacao Anthropic
│   │   │   ├── ollama.go             # Implementacao Ollama (local)
│   │   │   └── mock.go
│   │   └── classifier/
│   │       ├── client.go              # Client HTTP para python-ml
│   │       └── mock.go
│   ├── model/
│   │   ├── agent.go                    # AgentConfig, AgentState
│   │   ├── emotion.go                  # EmotionVector, FsmState
│   │   ├── memory.go                   # Memory, MemoryLevel
│   │   ├── cognitive.go                # CognitiveContext
│   │   └── interaction.go              # InteractionRequest, InteractionResponse
│   └── config/
│       └── config.go                    # Configuracao do servidor (env vars)
├── pkg/
│   └── proto/                           # Codigo gerado do protobuf
│       └── emotion_engine/
│           └── v1/
└── go.mod
```

---

## 2.3 Entregavel 2.1 - API Gateway (Semana 5-6)

### 2.3.1 Router

```go
// internal/api/router.go
package api

import (
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
)

func NewRouter(h *Handlers) http.Handler {
    r := chi.NewRouter()

    // Middlewares globais
    r.Use(middleware.RequestID)
    r.Use(middleware.RealIP)
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)
    r.Use(middleware.Timeout(30 * time.Second))

    // Health
    r.Get("/health", h.Health)
    r.Get("/ready", h.Ready)

    // API v1
    r.Route("/api/v1", func(r chi.Router) {
        // Interacao principal
        r.Post("/interact", h.Interact)

        // CRUD de agentes
        r.Route("/agents", func(r chi.Router) {
            r.Post("/", h.CreateAgent)
            r.Get("/", h.ListAgents)
            r.Route("/{agentID}", func(r chi.Router) {
                r.Get("/", h.GetAgent)
                r.Put("/", h.UpdateAgent)
                r.Delete("/", h.DeleteAgent)
                r.Get("/state", h.GetAgentState)
                r.Get("/history", h.GetEmotionHistory)
            })
        })
    })

    return r
}
```

### 2.3.2 Handler de Interacao

```go
// internal/api/handler_interact.go
package api

import (
    "encoding/json"
    "net/http"

    "github.com/swarm-emotions/orchestrator/internal/model"
    "github.com/swarm-emotions/orchestrator/internal/pipeline"
)

type InteractRequest struct {
    AgentID string `json:"agent_id"`
    Text    string `json:"text"`
    // Metadata opcional
    Metadata map[string]any `json:"metadata,omitempty"`
}

type InteractResponse struct {
    Response       string              `json:"response"`
    EmotionState   model.EmotionVector `json:"emotion_state"`
    FsmState       string              `json:"fsm_state"`
    Intensity      float32             `json:"intensity"`
    LatencyMs      int64               `json:"latency_ms"`
    TraceID        string              `json:"trace_id,omitempty"`
}

func (h *Handlers) Interact(w http.ResponseWriter, r *http.Request) {
    var req InteractRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        respondError(w, http.StatusBadRequest, "invalid request body")
        return
    }

    if req.AgentID == "" || req.Text == "" {
        respondError(w, http.StatusBadRequest, "agent_id and text are required")
        return
    }

    result, err := h.pipeline.Execute(r.Context(), pipeline.Input{
        AgentID: req.AgentID,
        Text:    req.Text,
        Metadata: req.Metadata,
    })
    if err != nil {
        respondError(w, http.StatusInternalServerError, err.Error())
        return
    }

    respondJSON(w, http.StatusOK, InteractResponse{
        Response:     result.LLMResponse,
        EmotionState: result.NewEmotion,
        FsmState:     result.NewFsmState.StateName,
        Intensity:    result.NewIntensity,
        LatencyMs:    result.LatencyMs,
    })
}
```

### 2.3.3 Modelos de Dominio

```go
// internal/model/emotion.go
package model

// EmotionVector representa um vetor emocional n-dimensional.
// Componentes: [valencia, ativacao, dominancia, certeza, social, novidade]
type EmotionVector struct {
    Components []float32 `json:"components"`
}

func (v EmotionVector) Intensity() float32 {
    var sum float32
    for _, c := range v.Components {
        sum += c * c
    }
    return float32(math.Sqrt(float64(sum)))
}

// FsmState representa o estado da maquina de estados emocional
type FsmState struct {
    StateName  string `json:"state_name"`
    MacroState string `json:"macro_state"`
    EnteredAt  int64  `json:"entered_at_ms"`
}
```

```go
// internal/model/agent.go
package model

type AgentConfig struct {
    AgentID       string          `json:"agent_id" db:"agent_id"`
    DisplayName   string          `json:"display_name" db:"display_name"`
    Baseline      EmotionVector   `json:"baseline" db:"baseline"`
    WMatrix       []float32       `json:"w_matrix" db:"w_matrix"`
    WDimension    int             `json:"w_dimension" db:"w_dimension"`
    Weights       ScoreWeights    `json:"weights" db:"weights"`
    DecayLambda   float32         `json:"decay_lambda" db:"decay_lambda"`
    NoiseEnabled  bool            `json:"noise_enabled" db:"noise_enabled"`
    NoiseSigma    float32         `json:"noise_sigma" db:"noise_sigma"`
}

type ScoreWeights struct {
    Alpha float32 `json:"alpha"` // Peso semantico
    Beta  float32 `json:"beta"`  // Peso emocional
    Gamma float32 `json:"gamma"` // Peso cognitivo
}
```

---

## 2.4 Entregavel 2.2 - Orquestrador de Pipeline (Semana 6-8)

### 2.4.1 Pipeline Principal

```go
// internal/pipeline/orchestrator.go
package pipeline

import (
    "context"
    "fmt"
    "time"
)

// Input para o pipeline
type Input struct {
    AgentID  string
    Text     string
    Metadata map[string]any
}

// Output do pipeline
type Output struct {
    LLMResponse  string
    NewEmotion   model.EmotionVector
    NewFsmState  model.FsmState
    NewIntensity float32
    LatencyMs    int64
}

// Orchestrator coordena os 8 steps do pipeline EmotionRAG
type Orchestrator struct {
    emotionClient  connector.EmotionEngineClient
    vectorStore    connector.VectorStoreClient
    cache          connector.CacheClient
    db             connector.DBClient
    llm            connector.LLMProvider
    classifier     connector.ClassifierClient
}

func (o *Orchestrator) Execute(ctx context.Context, input Input) (*Output, error) {
    start := time.Now()

    // Step 1: Buscar estado corrente do agente (Redis)
    agentState, err := o.cache.GetAgentState(ctx, input.AgentID)
    if err != nil {
        return nil, fmt.Errorf("step1 get state: %w", err)
    }

    agentConfig, err := o.db.GetAgentConfig(ctx, input.AgentID)
    if err != nil {
        return nil, fmt.Errorf("step1 get config: %w", err)
    }

    // Step 2: Percepcao - classificar emocao do input
    stimulusVector, stimulusType, err := o.stepPerceive(ctx, input.Text)
    if err != nil {
        return nil, fmt.Errorf("step2 perceive: %w", err)
    }

    // Step 3: FSM + Vetor emocional (chamada ao motor Rust)
    fsmResult, err := o.stepFSMAndVector(ctx, agentState, agentConfig, stimulusVector, stimulusType)
    if err != nil {
        return nil, fmt.Errorf("step3 fsm+vector: %w", err)
    }

    // Step 4: Atualizar estado no Redis
    if err := o.cache.SetAgentState(ctx, input.AgentID, fsmResult); err != nil {
        return nil, fmt.Errorf("step4 update state: %w", err)
    }

    // Step 5: Queries paralelas (semantico + emocional + cognitivo)
    candidates, cogContext, err := o.stepRetrieve(ctx, input, fsmResult, agentConfig)
    if err != nil {
        return nil, fmt.Errorf("step5 retrieve: %w", err)
    }

    // Step 6: Fusao de scores (chamada ao motor Rust)
    ranked, err := o.stepFuse(ctx, candidates, agentConfig, fsmResult.NewEmotion)
    if err != nil {
        return nil, fmt.Errorf("step6 fuse: %w", err)
    }

    // Step 7: Construir prompt + chamar LLM
    llmResponse, err := o.stepGenerate(ctx, input, ranked, fsmResult, cogContext)
    if err != nil {
        return nil, fmt.Errorf("step7 generate: %w", err)
    }

    // Step 8: Pos-processamento (async - nao bloqueia resposta)
    go o.stepPostProcess(context.Background(), input, llmResponse, fsmResult, agentConfig)

    return &Output{
        LLMResponse:  llmResponse,
        NewEmotion:   fsmResult.NewEmotion,
        NewFsmState:  fsmResult.NewFsmState,
        NewIntensity: fsmResult.NewIntensity,
        LatencyMs:    time.Since(start).Milliseconds(),
    }, nil
}
```

### 2.4.2 Step de Queries Paralelas

```go
// internal/pipeline/step_retrieve.go
package pipeline

import (
    "context"

    "golang.org/x/sync/errgroup"
)

// stepRetrieve executa 3 queries em paralelo:
// 1. Qdrant semantico (embedding do texto)
// 2. Qdrant emocional (vetor emocional corrente)
// 3. PostgreSQL cognitivo (contexto cognitivo)
func (o *Orchestrator) stepRetrieve(
    ctx context.Context,
    input Input,
    fsmResult *FSMResult,
    config *model.AgentConfig,
) ([]model.ScoreCandidate, *model.CognitiveContext, error) {

    g, ctx := errgroup.WithContext(ctx)

    var semanticResults []model.MemoryHit
    var emotionalResults []model.MemoryHit
    var cogContext *model.CognitiveContext

    // Goroutine 1: Query semantica
    g.Go(func() error {
        var err error
        semanticResults, err = o.vectorStore.QuerySemantic(ctx, QuerySemanticParams{
            AgentID: input.AgentID,
            Text:    input.Text,
            TopK:    50,
        })
        return err
    })

    // Goroutine 2: Query emocional
    g.Go(func() error {
        var err error
        emotionalResults, err = o.vectorStore.QueryEmotional(ctx, QueryEmotionalParams{
            AgentID:        input.AgentID,
            EmotionVector:  fsmResult.NewEmotion,
            TopK:           50,
        })
        return err
    })

    // Goroutine 3: Contexto cognitivo
    g.Go(func() error {
        var err error
        cogContext, err = o.db.GetCognitiveContext(ctx, input.AgentID)
        return err
    })

    if err := g.Wait(); err != nil {
        return nil, nil, err
    }

    // Merge dos resultados em ScoreCandidate unificado
    candidates := mergeResults(semanticResults, emotionalResults, cogContext)
    return candidates, cogContext, nil
}
```

### 2.4.3 Timeout Budget Pattern

```go
// internal/pipeline/orchestrator.go (complemento)

// timeoutBudget distribui o timeout restante entre steps.
// Garante que steps tardios (especialmente LLM) sempre tem budget minimo.
func timeoutBudget(ctx context.Context) (remaining time.Duration) {
    deadline, ok := ctx.Deadline()
    if !ok {
        return 30 * time.Second // default
    }
    return time.Until(deadline)
}

// withStepTimeout cria subcontext com fracao do budget.
// Se budget restante < minimo, usa o minimo.
func withStepTimeout(ctx context.Context, fraction float64, minimum time.Duration) (context.Context, context.CancelFunc) {
    budget := timeoutBudget(ctx)
    stepTimeout := time.Duration(float64(budget) * fraction)
    if stepTimeout < minimum {
        stepTimeout = minimum
    }
    return context.WithTimeout(ctx, stepTimeout)
}
```

---

## 2.5 Entregavel 2.3 - Connector Hub (Semana 7-9)

### 2.5.1 Interfaces (Contratos)

```go
// internal/connector/interfaces.go
package connector

import "context"

// EmotionEngineClient - client gRPC para motor Rust
type EmotionEngineClient interface {
    TransitionState(ctx context.Context, req *TransitionRequest) (*TransitionResponse, error)
    ComputeEmotionVector(ctx context.Context, req *ComputeRequest) (*ComputeResponse, error)
    FuseScores(ctx context.Context, req *FuseRequest) (*FuseResponse, error)
    EvaluatePromotion(ctx context.Context, req *PromotionRequest) (*PromotionResponse, error)
    ProcessInteraction(ctx context.Context, req *ProcessRequest) (*ProcessResponse, error)
}

// VectorStoreClient - client para Qdrant
type VectorStoreClient interface {
    QuerySemantic(ctx context.Context, params QuerySemanticParams) ([]MemoryHit, error)
    QueryEmotional(ctx context.Context, params QueryEmotionalParams) ([]MemoryHit, error)
    UpsertMemory(ctx context.Context, memory Memory) error
    DeleteMemory(ctx context.Context, memoryID string) error
}

// CacheClient - client para Redis
type CacheClient interface {
    GetAgentState(ctx context.Context, agentID string) (*AgentState, error)
    SetAgentState(ctx context.Context, agentID string, state *AgentState) error
    GetWorkingMemory(ctx context.Context, agentID string) ([]WorkingMemoryEntry, error)
    PushWorkingMemory(ctx context.Context, agentID string, entry WorkingMemoryEntry) error
}

// DBClient - client para PostgreSQL
type DBClient interface {
    GetAgentConfig(ctx context.Context, agentID string) (*AgentConfig, error)
    SaveAgentConfig(ctx context.Context, config *AgentConfig) error
    GetCognitiveContext(ctx context.Context, agentID string) (*CognitiveContext, error)
    UpdateCognitiveContext(ctx context.Context, agentID string, ctx *CognitiveContext) error
    LogInteraction(ctx context.Context, log *InteractionLog) error
}

// LLMProvider - client para LLM (OpenAI, Anthropic, Ollama)
type LLMProvider interface {
    Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error)
    GenerateStream(ctx context.Context, prompt string, opts GenerateOpts) (<-chan StreamChunk, error)
}

// ClassifierClient - client HTTP para python-ml
type ClassifierClient interface {
    ClassifyEmotion(ctx context.Context, text string) (*EmotionClassification, error)
}
```

### 2.5.2 Client gRPC para Motor Rust

```go
// internal/connector/emotion/client.go
package emotion

import (
    "context"
    "fmt"
    "time"

    pb "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

type Client struct {
    conn   *grpc.ClientConn
    client pb.EmotionEngineServiceClient
}

func NewClient(addr string) (*Client, error) {
    conn, err := grpc.NewClient(addr,
        grpc.WithTransportCredentials(insecure.NewCredentials()),
        grpc.WithDefaultCallOptions(
            grpc.MaxCallRecvMsgSize(4*1024*1024), // 4MB
        ),
    )
    if err != nil {
        return nil, fmt.Errorf("connect to emotion engine: %w", err)
    }

    return &Client{
        conn:   conn,
        client: pb.NewEmotionEngineServiceClient(conn),
    }, nil
}

func (c *Client) ProcessInteraction(ctx context.Context, req *pb.ProcessInteractionRequest) (*pb.ProcessInteractionResponse, error) {
    ctx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
    defer cancel()
    return c.client.ProcessInteraction(ctx, req)
}

func (c *Client) Close() error {
    return c.conn.Close()
}
```

### 2.5.3 Client Redis

```go
// internal/connector/cache/client.go
package cache

import (
    "context"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

type Client struct {
    rdb *redis.Client
}

func NewClient(addr string) *Client {
    return &Client{
        rdb: redis.NewClient(&redis.Options{
            Addr:         addr,
            ReadTimeout:  100 * time.Millisecond,
            WriteTimeout: 100 * time.Millisecond,
        }),
    }
}

// Chaves Redis:
// emotion_state:{agent_id}  -> JSON do vetor emocional + FSM state
// working_memory:{agent_id} -> Sorted set de memorias L1 (score = timestamp)
// agent_lock:{agent_id}     -> Lock para serializar updates por agente

func (c *Client) GetAgentState(ctx context.Context, agentID string) (*AgentState, error) {
    key := fmt.Sprintf("emotion_state:%s", agentID)
    data, err := c.rdb.Get(ctx, key).Bytes()
    if err == redis.Nil {
        return nil, nil // Agente novo, sem estado
    }
    if err != nil {
        return nil, err
    }

    var state AgentState
    if err := json.Unmarshal(data, &state); err != nil {
        return nil, err
    }
    return &state, nil
}

func (c *Client) SetAgentState(ctx context.Context, agentID string, state *AgentState) error {
    key := fmt.Sprintf("emotion_state:%s", agentID)
    data, err := json.Marshal(state)
    if err != nil {
        return err
    }
    return c.rdb.Set(ctx, key, data, 0).Err() // Sem TTL - estado permanece
}

// Lock por agente para prevenir lost updates (R6 do catalogo de riscos)
func (c *Client) AcquireAgentLock(ctx context.Context, agentID string, ttl time.Duration) (bool, error) {
    key := fmt.Sprintf("agent_lock:%s", agentID)
    return c.rdb.SetNX(ctx, key, "locked", ttl).Result()
}

func (c *Client) ReleaseAgentLock(ctx context.Context, agentID string) error {
    key := fmt.Sprintf("agent_lock:%s", agentID)
    return c.rdb.Del(ctx, key).Err()
}
```

### 2.5.4 Interface LLM Provider

```go
// internal/connector/llm/provider.go
package llm

import "context"

type GenerateOpts struct {
    Model       string
    MaxTokens   int
    Temperature float32
    SystemPrompt string
}

type StreamChunk struct {
    Text  string
    Done  bool
    Error error
}

type Provider interface {
    Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error)
    GenerateStream(ctx context.Context, prompt string, opts GenerateOpts) (<-chan StreamChunk, error)
}
```

---

## 2.6 Configuracao

```go
// internal/config/config.go
package config

import "os"

type Config struct {
    HTTPPort          string
    EmotionEngineAddr string
    QdrantAddr        string
    RedisAddr         string
    PostgresDSN       string
    PythonMLURL       string
    LLMProvider       string   // "openai", "anthropic", "ollama"
    LLMAPIKey         string
    LLMModel          string
    DefaultTimeout    int      // segundos
}

func Load() *Config {
    return &Config{
        HTTPPort:          getEnv("HTTP_PORT", "8080"),
        EmotionEngineAddr: getEnv("EMOTION_ENGINE_ADDR", "localhost:50051"),
        QdrantAddr:        getEnv("QDRANT_ADDR", "localhost:6334"),
        RedisAddr:         getEnv("REDIS_ADDR", "localhost:6379"),
        PostgresDSN:       getEnv("POSTGRES_DSN", "postgres://emotionrag:dev@localhost:5432/emotionrag?sslmode=disable"),
        PythonMLURL:       getEnv("PYTHON_ML_URL", "http://localhost:8090"),
        LLMProvider:       getEnv("LLM_PROVIDER", "openai"),
        LLMAPIKey:         os.Getenv("LLM_API_KEY"),
        LLMModel:          getEnv("LLM_MODEL", "gpt-4o-mini"),
        DefaultTimeout:    30,
    }
}

func getEnv(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}
```

---

## 2.7 Testes

### 2.7.1 Testes Unitarios com Mocks

```go
// internal/pipeline/orchestrator_test.go
package pipeline_test

// Testes com mocks de todos os connectors.
// Verificam:
// - Pipeline executa os 8 steps na ordem correta
// - Erro em qualquer step propaga corretamente
// - Queries paralelas (step 5) realmente executam em paralelo
// - Timeout budget distribui tempo corretamente
// - Step 8 (pos-processamento) roda em background sem bloquear
```

### 2.7.2 Testes de Integracao

```go
// internal/connector/cache/client_test.go
// Requer: docker compose up redis
//
// Testa:
// - SetAgentState / GetAgentState roundtrip
// - AcquireAgentLock / ReleaseAgentLock
// - Lock exclui update concorrente
// - TTL da working memory expira corretamente
```

### 2.7.3 Testes de Concorrencia

```go
// internal/pipeline/concurrency_test.go
//
// Testa cenario R6 (lost update):
// - 10 goroutines enviando requests para o mesmo agente simultaneamente
// - Com lock: todas completam sem lost update
// - Sem lock: demonstrar que lost update ocorre (teste negativo)
```

---

## 2.8 Checklist de Aceitacao

> **Status atualizado em 2026-03-05 neste ambiente**

> **Escopo implementado nesta iteracao:** orquestrador funcional com mocks, API REST completa, pipeline de 8 steps, client gRPC do motor emocional, client HTTP do classificador e testes unitarios/concorrencia.

### API Gateway
- [x] `POST /api/v1/interact` aceita request e retorna response (com mocks)
- [ ] `POST /api/v1/agents` cria agente no PostgreSQL  
  Implementado com mock in-memory do DB; persistencia real em PostgreSQL ainda nao foi conectada.
- [ ] `GET /api/v1/agents/{id}/state` retorna estado do Redis  
  Implementado com mock in-memory de cache; persistencia real em Redis ainda nao foi conectada.
- [x] `GET /health` e `GET /ready` funcionam
- [x] Request ID propagado em todos os logs
- [x] Timeout global de 30s aplicado

### Pipeline
- [x] 8 steps executam na sequencia correta
- [x] Step 5 (retrieve) executa 3 queries em paralelo via errgroup
- [x] Step 8 (pos-processamento) roda em background
- [x] Erro em qualquer step retorna mensagem clara
- [x] Timeout budget distribui tempo entre steps

### Connectors
- [x] Emotion Engine client: chamada gRPC funcional (com mock server)
- [ ] VectorStore client: query Qdrant funcional (teste de integracao)  
  Apenas mock do vector store foi implementado nesta etapa.
- [ ] Cache client: Redis get/set/lock funcional  
  Apenas mock do cache foi implementado nesta etapa.
- [ ] DB client: PostgreSQL CRUD funcional  
  Apenas mock do DB foi implementado nesta etapa.
- [x] LLM client: pelo menos 1 provider implementado (OpenAI ou mock)
- [x] Classifier client: HTTP call para python-ml funcional

### Concorrencia
- [x] Lock por agent_id previne lost updates
- [ ] Goroutines nao vazam (pprof mostra contagem estavel)
- [ ] Circuit breaker testado para Rust engine indisponivel

---

## 2.9 Riscos Especificos

| Risco | Prob. | Impacto | Mitigacao |
|-------|-------|---------|-----------|
| Goroutines presas em gRPC | Media | Alto | Timeout 500ms + circuit breaker |
| Lost update em estado emocional | Media | Alto | Lock por agent_id no Redis |
| Timeout cascade entre steps | Media | Medio | Timeout budget pattern |
| Acoplamento com interface Protobuf instavel | Alta | Medio | Adapter layer entre proto e model |
| Complexidade de wiring de dependencias | Baixa | Baixo | Constructor injection em main.go |

---

## 2.10 Transicao para Fase 3

Ao final da Fase 2:
- O orquestrador Go funciona com mocks e testes unitarios passam
- Todos os connectors estao implementados e testados contra servicos reais
- O pipeline executa os 8 steps com mocks

A **Fase 3** conecta Go ao motor Rust real (substituindo mock) e ao servico Python,
formando o primeiro pipeline E2E funcional.
