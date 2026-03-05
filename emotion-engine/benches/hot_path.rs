use criterion::{black_box, criterion_group, criterion_main, Criterion};
use emotion_engine::fsm::state::{EmotionalState, Stimulus};
use emotion_engine::fsm::{FsmEngine, StateSnapshot};
use emotion_engine::scoring::fusion::{fuse_and_rank, FusionWeights, ScoreCandidate};
use emotion_engine::vector::{
    apply_decay, batch_cosine_similarity, compute_next_emotion, ComputeParams, EmotionVector,
    SusceptibilityMatrix,
};
use std::hint::spin_loop;
use std::path::PathBuf;

fn load_engine() -> FsmEngine {
    let path = PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("config/default_fsm.toml");
    FsmEngine::from_path(path).expect("default FSM config should load")
}

fn stable_matrix() -> SusceptibilityMatrix {
    SusceptibilityMatrix::scaled_identity(6, 0.1)
}

fn sample_vector(seed: f32) -> EmotionVector {
    EmotionVector::new(vec![
        seed,
        seed * 0.5,
        seed * -0.4,
        seed * 0.3,
        seed * 0.2,
        seed * -0.1,
    ])
}

fn bench_fsm_transition(c: &mut Criterion) {
    let engine = load_engine();
    let snapshot = StateSnapshot {
        state: EmotionalState::Neutral,
        entered_at_ms: 0,
    };

    c.bench_function("fsm_transition", |b| {
        b.iter(|| {
            black_box(engine.transition(black_box(snapshot), black_box(&Stimulus::Praise), 5_000))
        })
    });
}

fn bench_vector_compute(c: &mut Criterion) {
    let current = sample_vector(0.2);
    let trigger = sample_vector(0.7);
    let matrix = stable_matrix();
    let params = ComputeParams::default();

    c.bench_function("vector_compute_6d", |b| {
        b.iter(|| {
            black_box(
                compute_next_emotion(
                    black_box(&current),
                    black_box(&trigger),
                    black_box(&matrix),
                    black_box(&params),
                )
                .expect("vector compute should succeed"),
            )
        })
    });
}

fn bench_score_fusion_100(c: &mut Criterion) {
    let candidates: Vec<ScoreCandidate> = (0..100)
        .map(|idx| ScoreCandidate {
            memory_id: format!("memory-{idx}"),
            semantic_score: (idx % 10) as f32 / 10.0,
            emotional_score: ((idx + 3) % 10) as f32 / 10.0,
            cognitive_score: ((idx + 6) % 10) as f32 / 10.0,
            memory_level: (idx % 3 + 1) as u32,
            is_pseudopermanent: idx % 7 == 0,
        })
        .collect();
    let weights = FusionWeights {
        alpha: 0.4,
        beta: 0.3,
        gamma: 0.3,
        pseudoperm_boost: 0.15,
    };

    c.bench_function("score_fusion_100_candidates", |b| {
        b.iter(|| {
            black_box(
                fuse_and_rank(black_box(&candidates), black_box(&weights))
                    .expect("score fusion should succeed"),
            )
        })
    });
}

fn bench_decay_batch(c: &mut Criterion) {
    let baseline = EmotionVector::zeros(6);
    let batch: Vec<EmotionVector> = (0..1_000)
        .map(|idx| sample_vector(idx as f32 / 1_000.0))
        .collect();

    c.bench_function("decay_batch_1000", |b| {
        b.iter(|| {
            for emotion in &batch {
                black_box(
                    apply_decay(black_box(emotion), black_box(&baseline), 0.1, 1.0)
                        .expect("decay should succeed"),
                );
            }
            spin_loop();
        })
    });
}

fn bench_cosine_similarity_batch(c: &mut Criterion) {
    let query = sample_vector(0.6);
    let candidates: Vec<EmotionVector> = (0..100)
        .map(|idx| sample_vector(idx as f32 / 100.0))
        .collect();

    c.bench_function("cosine_similarity_100x6d", |b| {
        b.iter(|| {
            black_box(batch_cosine_similarity(
                black_box(&query),
                black_box(&candidates),
            ))
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
