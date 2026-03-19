# FASE 5 - Camada Cognitiva + Geracao LLM

> **Duracao estimada:** 5 semanas (Semana 13-17)
> **Equipe minima:** 1-2 engenheiros
> **Pre-requisitos:** Fase 3 (pipeline E2E basico), Fase 4 (stores e memoria)
> **Resultado:** Prompt cognitivo estruturado em 3 camadas, integracao LLM com streaming,
>   traducao vetor->diretriz, pos-processamento async
> **Paralelizavel com:** Fase 4 (parcial, se stores basicos ja estiverem prontos)

---

## 5.1 Objetivo

Transformar o prompt plano usado na Fase 3 em um **prompt cognitivo estruturado**
com tres camadas (cenario semantico, estado interno, ressonancia de memoria).
Implementar a camada cognitiva que modela intencoes, crencas e normas do agente.
Integrar com LLM via streaming e pos-processamento assincrono.

---

## 5.2 Entregavel 5.1 - Contexto Cognitivo (Semana 13-14)

### 5.2.1 Schema PostgreSQL

```sql
-- Ja criado na Fase 0 (docker/init.sql). Aqui detalhamos o uso.

-- cognitive_contexts: um registro por agente, atualizado a cada interacao
-- active_goals: [{id, description, priority}]
-- beliefs: {user_expertise, task_complexity, time_pressure, ...}
-- norms: {formality_level, honesty_commitment, emotional_expressiveness}
-- conversation_phase: idle, greeting, problem_diagnosis, resolution, farewell
```

### 5.2.2 Atualizacao Cognitiva

```go
// internal/pipeline/step_cognitive.go
package pipeline

import (
    "context"
    "github.com/swarm-emotions/orchestrator/internal/model"
)

// stepUpdateCognitive atualiza o contexto cognitivo apos cada interacao.
// Usa heuristicas baseadas no estado emocional e conteudo da conversa.
func (o *Orchestrator) stepUpdateCognitive(
    ctx context.Context,
    agentID string,
    input string,
    emotion model.EmotionVector,
    currentPhase string,
) (*model.CognitiveContext, error) {

    cogCtx, err := o.db.GetCognitiveContext(ctx, agentID)
    if err != nil {
        return nil, err
    }

    // Inferir phase da conversa
    cogCtx.ConversationPhase = inferPhase(currentPhase, input, emotion)

    // Atualizar beliefs sobre o usuario
    cogCtx.Beliefs = updateBeliefs(cogCtx.Beliefs, input, emotion)

    // Ajustar prioridade dos goals baseado no contexto
    cogCtx.ActiveGoals = reprioritizeGoals(cogCtx.ActiveGoals, emotion, cogCtx.ConversationPhase)

    // Persistir
    if err := o.db.UpdateCognitiveContext(ctx, agentID, cogCtx); err != nil {
        return nil, err
    }

    return cogCtx, nil
}

// inferPhase determina a fase da conversa baseado em heuristicas
func inferPhase(current string, input string, emotion model.EmotionVector) string {
    // Heuristicas simples (Fase 5). Pode evoluir para classificador na Fase 7.
    intensity := emotion.Intensity()

    switch {
    case current == "idle":
        return "greeting"
    case containsAny(input, []string{"bye", "thanks", "goodbye", "tchau", "obrigado"}):
        return "farewell"
    case containsAny(input, []string{"error", "bug", "broken", "help", "problema", "erro"}):
        return "problem_diagnosis"
    case intensity < 0.3 && current == "problem_diagnosis":
        return "resolution"
    default:
        return current
    }
}

// updateBeliefs atualiza crencas sobre o usuario
func updateBeliefs(beliefs model.Beliefs, input string, emotion model.EmotionVector) model.Beliefs {
    // Estimar expertise pelo vocabulario
    if containsTechnicalTerms(input) {
        beliefs.UserExpertise = clampString(beliefs.UserExpertise, "intermediate", "advanced")
    }

    // Detectar pressao de tempo pela ativacao emocional
    if emotion.Arousal() > 0.7 && emotion.Valence() < 0.0 {
        beliefs.TimePressure = true
    }

    // Estimar estado emocional do usuario
    beliefs.UserEmotionalEstimate = emotion.Components

    return beliefs
}
```

