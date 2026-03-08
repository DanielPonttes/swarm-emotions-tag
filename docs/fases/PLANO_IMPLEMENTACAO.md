# EmotionRAG - Plano de Implementacao em Etapas

> Plano derivado dos documentos de arquitetura multiagente e implementacao Go+Rust.
> Cada fase inclui entregaveis, criterios de saida, riscos identificados e gargalos potenciais.

---

## Visao Geral das Fases

```
FASE 0  [Fundacao]         ── Contratos, estrutura, infra local
FASE 1  [Motor Emocional]  ── Rust: FSM + vetor emocional + decay
FASE 2  [Plano de Controle]── Go: API + orquestrador + conectores
FASE 3  [Integracao]       ── gRPC Go<->Rust + pipeline E2E single-agent
FASE 4  [Memoria]          ── Stores vetoriais + hierarquia L1/L2/L3
FASE 5  [Cognicao + LLM]   ── Camada cognitiva + prompt estruturado + geracao
FASE 6  [Multiagente]      ── Agent manager + contagio emocional
FASE 7  [Producao]         ── Observabilidade + hardening + eliminacao Python
```

---

## FASE 0 - Fundacao e Contratos (Semana 1-2)

### Entregaveis
1. **Monorepo estruturado:**
   ```
   swarm-emotions/
   ├── proto/                    # Definicoes Protobuf (contrato Go<->Rust)
   │   └── emotion_engine.proto
   ├── emotion-engine/           # Projeto Rust (cargo init)
   │   ├── Cargo.toml
   │   └── src/
   ├── orchestrator/             # Projeto Go (go mod init)
   │   ├── go.mod
   │   └── cmd/server/
   ├── python-ml/                # Servico auxiliar Python (fase inicial)
   │   ├── pyproject.toml
   │   └── app/
   ├── docker/                   # Dockerfiles por servico
   ├── docker-compose.yml
   ├── Makefile                  # Comandos unificados
   └── docs/
   ```

2. **Contrato Protobuf completo** (`emotion_engine.proto`):
   - `TransitionState` - transicao FSM
   - `ComputeEmotionVector` - calculo e(t+1) = e(t) + W x g(t) + epsilon
   - `FuseScores` - fusao de rankings triplo
   - `ClassifyEmotion` - classificacao emocional via ONNX
   - `EvaluatePromotion` - decisao de promocao de memoria
   - `ProcessInteraction` - endpoint batch (pipeline Rust completo)

3. **docker-compose.yml funcional** com:
   - Qdrant (port 6333/6334)
   - PostgreSQL (port 5432)
   - Redis (port 6379)
   - Stubs para go-orchestrator e rust-emotion-engine

4. **CI basico:** lint (clippy + golangci-lint), build, testes unitarios

### Criterios de Saida
- `docker compose up -d` sobe toda a infra de dependencias
- `cargo build` e `go build` compilam sem erros
- Protobuf gera codigo Go e Rust sem conflitos
- Makefile com targets: `build`, `test`, `lint`, `proto-gen`, `docker-up`

### ANALISE DE FALHAS E GARGALOS

**GARGALO: Definicao prematura do Protobuf.**
O contrato gRPC e a interface mais consequente da arquitetura. Se definido cedo demais
sem validacao experimental, vai gerar retrabalho em todas as fases seguintes.
- *Mitigacao:* Marcar o proto como `v0` e usar versionamento semântico. Projetar
  mensagens com campos `reserved` e extensibilidade via `oneof`. Aceitar que o proto
  vai mudar ate a Fase 3.

**RISCO: Acoplamento monorepo.**
Colocar Go e Rust no mesmo repo simplifica CI mas complica builds independentes.
O `cargo build` nao sabe do Go e vice-versa.
- *Mitigacao:* Makefile como orquestrador de build. Cada linguagem tem seu proprio
  toolchain isolado. O unico ponto de contato e o diretorio `proto/`.

---

## FASE 1 - Motor Emocional em Rust (Semana 3-7)

### Entregaveis

#### 1.1 FSM Engine (Semana 3-4)
- Representacao de estados emocionais via `enum` + `match`
- FSM plana (sem hierarquia) com ~8 estados base:
  `Neutro, Alegre, Curioso, Empatico, Calmo, Preocupado, Frustrado, Ansioso`
- Tabela de transicoes configuravel (TOML/JSON, nao hardcoded)
- Sistema de restricoes Omega (transicoes proibidas, tempo minimo por estado)
- Modo deterministico apenas nesta fase

#### 1.2 Motor Vetorial (Semana 4-5)
- Calculo de e(t+1) = e(t) + W x g(t) + epsilon usando `ndarray`
- Vetor emocional 6D: [valencia, ativacao, dominancia, certeza, orientacao_social, novidade]
- Matriz de suscetibilidade W (8x8 quando incluir acoplamentos, 6x6 diagonal simples)
- Decaimento temporal: e(t) = e_baseline + (e(t) - e_baseline) * exp(-lambda * dt)
- Similaridade cosseno batch
- Score fusion: alpha*sem + beta*emo + gamma*cog com sort

