use crate::fsm::config::FsmConfig;
use crate::fsm::state::{EmotionalState, Stimulus};
use crate::fsm::{FsmEngine, StateSnapshot};
use crate::promotion::evaluator::{
    evaluate_promotions, MemoryCandidate, PromotionDecision, PromotionThresholds,
};
use crate::proto::emotion_engine_service_server::EmotionEngineService;
use crate::proto::{
    self, ComputeEmotionVectorRequest, ComputeEmotionVectorResponse, EmotionVector as ProtoVector,
    EvaluatePromotionRequest, EvaluatePromotionResponse, FsmState, FuseScoresRequest,
    FuseScoresResponse, ProcessInteractionRequest, ProcessInteractionResponse,
    PromotionDecision as ProtoPromotionDecision, RankedMemory as ProtoRankedMemory,
    TransitionStateRequest, TransitionStateResponse,
};
use crate::scoring::fusion::{fuse_and_rank, FusionWeights, RankedMemory, ScoreCandidate};
use crate::vector::{compute_with_decay, ComputeParams, EmotionVector, SusceptibilityMatrix};
use std::path::{Path, PathBuf};
use std::str::FromStr;
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};
use tonic::metadata::MetadataMap;
use tonic::{Request, Response, Status};

const DEFAULT_FSM_CONFIG_PATH: &str = "config/default_fsm.toml";
pub const TRACE_ID_METADATA_KEY: &str = "x-trace-id";
pub const TRACEPARENT_METADATA_KEY: &str = "traceparent";

#[derive(Debug, Clone, Default)]
pub struct TraceContext {
    pub trace_id: Option<String>,
    pub traceparent: Option<String>,
}

#[derive(Debug, Clone)]
pub struct EmotionEngineServer {
    fsm: Arc<FsmEngine>,
}

#[allow(clippy::result_large_err)]
impl EmotionEngineServer {
    pub fn from_environment() -> Result<Self, String> {
        let config_path = std::env::var("FSM_CONFIG_PATH")
            .unwrap_or_else(|_| DEFAULT_FSM_CONFIG_PATH.to_string());
        Self::from_config_path(config_path)
    }

    pub fn from_config_path(path: impl Into<PathBuf>) -> Result<Self, String> {
        let path = path.into();
        tracing::info!(path = %path.display(), "loading FSM configuration");
        let config = FsmConfig::from_path(&path)?;
        tracing::info!(
            name = %config.metadata.name,
            version = %config.metadata.version,
            description = %config.metadata.description,
            "FSM configuration loaded"
        );
        let fsm = FsmEngine::from_config(&config)?;
        Ok(Self { fsm: Arc::new(fsm) })
    }

