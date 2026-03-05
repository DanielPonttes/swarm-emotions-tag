#[derive(Debug, Clone)]
pub struct MemoryCandidate {
    pub memory_id: String,
    pub intensity: f32,
    pub current_level: u32,
    pub access_frequency: u32,
    pub valence_magnitude: f32,
}

#[derive(Debug, Clone, Copy)]
pub struct PromotionThresholds {
    pub intensity_threshold: f32,
    pub frequency_threshold: u32,
    pub valence_threshold: f32,
}

#[derive(Debug, Clone)]
pub struct PromotionDecision {
    pub memory_id: String,
    pub should_promote: bool,
    pub target_level: u32,
    pub reason: String,
}

pub fn evaluate_promotions(
    candidates: &[MemoryCandidate],
    thresholds: &PromotionThresholds,
) -> Vec<PromotionDecision> {
    candidates
        .iter()
        .map(|candidate| {
            if candidate.current_level >= 3 {
                return PromotionDecision {
                    memory_id: candidate.memory_id.clone(),
                    should_promote: false,
                    target_level: 3,
                    reason: "Already at max level".to_string(),
                };
            }

            let high_intensity = candidate.intensity > thresholds.intensity_threshold;
            let cumulative = candidate.access_frequency > thresholds.frequency_threshold
                && candidate.valence_magnitude > thresholds.valence_threshold;
            let should_promote = high_intensity || cumulative;
            let target_level = if should_promote {
                candidate.current_level + 1
            } else {
                candidate.current_level
            };

            let reason = if high_intensity {
                format!(
                    "High intensity ({:.3} > {:.3})",
                    candidate.intensity, thresholds.intensity_threshold
                )
            } else if cumulative {
                format!(
                    "Cumulative: freq {} > {} AND valence {:.3} > {:.3}",
                    candidate.access_frequency,
                    thresholds.frequency_threshold,
                    candidate.valence_magnitude,
                    thresholds.valence_threshold
                )
            } else {
                "Below thresholds".to_string()
            };

            PromotionDecision {
                memory_id: candidate.memory_id.clone(),
                should_promote,
                target_level: target_level.min(3),
                reason,
            }
        })
        .collect()
}
