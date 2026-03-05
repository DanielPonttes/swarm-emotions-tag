use crate::fsm::config::FsmConfig;
use crate::fsm::state::{EmotionalState, Stimulus};
use std::collections::{HashMap, HashSet, VecDeque};
use std::str::FromStr;

#[derive(Debug, Clone, Default)]
pub struct TransitionTable {
    transitions: HashMap<(EmotionalState, Stimulus), EmotionalState>,
}

impl TransitionTable {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn from_config(config: &FsmConfig) -> Result<Self, String> {
        let mut table = Self::new();
        for rule in &config.rules {
            let from = EmotionalState::from_str(&rule.from)?;
            let stimulus = Stimulus::from_str(&rule.stimulus)?;
            let to = EmotionalState::from_str(&rule.to)?;
            table.transitions.insert((from, stimulus), to);
        }
        Ok(table)
    }

    pub fn get(&self, current: EmotionalState, stimulus: &Stimulus) -> Option<EmotionalState> {
        self.transitions.get(&(current, stimulus.clone())).copied()
    }

    pub fn reachable_states_from(&self, start: EmotionalState) -> HashSet<EmotionalState> {
        let mut visited = HashSet::new();
        let mut queue = VecDeque::from([start]);
        visited.insert(start);

        while let Some(state) = queue.pop_front() {
            for ((from, _), to) in &self.transitions {
                if *from == state && visited.insert(*to) {
                    queue.push_back(*to);
                }
            }
        }

        visited
    }

    pub fn validate(&self) -> Result<(), Vec<String>> {
        let mut errors = Vec::new();

        for state in EmotionalState::ALL {
            let has_exit = self.transitions.keys().any(|(from, _)| *from == state);
            if !has_exit {
                errors.push(format!(
                    "state {state} has no outgoing transitions (sink state)"
                ));
            }
        }

        let reachable = self.reachable_states_from(EmotionalState::Neutral);
        for state in EmotionalState::ALL {
            if !reachable.contains(&state) {
                errors.push(format!(
                    "state {state} is unreachable from {}",
                    EmotionalState::Neutral
                ));
            }
        }

        if errors.is_empty() {
            Ok(())
        } else {
            Err(errors)
        }
    }
}
