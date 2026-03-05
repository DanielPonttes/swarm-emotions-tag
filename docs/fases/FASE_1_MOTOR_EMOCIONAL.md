# FASE 1 - Motor Emocional em Rust

> **Duracao estimada:** 5 semanas (Semana 3-7)
> **Equipe minima:** 1 engenheiro Rust
> **Pre-requisitos:** Fase 0 concluida (monorepo, proto, infra)
> **Resultado:** Motor Rust funcional com FSM, calculo vetorial, score fusion e servidor gRPC
> **Paralelizavel com:** Fase 2 (Go) a partir da Semana 5

---

## 1.1 Objetivo

Implementar o **plano computacional** do EmotionRAG inteiramente em Rust:
- FSM/HFSM Engine com transicoes determinísticas
- Motor vetorial (calculo de e(t+1), decaimento, similaridade cosseno)
- Score fusion ponderado
- Servidor gRPC expondo todas as operacoes

Ao final, o motor Rust e um servico autonomo testavel via `grpcurl`.

---

## 1.2 Organizacao dos Modulos Rust

```
emotion-engine/
├── Cargo.toml
├── build.rs
├── src/
│   ├── main.rs                  # Entrypoint: monta servidor gRPC
│   ├── lib.rs                   # Re-exporta modulos publicos
│   ├── server.rs                # Implementacao do trait gRPC (tonic)
│   ├── fsm/
│   │   ├── mod.rs
│   │   ├── state.rs             # Enum EmotionalState + mapeamento para vetores
│   │   ├── transition.rs        # Tabela de transicoes + funcao delta
│   │   ├── constraints.rs       # Restricoes Omega (proibicoes, tempo minimo)
│   │   └── config.rs            # Desserializacao de config FSM (TOML/JSON)
│   ├── vector/
│   │   ├── mod.rs
│   │   ├── emotion.rs           # EmotionVector: struct wrapper sobre ndarray
│   │   ├── compute.rs           # e(t+1) = e(t) + W*g(t) + epsilon
│   │   ├── decay.rs             # Decaimento exponencial
│   │   ├── similarity.rs        # Similaridade cosseno (single + batch)
│   │   └── matrix.rs            # SusceptibilityMatrix: wrapper sobre ndarray
│   ├── scoring/
│   │   ├── mod.rs
│   │   └── fusion.rs            # alpha*sem + beta*emo + gamma*cog + boost
│   ├── promotion/
│   │   ├── mod.rs
│   │   └── evaluator.rs         # Logica de promocao L1->L2->L3
│   └── proto.rs                 # Re-export do modulo gerado por tonic-build
├── tests/
│   ├── fsm_tests.rs             # Testes de transicao + restricoes
│   ├── vector_tests.rs          # Golden tests de calculo vetorial
│   ├── scoring_tests.rs         # Testes de fusao e ranking
│   ├── promotion_tests.rs       # Testes de criterios de promocao
│   ├── stability_tests.rs       # Teste de 10.000 iteracoes sem divergencia
│   └── integration_tests.rs     # Testes gRPC end-to-end
├── benches/
│   └── hot_path.rs              # Benchmarks com criterion
└── config/
    ├── default_fsm.toml         # FSM padrao com 8 estados
    └── presets/
        ├── resilient.toml       # Personalidade resiliente
        ├── reactive.toml        # Personalidade reativa
        └── empathic.toml        # Personalidade empatica
```

---

## 1.3 Entregavel 1.1 - FSM Engine (Semana 3-4)

### 1.3.1 Estados Emocionais

```rust
// src/fsm/state.rs

use serde::{Deserialize, Serialize};

/// Estados emocionais discretos do agente.
/// Fase 1: FSM plana (8 estados). Fase 6: HFSM com hierarquia.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EmotionalState {
    Neutral,
    Joyful,
    Curious,
    Empathetic,
    Calm,
    Worried,
    Frustrated,
    Anxious,
}

impl EmotionalState {
    /// Macro-estado para futura HFSM (Fase 6).
    /// Por ora, usado apenas para logging e metricas.
    pub fn macro_state(&self) -> MacroState {
        match self {
            Self::Joyful | Self::Curious | Self::Empathetic => MacroState::Positive,
            Self::Neutral | Self::Calm => MacroState::Neutral,
            Self::Worried | Self::Frustrated | Self::Anxious => MacroState::Negative,
        }
    }

    /// Vetor emocional prototipico associado a cada estado discreto.
    /// Formato: [valencia, ativacao, dominancia, certeza, social, novidade]
    /// Valores em [-1, 1].
    pub fn prototype_vector(&self) -> [f32; 6] {
        match self {
            //                    V     A     D     C     S     N
            Self::Neutral    => [ 0.0,  0.0,  0.0,  0.5,  0.0,  0.0],
            Self::Joyful     => [ 0.8,  0.7,  0.6,  0.7,  0.5,  0.3],
            Self::Curious    => [ 0.4,  0.6,  0.3,  0.2,  0.3,  0.9],
            Self::Empathetic => [ 0.3,  0.3,  0.2,  0.4,  0.9,  0.2],
            Self::Calm       => [ 0.3,  -0.5, 0.5,  0.8,  0.2,  -0.3],
            Self::Worried    => [-0.4,  0.5,  -0.3, -0.5, 0.4,  0.3],
            Self::Frustrated => [-0.6,  0.7,  -0.2, -0.3, -0.4, -0.2],
            Self::Anxious    => [-0.5,  0.8,  -0.5, -0.7, 0.1,  0.5],
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum MacroState {
    Positive,
    Neutral,
    Negative,
}
```