### 5.2.3 Re-ranker Cognitivo

```go
// internal/pipeline/step_retrieve.go (complemento)

// applyCognitiveReranking ajusta scores baseado no contexto cognitivo
func applyCognitiveReranking(
    candidates []model.ScoreCandidate,
    cogCtx *model.CognitiveContext,
) {
    for i := range candidates {
        var cogBoost float32

        // Boost para memorias alinhadas com goals ativos
        for _, goal := range cogCtx.ActiveGoals {
            if isRelevantToGoal(candidates[i], goal) {
                cogBoost += 0.1 * goal.Priority
            }
        }

        // Penalidade para documentos tecnicos se usuario e iniciante
        if cogCtx.Beliefs.UserExpertise == "beginner" && candidates[i].IsHighlyTechnical {
            cogBoost -= 0.2
        }

        // Boost para respostas concisas se time_pressure
        if cogCtx.Beliefs.TimePressure && candidates[i].ContentLength < 200 {
            cogBoost += 0.1
        }

        candidates[i].CognitiveScore = clampFloat(cogBoost, 0.0, 1.0)
    }
}
```

---

## 5.3 Entregavel 5.2 - Prompt Cognitivo Estruturado (Semana 14-16)

### 5.3.1 Arquitetura do Prompt

O prompt e composto por 3 camadas, cada uma com budget de tokens alocado dinamicamente:

```
┌──────────────────────────────────────────────────────┐
│  SYSTEM PROMPT                                        │
│  ├── Identidade do agente (fixa, ~100 tokens)        │
│  ├── Estado Interno (dinamico, ~150-300 tokens)       │
│  └── Normas comportamentais (~50-100 tokens)          │
├──────────────────────────────────────────────────────┤
│  CONTEXT (documentos recuperados)                     │
│  ├── Camada 1: Cenario Semantico (~40% do budget)    │
│  ├── Camada 2: Ressonancia de Memoria (~30%)         │
│  └── Camada 3: Working Memory L1 (~20%)              │
├──────────────────────────────────────────────────────┤
│  USER MESSAGE                                         │
│  └── Input do usuario (~10% do budget)                │
└──────────────────────────────────────────────────────┘
```

### 5.3.2 Traducao Vetor -> Diretriz

