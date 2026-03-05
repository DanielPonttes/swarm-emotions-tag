use crate::vector::emotion::EmotionVector;
use ndarray::{Array1, Array2};

#[derive(Debug, Clone)]
pub struct SusceptibilityMatrix {
    data: Array2<f32>,
}

impl SusceptibilityMatrix {
    pub fn from_row_major(values: Vec<f32>, dim: usize) -> Result<Self, String> {
        if dim == 0 {
            return Err("matrix dimension must be greater than zero".to_string());
        }

        if values.len() != dim * dim {
            return Err(format!(
                "expected {} values for a {}x{} matrix, got {}",
                dim * dim,
                dim,
                dim,
                values.len()
            ));
        }

        let data = Array2::from_shape_vec((dim, dim), values)
            .map_err(|error| format!("failed to build susceptibility matrix: {error}"))?;
        Ok(Self { data })
    }

    pub fn scaled_identity(dim: usize, scale: f32) -> Self {
        let mut data = Array2::zeros((dim, dim));
        for idx in 0..dim {
            data[[idx, idx]] = scale;
        }
        Self { data }
    }

    pub fn multiply_vector(&self, vector: &EmotionVector) -> Array1<f32> {
        self.data.dot(vector.raw())
    }

    pub fn validate_stability(&self) -> Result<(), String> {
        let frobenius = self
            .data
            .iter()
            .map(|value| value * value)
            .sum::<f32>()
            .sqrt();
        if frobenius >= 1.0 {
            Err(format!(
                "matrix Frobenius norm ({frobenius:.4}) >= 1.0 - risk of divergence"
            ))
        } else {
            Ok(())
        }
    }

    pub fn dimension(&self) -> usize {
        self.data.nrows()
    }
}