### 1.3.2 Estimulos

```rust
// src/fsm/state.rs (continuacao)

/// Tipos de estimulo que disparam transicoes na FSM.
#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Stimulus {
    Praise,          // Elogio
    MildCriticism,   // Critica leve
    SevereCriticism, // Critica severa
    Success,         // Sucesso na tarefa
    Failure,         // Falha na tarefa
    Urgency,         // Sinal de urgencia
    Ambiguity,       // Ambiguidade/confusao
    Novelty,         // Novidade/surpresa
    Resolution,      // Resolucao de problema
    UserFrustration, // Frustracao detectada no usuario
    Boredom,         // Tedio / falta de estimulo
    Empathy,         // Sinal empatico recebido
    Custom(String),  // Estimulos customizados
}
```

### 1.3.3 Tabela de Transicoes

```rust
// src/fsm/transition.rs

use std::collections::HashMap;
use super::state::{EmotionalState, Stimulus};

/// Tabela de transicoes: (estado_atual, estimulo) -> proximo_estado
pub struct TransitionTable {
    transitions: HashMap<(EmotionalState, Stimulus), EmotionalState>,
}

impl TransitionTable {
    pub fn new() -> Self {
        Self {
            transitions: HashMap::new(),
        }
    }

    /// Carrega transicoes de config (TOML/JSON)
    pub fn from_config(config: &FsmConfig) -> Self {
        let mut table = Self::new();
        for rule in &config.rules {
            let from = rule.from.parse().expect("invalid state");
            let stimulus = rule.stimulus.parse().expect("invalid stimulus");
            let to = rule.to.parse().expect("invalid state");
            table.transitions.insert((from, stimulus), to);
        }
        table
    }

    /// Busca a transicao. Retorna None se nao ha regra definida.
    pub fn get(&self, current: EmotionalState, stimulus: &Stimulus) -> Option<EmotionalState> {
        self.transitions.get(&(current, stimulus.clone())).copied()
    }

    /// Valida que nao ha estados "sink" (sem saida) nem estados inalcancaveis.
    pub fn validate(&self) -> Result<(), Vec<String>> {
        let mut errors = Vec::new();
        let all_states: Vec<EmotionalState> = vec![
            EmotionalState::Neutral, EmotionalState::Joyful,
            EmotionalState::Curious, EmotionalState::Empathetic,
            EmotionalState::Calm, EmotionalState::Worried,
            EmotionalState::Frustrated, EmotionalState::Anxious,
        ];

        // Verificar que todo estado tem pelo menos uma saida
        for state in &all_states {
            let has_exit = self.transitions.keys().any(|(s, _)| s == state);
            if !has_exit {
                errors.push(format!("State {:?} has no outgoing transitions (sink state)", state));
            }
        }

        // Verificar que todo estado e alcancavel (exceto Neutral que e inicial)
        for state in &all_states {
            if *state == EmotionalState::Neutral {
                continue;
            }
            let is_reachable = self.transitions.values().any(|s| s == state);
            if !is_reachable {
                errors.push(format!("State {:?} is unreachable", state));
            }
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors)
        }
    }
}
```

### 1.3.4 Restricoes Omega

```rust
// src/fsm/constraints.rs

use std::collections::HashSet;
use std::time::{Duration, Instant};
use super::state::{EmotionalState, Stimulus};

/// Restricoes que bloqueiam transicoes mesmo quando a tabela as permite.
pub struct Constraints {
    /// Transicoes explicitamente proibidas
    forbidden: HashSet<(EmotionalState, Stimulus, EmotionalState)>,

    /// Tempo minimo de permanencia em cada estado
    min_duration: std::collections::HashMap<EmotionalState, Duration>,
}

/// Resultado de verificacao de restricao
pub enum ConstraintCheck {
    Allowed,
    Blocked { reason: String },
}

impl Constraints {
    pub fn check(
        &self,
        current: EmotionalState,
        stimulus: &Stimulus,
        target: EmotionalState,
        state_entered_at: Instant,
    ) -> ConstraintCheck {
        // Verificar transicao proibida
        if self.forbidden.contains(&(current, stimulus.clone(), target)) {
            return ConstraintCheck::Blocked {
                reason: format!(
                    "Transition {:?} --{:?}--> {:?} is forbidden by Omega constraints",
                    current, stimulus, target
                ),
            };
        }

        // Verificar tempo minimo no estado atual
        if let Some(min_dur) = self.min_duration.get(&current) {
            let elapsed = state_entered_at.elapsed();
            if elapsed < *min_dur {
                return ConstraintCheck::Blocked {
                    reason: format!(
                        "State {:?} requires minimum {:?}, only {:?} elapsed",
                        current, min_dur, elapsed
                    ),
                };
            }
        }

        ConstraintCheck::Allowed
    }
}
```

