use ndarray::Array1;

const COMPONENT_MIN: f32 = -1.0;
const COMPONENT_MAX: f32 = 1.0;

#[derive(Debug, Clone)]
pub struct EmotionVector {
    data: Array1<f32>,
}

impl EmotionVector {
    pub fn new(components: Vec<f32>) -> Self {
        Self {
            data: Array1::from_vec(components),
        }
    }

    pub fn zeros(dim: usize) -> Self {
        Self {
            data: Array1::zeros(dim),
        }
    }

    pub fn from_raw(data: Array1<f32>) -> Self {
        Self { data }
    }

    pub fn dim(&self) -> usize {
        self.data.len()
    }

    pub fn as_slice(&self) -> &[f32] {
        self.data
            .as_slice()
            .expect("emotion vectors must be contiguous in memory")
    }

    pub fn to_vec(&self) -> Vec<f32> {
        self.as_slice().to_vec()
    }

    pub fn raw(&self) -> &Array1<f32> {
        &self.data
    }

    pub fn intensity(&self) -> f32 {
        self.data.dot(&self.data).sqrt()
    }

    pub fn cosine_similarity(&self, other: &EmotionVector) -> f32 {
        if self.dim() != other.dim() {
            return 0.0;
        }

        let dot = self.data.dot(&other.data);
        let norm_a = self.intensity();
        let norm_b = other.intensity();

        if norm_a == 0.0 || norm_b == 0.0 {
            return 0.0;
        }

        (dot / (norm_a * norm_b)).clamp(-1.0, 1.0)
    }

    pub fn clamp(&mut self) {
        self.data
            .mapv_inplace(|value| value.clamp(COMPONENT_MIN, COMPONENT_MAX));
    }

    pub fn valence(&self) -> f32 {
        self.data[0]
    }

    pub fn arousal(&self) -> f32 {
        self.data[1]
    }
}
