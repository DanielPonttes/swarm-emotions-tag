# FASE 4 - Memoria Hierarquica

> **Duracao estimada:** 5 semanas (Semana 10-14)
> **Equipe minima:** 1-2 engenheiros
> **Pre-requisitos:** Fase 3 concluida (pipeline E2E funcional)
> **Resultado:** Stores vetoriais configurados, hierarquia L1/L2/L3, motor de promocao, decaimento
> **Paralelizavel com:** Fase 5 (parcial)

---

## 4.1 Objetivo

Implementar o sistema de memoria hierarquica do EmotionRAG:
- **L1 (Working Memory):** Buffer volatil em Redis com TTL automatico
- **L2 (Episodica):** Memorias de medio prazo em Qdrant + PostgreSQL
- **L3 (Pseudopermanente):** Memorias de longo prazo ancoradas em emocao

Cada memoria e uma tupla multimodal: (vetor_semantico, vetor_emocional, intensidade, timestamp, metadata).

---

## 4.2 Decisao Arquitetural: Named Vectors vs Collections Separadas

### Opcao Escolhida: Named Vectors (Qdrant >= 1.7)

Usar uma **unica collection** com named vectors elimina o risco R2 (inconsistencia
entre stores separados) por design.

```
Collection: agent_memories
├── Named Vector "semantic" (768d) - embedding semantico do conteudo
├── Named Vector "emotional" (6d)  - vetor emocional no momento da criacao
└── Payload:
    ├── agent_id: string
    ├── memory_level: int (1, 2, 3)
    ├── intensity: float
    ├── content_text: string
    ├── content_hash: string
    ├── is_pseudopermanent: bool
    ├── access_count: int
    ├── created_at: int64 (unix ms)
    ├── last_accessed_at: int64
    └── metadata: object
```

### Vantagens desta abordagem
- **Atomicidade:** inserir/deletar um ponto garante consistencia entre vetores
- **Simplificacao:** uma query com dois named vectors, nao duas queries separadas
- **Performance:** Qdrant otimiza indices HNSW separadamente por named vector

---

## 4.3 Setup do Qdrant

### 4.3.1 Criacao da Collection

```go
// internal/connector/vectorstore/setup.go
package vectorstore

import (
    "context"

    pb "github.com/qdrant/go-client/qdrant"
)

func (c *Client) EnsureCollection(ctx context.Context) error {
    _, err := c.collections.Create(ctx, &pb.CreateCollection{
        CollectionName: "agent_memories",
        VectorsConfig: &pb.VectorsConfig{
            Config: &pb.VectorsConfig_ParamsMap{
                ParamsMap: &pb.VectorParamsMap{
                    Map: map[string]*pb.VectorParams{
                        "semantic": {
                            Size:     768, // all-MiniLM-L6-v2
                            Distance: pb.Distance_Cosine,
                            HnswConfig: &pb.HnswConfigDiff{
                                M:              uintPtr(16),
                                EfConstruct:    uintPtr(200),
                            },
                        },
                        "emotional": {
                            Size:     6,
                            Distance: pb.Distance_Cosine,
                            HnswConfig: &pb.HnswConfigDiff{
                                M:              uintPtr(8),   // Menor para baixa dim
                                EfConstruct:    uintPtr(100),
                            },
                        },
                    },
                },
            },
        },
    })
    if err != nil {
        // Collection ja existe - ok
        return nil
    }

    // Criar indices de payload para filtragem eficiente
    c.createPayloadIndex(ctx, "agent_memories", "agent_id", pb.FieldType_FieldTypeKeyword)
    c.createPayloadIndex(ctx, "agent_memories", "memory_level", pb.FieldType_FieldTypeInteger)
    c.createPayloadIndex(ctx, "agent_memories", "intensity", pb.FieldType_FieldTypeFloat)
    c.createPayloadIndex(ctx, "agent_memories", "created_at", pb.FieldType_FieldTypeInteger)

    return nil
}
```

### 4.3.2 Insercao de Memoria