#### 1.3 Servidor gRPC (Semana 5-7)
- `tonic` server implementando o contrato proto
- Endpoint batch `ProcessInteraction`
- Tracing via `tracing` + `tracing-opentelemetry`
- Benchmarks com `criterion` para hot-path

### Criterios de Saida
- FSM passa suite de testes com 100% das transicoes validas e rejeicao de invalidas
- Motor vetorial: testes de correctness com vetores conhecidos (golden tests)
- Benchmark: FSM transition < 1us, vector compute < 10us, score fusion 100 candidatos < 100us
- Servidor gRPC responde a chamadas via `grpcurl`

### ANALISE DE FALHAS E GARGALOS

**FALHA CRITICA: Instabilidade numerica no calculo vetorial.**
A formula e(t+1) = e(t) + W*g(t) + epsilon pode divergir se W tiver autovalores > 1.
Apos N iteracoes sem decaimento, o vetor emocional explode para infinito.
- *Mitigacao:* Impor clamping por componente (ex: cada dimensao em [-1, 1]).
  Validar que a norma espectral de W < 1 na inicializacao. Monitorar ||e(t)|| e
  aplicar normalizacao se ultrapassar threshold.
- *Teste obrigatorio:* Simulacao de 10.000 turnos sem input externo - vetor deve
  convergir para baseline, nao divergir.

**FALHA: Starvation de estados na FSM.**
Se a tabela de transicoes tiver estados "sink" (entram mas nao saem) ou estados
inalcancaveis, o agente pode ficar preso.
- *Mitigacao:* Validacao estatica do grafo de transicoes na inicializacao:
  verificar que todo estado e alcancavel e que nenhum estado e absorvente
  (exceto se explicitamente marcado). Implementar como teste de propriedade.

**GARGALO: Configuracao da matriz W.**
Nao existe ground truth para calibrar W. Valores arbitrarios vao gerar
comportamentos imprevisíveis.
- *Mitigacao:* Comecar com W = identidade escalada (0.1*I) - efeito minimo.
  Fornecer presets de personalidade (resiliente, reativo, empatico) com
  matrizes pre-calibradas. Logging extensivo para iteracao manual.

**RISCO: SIMD prematuro.**
Otimizar com `packed_simd2` ou `std::simd` (nightly) antes de ter o pipeline
funcional e otimizacao prematura que complica builds.
- *Mitigacao:* Usar `ndarray` puro ate a Fase 7. SIMD so se profiling indicar
  que o motor Rust e gargalo real (improvavel dado que LLM domina >90% latencia).

---

## FASE 2 - Plano de Controle em Go (Semana 5-9)

> Nota: Inicia em paralelo com Fase 1.3 (servidor gRPC Rust).
>
> **Status real em 2026-03-08:** Bloco A concluido, Bloco B concluido, Bloco C parcialmente validado neste ambiente.
> - Implementado: connectors reais (Redis/PostgreSQL/Qdrant), circuit breaker, retry/backoff, timeout budget, `/metrics` e `/debug/pprof/*`.
> - Validado: `go test ./...` e testes de falha/estabilidade.
> - Pendente: execucao deterministica da integracao real em ambiente limpo (infra Docker bloqueada por DNS no pull de imagens).

### Entregaveis

#### 2.1 API Gateway (Semana 5-6)
- HTTP server (`chi` ou stdlib `net/http`)
- Endpoint `POST /interact` - recebe mensagem, retorna resposta
- Endpoint `GET /agent/{id}/state` - estado emocional corrente
- Endpoint `POST /agent` - criar agente com configuracao (baseline, W, pesos)
- Health check, readiness probe
- Exposicao de `/metrics` (Prometheus) e `/debug/pprof/*`

#### 2.2 Orquestrador de Pipeline (Semana 6-8)
- Implementacao dos 8 steps do pipeline (Secao 8 do doc. arquitetural)
- Uso de `errgroup` para paralelizacao de queries (semantico + emocional + cognitivo)
- Circuit breaker para dependencias externas (Qdrant, Redis, Rust engine)
- Context propagation com timeouts por step

#### 2.3 Connector Hub (Semana 7-9)
- Client gRPC para o motor Rust (`google.golang.org/grpc`)
- Client Qdrant HTTP - queries semanticas e emocionais em paralelo
- Client Redis - leitura/escrita de working memory (L1) e estado corrente
- Client PostgreSQL - configuracoes de agentes, contexto cognitivo, audit log
- Client LLM generico (interface com implementacoes para OpenAI, Anthropic, local)
- Circuit breaker para dependencia Rust + retry/backoff/jitter para Redis/PostgreSQL/Qdrant

