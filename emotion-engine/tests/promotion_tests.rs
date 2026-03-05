use emotion_engine::promotion::evaluator::{
    evaluate_promotions, MemoryCandidate, PromotionThresholds,
};

#[test]
fn high_intensity_promotes_memory() {
    let decisions = evaluate_promotions(
        &[MemoryCandidate {
            memory_id: "m1".to_string(),
            intensity: 0.95,
            current_level: 1,
            access_frequency: 1,
            valence_magnitude: 0.1,
        }],
        &PromotionThresholds {
            intensity_threshold: 0.9,
            frequency_threshold: 10,
            valence_threshold: 0.8,
        },
    );

    assert!(decisions[0].should_promote);
    assert_eq!(decisions[0].target_level, 2);
    assert!(decisions[0].reason.contains("High intensity"));
}

#[test]
fn cumulative_signal_promotes_memory() {
    let decisions = evaluate_promotions(
        &[MemoryCandidate {
            memory_id: "m2".to_string(),
            intensity: 0.4,
            current_level: 1,
            access_frequency: 12,
            valence_magnitude: 0.85,
        }],
        &PromotionThresholds {
            intensity_threshold: 0.9,
            frequency_threshold: 10,
            valence_threshold: 0.8,
        },
    );

    assert!(decisions[0].should_promote);
    assert_eq!(decisions[0].target_level, 2);
    assert!(decisions[0].reason.contains("Cumulative"));
}

#[test]
fn max_level_is_capped_at_three() {
    let decisions = evaluate_promotions(
        &[MemoryCandidate {
            memory_id: "m3".to_string(),
            intensity: 1.0,
            current_level: 3,
            access_frequency: 100,
            valence_magnitude: 1.0,
        }],
        &PromotionThresholds {
            intensity_threshold: 0.5,
            frequency_threshold: 1,
            valence_threshold: 0.1,
        },
    );

    assert!(!decisions[0].should_promote);
    assert_eq!(decisions[0].target_level, 3);
    assert_eq!(decisions[0].reason, "Already at max level");
}
