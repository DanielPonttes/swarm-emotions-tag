# Product Definition

## Overview
Swarm Emotions is a multi-service EmotionRAG platform. The core consists of an emotion-engine in Rust, an orchestrator in Go, and a python-ml classifier.

## Architecture
- `emotion-engine` (Rust): gRPC service for emotional state, vector calculation, score fusion, promotion, FSM.
- `orchestrator` (Go): HTTP control plane, pipeline of 8 steps.
- `python-ml` (Python): FastAPI service for classification.