#### 2.4 Validacao do Bloco C (2026-03-08)
- Unitarios/falha:
  - `go test ./...` -> PASS
- Integracao real automatizada:
  - testes com tag `integration` criados para Redis/PostgreSQL/Qdrant
  - `go test -tags=integration -v ./internal/connector/cache ./internal/connector/db ./internal/connector/vectorstore`
  - Resultado no ambiente atual: SKIP por `connection refused` (servicos locais indisponiveis)
- Estabilidade:
  - `go test -tags=stability -run TestStability_NoGoroutineLeakUnderLoad -v ./internal/pipeline` -> PASS
- Tentativa de provisionar dependencias reais:
  - `docker compose up -d redis postgresql qdrant`
  - Resultado: FAIL por DNS externo no pull de imagens (`docker-images-prod...cloudflarestorage.com`)

### Criterios de Saida
- API Gateway responde a requests em modo real e modo mock
- Orquestrador executa pipeline completo com resiliencia (timeout + retry + circuit breaker)
- Cada connector possui testes de integracao automatizados contra servicos reais
- Integracao real executa de forma deterministica em ambiente limpo (pendente neste ambiente por bloqueio de infra)
- Latencia do pipeline sem LLM: alvo < 100ms com mocks

### ANALISE DE FALHAS E GARGALOS

**GARGALO: Goroutines presas em chamadas ao motor Rust.**
Se o motor Rust estiver lento ou indisponivel, goroutines acumulam esperando
resposta gRPC, eventualmente exaurindo memoria.
- *Mitigacao:* Timeout agressivo nas chamadas gRPC (ex: 500ms). Circuit breaker
  com fallback (retornar vetor emocional anterior cached). Limitar concorrencia
  com semaphore pattern.

**FALHA: Race condition na atualizacao de estado emocional.**
Se duas requests do mesmo agente chegarem simultaneamente, ambas leem e(t) do
Redis, computam e(t+1) independentemente, e a ultima escrita vence (lost update).
- *Mitigacao:* Lock por agent_id no Redis (SETNX com TTL) antes de ler estado.
  Ou usar Redis WATCH/MULTI para CAS (compare-and-swap). Para MVP, serializar
  requests por agente via channel no Go.

**RISCO: Timeout cascade.**
O pipeline tem dependencias sequenciais (FSM -> queries -> fusion -> LLM).
Se um step demora, o timeout do step seguinte pode ser insuficiente.
- *Mitigacao:* Timeout budget pattern: o contexto raiz tem timeout total (ex: 30s),
  cada step consome parte do budget restante. Se sobrar < 5s quando chegar ao LLM,
  usar modelo menor ou retornar resposta degradada.

---

## FASE 3 - Integracao Go <-> Rust + Pipeline E2E (Semana 8-11)

### Entregaveis

#### 3.1 Integracao gRPC (Semana 8-9)
- Go chama Rust via gRPC com Unix domain socket (latencia minima)
- Propagacao de trace IDs (OpenTelemetry) entre Go e Rust
- Testes de integracao end-to-end: request HTTP -> Go -> Rust -> Go -> response
- Error handling: erros Rust propagam como gRPC status codes com detalhes

#### 3.2 Servico Python Auxiliar (Semana 9-10)
- FastAPI com `POST /classify-emotion` -> EmotionVector
- Usa GoEmotions (BERT fine-tuned) para classificacao
- Usa sentence-transformers para embeddings semanticos
- Containerizado, exposto via HTTP para o Go chamar

#### 3.3 Pipeline E2E Single-Agent (Semana 10-11)
- Fluxo completo: input -> percepcao -> FSM -> queries -> fusion -> prompt -> LLM -> pos-proc
- Um unico agente funcional com personalidade fixa
- Integrado com LLM real (OpenAI/Anthropic API)
- Testes E2E com cenarios de conversacao (happy path + edge cases)

### Criterios de Saida
- Pipeline completo executa em < 5s (incluindo LLM)
- Pipeline sem LLM executa em < 100ms
- Estado emocional evolui coerentemente ao longo de 20+ turnos de conversacao
- Logs mostram trace distribuido Go -> Rust com correlation ID
- Teste de regressao: mesmos inputs produzem mesmos estados (modo deterministico)

### ANALISE DE FALHAS E GARGALOS

**GARGALO CRITICO: Servico Python como SPOF.**
Na Fase 3, Python faz classificacao emocional E embeddings. Se cair, o pipeline
inteiro para. E o servico Python e o mais fragil (GIL, memory leaks em modelos).
- *Mitigacao:* Cache agressivo de classificacoes (mesmo texto = mesmo vetor emocional).
  Redis como cache layer. Fallback: se Python indisponivel, usar vetor emocional
  neutro [0,0,0,0,0,0] e logar warning. Replica Python com pelo menos 2 instancias.

