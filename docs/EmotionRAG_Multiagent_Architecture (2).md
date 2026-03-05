# Emotion-Oriented RAG: Arquitetura Cognitivo-Emocional para Sistemas Multiagentes

> **Documento conceitual e técnico** — Proposta de uma nova estrutura de Retrieval-Augmented Generation orientada por emoções vetoriais temporárias, memórias pseudopermanentes ancoradas em valência emocional e contexto baseado em cognição, com orquestração via máquina de estados.

---

## 1. Introdução e Motivação

Sistemas multiagentes modernos — desde orquestrações LLM-to-LLM até arquiteturas de agentes autônomos com ferramentas — operam hoje sobre um eixo quase exclusivamente **lógico-semântico**. A recuperação de contexto (RAG clássico) busca documentos por similaridade vetorial ao *query embedding*, sem qualquer noção de **estado afetivo** do agente, do usuário ou do ambiente. Isso produz respostas tecnicamente corretas, mas frequentemente **descontextualizadas do ponto de vista relacional e afetivo**.

A proposta deste documento é explorar, formalizar e avaliar criticamente uma arquitetura que adiciona uma **camada emocional explícita** ao pipeline de RAG, transformando o processo de recuperação e geração em algo que é simultaneamente:

- **Semanticamente relevante** (como o RAG tradicional),
- **Emocionalmente coerente** (com o estado afetivo corrente do agente),
- **Cognitivamente contextualizado** (considerando a "história emocional" do sistema e suas memórias consolidadas).

A inspiração teórica vem de modelos da psicologia cognitiva — em especial a **teoria de avaliação cognitiva (appraisal theory)** de Lazarus e o **modelo circunflexo de afeto** de Russell — combinados com técnicas modernas de representação vetorial, máquinas de estado finitas e armazenamento hierárquico de memória.

---

## 2. Fundamentos Teóricos

### 2.1 Modelos de Emoção Aplicáveis a Agentes Artificiais

#### 2.1.1 Modelo Circunflexo de Russell

O modelo circunflexo propõe que qualquer estado emocional pode ser representado num espaço bidimensional contínuo definido por dois eixos:

- **Valência** (valence): prazer ↔ desprazer
- **Ativação** (arousal): excitação ↔ calma

Assim, "alegria" ocupa uma região de alta valência e alta ativação; "tristeza", baixa valência e baixa ativação; "raiva", baixa valência e alta ativação. Esse modelo é especialmente atraente para representação computacional porque **qualquer emoção se torna um ponto (ou região) num espaço vetorial contínuo**, viabilizando operações de distância, interpolação e clustering.

#### 2.1.2 Modelo OCC (Ortony, Clore & Collins)

O modelo OCC categoriza emoções com base na **avaliação cognitiva** de eventos, ações e objetos. Para um agente artificial, isso se traduz em: dado um evento percebido, o agente avalia se ele é **desejável** (em relação aos seus objetivos), se a **ação** que o provocou é **elogiável** (em relação a padrões normativos) e se o **objeto** envolvido é **atraente**. O resultado dessa avaliação determina a emoção resultante. Esse modelo é poderoso para derivar emoções de forma **causal e determinística** a partir de inputs estruturados.

#### 2.1.3 Extensão para N Dimensões — Vetor Emocional

Para sistemas computacionais, propomos generalizar o modelo circunflexo para um **vetor emocional n-dimensional** $\vec{e} \in \mathbb{R}^n$, onde cada dimensão captura uma faceta do estado afetivo. Uma configuração de referência pode ser:

$$\vec{e} = [\text{valência},\ \text{ativação},\ \text{dominância},\ \text{certeza},\ \text{orientação social},\ \text{novidade}]$$

Essa representação permite que o estado emocional seja tratado como um embedding — operável com as mesmas ferramentas matemáticas (similaridade cosseno, projeção, clustering) que já usamos para embeddings semânticos.

### 2.2 Memória Emocional: Evidências da Neurociência

A neurociência oferece uma metáfora poderosa e cientificamente embasada: a **amígdala** funciona como um marcador emocional que determina quais memórias episódicas recebem prioridade de consolidação. Memórias associadas a alta ativação emocional (medo, surpresa, grande alegria) são consolidadas com maior força e têm recuperação facilitada — o chamado **efeito de memória dependente de emoção (mood-congruent memory)**.

Traduzido para um sistema de RAG:

- Memórias (documentos, interações, eventos) com alta **magnitude emocional** ($\|\vec{e}\|$ elevado) devem receber **peso maior** no armazenamento e na recuperação.
- A recuperação pode ser **enviesada pelo estado emocional corrente** do agente (mood-congruent retrieval), priorizando memórias cujo vetor emocional associado é similar ao estado corrente.

### 2.3 Cognição como Camada Metacontextual

A cognição aqui não se refere à capacidade de raciocínio lógico do LLM (que já existe), mas sim a uma **camada metacontextual** que modela:

- **Intenções ativas** do agente (o que ele está tentando alcançar),
- **Crenças sobre o ambiente** (modelo de mundo),
- **Modelo do interlocutor** (teoria da mente simplificada — o que o agente "acredita" que o outro sente ou sabe),
- **Normas e valores** atribuídos ao agente (limites éticos, estilo comunicativo).

Essa tríade **emoção–memória–cognição** forma a base da arquitetura proposta.

