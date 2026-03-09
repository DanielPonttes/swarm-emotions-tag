# FASE 3 - Integracao Go <-> Rust + Pipeline E2E

> **Duracao estimada:** 4 semanas (Semana 8-11)
> **Equipe minima:** 2 engenheiros (Go + Rust)
> **Pre-requisitos:** Fase 1 (motor Rust) e Fase 2 (orquestrador Go) concluidas
> **Resultado:** Pipeline E2E funcional com single-agent, LLM real, classificacao Python

---

## 3.1 Objetivo

Conectar o motor Rust ao orquestrador Go, integrar o servico Python auxiliar de
classificacao emocional, e validar o pipeline completo end-to-end com um unico agente
conversando com um LLM real.

**Este e o primeiro milestone onde o sistema faz algo util de verdade.**

---

## 3.2 Entregavel 3.1 - Integracao gRPC Go <-> Rust (Semana 8-9)

### 3.2.1 Comunicacao via Unix Domain Socket

Para latencia minima em ambiente containerizado (mesma maquina), usar Unix socket:

```yaml
# docker-compose.yml (update)
services:
  emotion-engine:
    # ...
    volumes:
      - grpc_socket:/var/run/emotion-engine
    environment:
      GRPC_SOCKET_PATH: /var/run/emotion-engine/engine.sock

  orchestrator:
    # ...
    volumes:
      - grpc_socket:/var/run/emotion-engine
    environment:
      EMOTION_ENGINE_ADDR: unix:///var/run/emotion-engine/engine.sock

volumes:
  grpc_socket:
```

**Rust - escutar em Unix socket:**
```rust
// emotion-engine/src/main.rs
use tokio::net::UnixListener;
use tonic::transport::server::UdsConnectInfo;

async fn serve_unix(socket_path: &str) -> Result<(), Box<dyn std::error::Error>> {
    let _ = std::fs::remove_file(socket_path); // Remover socket antigo
    let uds = UnixListener::bind(socket_path)?;
    let uds_stream = tokio_stream::wrappers::UnixListenerStream::new(uds);

    Server::builder()
        .add_service(EmotionEngineServiceServer::new(EmotionEngine::default()))
        .serve_with_incoming(uds_stream)
        .await?;
    Ok(())
}
```

**Go - conectar via Unix socket:**
```go
conn, err := grpc.NewClient(
    "unix:///var/run/emotion-engine/engine.sock",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
```

### 3.2.2 Propagacao de Trace IDs

```go
// Go - injetar trace ID no metadata gRPC
import "go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"

conn, err := grpc.NewClient(addr,
    grpc.WithUnaryInterceptor(otelgrpc.UnaryClientInterceptor()),
    grpc.WithStreamInterceptor(otelgrpc.StreamClientInterceptor()),
)
```

```rust
// Rust - extrair trace ID do metadata gRPC
// Via tonic middleware que le o header `traceparent` e propaga para tracing spans
use tracing_opentelemetry::OpenTelemetrySpanExt;
use opentelemetry::propagation::TextMapPropagator;
```

### 3.2.3 Error Handling Cross-Language

Mapear erros Rust para gRPC status codes consistentes:

| Erro Rust | gRPC Status | Descricao |
|-----------|-------------|-----------|
| Validacao de input (W invalida, vetor vazio) | `INVALID_ARGUMENT` | Client deve corrigir request |
| Estado FSM desconhecido | `NOT_FOUND` | Estado nao existe na config |
| Restricao Omega bloqueou transicao | `FAILED_PRECONDITION` | Transicao nao permitida agora |
| Erro interno (panic, overflow) | `INTERNAL` | Bug no motor |
| Config nao carregada | `UNAVAILABLE` | Servico nao esta pronto |

```rust
// Rust - converter erros de dominio para gRPC Status
impl From<FsmError> for tonic::Status {
    fn from(err: FsmError) -> Self {
        match err {
            FsmError::UnknownState(s) => Status::not_found(format!("Unknown state: {}", s)),
            FsmError::ConstraintBlocked(reason) => Status::failed_precondition(reason),
            FsmError::InvalidConfig(msg) => Status::invalid_argument(msg),
        }
    }
}
```

### 3.2.4 Testes de Integracao gRPC