### 1.3.5 Config FSM (TOML)

```toml
# config/default_fsm.toml

[metadata]
name = "default"
version = "0.1.0"
description = "FSM padrao com 8 estados emocionais"

# Transicoes: from + stimulus -> to
[[rules]]
from = "neutral"
stimulus = "praise"
to = "joyful"

[[rules]]
from = "neutral"
stimulus = "mild_criticism"
to = "empathetic"

[[rules]]
from = "neutral"
stimulus = "severe_criticism"
to = "worried"

[[rules]]
from = "neutral"
stimulus = "novelty"
to = "curious"

[[rules]]
from = "neutral"
stimulus = "urgency"
to = "anxious"

[[rules]]
from = "joyful"
stimulus = "success"
to = "joyful"

[[rules]]
from = "joyful"
stimulus = "failure"
to = "worried"

[[rules]]
from = "joyful"
stimulus = "boredom"
to = "neutral"

[[rules]]
from = "curious"
stimulus = "success"
to = "joyful"

[[rules]]
from = "curious"
stimulus = "failure"
to = "frustrated"

[[rules]]
from = "curious"
stimulus = "resolution"
to = "calm"

[[rules]]
from = "empathetic"
stimulus = "resolution"
to = "calm"

[[rules]]
from = "empathetic"
stimulus = "user_frustration"
to = "anxious"

[[rules]]
from = "empathetic"
stimulus = "praise"
to = "joyful"

[[rules]]
from = "worried"
stimulus = "resolution"
to = "calm"

[[rules]]
from = "worried"
stimulus = "failure"
to = "frustrated"

[[rules]]
from = "worried"
stimulus = "empathy"
to = "empathetic"

[[rules]]
from = "frustrated"
stimulus = "resolution"
to = "calm"

[[rules]]
from = "frustrated"
stimulus = "praise"
to = "empathetic"

[[rules]]
from = "frustrated"
stimulus = "failure"
to = "anxious"

[[rules]]
from = "anxious"
stimulus = "resolution"
to = "calm"

[[rules]]
from = "anxious"
stimulus = "empathy"
to = "empathetic"

[[rules]]
from = "anxious"
stimulus = "success"
to = "worried"

[[rules]]
from = "calm"
stimulus = "boredom"
to = "neutral"

[[rules]]
from = "calm"
stimulus = "novelty"
to = "curious"

[[rules]]
from = "calm"
stimulus = "urgency"
to = "worried"

# Restricoes Omega
[constraints]
# Transicoes proibidas: agente nunca transita diretamente de positivo para frustrado
forbidden = [
    { from = "joyful", stimulus = "mild_criticism", to = "frustrated" },
    { from = "calm", stimulus = "mild_criticism", to = "frustrated" },
]

# Tempo minimo em cada estado (em milissegundos)
[constraints.min_duration_ms]
joyful = 2000
anxious = 1000
frustrated = 1500
```

---

## 1.4 Entregavel 1.2 - Motor Vetorial (Semana 4-5)

### 1.4.1 EmotionVector

```rust
// src/vector/emotion.rs

use ndarray::Array1;

/// Vetor emocional n-dimensional.
/// Componentes: [valencia, ativacao, dominancia, certeza, social, novidade]
#[derive(Debug, Clone)]
pub struct EmotionVector {
    data: Array1<f32>,
}

/// Limites por componente para clamping (previne divergencia)
const COMPONENT_MIN: f32 = -1.0;
const COMPONENT_MAX: f32 = 1.0;

impl EmotionVector {
    pub fn new(components: Vec<f32>) -> Self {
        Self {
            data: Array1::from_vec(components),
        }
    }

    pub fn zeros(dim: usize) -> Self {
        Self {
            data: Array1::zeros(dim),
        }
    }

    pub fn dim(&self) -> usize {
        self.data.len()
    }

    pub fn as_slice(&self) -> &[f32] {
        self.data.as_slice().unwrap()
    }

    /// Norma L2 (intensidade emocional)
    pub fn intensity(&self) -> f32 {
        self.data.dot(&self.data).sqrt()
    }

    /// Similaridade cosseno com outro vetor
    pub fn cosine_similarity(&self, other: &EmotionVector) -> f32 {
        let dot = self.data.dot(&other.data);
        let norm_a = self.intensity();
        let norm_b = other.intensity();
        if norm_a == 0.0 || norm_b == 0.0 {
            return 0.0;
        }
        (dot / (norm_a * norm_b)).clamp(-1.0, 1.0)
    }

    /// Clamp cada componente para [-1, 1] (previne divergencia)
    pub fn clamp(&mut self) {
        self.data.mapv_inplace(|v| v.clamp(COMPONENT_MIN, COMPONENT_MAX));
    }

    /// Componente de valencia (indice 0)
    pub fn valence(&self) -> f32 {
        self.data[0]
    }

    /// Componente de ativacao (indice 1)
    pub fn arousal(&self) -> f32 {
        self.data[1]
    }
}
```