---

## 3. Arquitetura Proposta: EmotionRAG

### 3.1 Visão Geral

```
┌──────────────────────────────────────────────────────────────────────┐
│                        SISTEMA MULTIAGENTE                           │
│                                                                      │
│  ┌────────────┐    ┌──────────────┐    ┌─────────────────────────┐  │
│  │  PERCEPÇÃO  │───▶│  MÁQUINA DE  │───▶│  MOTOR DE RECUPERAÇÃO   │  │
│  │  (Input     │    │  ESTADOS     │    │  (EmotionRAG)           │  │
│  │   Parser)   │    │  EMOCIONAL   │    │                         │  │
│  └────────────┘    └──────┬───────┘    │  ┌───────────────────┐  │  │
│                           │            │  │ Memória Vetorial   │  │  │
│                           │            │  │ Semântica          │  │  │
│                           ▼            │  └───────────────────┘  │  │
│                    ┌──────────────┐    │  ┌───────────────────┐  │  │
│                    │  VETOR       │────│  │ Memória Emocional  │  │  │
│                    │  EMOCIONAL   │    │  │ Pseudopermanente   │  │  │
│                    │  CORRENTE    │    │  └───────────────────┘  │  │
│                    │  ē(t)        │    │  ┌───────────────────┐  │  │
│                    └──────────────┘    │  │ Contexto Cognitivo │  │  │
│                                       │  │ (Intenções/Crenças)│  │  │
│                                       │  └───────────────────┘  │  │
│                                       └─────────────┬───────────┘  │
│                                                     │              │
│                                                     ▼              │
│                                            ┌──────────────┐       │
│                                            │  GERAÇÃO      │       │
│                                            │  (LLM +       │       │
│                                            │   Contexto    │       │
│                                            │   Emocional)  │       │
│                                            └──────────────┘       │
└──────────────────────────────────────────────────────────────────────┘
```

### 3.2 Componentes Centrais

#### Componente 1 — Percepção (Input Parser)

Responsável por analisar o input recebido (mensagem do usuário, evento do ambiente, comunicação de outro agente) e extrair:

- **Conteúdo semântico**: o embedding convencional do texto/dado.
- **Sinal emocional do input**: análise de sentimento/emoção do texto recebido, convertida em vetor emocional $\vec{e}_{\text{input}}$.
- **Sinais contextuais**: metadados como urgência, formalidade, tom, presença de marcadores explícitos de emoção.

A extração do sinal emocional pode usar modelos especializados (como GoEmotions fine-tuned em BERT) ou o próprio LLM com prompting estruturado que retorne o vetor emocional em formato numérico.

#### Componente 2 — Máquina de Estados Emocional (FSM/HFSM)

Este é o coração determinístico (ou estocástico) da arquitetura. Modela o **estado emocional corrente do agente** como um nó em um grafo dirigido, onde as transições são disparadas por sinais da camada de percepção.

#### Componente 3 — Motor de Recuperação EmotionRAG

O motor de recuperação combina três fontes de informação ponderadas:

1. **Similaridade semântica** (como RAG clássico): $\text{sim}_{\text{sem}}(q, d_i)$
2. **Similaridade emocional**: $\text{sim}_{\text{emo}}(\vec{e}(t), \vec{e}_{d_i})$
3. **Relevância cognitiva**: $\text{rel}_{\text{cog}}(C(t), d_i)$

A pontuação final para cada documento candidato $d_i$ é:

$$\text{score}(d_i) = \alpha \cdot \text{sim}_{\text{sem}}(q, d_i) + \beta \cdot \text{sim}_{\text{emo}}(\vec{e}(t), \vec{e}_{d_i}) + \gamma \cdot \text{rel}_{\text{cog}}(C(t), d_i)$$

onde $\alpha + \beta + \gamma = 1$ e os pesos podem ser dinâmicos (ajustados pela intensidade emocional corrente, por exemplo).

#### Componente 4 — Geração Contextualizada

O LLM recebe:
- O query original,
- Os documentos recuperados (já rankeados pelo score triplo),
- O estado emocional corrente $\vec{e}(t)$ em linguagem natural ou como instrução estruturada,
- O contexto cognitivo $C(t)$ (intenções, crenças, modelo do interlocutor).

O **prompt cognitivo estruturado** que substitui o simples "últimas N mensagens" do RAG convencional é composto por três camadas:

1. **Cenário Semântico**: o que está acontecendo agora — os documentos e memórias recuperados, organizados por relevância.
2. **Estado Interno Traduzido em Diretrizes Comportamentais**: o vetor emocional atual não é injetado como números brutos, mas convertido em instruções acionáveis pelo LLM. Exemplo: *"Seu estado emocional atual [V:-0.8, A:0.9, D:0.1] indica pânico. Suas respostas devem ser curtas, urgentes e focadas em resolução imediata."*
3. **Ressonância de Memória**: memórias recuperadas via EmotionRAG que combinam similaridade semântica com similaridade emocional — o agente não apenas sabe o que é relevante, mas *sente* o que é relevante.

Essa construção tripla garante que o LLM não receba apenas contexto factual, mas um **enquadramento afetivo-cognitivo completo** que molda o tom, o estilo e a priorização da resposta.

---

## 4. Emoções Vetoriais Temporárias — Formalização