```go
// orchestrator/internal/connector/emotion/integration_test.go
//go:build integration

package emotion_test

import (
    "context"
    "testing"
    "time"

    pb "github.com/swarm-emotions/orchestrator/pkg/proto/emotion_engine/v1"
)

// Requer: motor Rust rodando (docker compose up emotion-engine)

func TestTransitionState_HappyPath(t *testing.T) {
    client := mustConnect(t)
    defer client.Close()

    resp, err := client.TransitionState(context.Background(), &pb.TransitionStateRequest{
        CurrentState: &pb.FsmState{StateName: "neutral"},
        Stimulus:     "praise",
        AgentId:      "test-agent-1",
    })
    if err != nil {
        t.Fatalf("TransitionState failed: %v", err)
    }

    if resp.NewState.StateName != "joyful" {
        t.Errorf("Expected joyful, got %s", resp.NewState.StateName)
    }
    if !resp.TransitionOccurred {
        t.Error("Expected transition to occur")
    }
}

func TestProcessInteraction_FullPipeline(t *testing.T) {
    client := mustConnect(t)
    defer client.Close()

    resp, err := client.ProcessInteraction(context.Background(), &pb.ProcessInteractionRequest{
        AgentId: "test-agent-1",
        CurrentFsmState: &pb.FsmState{StateName: "neutral"},
        CurrentEmotion:  &pb.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
        Stimulus:        "praise",
        StimulusVector:  &pb.EmotionVector{Components: []float32{0.7, 0.5, 0.3, 0.4, 0.6, 0.2}},
        WMatrix: &pb.SusceptibilityMatrix{
            Values:    scaleIdentity(6, 0.1),
            Dimension: 6,
        },
        Baseline:    &pb.EmotionVector{Components: []float32{0, 0, 0, 0, 0, 0}},
        DecayLambda: 0.1,
        DeltaTime:   1.0,
        // Score candidates para fusion
        ScoreCandidates: []*pb.ScoreCandidate{
            {MemoryId: "mem-1", SemanticScore: 0.8, EmotionalScore: 0.3, CognitiveScore: 0.5},
            {MemoryId: "mem-2", SemanticScore: 0.4, EmotionalScore: 0.9, CognitiveScore: 0.2},
        },
        Alpha: 0.5, Beta: 0.3, Gamma: 0.2,
    })
    if err != nil {
        t.Fatalf("ProcessInteraction failed: %v", err)
    }

    // Verificar que FSM transitou
    if resp.NewFsmState.StateName == "" {
        t.Error("Expected new FSM state")
    }

    // Verificar que vetor emocional foi computado
    if len(resp.NewEmotion.Components) != 6 {
        t.Errorf("Expected 6 components, got %d", len(resp.NewEmotion.Components))
    }

    // Verificar que ranking foi produzido
    if len(resp.RankedMemories) != 2 {
        t.Errorf("Expected 2 ranked memories, got %d", len(resp.RankedMemories))
    }
}
```

---

## 3.3 Entregavel 3.2 - Servico Python Auxiliar (Semana 9-10)

### 3.3.1 Classificador Emocional

