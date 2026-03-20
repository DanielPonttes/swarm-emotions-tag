use emotion_engine::proto::emotion_engine_service_server::EmotionEngineServiceServer;
use emotion_engine::{
    attach_trace_context, grpc_addr_from_env, grpc_socket_path_from_env, EmotionEngineServer,
    FILE_DESCRIPTOR_SET,
};
use std::io;
use std::path::Path;
use tokio::net::UnixListener;
use tokio_stream::wrappers::UnixListenerStream;
use tonic::transport::Server;
use tracing_subscriber::EnvFilter;

type DynError = Box<dyn std::error::Error + Send + Sync>;

#[tokio::main]
async fn main() -> Result<(), DynError> {
    let env_filter = EnvFilter::try_from_default_env()
        .unwrap_or_else(|_| EnvFilter::new("info,emotion_engine=debug"));
    tracing_subscriber::fmt().with_env_filter(env_filter).init();

    let addr = grpc_addr_from_env().parse()?;
    let socket_path = grpc_socket_path_from_env();
    let engine = EmotionEngineServer::from_environment()?;

    if let Some(socket_path) = socket_path {
        tracing::info!(%addr, socket_path = %socket_path.display(), "emotion-engine listening on tcp and unix socket");
        tokio::try_join!(
            serve_tcp(addr, engine.clone()),
            serve_unix(&socket_path, engine)
        )?;
    } else {
        tracing::info!(%addr, "emotion-engine listening on tcp");
        serve_tcp(addr, engine).await?;
    }

    Ok(())
}

async fn serve_tcp(
    addr: std::net::SocketAddr,
    engine: EmotionEngineServer,
) -> Result<(), DynError> {
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;
    let emotion_service = tonic::service::interceptor::InterceptedService::new(
        EmotionEngineServiceServer::new(engine),
        attach_trace_context,
    );

    Server::builder()
        .add_service(reflection_service)
        .add_service(emotion_service)
        .serve_with_shutdown(addr, shutdown_signal("tcp"))
        .await?;
    Ok(())
}

async fn serve_unix(socket_path: &Path, engine: EmotionEngineServer) -> Result<(), DynError> {
    if let Some(parent) = socket_path.parent() {
        std::fs::create_dir_all(parent)?;
    }
    remove_socket_if_exists(socket_path)?;

    let listener = UnixListener::bind(socket_path)?;
    let incoming = UnixListenerStream::new(listener);
    let reflection_service = tonic_reflection::server::Builder::configure()
        .register_encoded_file_descriptor_set(FILE_DESCRIPTOR_SET)
        .build_v1()?;
    let emotion_service = tonic::service::interceptor::InterceptedService::new(
        EmotionEngineServiceServer::new(engine),
        attach_trace_context,
    );

    let result = Server::builder()
        .add_service(reflection_service)
        .add_service(emotion_service)
        .serve_with_incoming_shutdown(incoming, shutdown_signal("unix"))
        .await;

    if let Err(error) = remove_socket_if_exists(socket_path) {
        tracing::warn!(?error, socket_path = %socket_path.display(), "failed to remove unix socket");
    }

    result?;
    Ok(())
}

async fn shutdown_signal(listener: &'static str) {
    if let Err(error) = tokio::signal::ctrl_c().await {
        tracing::error!(?error, listener, "failed to listen for shutdown signal");
        return;
    }
    tracing::info!(listener, "shutdown signal received");
}

fn remove_socket_if_exists(socket_path: &Path) -> io::Result<()> {
    match std::fs::remove_file(socket_path) {
        Ok(()) => Ok(()),
        Err(error) if error.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(error) => Err(error),
    }
}
