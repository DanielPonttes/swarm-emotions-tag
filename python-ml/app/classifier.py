from dataclasses import dataclass


NEUTRAL_VECTOR = [0.0, 0.0, 0.0, 0.0, 0.0, 0.0]


@dataclass(frozen=True)
class ClassificationResult:
    emotion_vector: list[float]
    label: str
    confidence: float


def classify_text(text: str) -> ClassificationResult:
    label = "neutral" if text.strip() else "empty"
    confidence = 1.0 if text.strip() else 0.0
    return ClassificationResult(
        emotion_vector=NEUTRAL_VECTOR.copy(),
        label=label,
        confidence=confidence,
    )

