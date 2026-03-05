pub mod config;
pub mod constraints;
pub mod state;
pub mod transition;

use crate::fsm::config::FsmConfig;
use crate::fsm::constraints::{ConstraintCheck, Constraints};
use crate::fsm::state::{EmotionalState, Stimulus};
use crate::fsm::transition::TransitionTable;
use std::path::Path;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub struct StateSnapshot {
    pub state: EmotionalState,
    pub entered_at_ms: i64,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct TransitionOutcome {
    pub new_snapshot: StateSnapshot,
    pub transition_occurred: bool,
    pub blocked_reason: Option<String>,
}

#[derive(Debug, Clone)]
pub struct FsmEngine {
    table: TransitionTable,
    constraints: Constraints,
}

impl FsmEngine {
    pub fn from_config(config: &FsmConfig) -> Result<Self, String> {
        let table = TransitionTable::from_config(config)?;
        if let Err(errors) = table.validate() {
            return Err(errors.join("; "));
        }

        Ok(Self {
            table,
            constraints: Constraints::from_config(&config.constraints)?,
        })
    }

    pub fn from_path(path: impl AsRef<Path>) -> Result<Self, String> {
        let config = FsmConfig::from_path(path)?;
        Self::from_config(&config)
    }

    pub fn transition(
        &self,
        current: StateSnapshot,
        stimulus: &Stimulus,
        now_ms: i64,
    ) -> TransitionOutcome {
        let Some(target) = self.table.get(current.state, stimulus) else {
            return TransitionOutcome {
                new_snapshot: current,
                transition_occurred: false,
                blocked_reason: Some(format!(
                    "No transition rule for state {} and stimulus {}",
                    current.state, stimulus
                )),
            };
        };

        if target != current.state {
            match self.constraints.check(
                current.state,
                stimulus,
                target,
                current.entered_at_ms,
                now_ms,
            ) {
                ConstraintCheck::Allowed => TransitionOutcome {
                    new_snapshot: StateSnapshot {
                        state: target,
                        entered_at_ms: now_ms,
                    },
                    transition_occurred: true,
                    blocked_reason: None,
                },
                ConstraintCheck::Blocked { reason } => TransitionOutcome {
                    new_snapshot: current,
                    transition_occurred: false,
                    blocked_reason: Some(reason),
                },
            }
        } else {
            TransitionOutcome {
                new_snapshot: current,
                transition_occurred: false,
                blocked_reason: None,
            }
        }
    }

    pub fn table(&self) -> &TransitionTable {
        &self.table
    }

    pub fn constraints(&self) -> &Constraints {
        &self.constraints
    }
}