    pub fn default_config_path() -> &'static Path {
        Path::new(DEFAULT_FSM_CONFIG_PATH)
    }

    fn now_ms() -> i64 {
        SystemTime::now()
            .duration_since(UNIX_EPOCH)
            .expect("system clock is before UNIX_EPOCH")
            .as_millis() as i64
    }

    fn parse_state_snapshot(state: Option<FsmState>) -> Result<StateSnapshot, Status> {
        let state = state.ok_or_else(|| Status::invalid_argument("missing current_state"))?;
        let parsed_state = EmotionalState::from_str(&state.state_name)
            .map_err(|error| Status::invalid_argument(error.to_string()))?;

        Ok(StateSnapshot {
            state: parsed_state,
            entered_at_ms: state.entered_at_ms,
        })
    }

    fn to_proto_state(snapshot: StateSnapshot) -> FsmState {
        FsmState {
            state_name: snapshot.state.to_string(),
            macro_state: snapshot.state.macro_state().to_string(),
            entered_at_ms: snapshot.entered_at_ms,
        }
    }

    fn parse_stimulus(value: &str) -> Result<Stimulus, Status> {
        Stimulus::from_str(value).map_err(|error| Status::invalid_argument(error.to_string()))
    }

    fn parse_vector(value: Option<ProtoVector>, field_name: &str) -> Result<EmotionVector, Status> {
        let value =
            value.ok_or_else(|| Status::invalid_argument(format!("missing {field_name}")))?;
        if value.components.is_empty() {
            return Err(Status::invalid_argument(format!(
                "{field_name} must not be empty"
            )));
        }
        Ok(EmotionVector::new(value.components))
    }

    fn to_proto_vector(vector: &EmotionVector) -> ProtoVector {
        ProtoVector {
            components: vector.to_vec(),
        }
    }

    fn parse_matrix(
        matrix: Option<proto::SusceptibilityMatrix>,
    ) -> Result<SusceptibilityMatrix, Status> {
        let matrix = matrix.ok_or_else(|| Status::invalid_argument("missing w_matrix"))?;
        let parsed = SusceptibilityMatrix::from_row_major(matrix.values, matrix.dimension as usize)
            .map_err(Status::invalid_argument)?;
        parsed
            .validate_stability()
            .map_err(Status::invalid_argument)?;
        Ok(parsed)
    }

    fn compute_emotion_from_request(
        request: &ComputeEmotionVectorRequest,
    ) -> Result<(EmotionVector, f32), Status> {
        let current = Self::parse_vector(request.current_emotion.clone(), "current_emotion")?;
        let trigger = Self::parse_vector(request.trigger.clone(), "trigger")?;
        let baseline = Self::parse_vector(request.baseline.clone(), "baseline")?;
        let w = Self::parse_matrix(request.w_matrix.clone())?;
        let params = ComputeParams {
            enable_noise: request.enable_noise,
            noise_sigma: request.noise_sigma,
        };

        let new_emotion = compute_with_decay(
            &current,
            &trigger,
            &w,
            &baseline,
            request.decay_lambda,
            request.delta_time,
            &params,
        )
        .map_err(Status::invalid_argument)?;

        let intensity = new_emotion.intensity();
        Ok((new_emotion, intensity))
    }

    fn parse_score_candidates(
        candidates: &[proto::ScoreCandidate],
    ) -> Result<Vec<ScoreCandidate>, Status> {
        candidates
            .iter()
            .map(|candidate| {
                if candidate.memory_id.trim().is_empty() {
                    return Err(Status::invalid_argument(
                        "score candidate memory_id is empty",
                    ));
                }
                Ok(ScoreCandidate {
                    memory_id: candidate.memory_id.clone(),
                    semantic_score: candidate.semantic_score,
                    emotional_score: candidate.emotional_score,
                    cognitive_score: candidate.cognitive_score,
                    memory_level: candidate.memory_level,
                    is_pseudopermanent: candidate.is_pseudopermanent,
                })
            })
            .collect()
    }

    fn to_proto_ranked_memory(ranked: RankedMemory) -> ProtoRankedMemory {
        ProtoRankedMemory {
            memory_id: ranked.memory_id,
            final_score: ranked.final_score,
            semantic_contribution: ranked.semantic_contribution,
            emotional_contribution: ranked.emotional_contribution,
            cognitive_contribution: ranked.cognitive_contribution,
        }
    }

    fn parse_promotion_candidates(
        candidates: &[proto::MemoryForPromotion],
    ) -> Result<Vec<MemoryCandidate>, Status> {
        candidates
            .iter()
            .map(|candidate| {
                if candidate.memory_id.trim().is_empty() {
                    return Err(Status::invalid_argument(
                        "promotion candidate memory_id is empty",
                    ));
                }
                Ok(MemoryCandidate {
                    memory_id: candidate.memory_id.clone(),
                    intensity: candidate.intensity,
                    current_level: candidate.current_level,
                    access_frequency: candidate.access_frequency,
                    valence_magnitude: candidate.valence_magnitude,
                })
            })
            .collect()
    }

    fn to_proto_promotion(decision: PromotionDecision) -> ProtoPromotionDecision {
        ProtoPromotionDecision {
            memory_id: decision.memory_id,
            should_promote: decision.should_promote,
            target_level: decision.target_level,
            reason: decision.reason,
        }
    }

    fn rpc_trace_context<T>(request: &Request<T>) -> TraceContext {
        request
            .extensions()
            .get::<TraceContext>()
            .cloned()
            .unwrap_or_else(|| capture_trace_context(request.metadata()))
    }

    fn rpc_span<T>(method: &'static str, request: &Request<T>) -> tracing::Span {
        let trace = Self::rpc_trace_context(request);
        tracing::info_span!(
            "grpc_request",
            rpc_method = method,
            trace_id = tracing::field::display(trace.trace_id.as_deref().unwrap_or("")),
            traceparent = tracing::field::display(trace.traceparent.as_deref().unwrap_or("")),
        )
    }
}

