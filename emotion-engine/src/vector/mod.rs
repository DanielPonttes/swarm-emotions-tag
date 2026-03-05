pub mod compute;
pub mod decay;
pub mod emotion;
pub mod matrix;
pub mod similarity;

pub use compute::{compute_next_emotion, compute_with_decay, ComputeParams};
pub use decay::apply_decay;
pub use emotion::EmotionVector;
pub use matrix::SusceptibilityMatrix;
pub use similarity::batch_cosine_similarity;