### 1.4.2 Calculo de e(t+1) = e(t) + W * g(t) + epsilon

```rust
// src/vector/compute.rs

use ndarray::Array2;
use rand::Rng;
use rand_distr::Normal;

use super::emotion::EmotionVector;
use super::matrix::SusceptibilityMatrix;

/// Parametros para calculo do proximo vetor emocional
pub struct ComputeParams {
    pub enable_noise: bool,
    pub noise_sigma: f32,
}

/// Calcula e(t+1) = e(t) + W * g(t) + epsilon
///
/// - current: e(t) - vetor emocional atual
/// - trigger: g(t) - vetor do gatilho externo
/// - w: W - matriz de suscetibilidade
/// - params: parametros de ruido
///
/// Retorna o novo vetor emocional (ja com clamping aplicado)
pub fn compute_next_emotion(
    current: &EmotionVector,
    trigger: &EmotionVector,
    w: &SusceptibilityMatrix,
    params: &ComputeParams,
) -> EmotionVector {
    // W * g(t)
    let w_times_g = w.multiply_vector(trigger);

    // e(t) + W * g(t)
    let mut result_data = current.raw() + &w_times_g;

    // + epsilon (ruido estocastico opcional)
    if params.enable_noise && params.noise_sigma > 0.0 {
        let mut rng = rand::thread_rng();
        let normal = Normal::new(0.0, params.noise_sigma as f64).unwrap();
        for val in result_data.iter_mut() {
            *val += rng.sample(normal) as f32;
        }
    }

    let mut vec = EmotionVector::from_raw(result_data);
    vec.clamp(); // CRITICO: previne divergencia
    vec
}
```

### 1.4.3 Decaimento Temporal

```rust
// src/vector/decay.rs

use super::emotion::EmotionVector;

/// Aplica decaimento exponencial em direcao ao baseline.
///
/// Formula: e(t) = baseline + (e(t) - baseline) * exp(-lambda * delta_t)
///
/// - emotion: vetor emocional atual
/// - baseline: ponto de equilibrio do agente
/// - lambda: taxa de decaimento (maior = decai mais rapido)
/// - delta_t: tempo desde ultimo estimulo (em segundos ou turnos)
pub fn apply_decay(
    emotion: &EmotionVector,
    baseline: &EmotionVector,
    lambda: f32,
    delta_t: f32,
) -> EmotionVector {
    let decay_factor = (-lambda * delta_t).exp();

    // baseline + (emotion - baseline) * decay_factor
    let diff = emotion.raw() - baseline.raw();
    let decayed = baseline.raw() + &(diff * decay_factor);

    EmotionVector::from_raw(decayed)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_decay_converges_to_baseline() {
        let emotion = EmotionVector::new(vec![0.8, 0.7, 0.6, 0.5, 0.4, 0.3]);
        let baseline = EmotionVector::new(vec![0.0, 0.0, 0.0, 0.0, 0.0, 0.0]);

        // Apos muito tempo, deve convergir para baseline
        let result = apply_decay(&emotion, &baseline, 0.1, 1000.0);

        for val in result.as_slice() {
            assert!(val.abs() < 1e-6, "Expected near-zero, got {}", val);
        }
    }

    #[test]
    fn test_decay_no_time_no_change() {
        let emotion = EmotionVector::new(vec![0.5, -0.3, 0.2, 0.0, 0.7, -0.1]);
        let baseline = EmotionVector::zeros(6);

        let result = apply_decay(&emotion, &baseline, 0.1, 0.0);

        for (a, b) in result.as_slice().iter().zip(emotion.as_slice()) {
            assert!((a - b).abs() < 1e-6);
        }
    }
}
```

### 1.4.4 Similaridade Cosseno em Batch