**FALHA: Divergencia semantica entre classificacao Python e futura classificacao Rust.**
Quando migrar de Python para ONNX em Rust (Fase 7), os vetores emocionais vao
diferir levemente devido a floating point, tokenizacao, etc. Isso pode alterar
transicoes FSM e rankings de memoria.
- *Mitigacao:* Suite de paridade desde a Fase 3: gerar dataset de 1000+ textos,
  classificar em Python, salvar como golden dataset. Quando implementar ONNX em
  Rust, comparar com tolerancia (ex: max diff < 0.01 por componente).

**FALHA: Serialization overhead dos vetores emocionais.**
Protobuf serializa vetores como `repeated float`. Para 100 candidatos com
vetores 768d, isso e ~300KB por request.
- *Mitigacao:* Na fusao de scores, enviar apenas os scores escalares pre-computados
  pelos stores, nao os vetores completos. O Rust precisa de scores, nao de embeddings.
  Redesenhar o proto se necessario.

**RISCO: Cold start do modelo Python.**
Carregar BERT na inicializacao do container leva 10-30s. Durante esse tempo,
requests falham.
- *Mitigacao:* Readiness probe que so retorna 200 apos modelo carregado.
  Go orquestrador espera readiness antes de enviar requests.

---

## FASE 4 - Memoria Hierarquica (Semana 10-14)

### Entregaveis

#### 4.1 Store Vetorial Semantico (Semana 10-11)
- Collection `semantic_memories` no Qdrant (768-3072d dependendo do modelo)
- Payload: agent_id, memory_level, timestamp, content_hash, emotion_ref_id
- HNSW tuning para alta dimensionalidade (ef_construct=200, m=16)
- Queries top-K com filtro por agent_id e memory_level

#### 4.2 Store Vetorial Emocional (Semana 11-12)
- Collection `emotional_memories` no Qdrant (6d - vetor emocional)
- Payload: intensity (I), agent_id, memory_level, is_pseudopermanent
- HNSW tuning para baixa dimensionalidade (parametros diferentes do semantico)
- Link por content_hash com store semantico

#### 4.3 Hierarquia de Memoria L1/L2/L3 (Semana 12-14)
- **L1 (Working Memory):** Redis com TTL automatico (segundos a minutos)
  - Sorted sets para ranking por recencia
  - Hashes para estado emocional corrente
- **L2 (Episodica medio prazo):** Qdrant + PostgreSQL
  - Decaimento: batch job periodico (horas a dias)
  - Indexacao semantica + temporal
- **L3 (Pseudopermanente):** Qdrant + PostgreSQL
  - Promocao por intensidade emocional: ||e(m_i)|| > theta
  - Criterio multifatorial: alta intensidade OU (frequencia alta E valencia significativa)
  - Decaimento muito lento (meses)

#### 4.4 Motor de Promocao (Rust + Go)
- Rust: avalia criterios de promocao, retorna decisao
- Go: persiste no store adequado
- Batch job de decaimento (goroutine com timer)
- Garbage collection de memorias L1/L2 expiradas

### Criterios de Saida
- Memorias sao armazenadas no nivel correto baseado em intensidade emocional
- Promocao L1->L2->L3 funciona e e rastreavel no log
- Queries paralelas (semantico + emocional + cognitivo) completam em < 30ms
- Decaimento temporal reduz scores de memorias antigas corretamente
- Teste: apos 100 interacoes, memorias de alta intensidade persistem em L3

### ANALISE DE FALHAS E GARGALOS

**FALHA CRITICA: Inconsistencia entre stores semantico e emocional.**
Uma memoria existe no store semantico mas nao no emocional (ou vice-versa)
por falha parcial durante insercao. A fusao de scores falha ou retorna
resultados incompletos.
- *Mitigacao:* Insercao transacional: gravar em ambos stores ou em nenhum.
  Como Qdrant nao suporta transacoes cross-collection nativamente, usar
  pattern de saga: inserir no semantico, inserir no emocional, se falhar
  compensar deletando do semantico. Ou melhor: inserir primeiro em ambos
  os stores, marcar como "pending", e confirmar com update posterior.
  **Alternativa mais simples:** usar uma unica collection no Qdrant com
  named vectors (um vetor semantico + um vetor emocional por ponto).
  Isso elimina a inconsistencia por design, mas exige Qdrant >= 1.7.

**GARGALO: Batch job de decaimento.**
Varrer todas as memorias L2 periodicamente para aplicar decaimento
exponencial e O(n) sobre todas as memorias de todos os agentes.
Com 1000 agentes e 10.000 memorias cada = 10M operacoes por ciclo.
- *Mitigacao:* Decaimento lazy: calcular score de decaimento no momento
  da query (multiplicar pelo fator temporal), nao pre-computar.
  O campo timestamp ja existe - o decaimento e: score * exp(-lambda * (now - t)).
  Isso e O(K) por query (K = top-K candidatos), nao O(N) global.
  Batch job so para garbage collection de memorias com score efetivo ~0.