```python
# python-ml/app/classifier.py
from transformers import pipeline
import numpy as np

# Mapeamento GoEmotions (27 labels) -> vetor 6D (VAD + certeza + social + novidade)
# Baseado em mapeamento manual dos labels para coordenadas VAD
GOEMOTIONS_TO_VAD = {
    "admiration":    [0.6, 0.4, 0.3, 0.5, 0.7, 0.3],
    "amusement":     [0.8, 0.6, 0.5, 0.6, 0.5, 0.5],
    "anger":         [-0.7, 0.8, 0.4, 0.5, -0.5, -0.2],
    "annoyance":     [-0.5, 0.5, 0.3, 0.4, -0.3, -0.3],
    "approval":      [0.5, 0.2, 0.4, 0.7, 0.6, 0.0],
    "caring":        [0.5, 0.3, 0.2, 0.4, 0.9, 0.1],
    "confusion":     [-0.2, 0.4, -0.3, -0.8, 0.1, 0.6],
    "curiosity":     [0.3, 0.6, 0.2, -0.2, 0.3, 0.9],
    "desire":        [0.4, 0.6, 0.3, 0.3, 0.4, 0.4],
    "disappointment":[-0.6, 0.3, -0.3, 0.4, 0.0, -0.3],
    "disapproval":   [-0.5, 0.4, 0.3, 0.5, -0.4, -0.2],
    "disgust":       [-0.7, 0.5, 0.3, 0.6, -0.6, -0.3],
    "embarrassment": [-0.5, 0.5, -0.5, 0.3, 0.2, 0.3],
    "excitement":    [0.7, 0.9, 0.4, 0.3, 0.4, 0.7],
    "fear":          [-0.7, 0.8, -0.6, -0.5, 0.1, 0.5],
    "gratitude":     [0.7, 0.4, 0.2, 0.6, 0.8, 0.1],
    "grief":         [-0.8, 0.3, -0.5, 0.4, 0.2, -0.4],
    "joy":           [0.9, 0.7, 0.6, 0.7, 0.5, 0.3],
    "love":          [0.8, 0.5, 0.3, 0.5, 0.9, 0.2],
    "nervousness":   [-0.4, 0.7, -0.5, -0.6, 0.1, 0.4],
    "neutral":       [0.0, 0.0, 0.0, 0.5, 0.0, 0.0],
    "optimism":      [0.6, 0.5, 0.4, 0.5, 0.4, 0.4],
    "pride":         [0.6, 0.5, 0.7, 0.6, 0.3, 0.2],
    "realization":   [0.2, 0.4, 0.3, 0.7, 0.1, 0.8],
    "relief":        [0.5, -0.3, 0.4, 0.6, 0.2, 0.0],
    "remorse":       [-0.6, 0.3, -0.3, 0.5, 0.4, -0.2],
    "sadness":       [-0.7, -0.3, -0.5, 0.4, 0.2, -0.4],
    "surprise":      [0.1, 0.8, -0.1, -0.6, 0.2, 0.9],
}


class EmotionClassifier:
    def __init__(self, model_name: str = "monologg/bert-base-cased-goemotions-original"):
        self.pipe = pipeline(
            "text-classification",
            model=model_name,
            top_k=5,  # Top 5 emotions
            device="cpu",
        )

    def classify(self, text: str) -> dict:
        """Classifica texto e retorna vetor emocional 6D ponderado."""
        results = self.pipe(text)

        # Combinar top-K emocoes ponderadas por confidence
        vector = np.zeros(6, dtype=np.float32)
        total_weight = 0.0

        for r in results:
            label = r["label"]
            score = r["score"]
            if label in GOEMOTIONS_TO_VAD:
                vad = np.array(GOEMOTIONS_TO_VAD[label], dtype=np.float32)
                vector += vad * score
                total_weight += score

        if total_weight > 0:
            vector /= total_weight

        # Clamp para [-1, 1]
        vector = np.clip(vector, -1.0, 1.0)

        top_label = results[0]["label"]
        top_confidence = results[0]["score"]

        return {
            "emotion_vector": vector.tolist(),
            "label": top_label,
            "confidence": float(top_confidence),
        }
```

### 3.3.2 Endpoint FastAPI Completo

```python
# python-ml/app/main.py
import logging
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from pydantic import BaseModel

from .classifier import EmotionClassifier

logger = logging.getLogger(__name__)

classifier: EmotionClassifier | None = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global classifier
    logger.info("Loading emotion classifier model...")
    classifier = EmotionClassifier()
    logger.info("Model loaded successfully")
    yield
    logger.info("Shutting down")


app = FastAPI(title="EmotionML Service", version="0.1.0", lifespan=lifespan)


class ClassifyRequest(BaseModel):
    text: str


class ClassifyResponse(BaseModel):
    emotion_vector: list[float]
    label: str
    confidence: float


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool


@app.get("/health", response_model=HealthResponse)
async def health():
    return HealthResponse(status="ok", model_loaded=classifier is not None)


@app.post("/classify-emotion", response_model=ClassifyResponse)
async def classify_emotion(req: ClassifyRequest):
    if classifier is None:
        raise HTTPException(status_code=503, detail="Model not loaded yet")
    if not req.text.strip():
        raise HTTPException(status_code=400, detail="Text cannot be empty")

    result = classifier.classify(req.text)
    return ClassifyResponse(**result)
```

