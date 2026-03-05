use crate::vector::emotion::EmotionVector;

pub fn batch_cosine_similarity(query: &EmotionVector, candidates: &[EmotionVector]) -> Vec<f32> {
    candidates
        .iter()
        .map(|candidate| query.cosine_similarity(candidate))
        .collect()
}
