#[test]
fn grpc_address_defaults_to_port_50051() {
    std::env::remove_var("GRPC_PORT");
    assert_eq!(emotion_engine::grpc_addr_from_env(), "0.0.0.0:50051");
}

#[test]
fn grpc_socket_path_is_disabled_by_default() {
    std::env::remove_var("GRPC_SOCKET_PATH");
    assert_eq!(emotion_engine::grpc_socket_path_from_env(), None);
}

#[test]
fn grpc_socket_path_uses_environment_when_present() {
    std::env::set_var("GRPC_SOCKET_PATH", "/tmp/emotion-engine.sock");
    assert_eq!(
        emotion_engine::grpc_socket_path_from_env(),
        Some(std::path::PathBuf::from("/tmp/emotion-engine.sock"))
    );
    std::env::remove_var("GRPC_SOCKET_PATH");
}