```rust
// src/vector/similarity.rs

use super::emotion::EmotionVector;

/// Calcula similaridade cosseno entre um vetor query e um batch de candidatos.
/// Retorna vetor de scores no mesmo order dos candidatos.
pub fn batch_cosine_similarity(
    query: &EmotionVector,
    candidates: &[EmotionVector],
) -> Vec<f32> {
    candidates.iter().map(|c| query.cosine_similarity(c)).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_identical_vectors_similarity_1() {
        let a = EmotionVector::new(vec![0.5, 0.3, -0.2, 0.1, 0.7, 0.0]);
        let b = a.clone();
        let sim = a.cosine_similarity(&b);
        assert!((sim - 1.0).abs() < 1e-5);
    }

    #[test]
    fn test_opposite_vectors_similarity_neg1() {
        let a = EmotionVector::new(vec![1.0, 0.0, 0.0, 0.0, 0.0, 0.0]);
        let b = EmotionVector::new(vec![-1.0, 0.0, 0.0, 0.0, 0.0, 0.0]);
        let sim = a.cosine_similarity(&b);
        assert!((sim + 1.0).abs() < 1e-5);
    }

    #[test]
    fn test_zero_vector_similarity_0() {
        let a = EmotionVector::new(vec![0.5, 0.3, 0.0, 0.0, 0.0, 0.0]);
        let b = EmotionVector::zeros(6);
        let sim = a.cosine_similarity(&b);
        assert_eq!(sim, 0.0);
    }
}
```

### 1.4.5 Matriz de Suscetibilidade

```rust
// src/vector/matrix.rs

use ndarray::{Array1, Array2};

/// Matriz de suscetibilidade W (n x n).
/// Modela a "personalidade reativa" do agente.
pub struct SusceptibilityMatrix {
    data: Array2<f32>,
}

impl SusceptibilityMatrix {
    /// Cria a partir de valores row-major
    pub fn from_row_major(values: Vec<f32>, dim: usize) -> Self {
        assert_eq!(values.len(), dim * dim, "Expected {} values for {}x{} matrix", dim*dim, dim, dim);
        let data = Array2::from_shape_vec((dim, dim), values).unwrap();
        Self { data }
    }

    /// Matriz identidade escalada: W = scale * I
    /// Preset seguro para inicio (efeito minimo, sem acoplamento cruzado)
    pub fn scaled_identity(dim: usize, scale: f32) -> Self {
        let mut data = Array2::zeros((dim, dim));
        for i in 0..dim {
            data[[i, i]] = scale;
        }
        Self { data }
    }

    /// Multiplica W * vetor
    pub fn multiply_vector(&self, v: &EmotionVector) -> Array1<f32> {
        self.data.dot(&v.raw())
    }

    /// Valida estabilidade: norma espectral deve ser < 1
    /// (simplificacao: verifica norma Frobenius < 1 como proxy conservador)
    pub fn validate_stability(&self) -> Result<(), String> {
        let frobenius = self.data.iter().map(|x| x * x).sum::<f32>().sqrt();
        if frobenius >= 1.0 {
            Err(format!(
                "Matrix Frobenius norm ({:.4}) >= 1.0 - risk of divergence. \
                 Scale down W or use spectral norm check.",
                frobenius
            ))
        } else {
            Ok(())
        }
    }

    pub fn dimension(&self) -> usize {
        self.data.nrows()
    }
}
```

---

## 1.5 Entregavel 1.3 - Score Fusion (Semana 5)

### 1.5.1 Fusao Ponderada

