use tonic::{Request, Response, Status};

pub mod proto {
    tonic::include_proto!("emotion_engine.v1");
}

pub const FILE_DESCRIPTOR_SET: &[u8] =
    tonic::include_file_descriptor_set!("emotion_engine_descriptor");

use proto::emotion_engine_service_server::EmotionEngineService;
use proto::{
    ComputeEmotionVectorRequest, ComputeEmotionVectorResponse, EvaluatePromotionRequest,
    EvaluatePromotionResponse, FuseScoresRequest, FuseScoresResponse, ProcessInteractionRequest,
    ProcessInteractionResponse, TransitionStateRequest, TransitionStateResponse,
};

#[derive(Debug, Default)]
pub struct EmotionEngine;

#[tonic::async_trait]
impl EmotionEngineService for EmotionEngine {
    async fn transition_state(
        &self,
        _request: Request<TransitionStateRequest>,
    ) -> Result<Response<TransitionStateResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn compute_emotion_vector(
        &self,
        _request: Request<ComputeEmotionVectorRequest>,
    ) -> Result<Response<ComputeEmotionVectorResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn fuse_scores(
        &self,
        _request: Request<FuseScoresRequest>,
    ) -> Result<Response<FuseScoresResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn evaluate_promotion(
        &self,
        _request: Request<EvaluatePromotionRequest>,
    ) -> Result<Response<EvaluatePromotionResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }

    async fn process_interaction(
        &self,
        _request: Request<ProcessInteractionRequest>,
    ) -> Result<Response<ProcessInteractionResponse>, Status> {
        Err(Status::unimplemented("Not yet implemented"))
    }
}

pub fn grpc_addr_from_env() -> String {
    let port = std::env::var("GRPC_PORT").unwrap_or_else(|_| "50051".to_string());
    format!("0.0.0.0:{port}")
}
