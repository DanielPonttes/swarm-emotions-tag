#[test]
fn grpc_address_defaults_to_port_50051() {
    std::env::remove_var("GRPC_PORT");
    assert_eq!(emotion_engine::grpc_addr_from_env(), "0.0.0.0:50051");
}
