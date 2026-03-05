use serde::{Deserialize, Serialize};
use std::fmt;
use std::str::FromStr;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum EmotionalState {
    Neutral,
    Joyful,
    Curious,
    Empathetic,
    Calm,
    Worried,
    Frustrated,
    Anxious,
}

impl EmotionalState {
    pub const ALL: [Self; 8] = [
        Self::Neutral,
        Self::Joyful,
        Self::Curious,
        Self::Empathetic,
        Self::Calm,
        Self::Worried,
        Self::Frustrated,
        Self::Anxious,
    ];

    pub fn macro_state(&self) -> MacroState {
        match self {
            Self::Joyful | Self::Curious | Self::Empathetic => MacroState::Positive,
            Self::Neutral | Self::Calm => MacroState::Neutral,
            Self::Worried | Self::Frustrated | Self::Anxious => MacroState::Negative,
        }
    }

    pub fn prototype_vector(&self) -> [f32; 6] {
        match self {
            Self::Neutral => [0.0, 0.0, 0.0, 0.5, 0.0, 0.0],
            Self::Joyful => [0.8, 0.7, 0.6, 0.7, 0.5, 0.3],
            Self::Curious => [0.4, 0.6, 0.3, 0.2, 0.3, 0.9],
            Self::Empathetic => [0.3, 0.3, 0.2, 0.4, 0.9, 0.2],
            Self::Calm => [0.3, -0.5, 0.5, 0.8, 0.2, -0.3],
            Self::Worried => [-0.4, 0.5, -0.3, -0.5, 0.4, 0.3],
            Self::Frustrated => [-0.6, 0.7, -0.2, -0.3, -0.4, -0.2],
            Self::Anxious => [-0.5, 0.8, -0.5, -0.7, 0.1, 0.5],
        }
    }
}

impl fmt::Display for EmotionalState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let value = match self {
            Self::Neutral => "neutral",
            Self::Joyful => "joyful",
            Self::Curious => "curious",
            Self::Empathetic => "empathetic",
            Self::Calm => "calm",
            Self::Worried => "worried",
            Self::Frustrated => "frustrated",
            Self::Anxious => "anxious",
        };
        write!(f, "{value}")
    }
}

impl FromStr for EmotionalState {
    type Err = String;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match normalize_key(value).as_str() {
            "neutral" => Ok(Self::Neutral),
            "joyful" => Ok(Self::Joyful),
            "curious" => Ok(Self::Curious),
            "empathetic" => Ok(Self::Empathetic),
            "calm" => Ok(Self::Calm),
            "worried" => Ok(Self::Worried),
            "frustrated" => Ok(Self::Frustrated),
            "anxious" => Ok(Self::Anxious),
            _ => Err(format!("unknown emotional state: {value}")),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum MacroState {
    Positive,
    Neutral,
    Negative,
}

impl fmt::Display for MacroState {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        let value = match self {
            Self::Positive => "positive",
            Self::Neutral => "neutral",
            Self::Negative => "negative",
        };
        write!(f, "{value}")
    }
}

#[derive(Debug, Clone, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Stimulus {
    Praise,
    MildCriticism,
    SevereCriticism,
    Success,
    Failure,
    Urgency,
    Ambiguity,
    Novelty,
    Resolution,
    UserFrustration,
    Boredom,
    Empathy,
    Custom(String),
}

impl fmt::Display for Stimulus {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::Praise => write!(f, "praise"),
            Self::MildCriticism => write!(f, "mild_criticism"),
            Self::SevereCriticism => write!(f, "severe_criticism"),
            Self::Success => write!(f, "success"),
            Self::Failure => write!(f, "failure"),
            Self::Urgency => write!(f, "urgency"),
            Self::Ambiguity => write!(f, "ambiguity"),
            Self::Novelty => write!(f, "novelty"),
            Self::Resolution => write!(f, "resolution"),
            Self::UserFrustration => write!(f, "user_frustration"),
            Self::Boredom => write!(f, "boredom"),
            Self::Empathy => write!(f, "empathy"),
            Self::Custom(value) => write!(f, "{value}"),
        }
    }
}

impl FromStr for Stimulus {
    type Err = String;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        let normalized = normalize_key(value);
        if normalized.is_empty() {
            return Err("stimulus cannot be empty".to_string());
        }

        Ok(match normalized.as_str() {
            "praise" => Self::Praise,
            "mild_criticism" => Self::MildCriticism,
            "severe_criticism" => Self::SevereCriticism,
            "success" => Self::Success,
            "failure" => Self::Failure,
            "urgency" => Self::Urgency,
            "ambiguity" => Self::Ambiguity,
            "novelty" => Self::Novelty,
            "resolution" => Self::Resolution,
            "user_frustration" => Self::UserFrustration,
            "boredom" => Self::Boredom,
            "empathy" => Self::Empathy,
            _ => Self::Custom(normalized),
        })
    }
}

fn normalize_key(value: &str) -> String {
    value.trim().to_ascii_lowercase().replace([' ', '-'], "_")
}
