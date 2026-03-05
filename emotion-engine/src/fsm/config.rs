use serde::Deserialize;
use std::collections::HashMap;
use std::fs;
use std::path::Path;

#[derive(Debug, Clone, Deserialize)]
pub struct FsmConfig {
    pub metadata: Metadata,
    pub rules: Vec<TransitionRule>,
    #[serde(default)]
    pub constraints: ConstraintsConfig,
}

impl FsmConfig {
    pub fn from_path(path: impl AsRef<Path>) -> Result<Self, String> {
        let path = path.as_ref();
        let contents = fs::read_to_string(path)
            .map_err(|error| format!("failed to read FSM config {}: {error}", path.display()))?;
        toml::from_str(&contents)
            .map_err(|error| format!("failed to parse FSM config {}: {error}", path.display()))
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct Metadata {
    pub name: String,
    pub version: String,
    pub description: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct TransitionRule {
    pub from: String,
    pub stimulus: String,
    pub to: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct ForbiddenRule {
    pub from: String,
    pub stimulus: String,
    pub to: String,
}

#[derive(Debug, Clone, Deserialize, Default)]
pub struct ConstraintsConfig {
    #[serde(default)]
    pub forbidden: Vec<ForbiddenRule>,
    #[serde(default)]
    pub min_duration_ms: HashMap<String, u64>,
}