**FALHA: Threshold de promocao mal calibrado.**
Se theta muito baixo: tudo vira pseudopermanente (L3 vira lixo).
Se theta muito alto: nada e promovido (L3 vazio, agente sem memoria longa).
- *Mitigacao:* Theta adaptativo por agente: manter percentil de intensidade
  das ultimas N memorias. Promover apenas memorias acima do P90 de intensidade.
  Ou usar theta fixo com cap de memorias L3 (ex: max 500 por agente), aplicando
  LRU quando exceder.

**RISCO: Dimensionalidade incompativel entre embedding models.**
Se trocar o modelo de embedding (ex: de text-embedding-ada-002 768d para
text-embedding-3-large 3072d), todas as memorias semanticas existentes ficam
incompativeis com novas queries.
- *Mitigacao:* Versionar collections por modelo de embedding. Na migracao,
  re-embeddar memorias existentes (custoso mas necessario). Documentar modelo
  usado como metadado da collection.

---

## FASE 5 - Camada Cognitiva + Geracao (Semana 13-17)

### Entregaveis

#### 5.1 Contexto Cognitivo (Semana 13-14)
- Schema PostgreSQL para `cognitive_contexts`:
  - active_goals (JSON array com id, descricao, prioridade)
  - beliefs (JSON object: user_expertise, task_complexity, time_pressure, etc.)
  - norms (JSON object: formality_level, honesty_commitment, emotional_expressiveness)
  - conversation_phase (enum)
- Atualizacao cognitiva apos cada interacao (step 4 do pipeline)
- Relevancia cognitiva como re-ranker baseado em regras (goals filtram, beliefs ajustam)

#### 5.2 Prompt Cognitivo Estruturado (Semana 14-16)
- Template engine (Go `text/template`) com tres camadas:
  1. **Cenario Semantico:** documentos recuperados ordenados por score
  2. **Estado Interno:** vetor emocional traduzido em diretriz comportamental
     (ex: [V:-0.8, A:0.9] -> "Suas respostas devem ser curtas e urgentes")
  3. **Ressonancia de Memoria:** memorias pseudopermanentes relevantes com contexto emocional
- Mapeamento vetor->diretriz configuravel por ranges de cada dimensao
- Budget de tokens: distribuir janela de contexto entre as tres camadas

#### 5.3 Integracao LLM com Streaming (Semana 15-17)
- Client LLM com interface abstrata (suportar OpenAI, Anthropic, ollama local)
- Streaming de resposta (SSE para o cliente HTTP)
- Pos-processamento: extrair vetor emocional da resposta gerada
  (via Python service ou prompting estruturado ao proprio LLM)
- Decisao de promocao de memoria baseada na interacao completa

### Criterios de Saida
- Prompt gerado inclui as tres camadas com distribuicao adequada de tokens
- Resposta do LLM reflete tom coerente com estado emocional (validacao humana)
- Streaming funciona end-to-end (HTTP -> Go -> LLM -> SSE)
- Contexto cognitivo se atualiza corretamente entre turnos

### ANALISE DE FALHAS E GARGALOS

**FALHA CRITICA: Traducao vetor->diretriz incoerente.**
O mapeamento de vetor emocional para instrucoes em linguagem natural e o ponto
mais fragil de toda a arquitetura. Se mal feito, o LLM recebe diretrizes
contraditorias ou incompreensiveis.
- *Exemplo:* [V:0.2, A:0.8, D:0.1] pode ser "levemente positivo mas muito
  ativado e com baixa dominancia" = ansiedade esperancosa? Como traduzir isso
  em diretriz acionavel?
- *Mitigacao:* NAO usar mapeamento linear simples. Usar lookup table com
  regioes prototipicas no espaco emocional (clusters pre-definidos) e
  diretrizes associadas. Cada regiao mapeada por vizinho mais proximo.
  Testar extensivamente com avaliadores humanos.

**GARGALO: Budget de tokens vs. riqueza de contexto.**
O prompt estruturado compete por tokens com: documentos recuperados, diretrizes
emocionais, memorias pseudopermanentes, historico de conversa, instrucoes de sistema.
Com modelos de 8K-32K tokens, nao cabe tudo.
- *Mitigacao:* Priorizacao dinamica: se intensidade emocional alta, alocar mais
  tokens para memorias emocionais e reduzir documentos semanticos. Se tarefa
  complexa, inverter. Implementar como funcao de alocacao parametrizada por
  estado emocional e contexto cognitivo.

**RISCO: Pos-processamento como gargalo de latencia.**
Extrair vetor emocional da resposta gerada requer outra chamada ao classificador
(Python ou LLM). Isso adiciona 5-500ms apos a geracao.
- *Mitigacao:* Fazer pos-processamento em background (nao bloquear resposta ao
  usuario). O vetor emocional da resposta so e necessario para o proximo turno,
  nao para o turno corrente.

