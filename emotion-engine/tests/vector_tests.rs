mod common;

use common::{assert_close, fixture_path};
use emotion_engine::vector::{
    apply_decay, batch_cosine_similarity, compute_next_emotion, ComputeParams, EmotionVector,
    SusceptibilityMatrix,
};
use serde::Deserialize;
use std::fs;

#[derive(Debug, Deserialize)]
struct PresetConfig {
    w_matrix: PresetMatrix,
    baseline: PresetBaseline,
    decay: PresetDecay,
}

#[derive(Debug, Deserialize)]
struct PresetMatrix {
    dimension: usize,
    values: Vec<f32>,
}

#[derive(Debug, Deserialize)]
struct PresetBaseline {
    components: Vec<f32>,
}

#[derive(Debug, Deserialize)]
struct PresetDecay {
    lambda: f32,
}

#[test]
fn emotion_vector_operations_work_for_6d_vectors() {
    let vector = EmotionVector::new(vec![3.0, 4.0, 0.0, 0.0, 0.0, 0.0]);
    assert_close(vector.intensity(), 5.0, 1e-6);

    let same_direction = EmotionVector::new(vec![1.0, 0.0, 0.0, 0.0, 0.0, 0.0]);
    let opposite_direction = EmotionVector::new(vec![-1.0, 0.0, 0.0, 0.0, 0.0, 0.0]);
    assert_close(same_direction.cosine_similarity(&same_direction), 1.0, 1e-6);
    assert_close(
        same_direction.cosine_similarity(&opposite_direction),
        -1.0,
        1e-6,
    );

    let mut clamped = EmotionVector::new(vec![1.5, -1.5, 0.2, 2.0, -2.0, 0.0]);
    clamped.clamp();
    assert_eq!(clamped.to_vec(), vec![1.0, -1.0, 0.2, 1.0, -1.0, 0.0]);
}

#[test]
fn compute_next_emotion_matches_golden_values() {
    let current = EmotionVector::new(vec![0.2, -0.4, 0.0, 0.5, 0.1, -0.2]);
    let trigger = EmotionVector::new(vec![1.0, 0.5, -0.5, 0.0, 0.4, 2.0]);
    let matrix = SusceptibilityMatrix::scaled_identity(6, 0.1);

    let result = compute_next_emotion(&current, &trigger, &matrix, &ComputeParams::default())
        .expect("vector computation should succeed");

    let expected = [0.3, -0.35, -0.05, 0.5, 0.14, 0.0];
    for (actual, expected) in result.as_slice().iter().zip(expected) {
        assert_close(*actual, expected, 1e-6);
    }
}

#[test]
fn decay_converges_towards_baseline() {
    let emotion = EmotionVector::new(vec![0.8, 0.7, 0.6, 0.5, 0.4, 0.3]);
    let baseline = EmotionVector::new(vec![0.1, -0.1, 0.4, 0.6, 0.2, 0.0]);

    let result = apply_decay(&emotion, &baseline, 0.2, 1000.0).expect("decay should succeed");

    for (actual, expected) in result.as_slice().iter().zip(baseline.as_slice()) {
        assert_close(*actual, *expected, 1e-5);
    }
}

#[test]
fn batch_cosine_similarity_scores_in_order() {
    let query = EmotionVector::new(vec![1.0, 0.0, 0.0, 0.0, 0.0, 0.0]);
    let candidates = vec![
        EmotionVector::new(vec![1.0, 0.0, 0.0, 0.0, 0.0, 0.0]),
        EmotionVector::new(vec![0.0, 1.0, 0.0, 0.0, 0.0, 0.0]),
        EmotionVector::new(vec![-1.0, 0.0, 0.0, 0.0, 0.0, 0.0]),
    ];

    let scores = batch_cosine_similarity(&query, &candidates);
    assert_close(scores[0], 1.0, 1e-6);
    assert_close(scores[1], 0.0, 1e-6);
    assert_close(scores[2], -1.0, 1e-6);
}

#[test]
fn personality_presets_have_stable_matrices_and_matching_baselines() {
    for preset_name in ["resilient", "reactive", "empathic"] {
        let path = fixture_path(&format!("config/presets/{preset_name}.toml"));
        let raw = fs::read_to_string(&path).expect("preset file should exist");
        let preset: PresetConfig = toml::from_str(&raw).expect("preset TOML should parse");

        assert_eq!(preset.baseline.components.len(), preset.w_matrix.dimension);
        assert!(preset.decay.lambda >= 0.0);

        let matrix =
            SusceptibilityMatrix::from_row_major(preset.w_matrix.values, preset.w_matrix.dimension)
                .expect("preset matrix should be well-formed");
        matrix
            .validate_stability()
            .expect("preset matrix should be numerically stable");
    }
}
