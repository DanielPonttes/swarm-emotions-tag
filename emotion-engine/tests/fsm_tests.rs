mod common;

use common::load_default_fsm_config;
use emotion_engine::fsm::config::{ConstraintsConfig, ForbiddenRule};
use emotion_engine::fsm::constraints::{ConstraintCheck, Constraints};
use emotion_engine::fsm::state::{EmotionalState, Stimulus};
use emotion_engine::fsm::transition::TransitionTable;
use emotion_engine::fsm::{FsmEngine, StateSnapshot};
use std::collections::HashMap;

#[test]
fn default_fsm_is_valid_and_reachable_from_neutral() {
    let config = load_default_fsm_config();
    let table = TransitionTable::from_config(&config).expect("transition table should build");

    table.validate().expect("default FSM should be valid");

    let reachable = table.reachable_states_from(EmotionalState::Neutral);
    assert_eq!(reachable.len(), EmotionalState::ALL.len());
    for state in EmotionalState::ALL {
        assert!(
            reachable.contains(&state),
            "missing reachable state {state}"
        );
    }
}

#[test]
fn constraints_block_forbidden_transitions() {
    let config = ConstraintsConfig {
        forbidden: vec![ForbiddenRule {
            from: "joyful".to_string(),
            stimulus: "mild_criticism".to_string(),
            to: "frustrated".to_string(),
        }],
        min_duration_ms: HashMap::new(),
    };
    let constraints = Constraints::from_config(&config).expect("constraints should build");

    let check = constraints.check(
        EmotionalState::Joyful,
        &Stimulus::MildCriticism,
        EmotionalState::Frustrated,
        0,
        5_000,
    );

    assert!(matches!(check, ConstraintCheck::Blocked { .. }));
}

#[test]
fn min_duration_blocks_then_allows_transition() {
    let config = load_default_fsm_config();
    let engine = FsmEngine::from_config(&config).expect("FSM engine should build");
    let now_ms = 10_000;

    let blocked = engine.transition(
        StateSnapshot {
            state: EmotionalState::Joyful,
            entered_at_ms: now_ms - 1_500,
        },
        &Stimulus::Failure,
        now_ms,
    );
    assert!(!blocked.transition_occurred);
    assert!(blocked
        .blocked_reason
        .as_deref()
        .unwrap_or_default()
        .contains("requires minimum"));

    let allowed = engine.transition(
        StateSnapshot {
            state: EmotionalState::Joyful,
            entered_at_ms: now_ms - 2_500,
        },
        &Stimulus::Failure,
        now_ms,
    );
    assert!(allowed.transition_occurred);
    assert_eq!(allowed.new_snapshot.state, EmotionalState::Worried);
    assert_eq!(allowed.new_snapshot.entered_at_ms, now_ms);
}

#[test]
fn missing_transition_rule_returns_blocked_reason() {
    let config = load_default_fsm_config();
    let engine = FsmEngine::from_config(&config).expect("FSM engine should build");

    let outcome = engine.transition(
        StateSnapshot {
            state: EmotionalState::Neutral,
            entered_at_ms: 1_000,
        },
        &Stimulus::Resolution,
        2_000,
    );

    assert!(!outcome.transition_occurred);
    assert_eq!(outcome.new_snapshot.state, EmotionalState::Neutral);
    assert!(outcome
        .blocked_reason
        .as_deref()
        .unwrap_or_default()
        .contains("No transition rule"));
}