**FALHA: LLM ignora diretrizes emocionais.**
Modelos grandes frequentemente ignoram instrucoes de tom quando o conteudo
semantico e forte. O agente pode receber diretriz "seja urgente e ansioso"
mas responder de forma calma e didatica se o conteudo for tecnico.
- *Mitigacao:* Posicionar diretrizes emocionais como system prompt (nao user
  message). Usar exemplos few-shot de tom desejado. Medir compliance de tom
  via classificador emocional na saida e alertar quando desvio > threshold.

---

## FASE 6 - Sistema Multiagente (Semana 16-22)

### Entregaveis

#### 6.1 Agent Manager (Semana 16-18)
- CRUD de agentes com configuracao completa:
  - Baseline emocional (e_baseline)
  - Matriz de suscetibilidade W
  - Pesos alpha, beta, gamma
  - Tabela de transicoes FSM
  - Restricoes Omega
- Ciclo de vida: criacao, ativacao, suspensao, destruicao
- Isolamento de estado: cada agente tem seus proprios stores de memoria
- Health monitoring por agente

#### 6.2 Contagio Emocional (Semana 18-20)
- Quando agente A envia mensagem a agente B:
  s_B(t) = alpha_contagio * e_A(t) + (1 - alpha_contagio) * s_externo(t)
- alpha_contagio configuravel por par de agentes
- Deteccao e prevencao de feedback loops:
  - Monitorar entropia emocional (variancia dos estados recentes)
  - Damping automatico se entropia > threshold
  - Cooldown period entre contagios do mesmo par

#### 6.3 Comunicacao Inter-Agente (Semana 19-22)
- Protocolo de mensagens entre agentes (via Go channels internamente, gRPC se distribuido)
- Formato de mensagem: conteudo + vetor emocional + metadata
- Padroes de interacao: request-response, broadcast, pub/sub por topico emocional
- Resolucao de conflitos via deteccao emocional vetorial
- Negociacao emocional: estrategias baseadas em estado emocional do interlocutor

#### 6.4 HFSM - Hierarquia de Estados (Semana 20-22)
- Upgrade da FSM plana para hierarquica:
  - Nivel 0 (Macro): Positivo | Neutro | Negativo
  - Nivel 1 (Sub): Alegre, Curioso, Empatico+ | Calmo, Analitico | Preocupado, Frustrado, etc.
- Transicoes inter-macro requerem estímulo de maior intensidade
- Transicoes intra-macro sao mais fluidas

### Criterios de Saida
- 10+ agentes funcionando simultaneamente sem interferencia de estado
- Contagio emocional observavel e controlavel (nao espiral)
- Agentes com personalidades distintas (mesmos inputs -> comportamentos diferentes)
- HFSM funciona com fallback para FSM plana por configuracao
- Throughput: > 100 agentes simultaneos por instancia

### ANALISE DE FALHAS E GARGALOS

**FALHA CRITICA: Feedback loop de contagio emocional.**
Agente A contagia B, B contagia A, ambos amplificam ate saturacao.
Isso e o risco mais perigoso da fase multiagente.
- *Cenario concreto:* Agente A (frustrado) envia mensagem a B. B absorve
  frustracao, fica frustrado, responde a A. A absorve frustracao amplificada
  de B, fica mais frustrado. Loop exponencial em 3-5 turnos.
- *Mitigacao multicamada:*
  1. Damping coefficient: cada transferencia de contagio reduz intensidade
     (ex: alpha_contagio * 0.7 a cada hop)
  2. Cooldown temporal: mesmo par nao pode contagiar mais que 1x a cada N segundos
  3. Circuit breaker emocional: se ||e(t)|| > threshold_maximo, clamp para threshold
  4. Monitor global de entropia do swarm com alerta

**GARGALO: Escala de estado por agente.**
Cada agente precisa de: estado FSM em Redis, vetor emocional em Redis,
working memory (L1) em Redis, memorias L2/L3 em Qdrant, config em Postgres.
Para 1000 agentes, isso e ~5000 chaves Redis + 1000 collections/filtros Qdrant.
- *Mitigacao:* NAO criar collection separada por agente no Qdrant. Usar
  agent_id como filtro de payload em collections compartilhadas. Redis:
  namespacing por prefixo (emotion_state:{agent_id}). PostgreSQL: agent_id
  como partition key.

**FALHA: Deadlock entre agentes em comunicacao sincrona.**
Se agente A espera resposta de B, e B espera resposta de A, deadlock.
- *Mitigacao:* Comunicacao assincrona por default (fire-and-forget com
  callback). Timeouts em toda comunicacao inter-agente. Deteccao de ciclos
  no grafo de dependencias de comunicacao.