```go
// internal/pipeline/emotion_directive.go
package pipeline

// EmotionDirective traduz vetor emocional em diretriz textual para o LLM.
// Usa lookup table com regioes prototipicas (NAO mapeamento linear).

// Regioes prototipicas no espaco VAD
type EmotionRegion struct {
    Name        string
    Center      [3]float32  // [V, A, D] - 3 primeiras dimensoes
    Radius      float32     // Raio de matching
    Directive   string      // Instrucao para o LLM
    ToneHints   []string    // Palavras-chave de tom
}

var emotionRegions = []EmotionRegion{
    {
        Name:   "panic",
        Center: [3]float32{-0.7, 0.9, -0.5},
        Radius: 0.4,
        Directive: "Voce esta em estado de alerta maximo. Suas respostas devem ser " +
            "curtas, diretas e focadas em resolucao imediata. Evite explicacoes longas. " +
            "Priorize acoes concretas.",
        ToneHints: []string{"urgente", "direto", "conciso"},
    },
    {
        Name:   "joyful_engaged",
        Center: [3]float32{0.8, 0.7, 0.5},
        Radius: 0.4,
        Directive: "Voce esta alegre e engajado. Suas respostas devem ser entusiastas, " +
            "encorajadoras e detalhadas. Demonstre interesse genuino e ofereca sugestoes " +
            "proativas.",
        ToneHints: []string{"entusiastico", "encorajador", "proativo"},
    },
    {
        Name:   "calm_analytical",
        Center: [3]float32{0.2, -0.3, 0.5},
        Radius: 0.4,
        Directive: "Voce esta calmo e analitico. Suas respostas devem ser equilibradas, " +
            "bem estruturadas e baseadas em evidencias. Mantenha tom neutro e profissional.",
        ToneHints: []string{"equilibrado", "estruturado", "objetivo"},
    },
    {
        Name:   "empathetic_concerned",
        Center: [3]float32{-0.3, 0.4, -0.2},
        Radius: 0.4,
        Directive: "Voce esta preocupado com a situacao. Suas respostas devem demonstrar " +
            "empatia explicita, validar sentimentos, e oferecer suporte antes de solucoes. " +
            "Pergunte como a pessoa esta se sentindo.",
        ToneHints: []string{"empatico", "acolhedor", "compreensivo"},
    },
    {
        Name:   "frustrated_but_trying",
        Center: [3]float32{-0.5, 0.6, 0.2},
        Radius: 0.4,
        Directive: "Voce esta um pouco frustrado mas determinado a resolver. Suas respostas " +
            "devem reconhecer a dificuldade, ser diretas sobre proximos passos, e evitar " +
            "otimismo falso. Seja honesto sobre desafios.",
        ToneHints: []string{"determinado", "realista", "direto"},
    },
    {
        Name:   "curious_exploratory",
        Center: [3]float32{0.3, 0.5, 0.2},
        Radius: 0.4,
        Directive: "Voce esta curioso e explorador. Suas respostas devem fazer perguntas " +
            "aprofundadas, explorar alternativas, e demonstrar fascinio genuino pelo tema. " +
            "Sugira angulos diferentes de analise.",
        ToneHints: []string{"curioso", "exploratorio", "questionador"},
    },
    {
        Name:   "neutral_baseline",
        Center: [3]float32{0.0, 0.0, 0.0},
        Radius: 0.5,  // Raio maior (catchall)
        Directive: "Mantenha tom neutro e profissional. Responda de forma clara e " +
            "objetiva, adaptando-se ao contexto da conversa.",
        ToneHints: []string{"neutro", "profissional", "claro"},
    },
}

// FindDirective encontra a diretriz mais adequada para o vetor emocional.
// Usa vizinho mais proximo no espaco VAD (3 primeiras dimensoes).
func FindDirective(emotion model.EmotionVector) *EmotionRegion {
    vad := [3]float32{emotion.Components[0], emotion.Components[1], emotion.Components[2]}

    var best *EmotionRegion
    bestDist := float32(math.MaxFloat32)

    for i := range emotionRegions {
        dist := euclideanDistance3(vad, emotionRegions[i].Center)
        if dist < emotionRegions[i].Radius && dist < bestDist {
            best = &emotionRegions[i]
            bestDist = dist
        }
    }

    if best == nil {
        // Fallback para neutral
        return &emotionRegions[len(emotionRegions)-1]
    }
    return best
}
```

### 5.3.3 Template do Prompt

```go
// internal/pipeline/step_prompt.go
package pipeline

import (
    "bytes"
    "text/template"
)

const systemPromptTemplate = `Voce e {{.AgentName}}, um agente com personalidade e emocoes.

## Estado Interno Atual
{{.EmotionDirective}}

Intensidade emocional: {{printf "%.1f" .Intensity}}/1.0
Estado: {{.FsmState}} ({{.MacroState}})
Fase da conversa: {{.ConversationPhase}}

## Normas Comportamentais
- Nivel de formalidade: {{printf "%.0f" (mul .Formality 100)}}%
- Expressividade emocional: {{printf "%.0f" (mul .Expressiveness 100)}}%
{{- if .TimePressure}}
- ATENCAO: O usuario parece estar com pressa. Seja conciso.
{{- end}}

## Memorias Relevantes (ressonancia emocional)
{{- range .ResonantMemories}}
- [Intensidade {{printf "%.2f" .Intensity}}] {{.Summary}}
{{- end}}
{{- if not .ResonantMemories}}
(Sem memorias emocionalmente ressonantes para este contexto)
{{- end}}`

const contextTemplate = `## Contexto Recuperado
{{- range $i, $doc := .Documents}}
### Documento {{add $i 1}} (relevancia: {{printf "%.0f" (mul $doc.Score 100)}}%)
{{$doc.Content}}
{{- end}}

