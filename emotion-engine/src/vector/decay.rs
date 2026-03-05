use crate::vector::emotion::EmotionVector;

pub fn apply_decay(
    emotion: &EmotionVector,
    baseline: &EmotionVector,
    lambda: f32,
    delta_t: f32,
) -> Result<EmotionVector, String> {
    if emotion.dim() != baseline.dim() {
        return Err(format!(
            "emotion dim {} does not match baseline dim {}",
            emotion.dim(),
            baseline.dim()
        ));
    }

    if lambda < 0.0 {
        return Err(format!("decay lambda must be non-negative, got {lambda}"));
    }

    if delta_t < 0.0 {
        return Err(format!("delta_time must be non-negative, got {delta_t}"));
    }

    let decay_factor = (-lambda * delta_t).exp();
    let diff = emotion.raw() - baseline.raw();
    let decayed = baseline.raw() + &(diff * decay_factor);
    Ok(EmotionVector::from_raw(decayed))
}
