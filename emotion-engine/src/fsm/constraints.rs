use crate::fsm::config::ConstraintsConfig;
use crate::fsm::state::{EmotionalState, Stimulus};
use std::collections::{HashMap, HashSet};
use std::str::FromStr;
use std::time::Duration;

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConstraintCheck {
    Allowed,
    Blocked { reason: String },
}

#[derive(Debug, Clone, Default)]
pub struct Constraints {
    forbidden: HashSet<(EmotionalState, Stimulus, EmotionalState)>,
    min_duration: HashMap<EmotionalState, Duration>,
}

impl Constraints {
    pub fn from_config(config: &ConstraintsConfig) -> Result<Self, String> {
        let mut forbidden = HashSet::new();
        for rule in &config.forbidden {
            forbidden.insert((
                EmotionalState::from_str(&rule.from)?,
                Stimulus::from_str(&rule.stimulus)?,
                EmotionalState::from_str(&rule.to)?,
            ));
        }

        let mut min_duration = HashMap::new();
        for (state, millis) in &config.min_duration_ms {
            min_duration.insert(
                EmotionalState::from_str(state)?,
                Duration::from_millis(*millis),
            );
        }

        Ok(Self {
            forbidden,
            min_duration,
        })
    }

    pub fn check(
        &self,
        current: EmotionalState,
        stimulus: &Stimulus,
        target: EmotionalState,
        state_entered_at_ms: i64,
        now_ms: i64,
    ) -> ConstraintCheck {
        if self
            .forbidden
            .contains(&(current, stimulus.clone(), target))
        {
            return ConstraintCheck::Blocked {
                reason: format!(
                    "Transition {} --{}--> {} is forbidden by Omega constraints",
                    current, stimulus, target
                ),
            };
        }

        if let Some(min_duration) = self.min_duration.get(&current) {
            let elapsed_ms = now_ms.saturating_sub(state_entered_at_ms).max(0) as u64;
            let elapsed = Duration::from_millis(elapsed_ms);
            if elapsed < *min_duration {
                return ConstraintCheck::Blocked {
                    reason: format!(
                        "State {} requires minimum {:?}, only {:?} elapsed",
                        current, min_duration, elapsed
                    ),
                };
            }
        }

        ConstraintCheck::Allowed
    }

    pub fn min_duration_for(&self, state: EmotionalState) -> Option<Duration> {
        self.min_duration.get(&state).copied()
    }
}