**RISCO: Explosao combinatoria na HFSM.**
Com hierarquia, o numero de transicoes e (estados_macro * estados_sub)^2
no pior caso.
- *Mitigacao:* Transicoes definidas por nivel. Nivel macro tem poucas transicoes.
  Nivel sub herda regras do macro e adiciona as suas. Nao permitir transicoes
  diretamente entre sub-estados de macros diferentes (forcar passagem pelo macro).

---

## FASE 7 - Producao e Hardening (Semana 20-28)

### Entregaveis

#### 7.1 Eliminacao do Python (Semana 20-23)
- Exportacao de modelo GoEmotions para ONNX
- Inference ONNX em Rust via `ort` ou `tract`
- Tokenizacao em Rust via `tokenizers` crate (HuggingFace bindings)
- Suite de paridade: comparar resultados ONNX/Rust vs Python com tolerancia 1e-5
- Embeddings semanticos: usar API externa (OpenAI/Cohere) ou modelo ONNX local
- Remocao do container Python do docker-compose

#### 7.2 Observabilidade Completa (Semana 22-25)
- OpenTelemetry tracing distribuido Go <-> Rust (com trace IDs propagados)
- Metricas Prometheus:
  - Latencia por step do pipeline (p50, p95, p99)
  - Estado emocional por agente (gauge vetorial)
  - Transicoes FSM por tipo (counter)
  - Entropia emocional do swarm (gauge)
  - Memorias por nivel por agente (gauge)
  - Cache hit ratio do Redis (counter)
- Logging estruturado com `slog` (Go) e `tracing` (Rust)
- Dashboard Grafana com paineis por agente e por swarm
- Alertas: entropia emocional alta, memoria L3 saturada, latencia P99 > threshold