### 3.3.3 Cache de Classificacoes

```go
// orchestrator/internal/connector/classifier/cached_client.go
package classifier

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "time"

    "github.com/redis/go-redis/v9"
)

// CachedClient wraps o client Python com cache Redis.
// Mesmo texto -> mesmo vetor emocional (determinístico).
type CachedClient struct {
    inner  Client
    redis  *redis.Client
    ttl    time.Duration
}

func (c *CachedClient) ClassifyEmotion(ctx context.Context, text string) (*EmotionClassification, error) {
    // Hash do texto como chave de cache
    hash := sha256.Sum256([]byte(text))
    key := fmt.Sprintf("emotion_cache:%s", hex.EncodeToString(hash[:]))

    // Tentar cache primeiro
    cached, err := c.redis.Get(ctx, key).Bytes()
    if err == nil {
        var result EmotionClassification
        if json.Unmarshal(cached, &result) == nil {
            return &result, nil
        }
    }

    // Cache miss: chamar Python
    result, err := c.inner.ClassifyEmotion(ctx, text)
    if err != nil {
        return nil, err
    }

    // Gravar no cache
    data, _ := json.Marshal(result)
    c.redis.Set(ctx, key, data, c.ttl) // fire-and-forget

    return result, nil
}
```

### 3.3.4 Golden Dataset para Paridade Futura

```python
# python-ml/scripts/generate_golden_dataset.py
"""
Gera dataset de referencia para validacao de paridade ONNX.
Executar ANTES de migrar para Rust (Fase 7).
Salvar output como fixture de teste.
"""
import json
from app.classifier import EmotionClassifier

TEXTS = [
    "I'm so happy to see you!",
    "This is really frustrating and annoying.",
    "I'm worried about the deadline tomorrow.",
    "Thank you so much for your help!",
    "I don't understand what's happening here.",
    "That's an interesting perspective, tell me more.",
    "I feel terrible about what happened.",
    "Great job on completing the project!",
    "I'm scared this won't work out.",
    "Everything is fine, nothing special.",
    # ... adicionar 990+ textos variados
]

def main():
    classifier = EmotionClassifier()
    results = []
    for text in TEXTS:
        result = classifier.classify(text)
        results.append({
            "text": text,
            "emotion_vector": result["emotion_vector"],
            "label": result["label"],
            "confidence": result["confidence"],
        })

    with open("golden_dataset.json", "w") as f:
        json.dump(results, f, indent=2)

    print(f"Generated {len(results)} golden test cases")

if __name__ == "__main__":
    main()
```

---

## 3.4 Entregavel 3.3 - Pipeline E2E Single-Agent (Semana 10-11)

### 3.4.1 Fluxo Completo Conectado

```
Request HTTP ──▶ Go API Gateway
                     │
                     ▼
               Go: Busca estado (Redis)
               Go: Busca config (PostgreSQL)
                     │
                     ▼
               Go: Chama Python (classify-emotion)
               Go: Determina tipo de estimulo
                     │
                     ▼
               Go ──gRPC──▶ Rust: FSM transition
                            Rust: Compute emotion vector
               Go ◀──gRPC── Rust: retorna novo estado + vetor
                     │
                     ▼
               Go: Atualiza Redis com novo estado
               Go: 3 goroutines paralelas:
                   ├── Query Qdrant semantico
                   ├── Query Qdrant emocional
                   └── Query PostgreSQL cognitivo
                     │
                     ▼
               Go ──gRPC──▶ Rust: Fuse scores
               Go ◀──gRPC── Rust: retorna ranking
                     │
                     ▼
               Go: Constroi prompt cognitivo (3 camadas)
               Go: Chama LLM (streaming)
                     │
                     ▼
               Go: Retorna resposta ao cliente
               Go: (async) Chama Python + Rust para pos-processamento
```

### 3.4.2 Seed de Agente para Teste

