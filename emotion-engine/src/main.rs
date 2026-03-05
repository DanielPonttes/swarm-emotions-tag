use emotion_engine::proto::emotion_engine_service_server::EmotionEngineServiceServer;
use emotion_engine::{grpc_addr_from_env, EmotionEngine, FILE_DESCRIPTOR_SET};
use tonic::transport::Server;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    let env_filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new("info,emotion_engine=debug"));
    tracing_subscriber::fmt().with_env_filter(env_filter).init();

    let addr = grpc_addr_from_env().parse()?;
    tracing::info!(%addr, "emotion-engine listening");

    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;

    Server::builder()
        .add_service(reflection_service)
        .add_service(EmotionEngineServiceServer::new(EmotionEngine))
        .serve(addr)
        .await?;

    Ok(())
}