```go
// internal/connector/vectorstore/upsert.go
package vectorstore

func (c *Client) UpsertMemory(ctx context.Context, mem Memory) error {
    pointID := uuid.New().String()

    _, err := c.points.Upsert(ctx, &pb.UpsertPoints{
        CollectionName: "agent_memories",
        Points: []*pb.PointStruct{
            {
                Id: &pb.PointId{
                    PointIdOptions: &pb.PointId_Uuid{Uuid: pointID},
                },
                Vectors: &pb.Vectors{
                    VectorsOptions: &pb.Vectors_Vectors{
                        Vectors: &pb.NamedVectors{
                            Vectors: map[string]*pb.Vector{
                                "semantic": {
                                    Data: mem.SemanticVector,
                                },
                                "emotional": {
                                    Data: mem.EmotionalVector,
                                },
                            },
                        },
                    },
                },
                Payload: map[string]*pb.Value{
                    "agent_id":            stringValue(mem.AgentID),
                    "memory_level":        intValue(int64(mem.Level)),
                    "intensity":           floatValue(float64(mem.Intensity)),
                    "content_text":        stringValue(mem.ContentText),
                    "content_hash":        stringValue(mem.ContentHash),
                    "is_pseudopermanent":  boolValue(mem.IsPseudoPermanent),
                    "access_count":        intValue(0),
                    "created_at":          intValue(mem.CreatedAt.UnixMilli()),
                    "last_accessed_at":    intValue(mem.CreatedAt.UnixMilli()),
                },
            },
        },
    })
    return err
}
```

### 4.3.3 Query Semantica com Filtro

```go
// internal/connector/vectorstore/query.go
package vectorstore

func (c *Client) QuerySemantic(ctx context.Context, params QuerySemanticParams) ([]MemoryHit, error) {
    resp, err := c.points.Search(ctx, &pb.SearchPoints{
        CollectionName: "agent_memories",
        Vector:         params.QueryEmbedding,
        VectorName:     stringPtr("semantic"),
        Limit:          uint64(params.TopK),
        Filter: &pb.Filter{
            Must: []*pb.Condition{
                fieldMatch("agent_id", params.AgentID),
                // Opcionalmente filtrar por nivel de memoria
            },
        },
        WithPayload: &pb.WithPayloadSelector{
            SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true},
        },
        Params: &pb.SearchParams{
            HnswEf: uintPtr(128), // ef para busca (tradeoff speed vs accuracy)
        },
    })
    if err != nil {
        return nil, err
    }

    return convertHits(resp.Result), nil
}

func (c *Client) QueryEmotional(ctx context.Context, params QueryEmotionalParams) ([]MemoryHit, error) {
    resp, err := c.points.Search(ctx, &pb.SearchPoints{
        CollectionName: "agent_memories",
        Vector:         params.EmotionVector,
        VectorName:     stringPtr("emotional"),
        Limit:          uint64(params.TopK),
        Filter: &pb.Filter{
            Must: []*pb.Condition{
                fieldMatch("agent_id", params.AgentID),
            },
        },
        WithPayload: &pb.WithPayloadSelector{
            SelectorOptions: &pb.WithPayloadSelector_Enable{Enable: true},
        },
    })
    if err != nil {
        return nil, err
    }

    return convertHits(resp.Result), nil
}
```

---

## 4.4 L1 - Working Memory (Redis)

### 4.4.1 Estruturas Redis por Agente

```
Chaves Redis:
├── emotion_state:{agent_id}          -> JSON: {emotion, fsm_state, updated_at}
├── working_memory:{agent_id}         -> Sorted Set (score = timestamp, member = JSON da memoria)
├── working_memory_count:{agent_id}   -> Integer (controle de tamanho)
└── agent_lock:{agent_id}             -> String com TTL (lock para concorrencia)
```

