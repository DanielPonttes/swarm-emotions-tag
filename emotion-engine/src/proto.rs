pub mod generated {
    tonic::include_proto!("emotion_engine.v1");
}

pub use generated::*;

pub const FILE_DESCRIPTOR_SET: &[u8] =
    tonic::include_file_descriptor_set!("emotion_engine_descriptor");
