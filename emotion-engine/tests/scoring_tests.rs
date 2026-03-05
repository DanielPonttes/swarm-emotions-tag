use emotion_engine::scoring::fusion::{fuse_and_rank, FusionWeights, ScoreCandidate};

#[test]
fn score_fusion_uses_weighted_sum() {
    let ranked = fuse_and_rank(
        &[ScoreCandidate {
            memory_id: "m1".to_string(),
            semantic_score: 0.9,
            emotional_score: 0.5,
            cognitive_score: 0.2,
            memory_level: 1,
            is_pseudopermanent: false,
        }],
        &FusionWeights {
            alpha: 0.5,
            beta: 0.3,
            gamma: 0.2,
            pseudoperm_boost: 0.25,
        },
    )
    .expect("score fusion should succeed");

    assert_eq!(ranked.len(), 1);
    let item = &ranked[0];
    assert_eq!(item.memory_id, "m1");
    assert!((item.semantic_contribution - 0.45).abs() < 1e-6);
    assert!((item.emotional_contribution - 0.15).abs() < 1e-6);
    assert!((item.cognitive_contribution - 0.04).abs() < 1e-6);
    assert!((item.final_score - 0.64).abs() < 1e-6);
}

#[test]
fn pseudopermanent_memories_receive_boost_and_ranking_is_descending() {
    let ranked = fuse_and_rank(
        &[
            ScoreCandidate {
                memory_id: "baseline".to_string(),
                semantic_score: 0.7,
                emotional_score: 0.6,
                cognitive_score: 0.5,
                memory_level: 1,
                is_pseudopermanent: false,
            },
            ScoreCandidate {
                memory_id: "boosted".to_string(),
                semantic_score: 0.6,
                emotional_score: 0.6,
                cognitive_score: 0.6,
                memory_level: 2,
                is_pseudopermanent: true,
            },
        ],
        &FusionWeights {
            alpha: 0.4,
            beta: 0.3,
            gamma: 0.3,
            pseudoperm_boost: 0.25,
        },
    )
    .expect("score fusion should succeed");

    assert_eq!(ranked[0].memory_id, "boosted");
    assert!(ranked[0].final_score > ranked[1].final_score);
}

#[test]
fn weights_must_sum_to_one() {
    let error = fuse_and_rank(
        &[],
        &FusionWeights {
            alpha: 0.5,
            beta: 0.3,
            gamma: 0.3,
            pseudoperm_boost: 0.0,
        },
    )
    .expect_err("invalid weights should fail");

    assert!(error.contains("sum to 1.0"));
}
