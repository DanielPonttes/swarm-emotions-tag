#[derive(Debug, Clone)]
pub struct ScoreCandidate {
    pub memory_id: String,
    pub semantic_score: f32,
    pub emotional_score: f32,
    pub cognitive_score: f32,
    pub memory_level: u32,
    pub is_pseudopermanent: bool,
}

#[derive(Debug, Clone)]
pub struct RankedMemory {
    pub memory_id: String,
    pub final_score: f32,
    pub semantic_contribution: f32,
    pub emotional_contribution: f32,
    pub cognitive_contribution: f32,
}

#[derive(Debug, Clone, Copy)]
pub struct FusionWeights {
    pub alpha: f32,
    pub beta: f32,
    pub gamma: f32,
    pub pseudoperm_boost: f32,
}

impl FusionWeights {
    pub fn validate(&self) -> Result<(), String> {
        let sum = self.alpha + self.beta + self.gamma;
        if self.alpha < 0.0 || self.beta < 0.0 || self.gamma < 0.0 {
            return Err("fusion weights must be non-negative".to_string());
        }
        if (sum - 1.0).abs() > 0.01 {
            return Err(format!("weights must sum to 1.0, got {sum:.4}"));
        }
        Ok(())
    }
}

pub fn fuse_and_rank(
    candidates: &[ScoreCandidate],
    weights: &FusionWeights,
) -> Result<Vec<RankedMemory>, String> {
    weights.validate()?;

    let mut ranked: Vec<RankedMemory> = candidates
        .iter()
        .map(|candidate| {
            let semantic = weights.alpha * candidate.semantic_score;
            let emotional = weights.beta * candidate.emotional_score;
            let cognitive = weights.gamma * candidate.cognitive_score;
            let mut final_score = semantic + emotional + cognitive;

            if candidate.is_pseudopermanent {
                final_score *= 1.0 + weights.pseudoperm_boost.max(0.0);
            }

            RankedMemory {
                memory_id: candidate.memory_id.clone(),
                final_score,
                semantic_contribution: semantic,
                emotional_contribution: emotional,
                cognitive_contribution: cognitive,
            }
        })
        .collect();

    ranked.sort_by(|left, right| right.final_score.total_cmp(&left.final_score));
    Ok(ranked)
}
