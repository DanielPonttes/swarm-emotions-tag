mod common;

use common::assert_close;
use emotion_engine::vector::{
    apply_decay, compute_next_emotion, ComputeParams, EmotionVector, SusceptibilityMatrix,
};
use rand::{rngs::StdRng, Rng, SeedableRng};

#[test]
fn runs_10000_iterations_without_divergence() {
    let mut rng = StdRng::seed_from_u64(42);
    let matrix = SusceptibilityMatrix::scaled_identity(6, 0.1);
    let mut emotion = EmotionVector::zeros(6);
    let limit = (6.0_f32).sqrt();

    for _ in 0..10_000 {
        let trigger =
            EmotionVector::new((0..6).map(|_| rng.gen_range(-1.0_f32..=1.0_f32)).collect());
        emotion = compute_next_emotion(&emotion, &trigger, &matrix, &ComputeParams::default())
            .expect("stable matrix should compute");

        assert!(emotion.intensity() <= limit + 1e-6);
        for component in emotion.as_slice() {
            assert!((-1.0..=1.0).contains(component));
        }
    }
}

#[test]
fn convergence_to_baseline_without_stimuli() {
    let baseline = EmotionVector::new(vec![0.1, -0.1, 0.4, 0.6, 0.2, 0.0]);
    let mut emotion = EmotionVector::new(vec![1.0, -1.0, 0.5, -0.5, 0.75, -0.25]);

    for _ in 0..10_000 {
        emotion = apply_decay(&emotion, &baseline, 0.2, 1.0).expect("decay should succeed");
    }

    for (actual, expected) in emotion.as_slice().iter().zip(baseline.as_slice()) {
        assert_close(*actual, *expected, 1e-6);
    }
}

#[test]
fn unstable_matrix_is_rejected() {
    let unstable = SusceptibilityMatrix::scaled_identity(6, 0.5);
    let error = unstable
        .validate_stability()
        .expect_err("unstable matrix should fail validation");

    assert!(error.contains("risk of divergence"));
}