```rust
// src/scoring/fusion.rs

/// Candidato com scores pre-computados pelos stores
pub struct ScoreCandidate {
    pub memory_id: String,
    pub semantic_score: f32,
    pub emotional_score: f32,
    pub cognitive_score: f32,
    pub memory_level: u32,
    pub is_pseudopermanent: bool,
}

/// Resultado rankeado
pub struct RankedMemory {
    pub memory_id: String,
    pub final_score: f32,
    pub semantic_contribution: f32,
    pub emotional_contribution: f32,
    pub cognitive_contribution: f32,
}

/// Pesos de fusao
pub struct FusionWeights {
    pub alpha: f32,   // peso semantico
    pub beta: f32,    // peso emocional
    pub gamma: f32,   // peso cognitivo
    pub pseudoperm_boost: f32,  // multiplicador para memorias L3
}

impl FusionWeights {
    /// Valida que alpha + beta + gamma = 1 (com tolerancia)
    pub fn validate(&self) -> Result<(), String> {
        let sum = self.alpha + self.beta + self.gamma;
        if (sum - 1.0).abs() > 0.01 {
            return Err(format!("Weights must sum to 1.0, got {:.4}", sum));
        }
        Ok(())
    }
}

/// Calcula score final para cada candidato e retorna ranking ordenado.
///
/// score(d_i) = alpha * sem + beta * emo + gamma * cog
/// Se pseudopermanente: score *= (1 + pseudoperm_boost)
pub fn fuse_and_rank(
    candidates: &[ScoreCandidate],
    weights: &FusionWeights,
) -> Vec<RankedMemory> {
    let mut ranked: Vec<RankedMemory> = candidates
        .iter()
        .map(|c| {
            let sem = weights.alpha * c.semantic_score;
            let emo = weights.beta * c.emotional_score;
            let cog = weights.gamma * c.cognitive_score;
            let mut score = sem + emo + cog;

            if c.is_pseudopermanent {
                score *= 1.0 + weights.pseudoperm_boost;
            }

            RankedMemory {
                memory_id: c.memory_id.clone(),
                final_score: score,
                semantic_contribution: sem,
                emotional_contribution: emo,
                cognitive_contribution: cog,
            }
        })
        .collect();

    // Ordenar por score decrescente
    ranked.sort_by(|a, b| b.final_score.partial_cmp(&a.final_score).unwrap());
    ranked
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_ranking_order() {
        let candidates = vec![
            ScoreCandidate {
                memory_id: "low".into(),
                semantic_score: 0.1, emotional_score: 0.1, cognitive_score: 0.1,
                memory_level: 1, is_pseudopermanent: false,
            },
            ScoreCandidate {
                memory_id: "high".into(),
                semantic_score: 0.9, emotional_score: 0.8, cognitive_score: 0.7,
                memory_level: 3, is_pseudopermanent: true,
            },
        ];
        let weights = FusionWeights {
            alpha: 0.5, beta: 0.3, gamma: 0.2, pseudoperm_boost: 0.2,
        };

        let ranked = fuse_and_rank(&candidates, &weights);
        assert_eq!(ranked[0].memory_id, "high");
        assert_eq!(ranked[1].memory_id, "low");
    }

    #[test]
    fn test_pseudoperm_boost() {
        let base = ScoreCandidate {
            memory_id: "base".into(),
            semantic_score: 0.5, emotional_score: 0.5, cognitive_score: 0.5,
            memory_level: 2, is_pseudopermanent: false,
        };
        let boosted = ScoreCandidate {
            memory_id: "boosted".into(),
            semantic_score: 0.5, emotional_score: 0.5, cognitive_score: 0.5,
            memory_level: 3, is_pseudopermanent: true,
        };
        let weights = FusionWeights {
            alpha: 0.5, beta: 0.3, gamma: 0.2, pseudoperm_boost: 0.5,
        };

        let ranked = fuse_and_rank(&[base, boosted], &weights);
        assert!(ranked[0].final_score > ranked[1].final_score);
        assert_eq!(ranked[0].memory_id, "boosted");
    }
}
```

---

## 1.6 Entregavel 1.4 - Motor de Promocao (Semana 5-6)

```rust
// src/promotion/evaluator.rs

pub struct MemoryCandidate {
    pub memory_id: String,
    pub intensity: f32,
    pub current_level: u32,
    pub access_frequency: u32,
    pub valence_magnitude: f32,
}

pub struct PromotionThresholds {
    pub intensity_threshold: f32,     // theta_1
    pub frequency_threshold: u32,     // theta_2
    pub valence_threshold: f32,       // theta_3
}

pub struct PromotionDecision {
    pub memory_id: String,
    pub should_promote: bool,
    pub target_level: u32,
    pub reason: String,
}

/// Avalia se memorias devem ser promovidas.
///
/// Criterio: promote(m) <=> ||e(m)|| > theta_1 OR (freq(m) > theta_2 AND |valence(m)| > theta_3)
pub fn evaluate_promotions(
    candidates: &[MemoryCandidate],
    thresholds: &PromotionThresholds,
) -> Vec<PromotionDecision> {
    candidates
        .iter()
        .map(|m| {
            let high_intensity = m.intensity > thresholds.intensity_threshold;
            let cumulative = m.access_frequency > thresholds.frequency_threshold
                && m.valence_magnitude > thresholds.valence_threshold;

            let should_promote = high_intensity || cumulative;
            let target = if should_promote { m.current_level + 1 } else { m.current_level };

            let reason = if high_intensity {
                format!("High intensity ({:.3} > {:.3})", m.intensity, thresholds.intensity_threshold)
            } else if cumulative {
                format!(
                    "Cumulative: freq {} > {} AND valence {:.3} > {:.3}",
                    m.access_frequency, thresholds.frequency_threshold,
                    m.valence_magnitude, thresholds.valence_threshold
                )
            } else {
                "Below thresholds".into()
            };

            PromotionDecision {
                memory_id: m.memory_id.clone(),
                should_promote,
                target_level: target.min(3), // Max level 3
                reason,
            }
        })
        .collect()
}
```

---

## 1.7 Entregavel 1.5 - Servidor gRPC (Semana 6-7)

### 1.7.1 Implementacao do Trait gRPC

```rust
// src/server.rs

// Implementacao que conecta o trait gerado pelo tonic com os modulos de negocio.
// Cada metodo do trait converte proto -> dominio, executa logica, converte dominio -> proto.
//
// Padrao:
//   1. Deserializar request proto para structs de dominio
//   2. Chamar funcoes puras de negocio (fsm, vector, scoring, promotion)
//   3. Serializar resultado de volta para proto response
//   4. Retornar Ok(Response) ou Err(Status) com detalhes

// O endpoint ProcessInteraction executa todos os passos em sequencia
// numa unica chamada, minimizando roundtrips gRPC.
```