fn metadata_value(metadata: &MetadataMap, key: &str) -> Option<String> {
    metadata
        .get(key)
        .and_then(|value| value.to_str().ok())
        .map(str::trim)
        .filter(|value| !value.is_empty())
        .map(ToOwned::to_owned)
}

pub fn capture_trace_context(metadata: &MetadataMap) -> TraceContext {
    TraceContext {
        trace_id: metadata_value(metadata, TRACE_ID_METADATA_KEY),
        traceparent: metadata_value(metadata, TRACEPARENT_METADATA_KEY),
    }
}

pub fn attach_trace_context(mut request: Request<()>) -> Result<Request<()>, Status> {
    let trace = capture_trace_context(request.metadata());
    if trace.trace_id.is_some() || trace.traceparent.is_some() {
        tracing::debug!(
            trace_id = trace.trace_id.as_deref().unwrap_or(""),
            traceparent = trace.traceparent.as_deref().unwrap_or(""),
            "captured grpc trace metadata"
        );
    }
    request.extensions_mut().insert(trace);
    Ok(request)
}

#[allow(clippy::result_large_err)]
#[tonic::async_trait]
impl EmotionEngineService for EmotionEngineServer {
    async fn transition_state(
        &self,
        request: Request<TransitionStateRequest>,
    ) -> Result<Response<TransitionStateResponse>, Status> {
        let span = Self::rpc_span("transition_state", &request);
        let _entered = span.enter();
        tracing::info!(
            agent_id = request.get_ref().agent_id,
            "handling transition_state"
        );
        let request = request.into_inner();
        let snapshot = Self::parse_state_snapshot(request.current_state)?;
        let stimulus = Self::parse_stimulus(&request.stimulus)?;
        let outcome = self.fsm.transition(snapshot, &stimulus, Self::now_ms());

        Ok(Response::new(TransitionStateResponse {
            new_state: Some(Self::to_proto_state(outcome.new_snapshot)),
            transition_occurred: outcome.transition_occurred,
            blocked_reason: outcome.blocked_reason.unwrap_or_default(),
        }))
    }

    async fn compute_emotion_vector(
        &self,
        request: Request<ComputeEmotionVectorRequest>,
    ) -> Result<Response<ComputeEmotionVectorResponse>, Status> {
        let span = Self::rpc_span("compute_emotion_vector", &request);
        let _entered = span.enter();
        tracing::info!("handling compute_emotion_vector");
        let (new_emotion, intensity) = Self::compute_emotion_from_request(&request.into_inner())?;

        Ok(Response::new(ComputeEmotionVectorResponse {
            new_emotion: Some(Self::to_proto_vector(&new_emotion)),
            intensity,
        }))
    }

    async fn fuse_scores(
        &self,
        request: Request<FuseScoresRequest>,
    ) -> Result<Response<FuseScoresResponse>, Status> {
        let span = Self::rpc_span("fuse_scores", &request);
        let _entered = span.enter();
        tracing::info!("handling fuse_scores");
        let request = request.into_inner();
        let candidates = Self::parse_score_candidates(&request.candidates)?;
        let weights = FusionWeights {
            alpha: request.alpha,
            beta: request.beta,
            gamma: request.gamma,
            pseudoperm_boost: request.pseudoperm_boost,
        };

        let ranked = fuse_and_rank(&candidates, &weights).map_err(Status::invalid_argument)?;

        Ok(Response::new(FuseScoresResponse {
            ranked: ranked
                .into_iter()
                .map(Self::to_proto_ranked_memory)
                .collect(),
        }))
    }

    async fn evaluate_promotion(
        &self,
        request: Request<EvaluatePromotionRequest>,
    ) -> Result<Response<EvaluatePromotionResponse>, Status> {
        let span = Self::rpc_span("evaluate_promotion", &request);
        let _entered = span.enter();
        tracing::info!("handling evaluate_promotion");
        let request = request.into_inner();
        let candidates = Self::parse_promotion_candidates(&request.memories)?;
        let thresholds = PromotionThresholds {
            intensity_threshold: request.intensity_threshold,
            frequency_threshold: request.frequency_threshold,
            valence_threshold: request.valence_threshold,
        };

        let decisions = evaluate_promotions(&candidates, &thresholds)
            .into_iter()
            .map(Self::to_proto_promotion)
            .collect();

        Ok(Response::new(EvaluatePromotionResponse { decisions }))
    }