```sql
-- seed para teste E2E
INSERT INTO agent_configs (agent_id, display_name, baseline, w_matrix, w_dimension, fsm_transitions, weights, decay_lambda)
VALUES (
    'test-agent-001',
    'Agente de Teste',
    '{"components": [0.0, 0.0, 0.0, 0.5, 0.0, 0.0]}',
    '[0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1]',
    6,
    '{"rules": []}',  -- usa default FSM do motor Rust
    '{"alpha": 0.5, "beta": 0.3, "gamma": 0.2}',
    0.1
);

INSERT INTO cognitive_contexts (agent_id, active_goals, beliefs, norms, conversation_phase)
VALUES (
    'test-agent-001',
    '[{"id": "g1", "description": "Ajudar o usuario", "priority": 0.9}]',
    '{"user_expertise": "intermediate"}',
    '{"formality_level": 0.5, "emotional_expressiveness": 0.6}',
    'greeting'
);
```

### 3.4.3 Testes E2E

```go
// e2e_test.go (na raiz ou em tests/)
//go:build e2e

package e2e_test

func TestConversation_20Turns_EmotionCoherence(t *testing.T) {
    // Cenario: 20 turnos de conversa com emocoes variadas
    // Verificar que:
    // 1. Estado emocional muda coerentemente
    // 2. Nenhum erro 500 durante toda a conversa
    // 3. Latencia < 5s por turno (incluindo LLM)
    // 4. FSM nunca entra em estado invalido

    turns := []struct {
        input    string
        wantDir  string // direcao esperada: "positive", "negative", "neutral"
    }{
        {"Hello! I'm excited to start this project!", "positive"},
        {"Great, let's do it!", "positive"},
        {"Hmm, I'm getting a bit confused about the requirements.", "negative"},
        {"Actually, I think I made a big mistake.", "negative"},
        {"Wait, I found the solution! It works!", "positive"},
        // ... 15 mais turnos
    }

    var prevIntensity float32
    for i, turn := range turns {
        resp := postInteract(t, "test-agent-001", turn.input)
        assert.Equal(t, 200, resp.StatusCode)

        // Verificar coerencia direcional
        emotion := resp.Body.EmotionState
        valence := emotion.Components[0]

        switch turn.wantDir {
        case "positive":
            // Valencia deve ser positiva ou ter aumentado
            assert.True(t, valence > -0.3, "Turn %d: expected positive direction", i)
        case "negative":
            assert.True(t, valence < 0.3, "Turn %d: expected negative direction", i)
        }

        prevIntensity = resp.Body.Intensity
    }
}

func TestDeterminism_SameInputsSameOutputs(t *testing.T) {
    // Modo deterministico (noise=false):
    // Mesma sequencia de inputs deve produzir mesma sequencia de estados.
    // Reset agente, executar 5 turnos, gravar estados.
    // Reset agente, executar mesmos 5 turnos, comparar estados.
}

func TestLatency_PipelineWithoutLLM(t *testing.T) {
    // Medir latencia do pipeline usando LLM mock (resposta instantanea)
    // Meta: < 100ms
}
```

---

## 3.5 Checklist de Aceitacao

### Integracao gRPC
- [ ] Go conecta ao Rust via Unix domain socket
- [ ] Trace IDs propagam de Go para Rust (visivel nos logs)
- [ ] Erros Rust aparecem como gRPC status codes adequados no Go
- [ ] Todas as 5 RPCs funcionam Go -> Rust
- [ ] ProcessInteraction batch executa FSM + vector + fusion em 1 chamada

### Servico Python
- [ ] `POST /classify-emotion` retorna vetor 6D para textos variados
- [ ] Health check indica modelo carregado
- [ ] Readiness probe funcional (Go espera Python estar pronto)
- [ ] Cache Redis de classificacoes funcional (hit ratio > 0 em testes)
- [ ] Golden dataset gerado com 1000+ textos (fixture para Fase 7)

### Pipeline E2E
- [ ] Pipeline completo funciona: HTTP -> Go -> Python -> Rust -> Qdrant -> LLM -> response
- [ ] Latencia < 5s por turno (com LLM real)
- [ ] Latencia < 100ms (sem LLM)
- [ ] Estado emocional evolui coerentemente em 20+ turnos
- [ ] Modo deterministico: mesmos inputs -> mesmos estados
- [ ] Logs com trace distribuido completo (request ID -> Go -> Rust -> Python)

