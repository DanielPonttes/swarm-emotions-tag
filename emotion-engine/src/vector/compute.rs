use crate::vector::decay::apply_decay;
use crate::vector::emotion::EmotionVector;
use crate::vector::matrix::SusceptibilityMatrix;
use rand::Rng;
use rand_distr::Normal;

#[derive(Debug, Clone, Copy, Default)]
pub struct ComputeParams {
    pub enable_noise: bool,
    pub noise_sigma: f32,
}

pub fn compute_next_emotion(
    current: &EmotionVector,
    trigger: &EmotionVector,
    w: &SusceptibilityMatrix,
    params: &ComputeParams,
) -> Result<EmotionVector, String> {
    validate_dimensions(current, trigger, w)?;

    let w_times_g = w.multiply_vector(trigger);
    let mut result_data = current.raw() + &w_times_g;

    if params.enable_noise && params.noise_sigma > 0.0 {
        let normal = Normal::new(0.0, params.noise_sigma as f64)
            .map_err(|error| format!("invalid noise sigma {}: {error}", params.noise_sigma))?;
        let mut rng = rand::thread_rng();
        for value in &mut result_data {
            *value += rng.sample(normal) as f32;
        }
    }

    let mut result = EmotionVector::from_raw(result_data);
    result.clamp();
    Ok(result)
}

pub fn compute_with_decay(
    current: &EmotionVector,
    trigger: &EmotionVector,
    w: &SusceptibilityMatrix,
    baseline: &EmotionVector,
    lambda: f32,
    delta_t: f32,
    params: &ComputeParams,
) -> Result<EmotionVector, String> {
    let decayed = apply_decay(current, baseline, lambda, delta_t)?;
    compute_next_emotion(&decayed, trigger, w, params)
}

fn validate_dimensions(
    current: &EmotionVector,
    trigger: &EmotionVector,
    w: &SusceptibilityMatrix,
) -> Result<(), String> {
    if current.dim() != trigger.dim() {
        return Err(format!(
            "current emotion dim {} does not match trigger dim {}",
            current.dim(),
            trigger.dim()
        ));
    }

    if current.dim() != w.dimension() {
        return Err(format!(
            "vector dim {} does not match matrix dimension {}",
            current.dim(),
            w.dimension()
        ));
    }

    Ok(())
}