## Conversa Recente
{{- range .WorkingMemory}}
{{.Role}}: {{.Text}}
{{- end}}`

// BuildPrompt constroi o prompt completo com budget de tokens
func (o *Orchestrator) BuildPrompt(params PromptParams) (system string, context string, err error) {
    // Calcular budget
    budget := calculateBudget(params.MaxTokens, params.Intensity)

    // Truncar documentos para caber no budget
    docs := truncateDocuments(params.RankedDocuments, budget.Context)
    memories := truncateMemories(params.ResonantMemories, budget.Memories)
    working := truncateWorking(params.WorkingMemory, budget.Working)

    // Renderizar system prompt
    var sysBuf bytes.Buffer
    sysTmpl := template.Must(template.New("system").Funcs(tmplFuncs).Parse(systemPromptTemplate))
    sysTmpl.Execute(&sysBuf, map[string]any{
        "AgentName":          params.AgentName,
        "EmotionDirective":   params.Directive.Directive,
        "Intensity":          params.Intensity,
        "FsmState":           params.FsmState,
        "MacroState":         params.MacroState,
        "ConversationPhase":  params.ConversationPhase,
        "Formality":          params.Norms.FormalityLevel,
        "Expressiveness":     params.Norms.EmotionalExpressiveness,
        "TimePressure":       params.Beliefs.TimePressure,
        "ResonantMemories":   memories,
    })

    // Renderizar contexto
    var ctxBuf bytes.Buffer
    ctxTmpl := template.Must(template.New("context").Funcs(tmplFuncs).Parse(contextTemplate))
    ctxTmpl.Execute(&ctxBuf, map[string]any{
        "Documents":     docs,
        "WorkingMemory": working,
    })

    return sysBuf.String(), ctxBuf.String(), nil
}

// calculateBudget distribui tokens entre as camadas.
// Se intensidade emocional alta, aloca mais para memorias emocionais.
func calculateBudget(maxTokens int, intensity float32) TokenBudget {
    contextRatio := float32(0.40)
    memoryRatio := float32(0.30)
    workingRatio := float32(0.20)
    systemRatio := float32(0.10)

    // Ajuste dinamico: intensidade alta -> mais memoria emocional
    if intensity > 0.7 {
        memoryRatio += 0.10
        contextRatio -= 0.10
    }

    available := maxTokens - 200 // reserva para system fixo
    return TokenBudget{
        Context:  int(float32(available) * contextRatio),
        Memories: int(float32(available) * memoryRatio),
        Working:  int(float32(available) * workingRatio),
        System:   int(float32(available) * systemRatio),
    }
}
```

---

## 5.4 Entregavel 5.3 - Integracao LLM com Streaming (Semana 15-17)

### 5.4.1 Client OpenAI

```go
// internal/connector/llm/openai.go
package llm

import (
    "context"
    "io"

    openai "github.com/sashabaranov/go-openai"
)

type OpenAIProvider struct {
    client *openai.Client
}

func NewOpenAIProvider(apiKey string) *OpenAIProvider {
    return &OpenAIProvider{
        client: openai.NewClient(apiKey),
    }
}

func (p *OpenAIProvider) Generate(ctx context.Context, prompt string, opts GenerateOpts) (string, error) {
    resp, err := p.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
        Model: opts.Model,
        Messages: []openai.ChatCompletionMessage{
            {Role: "system", Content: opts.SystemPrompt},
            {Role: "user", Content: prompt},
        },
        MaxTokens:   opts.MaxTokens,
        Temperature: opts.Temperature,
    })
    if err != nil {
        return "", err
    }
    return resp.Choices[0].Message.Content, nil
}

func (p *OpenAIProvider) GenerateStream(ctx context.Context, prompt string, opts GenerateOpts) (<-chan StreamChunk, error) {
    stream, err := p.client.CreateChatCompletionStream(ctx, openai.ChatCompletionRequest{
        Model: opts.Model,
        Messages: []openai.ChatCompletionMessage{
            {Role: "system", Content: opts.SystemPrompt},
            {Role: "user", Content: prompt},
        },
        MaxTokens:   opts.MaxTokens,
        Temperature: opts.Temperature,
        Stream:      true,
    })
    if err != nil {
        return nil, err
    }

    ch := make(chan StreamChunk, 64)
    go func() {
        defer close(ch)
        defer stream.Close()

        for {
            resp, err := stream.Recv()
            if err == io.EOF {
                ch <- StreamChunk{Done: true}
                return
            }
            if err != nil {
                ch <- StreamChunk{Error: err}
                return
            }
            if len(resp.Choices) > 0 {
                ch <- StreamChunk{Text: resp.Choices[0].Delta.Content}
            }
        }
    }()

    return ch, nil
}
```