### Resiliencia
- [ ] Python indisponivel -> fallback para vetor neutro (nao crash)
- [ ] Rust indisponivel -> erro claro retornado ao cliente
- [ ] Redis indisponivel -> erro claro (nao silencioso)

---

## 3.6 Riscos Especificos

| Risco | Prob. | Impacto | Mitigacao |
|-------|-------|---------|-----------|
| Python SPOF (modelo cai, GIL, OOM) | Media | Alto | Cache + fallback neutro + 2 replicas |
| Cold start Python 10-30s | Alta | Medio | Readiness probe + Go espera |
| Divergencia futura ONNX vs Python | Certa | Medio | Golden dataset criado agora |
| Serialization overhead no proto | Baixa | Baixo | Enviar scores, nao vetores raw |
| Mapeamento GoEmotions->VAD impreciso | Alta | Medio | Mapeamento manual revisado + testes com humanos |

---

## 3.7 Continuacao Executada (2026-03-09)

Objetivo desta continuacao: preparar a entrada do LLM real na Fase 3 com foco
inicial em `Qwen/Qwen3.5-27B`, sem bloquear o restante do pipeline E2E.

### 3.7.1 Foundation entregue

- Provider LLM real adicionado no orquestrador via API `openai-compatible`.
- Connector do classifier no Go preparado com:
  - cache Redis de classificacoes
  - fallback para vetor neutro em falha runtime do Python
- Configuracao de geracao externalizada em env:
  - `LLM_PROVIDER`
  - `LLM_BASE_URL`
  - `LLM_MODEL`
  - `LLM_SYSTEM_PROMPT`
  - `LLM_MAX_TOKENS`
  - `LLM_TEMPERATURE`
  - `LLM_TOP_P`
  - `LLM_TOP_K`
  - `LLM_PRESENCE_PENALTY`
  - `LLM_ENABLE_THINKING`
- `docker-compose.yml` preparado para consumir um servidor LLM local exposto no host.
- `python-ml` preparado com dois modos:
  - `heuristic` para smoke/dev
  - `transformers` para o modelo real de GoEmotions
- `README.md` atualizado com o bootstrap do Qwen local no modo recomendado para o pipeline atual.

### 3.7.2 Decisao tecnica para o primeiro corte

- Modelo alvo inicial: `Qwen/Qwen3.5-27B`
- Modo inicial: `LLM_ENABLE_THINKING=false`
- Perfil de resposta inicial:
  - `LLM_MAX_TOKENS=256`
  - `LLM_TEMPERATURE=0.2`
  - foco em respostas curtas e coerentes, nao em cadeias longas de raciocinio

Motivo: este pipeline ainda constroi um prompt simples e espera uma resposta
textual curta; o primeiro objetivo e integrar um LLM real com latencia
controlada antes de evoluir para streaming, traces distribuidos e tuning fino.

### 3.7.3 Sequencia recomendada a partir daqui

1. Subir um servidor local OpenAI-compatible para `Qwen/Qwen3.5-27B`.
2. Rodar o orquestrador com `LLM_PROVIDER=openai-compatible`.
3. Validar `GET /ready` e um `POST /api/v1/interact` com Rust/Python reais.
4. Medir latencia por turno com `LLM_MAX_TOKENS=256`.
5. So depois disso seguir para:
   - Unix domain socket Go <-> Rust
   - cache Redis do classifier
   - testes E2E de 20 turnos

### 3.7.4 Pendencias ainda abertas da Fase 3

- Python classifier real ja esta preparado, mas ainda depende de execucao com extras `ml` e validacao E2E no ambiente alvo.
- Trace distribuido Go -> Rust ainda nao foi ligado.
- Transporte Go -> Rust ainda usa TCP; Unix socket segue pendente.
- Suite E2E single-agent com LLM real ainda nao foi implementada.

---

## 3.7 Transicao para Fase 4 e 5

Ao final da Fase 3:
- O sistema funciona end-to-end com um unico agente
- O pipeline e validado com conversas reais
- Golden dataset existe para futura migracao ONNX

A **Fase 4** implementa stores vetoriais e hierarquia de memoria (L1/L2/L3).
A **Fase 5** implementa a camada cognitiva e prompt estruturado.
Ambas podem iniciar em paralelo parcial.