```go
// internal/connector/cache/working_memory.go
package cache

const (
    workingMemoryMaxSize = 20           // Max memorias L1 por agente
    workingMemoryTTL     = 5 * time.Minute // TTL de cada entrada
)

type WorkingMemoryEntry struct {
    Text            string    `json:"text"`
    EmotionVector   []float32 `json:"emotion_vector"`
    Intensity       float32   `json:"intensity"`
    Timestamp       int64     `json:"timestamp"`
    Role            string    `json:"role"` // "user" ou "agent"
}

// PushWorkingMemory adiciona entrada na working memory do agente.
// Usa sorted set com score = timestamp para ordenacao por recencia.
// Remove entradas mais antigas se exceder maxSize.
func (c *Client) PushWorkingMemory(ctx context.Context, agentID string, entry WorkingMemoryEntry) error {
    key := fmt.Sprintf("working_memory:%s", agentID)

    data, _ := json.Marshal(entry)

    pipe := c.rdb.Pipeline()

    // Adicionar com score = timestamp
    pipe.ZAdd(ctx, key, redis.Z{
        Score:  float64(entry.Timestamp),
        Member: string(data),
    })

    // Manter apenas as ultimas N entradas
    pipe.ZRemRangeByRank(ctx, key, 0, int64(-workingMemoryMaxSize-1))

    // Renovar TTL
    pipe.Expire(ctx, key, workingMemoryTTL)

    _, err := pipe.Exec(ctx)
    return err
}

// GetWorkingMemory retorna as memorias L1 mais recentes.
func (c *Client) GetWorkingMemory(ctx context.Context, agentID string) ([]WorkingMemoryEntry, error) {
    key := fmt.Sprintf("working_memory:%s", agentID)

    results, err := c.rdb.ZRevRange(ctx, key, 0, workingMemoryMaxSize-1).Result()
    if err != nil {
        return nil, err
    }

    entries := make([]WorkingMemoryEntry, 0, len(results))
    for _, r := range results {
        var entry WorkingMemoryEntry
        if err := json.Unmarshal([]byte(r), &entry); err == nil {
            entries = append(entries, entry)
        }
    }
    return entries, nil
}
```

---

## 4.5 L2/L3 - Logica de Promocao

### 4.5.1 Pipeline de Armazenamento de Memoria

```go
// internal/pipeline/step_postprocess.go (atualizado)

// stepPostProcess decide onde armazenar a memoria da interacao
func (o *Orchestrator) stepPostProcess(
    ctx context.Context,
    input Input,
    llmResponse string,
    fsmResult *FSMResult,
    config *model.AgentConfig,
) {
    // 1. Classificar emocao da resposta gerada
    responseEmotion, _ := o.classifier.ClassifyEmotion(ctx, llmResponse)

    // 2. Calcular intensidade da interacao
    interactionIntensity := fsmResult.NewIntensity

    // 3. Criar registro de memoria
    memory := model.Memory{
        AgentID:         input.AgentID,
        ContentText:     fmt.Sprintf("User: %s\nAgent: %s", input.Text, llmResponse),
        EmotionalVector: fsmResult.NewEmotion.Components,
        Intensity:       interactionIntensity,
        Level:           1, // Comecar em L1
        CreatedAt:       time.Now(),
    }

    // 4. Gravar em L1 (Redis working memory)
    o.cache.PushWorkingMemory(ctx, input.AgentID, toWorkingMemoryEntry(memory))

    // 5. Avaliar promocao imediata para L2
    if interactionIntensity > config.PromotionThresholds.L2Threshold {
        memory.Level = 2
        o.storeInQdrant(ctx, memory)
        o.logPromotion(ctx, memory, "immediate_intensity")
    }

    // 6. Chamar Rust para avaliacao de promocao L2 -> L3
    // (para memorias L2 existentes que podem ter acumulado relevancia)
    go o.evaluateL2Promotions(ctx, input.AgentID, config)

    // 7. Log da interacao
    o.db.LogInteraction(ctx, &model.InteractionLog{
        AgentID:       input.AgentID,
        InputText:     input.Text,
        LLMResponse:   llmResponse,
        EmotionBefore: fsmResult.PrevEmotion,
        EmotionAfter:  fsmResult.NewEmotion,
        Intensity:     interactionIntensity,
    })
}
```

### 4.5.2 Avaliacao Periodica de Promocoes L2 -> L3

