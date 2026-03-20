mod common;

use common::fixture_path;
use emotion_engine::proto::emotion_engine_service_client::EmotionEngineServiceClient;
use emotion_engine::proto::emotion_engine_service_server::EmotionEngineServiceServer;
use emotion_engine::proto::{
    ComputeEmotionVectorRequest, EmotionVector, EvaluatePromotionRequest, FsmState,
    FuseScoresRequest, MemoryForPromotion, ProcessInteractionRequest, ScoreCandidate,
    SusceptibilityMatrix, TransitionStateRequest,
};
use emotion_engine::{attach_trace_context, EmotionEngineServer, FILE_DESCRIPTOR_SET};
use std::error::Error;
use tokio::net::TcpListener;
use tokio::sync::oneshot;
use tokio::task::JoinHandle;
use tokio_stream::wrappers::TcpListenerStream;
use tonic::transport::{Channel, Server};
use tonic::Code;

async fn spawn_client() -> Result<
    (
        EmotionEngineServiceClient<Channel>,
        oneshot::Sender<()>,
        JoinHandle<Result<(), tonic::transport::Error>>,
    ),
    Box<dyn Error>,
> {
    let config_path = fixture_path("config/default_fsm.toml");
    let engine = EmotionEngineServer::from_config_path(config_path)?;
    let listener = TcpListener::bind("127.0.0.1:0").await?;
    let addr = listener.local_addr()?;
    let incoming = TcpListenerStream::new(listener);
    let (shutdown_tx, shutdown_rx) = oneshot::channel();

    let reflection = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;

    let handle = tokio::spawn(async move {
        let service = tonic::service::interceptor::InterceptedService::new(
            EmotionEngineServiceServer::new(engine),
            attach_trace_context,
        );
        Server::builder()
            .add_service(reflection)
            .add_service(service)
            .serve_with_incoming_shutdown(incoming, async move {
                let _ = shutdown_rx.await;
            })
            .await
    });

    let client = EmotionEngineServiceClient::connect(format!("http://{}", addr)).await?;
    Ok((client, shutdown_tx, handle))
}

fn vector(values: [f32; 6]) -> EmotionVector {
    EmotionVector {
        components: values.into_iter().collect(),
    }
}