### 1.7.2 Configuracao do Servidor

```rust
// src/main.rs (versao final)

// Carrega configuracao FSM do arquivo TOML
// Valida tabela de transicoes (sem sinks, sem inalcancaveis)
// Valida matriz W (norma espectral < 1)
// Inicia servidor gRPC com OpenTelemetry tracing
// Graceful shutdown via tokio signal handler
```

---

## 1.8 Benchmarks

### 1.8.1 Criterios de Performance

```rust
// benches/hot_path.rs

use criterion::{criterion_group, criterion_main, Criterion, black_box};

fn bench_fsm_transition(c: &mut Criterion) {
    // Setup: criar FSM com config padrao
    c.bench_function("fsm_transition", |b| {
        b.iter(|| {
            // Medir tempo de uma transicao
            // Meta: < 1 microsegundo
        })
    });
}

fn bench_vector_compute(c: &mut Criterion) {
    // Setup: vetor 6D, matriz 6x6
    c.bench_function("vector_compute_6d", |b| {
        b.iter(|| {
            // e(t+1) = e(t) + W * g(t) (sem ruido)
            // Meta: < 10 microsegundos
        })
    });
}

fn bench_score_fusion_100(c: &mut Criterion) {
    // Setup: 100 candidatos com scores aleatorios
    c.bench_function("score_fusion_100_candidates", |b| {
        b.iter(|| {
            // Fusao + sort de 100 candidatos
            // Meta: < 100 microsegundos
        })
    });
}

fn bench_decay_batch(c: &mut Criterion) {
    // Setup: batch de 1000 vetores emocionais
    c.bench_function("decay_batch_1000", |b| {
        b.iter(|| {
            // Aplicar decaimento em 1000 vetores
        })
    });
}

fn bench_cosine_similarity_batch(c: &mut Criterion) {
    // Setup: 1 query vs 100 candidatos (6D)
    c.bench_function("cosine_similarity_100x6d", |b| {
        b.iter(|| {
            // Batch similarity
        })
    });
}

criterion_group!(
    benches,
    bench_fsm_transition,
    bench_vector_compute,
    bench_score_fusion_100,
    bench_decay_batch,
    bench_cosine_similarity_batch,
);
criterion_main!(benches);
```

---

## 1.9 Testes Obrigatorios

### 1.9.1 Teste de Estabilidade Numerica (CRITICO)

```rust
// tests/stability_tests.rs

#[test]
fn test_10000_iterations_no_divergence() {
    // Simular 10.000 turnos com:
    // - Triggers aleatorios
    // - W = 0.1 * I (identidade escalada)
    // - Sem decaimento
    // - Sem ruido
    //
    // ASSERTIVA: ||e(t)|| nunca excede sqrt(6) (6 componentes clamped em [-1,1])
    //            e ao final, e(t) esta dentro de [-1, 1] por componente
    //
    // Se este teste falhar, ha bug no clamping ou na multiplicacao matricial.
}

#[test]
fn test_convergence_to_baseline_without_stimuli() {
    // Aplicar decaimento 10.000 vezes sem triggers
    // ASSERTIVA: e(t) converge para baseline (diff < 1e-6)
}

#[test]
fn test_w_validation_rejects_unstable_matrix() {
    // Criar W com norma > 1
    // ASSERTIVA: validate_stability() retorna Err
}
```

### 1.9.2 Teste de Validacao do Grafo FSM

```rust
// tests/fsm_tests.rs

#[test]
fn test_default_fsm_no_sink_states() {
    let config = load_config("config/default_fsm.toml");
    let table = TransitionTable::from_config(&config);
    table.validate().expect("Default FSM should be valid");
}

#[test]
fn test_all_states_reachable_from_neutral() {
    // BFS a partir de Neutral
    // ASSERTIVA: todos os 8 estados sao alcancaveis
}

#[test]
fn test_constraints_block_forbidden_transitions() {
    // Configurar restricao proibindo Joyful -> Frustrated via mild_criticism
    // ASSERTIVA: ConstraintCheck::Blocked retornado
}

#[test]
fn test_min_duration_constraint() {
    // Entrar em Joyful, tentar transitar imediatamente
    // ASSERTIVA: bloqueado por min_duration
    // Esperar o tempo minimo, tentar novamente
    // ASSERTIVA: permitido
}
```

---

## 1.10 Presets de Personalidade