### 4.1 Definição

O vetor emocional temporário $\vec{e}(t)$ representa o estado afetivo **instantâneo** do agente no momento $t$. Ele é efêmero: existe apenas durante o processamento de uma interação ou ciclo e é recalculado a cada novo estímulo.

Formalmente:

$$\vec{e}(t) = f_{\text{FSM}}(\vec{e}(t-1),\ \vec{s}(t),\ C(t))$$

onde:

- $\vec{e}(t-1)$: estado emocional anterior (inércia emocional),
- $\vec{s}(t)$: sinal emocional extraído do input corrente,
- $C(t)$: contexto cognitivo (intenções, crenças ativas),
- $f_{\text{FSM}}$: função de transição da máquina de estados.

### 4.1.1 Fórmula Unificada de Transição com Matriz de Suscetibilidade

Uma formalização alternativa e mais implementável expressa a transição como uma operação vetorial direta:

$$\vec{e}(t+1) = \vec{e}(t) + \mathbf{W} \times \vec{g}(t) + \epsilon$$

onde:

- $\vec{g}(t)$: vetor emocional do **gatilho externo** (avaliado por um sub-modelo rápido, como um classificador de sentimentos ou o próprio LLM com prompting estruturado),
- $\mathbf{W} \in \mathbb{R}^{n \times n}$: **matriz de suscetibilidade** do agente — o parâmetro central de personalidade,
- $\epsilon \sim \mathcal{N}(0, \sigma^2 \mathbf{I})$: ruído estocástico opcional (modo não-determinístico; $\epsilon = 0$ no modo determinístico puro).

A **matriz de suscetibilidade $\mathbf{W}$** é o que diferencia a "personalidade emocional" de cada agente no nível mais fundamental. Um agente "resiliente" ou "teimoso" tem valores baixos em $\mathbf{W}$ (estímulos externos o afetam pouco); um agente "empático" ou "reativo" tem valores altos (absorve fortemente a carga emocional dos inputs). Criticamente, $\mathbf{W}$ não precisa ser diagonal — termos fora da diagonal modelam **acoplamentos emocionais cruzados** (ex: alta ativação no input pode reduzir a dominância percebida pelo agente, modelando intimidação).

Esta fórmula unifica elegantemente os modos determinístico ($\epsilon = 0$) e estocástico ($\epsilon \neq 0$) em uma única expressão, e permite que a calibração de personalidade seja reduzida à configuração de uma única matriz por agente.

> **Nota de design:** $\mathbf{W}$ pode ser aprendida por reforço (ver Seção 11.1) ou configurada manualmente como hiperparâmetro de personalidade. Em ambientes multiagentes, cada agente mantém sua própria $\mathbf{W}$, criando uma ecologia de "temperamentos" distintos.

### 4.2 Decaimento Temporal

Emoções temporárias devem **decair** ao longo do tempo (ou dos turnos de interação) em direção a um baseline (ponto neutro ou "temperamento" configurado do agente):

$$\vec{e}(t) = \vec{e}_{\text{baseline}} + (\vec{e}(t) - \vec{e}_{\text{baseline}}) \cdot e^{-\lambda \Delta t}$$

onde $\lambda$ é a taxa de decaimento (configurável por emoção ou por agente) e $\Delta t$ é o tempo/turnos desde o último estímulo relevante.

Isso previne que o agente fique "preso" em um estado emocional indefinidamente na ausência de novos estímulos.

### 4.3 Representação Vetorial no Espaço de Embeddings

A decisão arquitetural crítica é: **o vetor emocional deve habitar o mesmo espaço vetorial dos embeddings semânticos?**

**Opção A — Espaço compartilhado (concatenação):** O embedding de cada documento no armazenamento é estendido para incluir a dimensão emocional: $\vec{d}_i^{\text{ext}} = [\vec{d}_i^{\text{sem}}\ ||\ \vec{e}_{d_i}]$. Vantagem: uma única busca vetorial. Desvantagem: as dimensões emocionais (tipicamente 4–8) são insignificantes numericamente em um espaço de 768–3072 dimensões semânticas, e a noção de "distância" mistura semântica e emoção de forma opaca.

**Opção B — Espaços separados com fusão tardia (recomendada):** Manter dois stores vetoriais independentes — um semântico e um emocional — e combinar os rankings com a função de score descrita na Seção 3.2. Vantagem: controle explícito dos pesos, interpretabilidade, e permite usar modelos de embedding diferentes para cada espaço. Desvantagem: duas buscas, maior latência.

**Recomendação:** A Opção B é preferível por questões de interpretabilidade, controle e modularidade. A latência adicional é desprezível em arquiteturas modernas com buscas vetoriais paralelas.

---

## 5. Memórias Pseudopermanentes Ancoradas em Emoção

### 5.1 Conceito

Nem todas as memórias (interações passadas, documentos processados, eventos) devem ter o mesmo peso no sistema. A hipótese central é: **memórias associadas a estados emocionais de alta intensidade devem ser promovidas a um armazenamento de longo prazo com acesso privilegiado** — análogo à consolidação de memória episódica na neurociência.

### 5.1.1 Estrutura Multimodal do Registro de Memória

Em um RAG tradicional, os chunks são armazenados apenas com vetores semânticos. No EmotionRAG, cada registro de memória $m_i$ é uma **tupla multimodal**:

$$m_i = (\vec{d}_i^{\text{sem}},\ \vec{e}_{m_i},\ I_{m_i},\ t_{m_i},\ \text{meta}_i)$$

onde:

- $\vec{d}_i^{\text{sem}}$: vetor semântico (embedding do conteúdo, como no RAG clássico),
- $\vec{e}_{m_i}$: vetor emocional (o estado afetivo do agente **no momento** em que a memória foi formada),
- $I_{m_i} = \|\vec{e}_{m_i}\|$: **intensidade emocional** — um escalar derivado da norma do vetor, rastreado explicitamente como campo separado para permitir filtragem e ordenação rápida sem recalcular normas,
- $t_{m_i}$: timestamp de criação,
- $\text{meta}_i$: metadados adicionais (agente de origem, interlocutor, domínio, fase da conversa).

A separação explícita de $I_{m_i}$ como campo indexável é uma otimização prática: permite queries eficientes do tipo "todas as memórias com intensidade > $\theta$" sem varredura vetorial completa, viabilizando o mecanismo de promoção descrito a seguir.

### 5.2 Critério de Promoção

Uma memória $m_i$ é promovida de **temporária** para **pseudopermanente** quando:

$$\|\vec{e}_{m_i}\| > \theta_{\text{promotion}}$$

onde $\theta_{\text{promotion}}$ é um limiar calibrável. Opcionalmente, o critério pode ser multifatorial:

$$\text{promote}(m_i) \iff \|\vec{e}_{m_i}\| > \theta_1 \quad \lor \quad (\text{freq}(m_i) > \theta_2 \land |\text{valência}(m_i)| > \theta_3)$$

Isso captura tanto eventos únicos de alto impacto (um "trauma" ou "eureka") quanto padrões repetidos com carga emocional moderada (reforço cumulativo).

### 5.3 Estrutura de Armazenamento

```
┌──────────────────────────────────────────────────────┐
│              HIERARQUIA DE MEMÓRIA                     │
│                                                        │
│  ┌──────────────────────────────────────────────┐     │
│  │  NÍVEL 3 — MEMÓRIA PSEUDOPERMANENTE          │     │
│  │  (Armazenamento persistente, acesso rápido)   │     │
│  │  • Alta carga emocional                       │     │
│  │  • Eventos marcantes / decisões críticas      │     │
│  │  • Decaimento: muito lento (meses/anos)       │     │
│  │  • Indexação: semântica + emocional + temporal │     │
│  └──────────────────────────┬───────────────────┘     │
│                             │ promoção                  │
│  ┌──────────────────────────▼───────────────────┐     │
│  │  NÍVEL 2 — MEMÓRIA EPISÓDICA DE MÉDIO PRAZO  │     │
│  │  (Cache persistente, acesso moderado)         │     │
│  │  • Carga emocional moderada                   │     │
│  │  • Interações recentes com significado        │     │
│  │  • Decaimento: horas a dias                   │     │
│  │  • Indexação: semântica + temporal             │     │
│  └──────────────────────────┬───────────────────┘     │
│                             │ promoção                  │
│  ┌──────────────────────────▼───────────────────┐     │
│  │  NÍVEL 1 — MEMÓRIA DE TRABALHO (BUFFER)      │     │
│  │  (Volátil, acesso imediato)                   │     │
│  │  • Contexto da conversa atual                 │     │
│  │  • Emoção temporária corrente ē(t)            │     │
│  │  • Decaimento: segundos a minutos             │     │
│  │  • Sem indexação vetorial (in-context)         │     │
│  └──────────────────────────────────────────────┘     │
└──────────────────────────────────────────────────────┘
```

### 5.4 Recuperação com Viés Emocional (Mood-Congruent Retrieval)

Na hora da recuperação, memórias pseudopermanentes cujo vetor emocional é **congruente** com o estado corrente recebem um boost:

$$\text{score}_{\text{mem}}(m_i) = \text{sim}_{\text{sem}}(q, m_i) + \beta \cdot \text{sim}_{\text{emo}}(\vec{e}(t), \vec{e}_{m_i}) + \delta \cdot \text{nivel}(m_i)$$

onde $\text{nivel}(m_i) \in \{1, 2, 3\}$ pondera a importância hierárquica da memória.

Isso reproduz o fenômeno psicológico: quando estamos tristes, lembramos mais facilmente de eventos tristes; quando felizes, de eventos alegres. Para um agente, isso gera **coerência emocional longitudinal**.

---

## 6. Contexto Baseado em Cognição

### 6.1 O Papel da Cognição no RAG

A camada cognitiva funciona como um **filtro de relevância de alto nível**. Enquanto a semântica responde "sobre o que estamos falando?" e a emoção responde "como estamos nos sentindo?", a cognição responde:

- **"O que estamos tentando alcançar?"** (intenções/goals),
- **"O que sabemos sobre o mundo?"** (crenças/modelo de mundo),
- **"O que sabemos sobre quem está conosco?"** (modelo do interlocutor),
- **"Quais são nossos princípios?"** (normas/valores).

### 6.2 Representação do Contexto Cognitivo

O contexto cognitivo $C(t)$ pode ser representado como um dicionário estruturado:

```json
{
  "active_goals": [
    {"id": "g1", "description": "Ajudar o usuário a depurar código", "priority": 0.9},
    {"id": "g2", "description": "Manter tom empático", "priority": 0.7}
  ],
  "beliefs": {
    "user_expertise": "intermediário",
    "user_emotional_state_estimate": [0.3, -0.2, 0.5, 0.4, 0.1, 0.6],
    "task_complexity": "alta",
    "time_pressure": true
  },
  "norms": {
    "formality_level": 0.6,
    "honesty_commitment": 1.0,
    "emotional_expressiveness": 0.7
  },
  "conversation_phase": "diagnóstico_do_problema"
}
```

### 6.3 Influência na Recuperação

A relevância cognitiva $\text{rel}_{\text{cog}}(C(t), d_i)$ pode ser implementada como:

1. **Filtragem por goals**: documentos etiquetados com metadados de domínio são filtrados pelos goals ativos.
2. **Ajuste por crenças**: se o agente acredita que o usuário é iniciante, penaliza documentos técnicos avançados; se acredita que há urgência, prioriza respostas concisas.
3. **Modulação por normas**: se o agente tem norma de alta expressividade emocional, amplifica o peso de $\beta$ (componente emocional) na função de score.

Essa camada pode ser implementada como um **re-ranker** baseado em regras ou como um modelo leve (cross-encoder) fine-tuned para relevância contextual.

---

## 7. Máquina de Estados Emocional — Design Detalhado

### 7.1 Justificativa

Por que uma máquina de estados e não simplesmente um modelo contínuo? Porque a FSM oferece:

- **Determinismo**: dado um estado e um estímulo, a transição é previsível e auditável.
- **Interpretabilidade**: é possível explicar "o agente está em estado X porque recebeu estímulo Y no estado Z".
- **Restrições explícitas**: certas transições podem ser proibidas (um agente de atendimento ao cliente não deve transitar para "raiva" contra o usuário, por exemplo).
- **Composição hierárquica**: usando HFSM (Hierarchical FSM), estados podem conter subestados, permitindo granularidade ajustável.

### 7.2 Definição Formal

Uma Máquina de Estados Emocional é definida pela 6-tupla:

$$\mathcal{M} = (S, S_0, \Sigma, \delta, \vec{E}, \Omega)$$

onde:

- $S = \{s_1, s_2, ..., s_k\}$: conjunto finito de **estados emocionais** (ex: neutro, alegre, triste, ansioso, empático, frustrado, curioso, calmo),
- $S_0 \in S$: estado inicial (tipicamente "neutro" ou o baseline configurado),
- $\Sigma$: alfabeto de **estímulos** (sinais da camada de percepção, classificados em categorias como: elogio, crítica, urgência, ambiguidade, sucesso, falha, tédio, novidade...),
- $\delta: S \times \Sigma \rightarrow S$ (determinístico) ou $\delta: S \times \Sigma \rightarrow \mathcal{P}(S)$ com distribuição de probabilidade (estocástico): função de **transição**,
- $\vec{E}: S \rightarrow \mathbb{R}^n$: função que mapeia cada estado para seu **vetor emocional** associado,
- $\Omega$: conjunto de **restrições** (transições proibidas, tempo mínimo em um estado, etc).

### 7.3 Transições Determinísticas vs. Estocásticas

#### Modo Determinístico

Adequado para agentes que precisam de **comportamento previsível e auditável** (ex: atendimento ao cliente, saúde, educação):

| Estado Atual | Estímulo         | Próximo Estado |
|-------------|------------------|----------------|
| Neutro      | Elogio           | Alegre         |
| Neutro      | Crítica leve     | Empático       |
| Neutro      | Crítica severa   | Preocupado     |
| Alegre      | Sucesso          | Alegre (reforço)|
| Alegre      | Falha            | Preocupado     |
| Empático    | Resolução        | Calmo          |
| Empático    | Frustração user  | Ansioso        |

#### Modo Estocástico (com Influência Ambiental)

Adequado para agentes que operam em **ambientes dinâmicos e imprevisíveis** (ex: jogos, simulações, agentes sociais). Aqui, a transição é probabilística e pode ser influenciada por variáveis externas.

**Enquadramento formal como Processo de Decisão de Markov (MDP):** As transições emocionais estocásticas podem ser formalizadas como um MDP onde os estados são os estados emocionais, as ações são as "respostas" do agente, e as transições são governadas por uma matriz de probabilidade condicional. Isso permite aplicar todo o ferramental teórico de MDPs — incluindo políticas ótimas e value iteration — para encontrar estratégias emocionais de longo prazo.

A probabilidade de transição é:

$$P(s' | s, \sigma, \vec{env}) = \text{softmax}\left(\frac{\vec{w}_{s,\sigma} \cdot \vec{env}}{\tau}\right)$$

onde:

- $\vec{env}$: vetor de variáveis ambientais (horário, carga de trabalho, número de interações recentes, sentimento médio dos últimos N inputs),
- $\vec{w}_{s,\sigma}$: pesos aprendidos para a transição do estado $s$ sob estímulo $\sigma$,
- $\tau$: temperatura (controla aleatoriedade — menor = mais determinístico).

Isso permite que o **mesmo estímulo** produza **transições diferentes** dependendo do contexto ambiental: o agente pode reagir com mais irritabilidade quando sobrecarregado, ou com mais calma quando o ambiente está tranquilo.

**Exemplo concreto de matriz de transição probabilística:** Mesmo recebendo um insulto (gatilho negativo), se o agente estiver no estado "Alegre", pode haver 80% de probabilidade de transição para "Confuso" e apenas 20% de transição imediata para "Irritado" — modelando a resistência natural de estados positivos a perturbações negativas brandas.

> **Risco crítico — Loops emocionais incoerentes:** Se a matriz de probabilidade não for bem calibrada, o modo estocástico pode produzir ciclos emocionais rápidos e incoerentes (ex: alegre → irritado → alegre → irritado em turnos consecutivos). Mitigações incluem: impor **tempo mínimo de permanência** em cada estado via $\Omega$ (restrições da FSM), aplicar **suavização exponencial** nas transições, e monitorar a **entropia emocional** (variância dos estados recentes) com alertas quando ela excede um limiar.

### 7.4 Hierarquia de Estados (HFSM)

Para evitar explosão combinatória, estados podem ser organizados hierarquicamente:

```
NÍVEL 0 (Macro): Positivo | Neutro | Negativo
    │
    ├── Positivo
    │     ├── Alegre
    │     ├── Curioso
    │     ├── Empático-Positivo
    │     └── Confiante
    │
    ├── Neutro
    │     ├── Calmo
    │     ├── Analítico
    │     └── Atento
    │
    └── Negativo
          ├── Preocupado
          ├── Frustrado
          ├── Ansioso
          └── Empático-Negativo
```

Transições entre macroestados requerem estímulos de maior intensidade, enquanto transições dentro de um macroestado são mais fluidas. Isso gera um comportamento emocional mais natural e menos "bipolar".

---

## 8. Pipeline Completo — Fluxo de Execução

```
1.  INPUT chega (mensagem do usuário / evento / sinal de outro agente)
          │
2.  ┌─────▼─────┐
    │ PERCEPÇÃO  │ → extrai embedding semântico + vetor emocional do input
    └─────┬─────┘
          │
3.  ┌─────▼──────────────┐
    │ MÁQUINA DE ESTADOS │ → transita para novo estado emocional ē(t)
    │    (FSM / HFSM)    │   com base em ē(t-1), sinal do input e contexto
    └─────┬──────────────┘
          │
4.  ┌─────▼───────────────┐
    │ ATUALIZAÇÃO         │ → atualiza C(t): revisa goals, crenças,
    │ COGNITIVA           │   modelo do interlocutor
    └─────┬───────────────┘
          │
5.  ┌─────▼───────────────┐
    │ RECUPERAÇÃO         │ → busca em paralelo:
    │ (EmotionRAG)        │   • Store semântico (sim. cosseno com query)
    │                     │   • Store emocional (sim. com ē(t))
    │                     │   • Memórias pseudopermanentes (boost)
    │                     │   • Filtro cognitivo (goals/crenças)
    │                     │   → fusão dos rankings via score ponderado
    └─────┬───────────────┘
          │
6.  ┌─────▼───────────────┐
    │ CONSTRUÇÃO DO       │ → monta prompt com:
    │ PROMPT              │   • Documentos recuperados
    │                     │   • Estado emocional em linguagem natural
    │                     │   • Diretrizes cognitivas
    │                     │   • Instrução de tom/estilo
    └─────┬───────────────┘
          │
7.  ┌─────▼───────────────┐
    │ GERAÇÃO (LLM)       │ → produz resposta
    └─────┬───────────────┘
          │
8.  ┌─────▼───────────────┐
    │ PÓS-PROCESSAMENTO   │ → extrai vetor emocional da resposta gerada
    │                     │ → avalia magnitude emocional da interação
    │                     │ → decide: armazenar como memória nível 1, 2 ou 3?
    │                     │ → atualiza stores
    └─────────────────────┘
```

---

## 9. Aplicações em Sistemas Multiagentes

### 9.1 Agentes com "Personalidades" Distintas

Em um sistema multiagente, cada agente pode ter:

- Um **baseline emocional** diferente ($\vec{e}_{\text{baseline}}$ configurado para o "temperamento" do agente),
- Uma **FSM com transições diferentes** (um agente mais "resiliente" resiste a transitar para estados negativos; um mais "sensível" transita mais facilmente),
- Um **store de memórias pseudopermanentes próprio** (experiências únicas do agente),
- **Pesos $\alpha, \beta, \gamma$ diferentes** na função de score (um agente mais "analítico" prioriza semântica; um mais "emocional" prioriza o componente afetivo).

### 9.2 Contágio Emocional Entre Agentes

Quando agentes se comunicam entre si, o output emocional de um agente torna-se input emocional do outro. Isso cria uma dinâmica de **contágio emocional** que pode ser modelada explicitamente:

$$\vec{s}_{\text{agente B}}(t) = \alpha_{\text{contágio}} \cdot \vec{e}_{\text{agente A}}(t) + (1 - \alpha_{\text{contágio}}) \cdot \vec{s}_{\text{externo}}(t)$$

Isso é útil para simular dinâmicas de grupo (equipes de agentes que "se contagiam" emocionalmente), mas deve ser calibrado para evitar loops de feedback positivo descontrolados (espiral emocional).

### 9.3 Negociação Emocional

Em cenários de negociação entre agentes, o estado emocional pode influenciar estratégias: um agente que detecta ansiedade no interlocutor pode adotar postura mais conciliadora (se cooperativo) ou mais agressiva (se competitivo). A FSM permite codificar essas heurísticas de forma transparente e auditável.

### 9.4 Resolução de Conflitos via Detecção Emocional Vetorial

Em sistemas multiagentes colaborativos, conflitos de decisão são frequentes. A detecção do vetor emocional dos agentes envolvidos abre uma via de resolução sofisticada: se o agente mediador detecta que o agente A apresenta **frustração vetorial alta** ($\|\vec{e}_A\|$ elevado com valência negativa e dominância baixa), o sistema pode automaticamente acionar protocolos de **concessão calibrada** — o agente com menor carga emocional cede primeiro, ou um agente mediador intervém com tom empático antes de propor soluções lógicas.

Isso transforma negociações multiagente de disputas puramente baseadas em lógica/utilidade para processos que incorporam **inteligência emocional artificial** — uma vantagem significativa em simulações sociais, tutoria colaborativa e atendimento ao cliente com múltiplos agentes especializados.

### 9.5 Posicionamento em Frameworks Existentes

A arquitetura EmotionRAG representa um provável **próximo passo evolutivo** para frameworks multiagentes como AutoGen, CrewAI, LangGraph e similares. Atualmente, esses sistemas orquestram agentes como "operários lógicos" — executores de tarefas sem estado afetivo. A camada emocional proposta transformaria esses agentes em **colaboradores cognitivos** com personalidades persistentes, memórias significativas e respostas emocionalmente contextualizadas. A integração pode ocorrer como uma camada middleware que se interpõe entre o orquestrador existente e os agentes, sem exigir mudanças na infraestrutura LLM subjacente.

---

## 10. Avaliação Crítica — Viabilidade e Limitações

### 10.1 Pontos Fortes

| Aspecto | Avaliação |
|--------|-----------|
| **Coerência emocional** | A arquitetura força o sistema a manter consistência afetiva ao longo da conversa, algo que LLMs "memoryless" não fazem naturalmente. |
| **Interpretabilidade** | A FSM torna as decisões emocionais auditáveis e explicáveis, atendendo requisitos de responsabilidade. |
| **Personalização** | Diferentes configurações de FSM, baseline e pesos permitem criar agentes com "personalidades" distintas sem re-treinar modelos. |
| **Memória emocional** | O mecanismo de promoção por intensidade emocional resolve elegantemente o problema de "o que vale a pena lembrar", priorizando automaticamente eventos significativos. |
| **Modularidade** | Cada componente pode ser desenvolvido, testado e substituído independentemente. |
| **Economia de tokens e contexto** | O descarte ativo de memórias de baixa intensidade emocional ($I < \theta$) funciona como um mecanismo natural de **garbage collection cognitiva**: economiza tokens no prompt do LLM, reduz custo computacional no RAG e mantém o contexto focado nas interações que realmente importam. Em pipelines com janelas de contexto limitadas, isso é uma vantagem operacional concreta. |

### 10.2 Limitações e Riscos

| Aspecto | Limitação |
|--------|-----------|
| **Complexidade de engenharia** | A arquitetura é significativamente mais complexa que RAG vanilla. Requer manutenção de FSM, múltiplos stores vetoriais, camada cognitiva e lógica de promoção de memórias. |
| **Risco de "uncanny valley" emocional** | Se mal calibrado, o agente pode parecer emocionalmente artificial ou manipulativo. A expressão emocional precisa ser sutil e contextualmente apropriada. |
| **Validação difícil** | Como medir se a "emoção" do agente é "correta"? Não há ground truth objetiva para estados emocionais artificiais. Métricas como coerência percebida pelo usuário são subjetivas. |
| **Custo computacional** | Múltiplas buscas vetoriais, avaliação da FSM e camada cognitiva adicionam latência. Em aplicações real-time com alta carga, pode ser proibitivo. |
| **Ética e manipulação** | Um agente que modela e responde a emoções pode ser usado para manipulação emocional. Guardrails éticos rigorosos são imprescindíveis. |
| **Explosão de estados** | Sem hierarquia (HFSM), o número de transições cresce quadraticamente com o número de estados e estímulos. |
| **Alinhamento de modelos de embedding** | O modelo de embeddings semânticos (ex: `text-embedding-3`, `BGE-M3`) não foi treinado para representar coordenadas no espaço VAD emocional. É necessário treinar ou fine-tunar um modelo separado de embedding emocional — ou usar um modelo multitarefa que projete texto simultaneamente em espaço semântico e emocional. Este é um desafio de engenharia de ML não trivial e uma das maiores barreiras à implementação de alta qualidade. |

### 10.3 Quando Usar (e Quando Não Usar)

**Usar quando:**
- O agente precisa de interação prolongada e relacional (terapia, educação, companhia),
- A personalização emocional é diferencial de produto,
- O sistema multiagente simula dinâmicas sociais (jogos, simulações),
- A auditabilidade do comportamento emocional é requisito (saúde, compliance).

**Não usar quando:**
- A tarefa é puramente factual e transacional (busca de dados, API calls),
- Latência é crítica e a carga emocional é irrelevante,
- O custo de engenharia não se justifica pelo ganho de UX.

---

## 11. Direções Futuras e Pesquisa Aberta

### 11.1 Aprendizado da FSM por Reforço

Em vez de projetar as transições manualmente, usar **Reinforcement Learning** para aprender a FSM ótima: o agente maximiza uma reward que combina satisfação do usuário, coerência emocional e eficácia na tarefa. Isso permitiria FSMs que se adaptam a populações de usuários ou domínios específicos.

### 11.2 Embeddings Emocionais Aprendidos

Treinar um modelo de embedding que projete texto diretamente em um espaço emocional otimizado para a tarefa (em vez de usar modelos genéricos de sentiment analysis). Isso pode ser feito com contrastive learning usando pares (texto, vetor emocional anotado).

### 11.3 Integração com Modelos de Mundo

Conectar a camada cognitiva a um **world model** aprendido, onde o agente pode simular as consequências emocionais de suas ações antes de executá-las: "se eu disser X, o usuário provavelmente sentirá Y, o que me levará ao estado Z". Isso se aproxima de teoria da mente computacional.

### 11.4 Emoção como Dimensão de Busca em Bases de Conhecimento

Estender a ideia para bases de conhecimento corporativas: documentos, tickets de suporte, interações com clientes podem ser indexados com vetores emocionais, permitindo buscas como "encontre casos similares onde o cliente estava frustrado e a resolução foi positiva". Isso já seria aplicável com tecnologia atual.

### 11.5 Benchmarks e Métricas

Desenvolvimento de benchmarks específicos para avaliação de RAG emocional: coerência emocional longitudinal, mood-congruency de recuperação, adequação de tom percebida por avaliadores humanos, e ausência de vieses manipulativos.

---

## 12. Conclusão

A proposta de um **EmotionRAG** — RAG orientado por emoções vetoriais temporárias, memórias pseudopermanentes e contexto cognitivo — representa uma extensão natural e teoricamente fundamentada dos sistemas de recuperação e geração atuais. Ao trazer conceitos da psicologia cognitiva e neurociência para o design de sistemas multiagentes, a arquitetura oferece um caminho para agentes que são não apenas inteligentes, mas **emocionalmente coerentes e contextualmente sensíveis**.

A máquina de estados emocional fornece o equilíbrio necessário entre **previsibilidade** (modo determinístico, adequado para domínios regulados) e **adaptabilidade** (modo estocástico com influência ambiental, adequado para cenários dinâmicos). A hierarquia de memória com promoção baseada em intensidade emocional resolve o problema de priorização de memórias de forma bioinspiradora e computacionalmente tratável.

Os desafios são reais — complexidade de engenharia, riscos éticos, dificuldade de validação — mas não são intransponíveis. Para domínios onde a dimensão emocional da interação é central (saúde mental, educação personalizada, agentes de companhia, simulações sociais), esta arquitetura oferece um framework concreto e implementável que transcende o paradigma puramente semântico do RAG convencional.

A emoção não é ruído a ser filtrado — é sinal a ser integrado.

---

## Apêndice A — Glossário de Referência

| Termo | Definição |
|-------|-----------|
| **Vetor emocional** $\vec{e}(t)$ | Representação n-dimensional do estado afetivo instantâneo do agente |
| **Memória pseudopermanente** | Registro de interação/evento promovido ao nível mais alto de persistência por critério de intensidade emocional |
| **FSM Emocional** | Autômato finito que modela transições entre estados emocionais discretos |
| **Mood-congruent retrieval** | Viés de recuperação que prioriza memórias emocionalmente congruentes com o estado corrente |
| **Contexto cognitivo** $C(t)$ | Estrutura que agrega intenções, crenças, modelo do interlocutor e normas do agente |
| **Baseline emocional** | Ponto de equilíbrio emocional para o qual o agente tende na ausência de estímulos |
| **Contágio emocional** | Transferência de estado emocional entre agentes em um sistema multiagente |
| **HFSM** | Hierarchical Finite State Machine — FSM com subestados aninhados |
| **Matriz de suscetibilidade** $\mathbf{W}$ | Matriz que modela como fortemente estímulos externos afetam o estado emocional do agente — parametriza a "personalidade reativa" |
| **Intensidade emocional** $I$ | Escalar derivado da norma do vetor emocional; determina promoção de memórias e peso na recuperação |
| **Entropia emocional** | Medida de variância dos estados emocionais recentes; indicador de instabilidade emocional do agente |
| **Prompt cognitivo estruturado** | Template de prompt em três camadas (cenário semântico, estado interno, ressonância de memória) que substitui o contexto plano do RAG clássico |

## Apêndice B — Referências Teóricas

1. Russell, J. A. (1980). *A circumplex model of affect.* Journal of Personality and Social Psychology.
2. Ortony, A., Clore, G. L., & Collins, A. (1988). *The Cognitive Structure of Emotions.* Cambridge University Press.
3. Lazarus, R. S. (1991). *Emotion and Adaptation.* Oxford University Press.
4. Picard, R. W. (1997). *Affective Computing.* MIT Press.
5. Lewis, P. et al. (2020). *Retrieval-Augmented Generation for Knowledge-Intensive NLP Tasks.* NeurIPS.
6. Park, J. S. et al. (2023). *Generative Agents: Interactive Simulacra of Human Behavior.* UIST.
7. Weidinger, L. et al. (2022). *Taxonomy of Risks posed by Language Models.* FAccT.