#### 7.3 Modo Estocastico (Semana 23-25)
- Implementar epsilon ~ N(0, sigma^2 * I) no motor vetorial Rust
- Transicoes FSM probabilisticas: P(s'|s,sigma,env) = softmax(w*env/tau)
- Temperatura tau configuravel (menor = mais deterministico)
- Variavel ambiental env: horario, carga, n_interacoes, sentimento medio recente

#### 7.4 Performance e Scaling (Semana 25-28)
- Load testing: 1000 agentes simultaneos
- Profiling Go (pprof) e Rust (flamegraph)
- Otimizacao baseada em profiling real (NAO prematura)
- Scaling horizontal do Go orchestrator (stateless)
- Scaling vertical do Rust engine (mais cores -> mais threads Tokio)
- Qdrant sharding por agent_id se necessario
- Avaliacao: FFI (Opcao A) necessario? So se profiling indicar gRPC como bottleneck

### Criterios de Saida
- Python eliminado do runtime (apenas toolchain de dev)
- Paridade ONNX Rust vs Python: max diff < 0.01 por componente
- Dashboard funcional com todas as metricas
- Load test: 1000 agentes com latencia P99 < 5s (incluindo LLM)
- Pipeline sem LLM: P99 < 100ms com 1000 agentes
- Zero memory leaks apos 24h de load test
- Modo estocastico nao produz loops incoerentes (entropia monitorada)

### ANALISE DE FALHAS E GARGALOS

**FALHA CRITICA: Divergencia ONNX na eliminacao do Python.**
Esta e a transicao mais arriscada de toda a implementacao. Mesmo com suite
de paridade, edge cases (textos muito longos, unicode especial, linguas
inesperadas) podem divergir.
- *Mitigacao:* Rollout gradual: manter Python como fallback por 2 semanas apos
  migrar para ONNX. Comparar resultados em producao (shadow mode): ambos
  classificam, Rust e usado, Python e comparado. Alertar se divergencia > 5%
  dos requests.

**GARGALO: Tokenizacao em Rust.**
O crate `tokenizers` (HuggingFace) e robusto mas adiciona dependencia C++
significativa. Alternativas puras Rust sao incompletas.
- *Mitigacao:* Usar `tokenizers` com feature flags minimas. Containerizar
  com multi-stage build para manter imagem pequena. Testar tokenizacao
  isoladamente com corpus diverso antes de integrar.

**FALHA: Observabilidade como overhead.**
Tracing distribuido com OpenTelemetry adiciona 1-5% de latencia e pode
gerar volume massivo de spans.
- *Mitigacao:* Sampling adaptativo: 100% em dev, 10% em producao normal,
  100% quando latencia > threshold (tail sampling). Rate limit no exporter.

---

## Resumo de Dependencias Criticas entre Fases

```
FASE 0 ─────────┬───────────────────────────────────────────────┐
                 │                                               │
                 ▼                                               ▼
           FASE 1 (Rust)                                  FASE 2 (Go)
                 │                                               │
                 └──────────────┬─────────────────────────┘
                                │
                                ▼
                          FASE 3 (Integracao)
                                │
                       ┌────────┴────────┐
                       ▼                 ▼
                 FASE 4 (Memoria)  FASE 5 (Cognicao)
                       │                 │
                       └────────┬────────┘
                                │
                                ▼
                          FASE 6 (Multiagente)
                                │
                                ▼
                          FASE 7 (Producao)
```

**Caminho critico:** Fase 0 -> Fase 1 -> Fase 3 -> Fase 4 -> Fase 6 -> Fase 7
**Paralelizavel:** Fase 1 e Fase 2 em paralelo. Fase 4 e Fase 5 parcialmente em paralelo.

---

## Catalogo Consolidado de Riscos Arquiteturais

### Riscos de Severidade CRITICA

| # | Risco | Fase | Probabilidade | Impacto |
|---|-------|------|---------------|---------|
| R1 | Divergencia numerica do vetor emocional (autovalores W > 1) | 1 | Media | Catastrofico |
| R2 | Inconsistencia entre stores semantico e emocional | 4 | Alta | Alto |
| R3 | Feedback loop exponencial de contagio emocional | 6 | Alta | Catastrofico |
| R4 | Divergencia ONNX Rust vs Python na migracao | 7 | Media | Alto |
| R5 | Traducao vetor->diretriz incoerente para o LLM | 5 | Alta | Alto |
| R6 | Lost update no estado emocional (concorrencia) | 2 | Media | Alto |

### Riscos de Severidade MEDIA

| # | Risco | Fase | Probabilidade | Impacto |
|---|-------|------|---------------|---------|
| R7 | Python SPOF na fase inicial | 3 | Media | Medio |
| R8 | Threshold de promocao mal calibrado | 4 | Alta | Medio |
| R9 | Budget de tokens insuficiente para prompt estruturado | 5 | Media | Medio |
| R10 | Deadlock entre agentes em comunicacao sincrona | 6 | Baixa | Alto |
| R11 | Explosao de estado por agente em escala | 6 | Media | Medio |
| R12 | LLM ignora diretrizes emocionais | 5 | Alta | Medio |
| R13 | Timeout cascade no pipeline | 2 | Media | Medio |

### Gargalos Arquiteturais Identificados

| Gargalo | Onde | Severidade | Mitigacao Primaria |
|---------|------|------------|-------------------|
| LLM latencia (500-3000ms) | Pipeline | Inerente | Streaming + async pos-proc |
| Serializacao vetores via Protobuf | gRPC | Baixa | Enviar scores, nao vetores |
| Batch job de decaimento O(N) | Memoria | Media | Decaimento lazy no query-time |
| Goroutines presas em gRPC | Go | Media | Timeout + circuit breaker |
| Estado por agente x N agentes | Redis/Qdrant | Media | Shared collections + namespacing |
| Tokenizacao ONNX em Rust | Fase 7 | Media | HuggingFace tokenizers crate |

---

## Estimativas de Esforco

| Fase | Duracao | Paralelizavel com | Equipe Minima |
|------|---------|-------------------|---------------|
| Fase 0 | 2 semanas | - | 1 eng |
| Fase 1 | 5 semanas | Fase 2 | 1 eng Rust |
| Fase 2 | 5 semanas | Fase 1 | 1 eng Go |
| Fase 3 | 4 semanas | - | 2 eng (Go+Rust) |
| Fase 4 | 5 semanas | Fase 5 (parcial) | 1-2 eng |
| Fase 5 | 5 semanas | Fase 4 (parcial) | 1-2 eng |
| Fase 6 | 7 semanas | - | 2-3 eng |
| Fase 7 | 8 semanas | - | 2-3 eng |

**Total estimado (caminho critico):** ~28 semanas (~7 meses) para arquitetura completa
**MVP funcional (single-agent, Fases 0-5):** ~17 semanas (~4 meses)

---

## Decisoes Arquiteturais que Devem Ser Tomadas ANTES de Comecar

1. **Modelo de embedding semantico:** OpenAI API (pago, facil) vs local (sentence-transformers, precisa Python)? Impacta dimensionalidade do Qdrant e dependencia do Python.

2. **Modelo de classificacao emocional:** GoEmotions (27 categorias -> mapeamento para 6D) vs NRCLex (8 categorias) vs VAD direto? Impacta toda a pipeline de emocao.

3. **Qdrant: collections separadas vs named vectors?** Named vectors (semantico+emocional no mesmo ponto) elimina R2 (inconsistencia) mas requer Qdrant >= 1.7 e muda a logica de queries.

4. **LLM provider principal:** OpenAI vs Anthropic vs local (ollama)? Impacta client LLM, budget de tokens, e qualidade da resposta a diretrizes emocionais.

5. **Modo estocastico no MVP?** O doc sugere deterministico primeiro. Concordo - estocastico adiciona complexidade e dificulta debugging. Implementar na Fase 7.

6. **Escala alvo:** Dezenas de agentes (simples) vs milhares (requer otimizacao desde o inicio)? Impacta decisoes de storage, caching e comunicacao.