### 5.4.2 Streaming SSE para o Cliente HTTP

```go
// internal/api/handler_interact.go (versao streaming)

func (h *Handlers) InteractStream(w http.ResponseWriter, r *http.Request) {
    // SSE headers
    w.Header().Set("Content-Type", "text/event-stream")
    w.Header().Set("Cache-Control", "no-cache")
    w.Header().Set("Connection", "keep-alive")

    flusher, ok := w.(http.Flusher)
    if !ok {
        http.Error(w, "streaming not supported", http.StatusInternalServerError)
        return
    }

    var req InteractRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        writeSSE(w, flusher, "error", `{"error":"invalid request"}`)
        return
    }

    // Executar pipeline ate step 6 (construir prompt)
    promptResult, err := h.pipeline.ExecuteUntilPrompt(r.Context(), pipeline.Input{
        AgentID: req.AgentID,
        Text:    req.Text,
    })
    if err != nil {
        writeSSE(w, flusher, "error", fmt.Sprintf(`{"error":"%s"}`, err))
        return
    }

    // Enviar metadata inicial
    writeSSE(w, flusher, "metadata", fmt.Sprintf(
        `{"fsm_state":"%s","emotion":%s,"intensity":%.3f}`,
        promptResult.FsmState, toJSON(promptResult.Emotion), promptResult.Intensity,
    ))

    // Stream da resposta LLM
    chunks, err := h.llm.GenerateStream(r.Context(), promptResult.UserPrompt, promptResult.LLMOpts)
    if err != nil {
        writeSSE(w, flusher, "error", fmt.Sprintf(`{"error":"%s"}`, err))
        return
    }

    var fullResponse strings.Builder
    for chunk := range chunks {
        if chunk.Error != nil {
            writeSSE(w, flusher, "error", fmt.Sprintf(`{"error":"%s"}`, chunk.Error))
            return
        }
        if chunk.Done {
            break
        }
        fullResponse.WriteString(chunk.Text)
        writeSSE(w, flusher, "chunk", fmt.Sprintf(`{"text":"%s"}`, escapeJSON(chunk.Text)))
    }

    writeSSE(w, flusher, "done", `{"status":"complete"}`)

    // Pos-processamento async (nao bloqueia o stream)
    go h.pipeline.PostProcess(context.Background(), req.AgentID, req.Text, fullResponse.String(), promptResult)
}

func writeSSE(w http.ResponseWriter, f http.Flusher, event, data string) {
    fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, data)
    f.Flush()
}
```

### 5.4.3 Pos-Processamento Assincrono