```toml
# config/presets/resilient.toml
# Agente resiliente: pouco afetado por estimulos externos
[personality]
name = "resilient"
description = "Baixa reatividade. Estímulos externos causam pouco efeito."

# W = 0.05 * I (diagonal muito baixa, sem acoplamento)
[w_matrix]
dimension = 6
values = [
    0.05, 0.00, 0.00, 0.00, 0.00, 0.00,
    0.00, 0.05, 0.00, 0.00, 0.00, 0.00,
    0.00, 0.00, 0.05, 0.00, 0.00, 0.00,
    0.00, 0.00, 0.00, 0.05, 0.00, 0.00,
    0.00, 0.00, 0.00, 0.00, 0.05, 0.00,
    0.00, 0.00, 0.00, 0.00, 0.00, 0.05,
]

[baseline]
components = [0.1, -0.1, 0.4, 0.6, 0.2, 0.0]  # Levemente positivo, calmo, dominante

[decay]
lambda = 0.2   # Decaimento rapido (volta ao baseline rapidamente)
```

```toml
# config/presets/reactive.toml
# Agente reativo: fortemente afetado por estimulos
[personality]
name = "reactive"
description = "Alta reatividade. Absorve fortemente carga emocional dos inputs."

# W = 0.15 * I com acoplamentos cruzados
[w_matrix]
dimension = 6
values = [
    0.15, 0.03, 0.00, 0.00, 0.02, 0.00,
    0.05, 0.15, 0.00, 0.00, 0.00, 0.03,
    0.00, -0.05, 0.15, 0.00, 0.00, 0.00,  # Alta ativacao reduz dominancia
    0.00, 0.00, 0.00, 0.12, 0.00, 0.05,
    0.03, 0.00, 0.00, 0.00, 0.15, 0.00,
    0.00, 0.05, 0.00, 0.00, 0.00, 0.15,
]

[baseline]
components = [0.0, 0.2, 0.0, 0.3, 0.3, 0.4]  # Neutro mas ativado e curioso

[decay]
lambda = 0.05   # Decaimento lento (emocoes persistem)
```

---

## 1.11 Checklist de Aceitacao

> **Status atualizado em 2026-03-05 neste ambiente**

### FSM
- [x] 8 estados emocionais implementados como enum
- [x] Tabela de transicoes carregada de TOML (nao hardcoded)
- [x] Validacao de grafo: sem sinks, sem estados inalcancaveis
- [x] Restricoes Omega funcionais (proibicoes + tempo minimo)
- [x] Config padrao cobre todas as combinacoes estado x estimulo relevantes

### Motor Vetorial
- [x] EmotionVector 6D com operacoes: intensidade, cosine similarity, clamp
- [x] Calculo e(t+1) = e(t) + W*g(t) + epsilon correto (golden tests)
- [x] Decaimento exponencial converge para baseline
- [x] SusceptibilityMatrix com validacao de estabilidade
- [x] 3 presets de personalidade (resilient, reactive, empathic)
- [x] Teste de 10.000 iteracoes sem divergencia PASSA

### Score Fusion
- [x] Fusao alpha*sem + beta*emo + gamma*cog correta
- [x] Boost para memorias pseudopermanentes
- [x] Ranking ordenado por score decrescente
- [x] Validacao de pesos (soma = 1)

### Motor de Promocao
- [x] Criterio simples (intensidade > theta) funciona
- [x] Criterio multifatorial (freq + valence) funciona
- [x] Nivel maximo = 3 (cap)

### gRPC
- [x] Todos os 5 endpoints implementados e respondendo
- [x] ProcessInteraction (batch) executa pipeline completo
- [x] Erros retornam gRPC status codes adequados
- [x] `grpcurl` funciona para todos os endpoints

### Performance
- [x] FSM transition < 1us (criterion bench: ~71.8 ns)
- [x] Vector compute 6D < 10us (criterion bench: ~88.7 ns)
- [x] Score fusion 100 candidatos < 100us (criterion bench: ~6.21 us)
- [x] Cosine similarity batch 100x6D < 50us (criterion bench: ~1.44 us)

---

## 1.12 Riscos Especificos e Mitigacoes

| Risco | Prob. | Impacto | Mitigacao | Teste |
|-------|-------|---------|-----------|-------|
| Divergencia de e(t) | Media | Catastrofico | Clamping + validacao W | stability_tests |
| FSM com estado sink | Media | Alto | Validacao estatica de grafo | fsm_tests |
| W mal calibrada | Alta | Medio | Preset 0.1*I + logging | Manual + bench |
| Overhead gRPC na serializacao | Baixa | Baixo | ProcessInteraction batch | Bench E2E |
| Complexidade de `ndarray` API | Baixa | Baixo | Encapsular em wrappers | - |

---

## 1.13 Transicao para Fase 3

Ao final da Fase 1, o motor Rust e um servico gRPC standalone que:
- Aceita chamadas de transicao FSM, calculo vetorial, fusao de scores e promocao
- Opera com latencias sub-milissegundo no hot-path
- E testavel independentemente via grpcurl

A **Fase 3** conectara este motor ao orquestrador Go (desenvolvido na Fase 2 em paralelo),
formando o pipeline E2E.