```go
// internal/pipeline/promotion.go

// evaluateL2Promotions busca memorias L2 do agente e avalia promocao para L3
func (o *Orchestrator) evaluateL2Promotions(ctx context.Context, agentID string, config *model.AgentConfig) {
    // Buscar memorias L2 do agente
    l2Memories, err := o.vectorStore.GetMemoriesByLevel(ctx, agentID, 2, 100)
    if err != nil {
        slog.Error("Failed to get L2 memories", "error", err, "agent_id", agentID)
        return
    }

    if len(l2Memories) == 0 {
        return
    }

    // Preparar candidatos para avaliacao pelo Rust
    candidates := make([]*pb.MemoryForPromotion, 0, len(l2Memories))
    for _, m := range l2Memories {
        candidates = append(candidates, &pb.MemoryForPromotion{
            MemoryId:          m.ID,
            Intensity:         m.Intensity,
            CurrentLevel:      2,
            AccessFrequency:   uint32(m.AccessCount),
            ValenceMagnitude:  abs(m.EmotionalVector[0]), // |valencia|
        })
    }

    // Chamar Rust para avaliacao
    resp, err := o.emotionClient.EvaluatePromotion(ctx, &pb.EvaluatePromotionRequest{
        Memories:            candidates,
        IntensityThreshold:  config.PromotionThresholds.L3IntensityThreshold,
        FrequencyThreshold:  uint32(config.PromotionThresholds.L3FrequencyThreshold),
        ValenceThreshold:    config.PromotionThresholds.L3ValenceThreshold,
    })
    if err != nil {
        slog.Error("Failed to evaluate promotions", "error", err)
        return
    }

    // Aplicar promocoes
    for _, decision := range resp.Decisions {
        if decision.ShouldPromote {
            err := o.vectorStore.UpdateMemoryLevel(ctx, decision.MemoryId, 3)
            if err != nil {
                slog.Error("Failed to promote memory", "memory_id", decision.MemoryId, "error", err)
                continue
            }
            slog.Info("Memory promoted to L3",
                "memory_id", decision.MemoryId,
                "reason", decision.Reason,
                "agent_id", agentID,
            )
        }
    }
}
```

---

## 4.6 Decaimento Lazy (Query-Time)

Em vez de batch job O(N), calcular decaimento no momento da query:

```go
// internal/pipeline/step_retrieve.go (atualizado)

// applyDecayToScores aplica fator de decaimento temporal nos scores retornados
func applyDecayToScores(hits []model.MemoryHit, lambda float32, now time.Time) {
    for i := range hits {
        age := now.Sub(hits[i].CreatedAt).Seconds()
        decayFactor := float32(math.Exp(float64(-lambda) * age))

        // Ajuste por nivel: L3 decai muito mais devagar
        switch hits[i].Level {
        case 3:
            decayFactor = float32(math.Exp(float64(-lambda*0.01) * age)) // 100x mais lento
        case 2:
            decayFactor = float32(math.Exp(float64(-lambda*0.1) * age))  // 10x mais lento
        }

        hits[i].Score *= decayFactor
    }

    // Re-ordenar apos decaimento
    sort.Slice(hits, func(i, j int) bool {
        return hits[i].Score > hits[j].Score
    })
}
```

---

## 4.7 Garbage Collection de Memorias

```go
// internal/pipeline/gc.go

// StartMemoryGC inicia goroutine de garbage collection periodica
func StartMemoryGC(ctx context.Context, store VectorStoreClient, interval time.Duration) {
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            gcMemories(ctx, store)
        }
    }
}

func gcMemories(ctx context.Context, store VectorStoreClient) {
    now := time.Now()

    // L2: remover memorias com mais de 7 dias e access_count < 3
    store.DeleteByFilter(ctx, "agent_memories", Filter{
        MemoryLevel:      2,
        CreatedBefore:    now.Add(-7 * 24 * time.Hour),
        AccessCountBelow: 3,
    })

    // L1 em Qdrant: nao existe (L1 e Redis com TTL automatico)
    // L3: nao faz GC automatico (pseudopermanente por design)

    slog.Info("Memory GC completed")
}
```

---

## 4.8 Testes

### 4.8.1 Testes de Integracao Qdrant

```go
// internal/connector/vectorstore/integration_test.go
//go:build integration

func TestUpsertAndQuerySemantic(t *testing.T) {
    // Inserir 10 memorias com vetores semanticos conhecidos
    // Fazer query com vetor proximo de uma delas
    // Verificar que a mais proxima aparece no top-1
}

func TestUpsertAndQueryEmotional(t *testing.T) {
    // Inserir memorias com vetores emocionais distintos
    // Query com vetor emocional [0.8, 0.7, ...] (alegria)
    // Verificar que memorias "alegres" aparecem primeiro
}

func TestNamedVectorsConsistency(t *testing.T) {
    // Inserir 1 ponto com ambos os named vectors
    // Deletar o ponto
    // Verificar que ambos os vetores sumiram (atomicidade)
}

func TestFilterByAgentAndLevel(t *testing.T) {
    // Inserir memorias de 3 agentes em 3 niveis
    // Query com filtro agent_id=X e level=2
    // Verificar que so retorna memorias do agente X nivel 2
}
```