```go
// internal/pipeline/step_postprocess.go

// PostProcess executa apos a resposta ser enviada ao cliente.
// NAO bloqueia a resposta - roda em goroutine separada.
func (o *Orchestrator) PostProcess(
    ctx context.Context,
    agentID string,
    userInput string,
    llmResponse string,
    promptResult *PromptResult,
) {
    // 1. Classificar emocao da resposta (Python)
    responseEmotion, err := o.classifier.ClassifyEmotion(ctx, llmResponse)
    if err != nil {
        slog.Warn("Failed to classify response emotion", "error", err)
        // Nao e critico - seguir sem
    }

    // 2. Armazenar interacao na working memory (L1)
    o.cache.PushWorkingMemory(ctx, agentID, WorkingMemoryEntry{
        Text:          fmt.Sprintf("User: %s", userInput),
        EmotionVector: promptResult.Emotion.Components,
        Role:          "user",
    })
    o.cache.PushWorkingMemory(ctx, agentID, WorkingMemoryEntry{
        Text:          fmt.Sprintf("Agent: %s", truncate(llmResponse, 500)),
        EmotionVector: responseEmotionVec(responseEmotion),
        Role:          "agent",
    })

    // 3. Avaliar promocao de memoria (se intensidade alta)
    if promptResult.Intensity > 0.5 {
        o.storeMemory(ctx, agentID, userInput, llmResponse, promptResult, 2) // Direto para L2
    }

    // 4. Medir compliance de tom
    if responseEmotion != nil {
        measureToneCompliance(promptResult.Directive, responseEmotion)
    }

    // 5. Log
    o.db.LogInteraction(ctx, &model.InteractionLog{
        AgentID:     agentID,
        InputText:   userInput,
        LLMResponse: llmResponse,
        EmotionAfter: promptResult.Emotion,
        Intensity:    promptResult.Intensity,
    })
}
```

---

## 5.5 Medicao de Compliance de Tom

```go
// internal/pipeline/tone_compliance.go

// measureToneCompliance verifica se a resposta do LLM seguiu a diretriz emocional
func measureToneCompliance(directive *EmotionRegion, responseEmotion *EmotionClassification) {
    // Calcular distancia entre emocao desejada (directive) e emocao real (response)
    desiredVAD := directive.Center
    actualVAD := [3]float32{
        responseEmotion.EmotionVector[0],
        responseEmotion.EmotionVector[1],
        responseEmotion.EmotionVector[2],
    }

    distance := euclideanDistance3(desiredVAD, actualVAD)

    // Metricas Prometheus
    toneComplianceGauge.WithLabelValues(directive.Name).Set(float64(1.0 - distance))

    if distance > 0.6 {
        slog.Warn("Low tone compliance",
            "directive", directive.Name,
            "desired_vad", desiredVAD,
            "actual_vad", actualVAD,
            "distance", distance,
        )
    }
}
```

---

## 5.6 Testes

### 5.6.1 Teste de Traducao Vetor -> Diretriz

```go
func TestFindDirective_PanicRegion(t *testing.T) {
    emotion := model.EmotionVector{Components: []float32{-0.8, 0.9, -0.6, -0.5, 0.0, 0.3}}
    directive := FindDirective(emotion)
    assert.Equal(t, "panic", directive.Name)
    assert.Contains(t, directive.Directive, "curtas")
    assert.Contains(t, directive.Directive, "diretas")
}

func TestFindDirective_NeutralFallback(t *testing.T) {
    // Vetor que nao cai em nenhuma regiao especifica
    emotion := model.EmotionVector{Components: []float32{0.05, -0.05, 0.1, 0.0, 0.0, 0.0}}
    directive := FindDirective(emotion)
    assert.Equal(t, "neutral_baseline", directive.Name)
}

func TestAllRegionsReachable(t *testing.T) {
    // Verificar que cada regiao pode ser alcancada por pelo menos um vetor
    for _, region := range emotionRegions {
        emotion := model.EmotionVector{Components: []float32{
            region.Center[0], region.Center[1], region.Center[2], 0, 0, 0,
        }}
        directive := FindDirective(emotion)
        assert.Equal(t, region.Name, directive.Name)
    }
}
```

### 5.6.2 Teste de Budget de Tokens

