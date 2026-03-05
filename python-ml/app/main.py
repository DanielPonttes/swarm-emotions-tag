from contextlib import asynccontextmanager

from fastapi import FastAPI
from pydantic import BaseModel

from app.classifier import classify_text


class HealthResponse(BaseModel):
    status: str
    model_loaded: bool


class ClassifyRequest(BaseModel):
    text: str


class ClassifyResponse(BaseModel):
    emotion_vector: list[float]
    label: str
    confidence: float


_model_loaded = False


@asynccontextmanager
async def lifespan(_: FastAPI):
    global _model_loaded
    _model_loaded = True
    yield


app = FastAPI(title="EmotionML Service", version="0.1.0", lifespan=lifespan)


@app.get("/health", response_model=HealthResponse)
async def health() -> HealthResponse:
    return HealthResponse(status="ok", model_loaded=_model_loaded)


@app.post("/classify-emotion", response_model=ClassifyResponse)
async def classify_emotion(req: ClassifyRequest) -> ClassifyResponse:
    result = classify_text(req.text)
    return ClassifyResponse(
        emotion_vector=result.emotion_vector,
        label=result.label,
        confidence=result.confidence,
    )