fn stable_matrix() -> SusceptibilityMatrix {
    SusceptibilityMatrix {
        dimension: 6,
        values: vec![
            0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0,
            0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0, 0.0, 0.1, 0.0, 0.0, 0.0, 0.0, 0.0,
            0.0, 0.1,
        ],
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn all_rpc_endpoints_respond_end_to_end() -> Result<(), Box<dyn Error>> {
    let (mut client, shutdown_tx, handle) = spawn_client().await?;

    let transition = client
        .transition_state(TransitionStateRequest {
            current_state: Some(FsmState {
                state_name: "neutral".to_string(),
                macro_state: "neutral".to_string(),
                entered_at_ms: 0,
            }),
            stimulus: "novelty".to_string(),
            stimulus_vector: None,
            agent_id: "agent-1".to_string(),
        })
        .await?
        .into_inner();
    assert!(transition.transition_occurred);
    assert_eq!(
        transition
            .new_state
            .as_ref()
            .expect("new state should exist")
            .state_name,
        "curious"
    );

    let compute = client
        .compute_emotion_vector(ComputeEmotionVectorRequest {
            current_emotion: Some(vector([0.2, -0.4, 0.0, 0.5, 0.1, -0.2])),
            trigger: Some(vector([1.0, 0.5, -0.5, 0.0, 0.4, 2.0])),
            w_matrix: Some(stable_matrix()),
            baseline: Some(vector([0.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            decay_lambda: 0.0,
            delta_time: 1.0,
            enable_noise: false,
            noise_sigma: 0.0,
        })
        .await?
        .into_inner();
    assert!(compute.intensity > 0.0);
    assert_eq!(
        compute
            .new_emotion
            .as_ref()
            .expect("new emotion should exist")
            .components
            .len(),
        6
    );

    let fused = client
        .fuse_scores(FuseScoresRequest {
            candidates: vec![
                ScoreCandidate {
                    memory_id: "m1".to_string(),
                    semantic_score: 0.4,
                    emotional_score: 0.4,
                    cognitive_score: 0.4,
                    memory_level: 1,
                    is_pseudopermanent: false,
                },
                ScoreCandidate {
                    memory_id: "m2".to_string(),
                    semantic_score: 0.3,
                    emotional_score: 0.4,
                    cognitive_score: 0.5,
                    memory_level: 2,
                    is_pseudopermanent: true,
                },
            ],
            alpha: 0.4,
            beta: 0.3,
            gamma: 0.3,
            pseudoperm_boost: 0.2,
            current_emotion: None,
        })
        .await?
        .into_inner();
    assert_eq!(fused.ranked.len(), 2);
    assert_eq!(fused.ranked[0].memory_id, "m2");

    let promotion = client
        .evaluate_promotion(EvaluatePromotionRequest {
            memories: vec![MemoryForPromotion {
                memory_id: "m3".to_string(),
                emotion_at_creation: None,
                intensity: 0.95,
                current_level: 1,
                access_frequency: 1,
                valence_magnitude: 0.1,
            }],
            intensity_threshold: 0.9,
            frequency_threshold: 10,
            valence_threshold: 0.8,
        })
        .await?
        .into_inner();
    assert_eq!(promotion.decisions.len(), 1);
    assert!(promotion.decisions[0].should_promote);

    let processed = client
        .process_interaction(ProcessInteractionRequest {
            agent_id: "agent-1".to_string(),
            current_fsm_state: Some(FsmState {
                state_name: "neutral".to_string(),
                macro_state: "neutral".to_string(),
                entered_at_ms: 0,
            }),
            current_emotion: Some(vector([0.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            stimulus: "praise".to_string(),
            stimulus_vector: Some(vector([0.5, 0.2, 0.1, 0.0, 0.1, 0.0])),
            w_matrix: Some(stable_matrix()),
            baseline: Some(vector([0.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            decay_lambda: 0.1,
            delta_time: 1.0,
            enable_noise: false,
            noise_sigma: 0.0,
            score_candidates: vec![ScoreCandidate {
                memory_id: "pm".to_string(),
                semantic_score: 0.2,
                emotional_score: 0.3,
                cognitive_score: 0.4,
                memory_level: 2,
                is_pseudopermanent: true,
            }],
            alpha: 0.4,
            beta: 0.3,
            gamma: 0.3,
            pseudoperm_boost: 0.2,
            promotion_candidates: vec![MemoryForPromotion {
                memory_id: "promo".to_string(),
                emotion_at_creation: None,
                intensity: 0.95,
                current_level: 1,
                access_frequency: 12,
                valence_magnitude: 0.9,
            }],
            intensity_threshold: 0.9,
            frequency_threshold: 10,
            valence_threshold: 0.8,
        })
        .await?
        .into_inner();
    assert!(processed.transition_occurred);
    assert_eq!(
        processed
            .new_fsm_state
            .as_ref()
            .expect("new FSM state should exist")
            .state_name,
        "joyful"
    );
    assert_eq!(processed.ranked_memories.len(), 1);
    assert_eq!(processed.promotion_decisions.len(), 1);

    let _ = shutdown_tx.send(());
    handle.await??;
    Ok(())
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn invalid_requests_return_invalid_argument() -> Result<(), Box<dyn Error>> {
    let (mut client, shutdown_tx, handle) = spawn_client().await?;

    let error = client
        .compute_emotion_vector(ComputeEmotionVectorRequest {
            current_emotion: Some(vector([0.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            trigger: Some(vector([1.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            w_matrix: Some(SusceptibilityMatrix {
                dimension: 3,
                values: vec![0.1; 9],
            }),
            baseline: Some(vector([0.0, 0.0, 0.0, 0.0, 0.0, 0.0])),
            decay_lambda: 0.1,
            delta_time: 1.0,
            enable_noise: false,
            noise_sigma: 0.0,
        })
        .await
        .expect_err("invalid dimensions should fail");

    assert_eq!(error.code(), Code::InvalidArgument);

    let _ = shutdown_tx.send(());
    handle.await??;
    Ok(())
}
