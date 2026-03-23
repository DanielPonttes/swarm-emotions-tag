import logging
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel, Field

from app.classifier import (
    DEFAULT_MODEL_NAME,
    DEFAULT_OLLAMA_BASE_URL,
    DEFAULT_OLLAMA_MAX_CONCURRENCY,
    DEFAULT_REQUEST_TIMEOUT_SEC,
    EmotionClassifier,
)
from app.runtime import collect_runtime_info, runtime_info_dict

logger = logging.getLogger("uvicorn.error")
TRACE_ID_HEADER = "x-trace-id"


class RuntimeInfoResponse(BaseModel):
    torch_version: str | None = None
    cuda_available: bool
    cuda_device_count: int
    cuda_devices: list[str] = Field(default_factory=list)


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool
    classifier_mode: str
    model_name: str
    classifier_device: str
    classifier_batch_size: int
    classifier_ollama_max_concurrency: int
    runtime: RuntimeInfoResponse
    load_error: str | None = None


class ClassifyRequest(BaseModel):
    text: str


class ClassifyResponse(BaseModel):
    emotion_vector: list[float]
    label: str
    confidence: float


_classifier: EmotionClassifier | None = None
_classifier_mode = "heuristic"
_classifier_model_name = DEFAULT_MODEL_NAME
_classifier_device = "cpu"
_classifier_batch_size = 8
_classifier_ollama_max_concurrency = DEFAULT_OLLAMA_MAX_CONCURRENCY
_runtime_info = runtime_info_dict(collect_runtime_info())
_load_error: str | None = None


def _classifier_config() -> tuple[str, str, str, int, int, str, float, int]:
    mode = os.getenv("CLASSIFIER_MODE", "heuristic").strip().lower() or "heuristic"
    model_name = (
        os.getenv("CLASSIFIER_MODEL_NAME", "").strip()
        or os.getenv("MODEL_NAME", "").strip()
        or os.getenv("LLM_MODEL", "").strip()
        or DEFAULT_MODEL_NAME
    )
    device = os.getenv("CLASSIFIER_DEVICE", "cpu").strip() or "cpu"
    top_k = int(os.getenv("CLASSIFIER_TOP_K", "5"))
    batch_size = int(os.getenv("CLASSIFIER_BATCH_SIZE", "8"))
    ollama_base_url = (
        os.getenv("CLASSIFIER_OLLAMA_BASE_URL", "").strip()
        or os.getenv("OLLAMA_BASE_URL", "").strip()
        or os.getenv("LLM_BASE_URL", "").strip()
        or DEFAULT_OLLAMA_BASE_URL
    )
    request_timeout_sec = float(
        os.getenv("CLASSIFIER_REQUEST_TIMEOUT_SEC", str(DEFAULT_REQUEST_TIMEOUT_SEC))
    )
    ollama_max_concurrency = int(
        os.getenv(
            "CLASSIFIER_OLLAMA_MAX_CONCURRENCY",
            str(DEFAULT_OLLAMA_MAX_CONCURRENCY),
        )
    )
    return (
        mode,
        model_name,
        device,
        top_k,
        batch_size,
        ollama_base_url,
        request_timeout_sec,
        ollama_max_concurrency,
    )


@asynccontextmanager
async def lifespan(_: FastAPI):
    global _classifier, _classifier_mode, _classifier_model_name
    global _classifier_device, _classifier_batch_size, _classifier_ollama_max_concurrency
    global _runtime_info, _load_error

    (
        _classifier_mode,
        _classifier_model_name,
        device,
        top_k,
        batch_size,
        ollama_base_url,
        request_timeout_sec,
        ollama_max_concurrency,
    ) = _classifier_config()
    _classifier_device = device
    _classifier_batch_size = max(1, batch_size)
    _classifier_ollama_max_concurrency = max(1, ollama_max_concurrency)
    _runtime_info = runtime_info_dict(collect_runtime_info())
    try:
        logger.info(
            "Loading emotion classifier",
            extra={
                "classifier_mode": _classifier_mode,
                "model_name": _classifier_model_name,
                "device": device,
                "top_k": top_k,
                "batch_size": _classifier_batch_size,
                "ollama_base_url": ollama_base_url,
                "request_timeout_sec": request_timeout_sec,
                "ollama_max_concurrency": _classifier_ollama_max_concurrency,
                "runtime": _runtime_info,
            },
        )
        _classifier = EmotionClassifier(
            mode=_classifier_mode,
            model_name=_classifier_model_name,
            device=device,
            top_k=top_k,
            batch_size=_classifier_batch_size,
            ollama_base_url=ollama_base_url,
            request_timeout_sec=request_timeout_sec,
            ollama_max_concurrency=_classifier_ollama_max_concurrency,
        )
        _load_error = None
        logger.info("Emotion classifier loaded")
    except Exception as exc:
        _classifier = None
        _load_error = str(exc)
        logger.exception("Failed to load emotion classifier")
    yield


app = FastAPI(title="EmotionML Service", version="0.1.0", lifespan=lifespan)


@app.middleware("http")
async def trace_logging_middleware(request: Request, call_next):
    trace_id = request.headers.get(TRACE_ID_HEADER, "").strip()
    logger.info(
        "HTTP request method=%s path=%s trace_id=%s",
        request.method,
        request.url.path,
        trace_id,
    )
    response = await call_next(request)
    if trace_id:
        response.headers[TRACE_ID_HEADER] = trace_id
    return response


@app.get("/health", response_model=HealthResponse)
async def health() -> HealthResponse:
    return HealthResponse(
        status="ok" if _classifier is not None else "degraded",
        model_loaded=_classifier is not None,
        classifier_mode=_classifier_mode,
        model_name=_classifier_model_name,
        classifier_device=_classifier_device,
        classifier_batch_size=_classifier_batch_size,
        classifier_ollama_max_concurrency=_classifier_ollama_max_concurrency,
        runtime=RuntimeInfoResponse(**_runtime_info),
        load_error=_load_error,
    )


@app.post("/classify-emotion", response_model=ClassifyResponse)
async def classify_emotion(req: ClassifyRequest) -> ClassifyResponse:
    if _classifier is None:
        raise HTTPException(status_code=503, detail=_load_error or "Model not loaded yet")
    if not req.text.strip():
        raise HTTPException(status_code=400, detail="Text cannot be empty")

    result = _classifier.classify(req.text)
    logger.info(
        "Emotion classified label=%s confidence=%.4f",
        result.label,
        result.confidence,
    )
    return ClassifyResponse(
        emotion_vector=result.emotion_vector,
        label=result.label,
        confidence=result.confidence,
    )
