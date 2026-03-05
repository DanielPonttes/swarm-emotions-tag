#![allow(dead_code)]

use emotion_engine::fsm::config::FsmConfig;
use std::path::PathBuf;

pub fn crate_root() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

pub fn fixture_path(relative: &str) -> PathBuf {
    crate_root().join(relative)
}

pub fn load_default_fsm_config() -> FsmConfig {
    FsmConfig::from_path(fixture_path("config/default_fsm.toml"))
        .expect("default FSM config should load")
}

pub fn assert_close(actual: f32, expected: f32, epsilon: f32) {
    let delta = (actual - expected).abs();
    assert!(
        delta <= epsilon,
        "expected {expected:.6}, got {actual:.6} (delta {delta:.6}, epsilon {epsilon:.6})"
    );
}
