fn main() -> Result<(), Box<dyn std::error::Error>> {
    let descriptor_path =
        std::path::PathBuf::from(std::env::var("OUT_DIR")?).join("emotion_engine_descriptor.bin");

    println!("cargo:rerun-if-changed=../proto/emotion_engine/v1/emotion_engine.proto");
    println!("cargo:rerun-if-changed=../proto/buf.yaml");

    tonic_build::configure()
        .build_server(true)
        .build_client(false)
        .file_descriptor_set_path(descriptor_path)
        .compile_protos(
            &["../proto/emotion_engine/v1/emotion_engine.proto"],
            &["../proto"],
        )?;

    Ok(())
}
