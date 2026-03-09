.DEFAULT_GOAL := help

PROTO_DIR := proto
PROTO_FILES := $(shell find $(PROTO_DIR) -name '*.proto' 2>/dev/null)
GO_PROTO_OUT := orchestrator/pkg/proto

.PHONY: help
help:
	@echo "Targets disponiveis:"
	@echo "  make build         - Compila Go + Rust"
	@echo "  make test          - Roda testes Go + Rust"
	@echo "  make lint          - Lint Go (golangci-lint) + Rust (fmt + clippy)"
	@echo "  make proto-gen     - Gera codigo Go e valida geracao Rust"
	@echo "  make docker-up     - Sobe todos os servicos via docker compose"
	@echo "  make docker-down   - Para todos os servicos"
	@echo "  make docker-infra  - Sobe apenas Qdrant, PostgreSQL e Redis"
	@echo "  make test-go-integration - Roda integracao real dos connectors Go"
	@echo "  make phase2-smoke-real   - Smoke test local do /interact com deps reais"
	@echo "  make phase2-stability-real - Carga longa com pprof para fechar a Fase 2"
	@echo "  make clean         - Limpa artefatos de build"

.PHONY: build build-rust build-go
build: build-rust build-go

build-rust:
	cd emotion-engine && cargo build --release

build-go:
	@mkdir -p bin
	cd orchestrator && go build -o ../bin/orchestrator ./cmd/server

.PHONY: test test-rust test-go
test: test-rust test-go

test-rust:
	cd emotion-engine && cargo test

test-go:
	cd orchestrator && go test ./...

.PHONY: test-go-integration
test-go-integration:
	cd orchestrator && go test -count=1 -tags=integration -v ./internal/connector/cache ./internal/connector/db ./internal/connector/vectorstore

.PHONY: lint lint-rust lint-go
lint: lint-rust lint-go

lint-rust:
	cd emotion-engine && cargo fmt --check
	cd emotion-engine && cargo clippy --all-targets -- -D warnings

lint-go:
	cd orchestrator && golangci-lint run ./...

.PHONY: proto-gen proto-gen-go proto-gen-rust
proto-gen: proto-gen-go proto-gen-rust
	@echo "Proto generation complete."

proto-gen-go:
	@command -v protoc >/dev/null || (echo "protoc nao encontrado" && exit 1)
	@command -v protoc-gen-go >/dev/null || (echo "protoc-gen-go nao encontrado" && exit 1)
	@command -v protoc-gen-go-grpc >/dev/null || (echo "protoc-gen-go-grpc nao encontrado" && exit 1)
	@mkdir -p $(GO_PROTO_OUT)/emotion_engine/v1
	protoc \
		--go_out=$(GO_PROTO_OUT) \
		--go_opt=paths=source_relative \
		--go-grpc_out=$(GO_PROTO_OUT) \
		--go-grpc_opt=paths=source_relative \
		-I $(PROTO_DIR) \
		$(PROTO_FILES)

proto-gen-rust:
	cd emotion-engine && cargo check

.PHONY: docker-up docker-down docker-infra docker-build
docker-up:
	docker compose up -d --build

docker-down:
	docker compose down

docker-infra:
	docker compose up -d qdrant postgresql redis

docker-build:
	docker compose build

.PHONY: phase2-smoke-real phase2-stability-real
phase2-smoke-real:
	./scripts/phase2/smoke_real.sh

phase2-stability-real:
	./scripts/phase2/stability_real.sh

.PHONY: bench
bench:
	cd emotion-engine && cargo bench

.PHONY: clean
clean:
	cd emotion-engine && cargo clean
	cd orchestrator && go clean
	rm -rf bin/
