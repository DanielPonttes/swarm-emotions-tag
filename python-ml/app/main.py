import logging
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel

from app.classifier import DEFAULT_MODEL_NAME, EmotionClassifier

logger = logging.getLogger("uvicorn.error")
TRACE_ID_HEADER = "x-trace-id"


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool
    classifier_mode: str
    model_name: str
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
_load_error: str | None = None


def _classifier_config() -> tuple[str, str, str, int]:
    mode = os.getenv("CLASSIFIER_MODE", "heuristic").strip().lower() or "heuristic"
    model_name = (
        os.getenv("CLASSIFIER_MODEL_NAME", "").strip()
        or os.getenv("MODEL_NAME", "").strip()
        or DEFAULT_MODEL_NAME
    )
    device = os.getenv("CLASSIFIER_DEVICE", "cpu").strip() or "cpu"
    top_k = int(os.getenv("CLASSIFIER_TOP_K", "5"))
    return mode, model_name, device, top_k


@asynccontextmanager
async def lifespan(_: FastAPI):
    global _classifier, _classifier_mode, _classifier_model_name, _load_error

    _classifier_mode, _classifier_model_name, device, top_k = _classifier_config()
    try:
        logger.info(
            "Loading emotion classifier",
            extra={
                "classifier_mode": _classifier_mode,
                "model_name": _classifier_model_name,
                "device": device,
                "top_k": top_k,
            },
        )
        _classifier = EmotionClassifier(
            mode=_classifier_mode,
            model_name=_classifier_model_name,
            device=device,
            top_k=top_k,
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
