pub mod fsm;
pub mod promotion;
pub mod proto;
pub mod scoring;
pub mod server;
pub mod vector;

pub use proto::FILE_DESCRIPTOR_SET;
pub use server::{grpc_addr_from_env, EmotionEngineServer};