```go
func TestTokenBudget_HighIntensity_MoreMemory(t *testing.T) {
    budgetLow := calculateBudget(4000, 0.3)
    budgetHigh := calculateBudget(4000, 0.9)

    assert.Greater(t, budgetHigh.Memories, budgetLow.Memories)
    assert.Less(t, budgetHigh.Context, budgetLow.Context)
}

func TestTokenBudget_TotalDoesNotExceedMax(t *testing.T) {
    budget := calculateBudget(4000, 0.5)
    total := budget.Context + budget.Memories + budget.Working + budget.System + 200
    assert.LessOrEqual(t, total, 4000)
}
```

### 5.6.3 Teste de Streaming E2E

```go
func TestInteractStream_ReceivesChunks(t *testing.T) {
    // POST para /api/v1/interact/stream
    // Verificar que recebe eventos SSE:
    //   event: metadata (com estado emocional)
    //   event: chunk (multiplos, com texto parcial)
    //   event: done
}
```

### 5.6.4 Teste de Compliance de Tom

```go
func TestToneCompliance_HighWhenAligned(t *testing.T) {
    // Diretriz: panic (urgente, direto)
    // Resposta classificada como: alta ativacao, valencia negativa
    // -> compliance deve ser alta (distancia VAD baixa)
}

func TestToneCompliance_LowWhenMisaligned(t *testing.T) {
    // Diretriz: panic (urgente, direto)
    // Resposta classificada como: calma, valencia positiva
    // -> compliance deve ser baixa + warning no log
}
```

---

## 5.7 Checklist de Aceitacao

> **Status atualizado em 2026-03-19 com base no codigo e nos testes do repositório**
>
> **Resumo:** camada cognitiva, prompt estruturado parcial e streaming SSE ja estao funcionais; a parte de compliance de tom e o budget de tokens ainda seguem pendentes.

### Camada Cognitiva
- [x] Contexto cognitivo carrega/salva do PostgreSQL corretamente
- [x] ConversationPhase atualiza baseado em heuristicas
- [x] Beliefs (user_expertise, time_pressure) atualizam dinamicamente
- [x] Goals re-priorizam baseado em contexto
- [x] Re-ranker cognitivo ajusta scores dos candidatos

### Prompt Estruturado
- [ ] System prompt inclui diretriz emocional traduzida do vetor
- [x] 7 regioes prototipicas cobertas (panic, joyful, calm, empathetic, frustrated, curious, neutral)
- [ ] Budget de tokens distribui dinamicamente entre camadas
- [ ] Intensidade alta -> mais tokens para memorias emocionais
- [x] Memorias ressonantes incluidas no prompt com intensidade
- [x] Working memory (L1) incluida como historico recente

### Integracao LLM
- [x] OpenAI provider funcional (generate + stream)
- [x] SSE streaming funciona no endpoint `/api/v1/interact/stream`
- [x] Metadata emocional enviada como primeiro evento SSE
- [x] Pos-processamento roda async apos stream completar
- [x] Fallback para resposta nao-streaming se cliente nao suporta SSE

### Compliance de Tom
- [ ] Metrica de compliance medida a cada interacao
- [ ] Warning logado quando compliance < 40%
- [ ] Dashboard (Fase 7) mostra compliance por diretriz

---

## 5.8 Riscos Especificos

| Risco | Prob. | Impacto | Mitigacao |
|-------|-------|---------|-----------|
| Traducao vetor->diretriz incoerente | Alta | Alto | Regioes prototipicas + teste humano extensivo |
| Budget de tokens insuficiente | Media | Medio | Priorizacao dinamica + truncamento inteligente |
| LLM ignora diretrizes emocionais | Alta | Medio | System prompt + few-shot + compliance metrica |
| Pos-processamento falha silenciosamente | Media | Medio | Logging + metricas de sucesso/falha |
| Heuristicas cognitivas muito simplistas | Alta | Baixo | Aceitar para MVP, evoluir na Fase 7 |

---

## 5.9 Transicao para Fase 6

Ao final da Fase 5:
- O sistema gera respostas emocionalmente contextualizadas via prompt estruturado
- A camada cognitiva modela intencoes, crencas e normas
- Streaming funciona end-to-end
- Compliance de tom e medido

A **Fase 6** expande para multiplos agentes com personalidades distintas e contagio emocional.