    async fn process_interaction(
        &self,
        request: Request<ProcessInteractionRequest>,
    ) -> Result<Response<ProcessInteractionResponse>, Status> {
        let span = Self::rpc_span("process_interaction", &request);
        let _entered = span.enter();
        tracing::info!(
            agent_id = request.get_ref().agent_id,
            "handling process_interaction"
        );
        let request = request.into_inner();

        let current_state = Self::parse_state_snapshot(request.current_fsm_state.clone())?;
        let stimulus = Self::parse_stimulus(&request.stimulus)?;
        let fsm_outcome = self
            .fsm
            .transition(current_state, &stimulus, Self::now_ms());

        let compute_request = ComputeEmotionVectorRequest {
            current_emotion: request.current_emotion.clone(),
            trigger: request.stimulus_vector.clone(),
            w_matrix: request.w_matrix.clone(),
            baseline: request.baseline.clone(),
            decay_lambda: request.decay_lambda,
            delta_time: request.delta_time,
            enable_noise: request.enable_noise,
            noise_sigma: request.noise_sigma,
        };
        let (new_emotion, new_intensity) = Self::compute_emotion_from_request(&compute_request)?;

        let weights = FusionWeights {
            alpha: request.alpha,
            beta: request.beta,
            gamma: request.gamma,
            pseudoperm_boost: request.pseudoperm_boost,
        };
        let ranked_memories = fuse_and_rank(
            &Self::parse_score_candidates(&request.score_candidates)?,
            &weights,
        )
        .map_err(Status::invalid_argument)?
        .into_iter()
        .map(Self::to_proto_ranked_memory)
        .collect();

        let promotion_decisions = evaluate_promotions(
            &Self::parse_promotion_candidates(&request.promotion_candidates)?,
            &PromotionThresholds {
                intensity_threshold: request.intensity_threshold,
                frequency_threshold: request.frequency_threshold,
                valence_threshold: request.valence_threshold,
            },
        )
        .into_iter()
        .map(Self::to_proto_promotion)
        .collect();

        Ok(Response::new(ProcessInteractionResponse {
            new_fsm_state: Some(Self::to_proto_state(fsm_outcome.new_snapshot)),
            transition_occurred: fsm_outcome.transition_occurred,
            new_emotion: Some(Self::to_proto_vector(&new_emotion)),
            new_intensity,
            ranked_memories,
            promotion_decisions,
        }))
    }
}

pub fn grpc_addr_from_env() -> String {
    let port = std::env::var("GRPC_PORT").unwrap_or_else(|_| "50051".to_string());
    format!("0.0.0.0:{port}")
}

#[cfg(test)]
mod tests {
    use super::{
        attach_trace_context, capture_trace_context, TraceContext, TRACEPARENT_METADATA_KEY,
        TRACE_ID_METADATA_KEY,
    };
    use tonic::metadata::MetadataValue;
    use tonic::Request;

    #[test]
    fn capture_trace_context_reads_supported_headers() {
        let mut request = Request::new(());
        request.metadata_mut().insert(
            TRACE_ID_METADATA_KEY,
            MetadataValue::try_from("req-123").expect("trace id metadata"),
        );
        request.metadata_mut().insert(
            TRACEPARENT_METADATA_KEY,
            MetadataValue::try_from("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
                .expect("traceparent metadata"),
        );

        let trace = capture_trace_context(request.metadata());

        assert_eq!(trace.trace_id.as_deref(), Some("req-123"));
        assert_eq!(
            trace.traceparent.as_deref(),
            Some("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
        );
    }

    #[test]
    fn attach_trace_context_stores_extension() {
        let mut request = Request::new(());
        request.metadata_mut().insert(
            TRACE_ID_METADATA_KEY,
            MetadataValue::try_from("req-456").expect("trace id metadata"),
        );

        let request = attach_trace_context(request).expect("interceptor should succeed");
        let trace = request
            .extensions()
            .get::<TraceContext>()
            .expect("trace context extension");

        assert_eq!(trace.trace_id.as_deref(), Some("req-456"));
        assert_eq!(trace.traceparent, None);
    }
}