### 4.8.2 Teste de Promocao End-to-End

```go
func TestPromotionL1_to_L2_to_L3(t *testing.T) {
    // 1. Enviar interacao com alta intensidade emocional
    //    -> deve gravar em L2 direto (promocao imediata)
    //
    // 2. Enviar 10 interacoes que referenciam a mesma memoria
    //    -> access_count sobe
    //
    // 3. Rodar evaluateL2Promotions
    //    -> memoria deve ser promovida para L3
    //
    // 4. Verificar que memoria esta em L3 no Qdrant
}
```

### 4.8.3 Teste de Decaimento

```go
func TestDecayReducesScoreOverTime(t *testing.T) {
    // Inserir memoria com score 0.9
    // Aplicar decaimento com delta_t = 1 hora, lambda = 0.1
    // Score deve ser: 0.9 * exp(-0.1 * 3600) ≈ 0 (efetivamente eliminada)
    //
    // Aplicar com delta_t = 1 minuto
    // Score deve ser: 0.9 * exp(-0.1 * 60) ≈ 0.9 * 0.0025 (muito reduzido)
    //
    // L3 com lambda * 0.01:
    // Score: 0.9 * exp(-0.001 * 3600) ≈ 0.9 * 0.027 (decai mais devagar)
}
```

---

## 4.9 Checklist de Aceitacao

> **Status atualizado em 2026-03-19 com base no codigo e nos testes do repositório**
>
> **Resumo:** o read path de memoria, a hierarquia L1/L2/L3, o decaimento lazy e o GC basico de L2 ja estao implementados; ainda faltam named vectors, indices de payload dedicados e metas de performance.

### Qdrant
- [ ] Collection `agent_memories` criada com named vectors (semantic 768d + emotional 6d)
- [ ] Indices de payload criados (agent_id, memory_level, intensity, created_at)
- [ ] Upsert atomico com ambos named vectors funcional
- [x] Query semantica top-K com filtro por agent_id retorna resultados corretos
- [x] Query emocional top-K com filtro por agent_id retorna resultados corretos
- [ ] Delete de ponto remove ambos named vectors

### Hierarquia L1/L2/L3
- [x] L1 em Redis: push/get working memory funcional com TTL
- [x] L1 auto-expira apos TTL (sem batch job)
- [x] L2 armazenado em Qdrant com level=2
- [x] L3 armazenado em Qdrant com level=3 e is_pseudopermanent=true
- [x] Promocao L1->L2 por intensidade imediata funcional
- [x] Promocao L2->L3 por avaliacao (Rust) funcional

### Decaimento
- [x] Decaimento lazy aplicado no query-time
- [x] L3 decai 100x mais lento que L1
- [x] Re-ordenacao apos decaimento mantida

### GC
- [x] GC periodico remove memorias L2 expiradas
- [x] L3 nao e afetado por GC
- [x] Logging de memorias removidas

### Performance
- [ ] Query paralela (semantico + emocional) < 30ms no Qdrant
- [ ] Insercao de memoria < 10ms
- [ ] GC de 10.000 memorias completa em < 5s

---

## 4.10 Riscos Especificos

| Risco | Prob. | Impacto | Mitigacao |
|-------|-------|---------|-----------|
| Qdrant named vectors requer >= 1.7 | Baixa | Alto | Validar versao no setup. Fallback: 2 collections. |
| Theta de promocao mal calibrado | Alta | Medio | Theta adaptativo (P90) ou fixo com cap de L3 |
| Troca de modelo de embedding | Media | Alto | Versionar collections. Re-embeddar na migracao. |
| Perda de memorias em crash entre L1->L2 | Baixa | Medio | Redis AOF + log de promocao em Postgres |
| HNSW tuning subotimo | Media | Baixo | Tuning pos-Fase 7 com profiling real |

---

## 4.11 Transicao para Fase 5 e 6

Ao final da Fase 4:
- Memorias sao armazenadas, recuperadas e promovidas na hierarquia L1/L2/L3
- Queries paralelas alimentam a fusao de scores no Rust
- Decaimento temporal funciona lazy (sem batch job pesado)

A **Fase 5** usa estas memorias para construir o prompt cognitivo estruturado.
A **Fase 6** estende a memoria para suportar multiplos agentes isolados.
