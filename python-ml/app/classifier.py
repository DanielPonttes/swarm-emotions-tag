from __future__ import annotations

from dataclasses import dataclass
from typing import Any


NEUTRAL_VECTOR = [0.0, 0.0, 0.0, 0.5, 0.0, 0.0]
DEFAULT_MODEL_NAME = "monologg/bert-base-cased-goemotions-original"

GOEMOTIONS_TO_VECTOR = {
    "admiration": [0.6, 0.4, 0.3, 0.5, 0.7, 0.3],
    "amusement": [0.8, 0.6, 0.5, 0.6, 0.5, 0.5],
    "anger": [-0.7, 0.8, 0.4, 0.5, -0.5, -0.2],
    "annoyance": [-0.5, 0.5, 0.3, 0.4, -0.3, -0.3],
    "approval": [0.5, 0.2, 0.4, 0.7, 0.6, 0.0],
    "caring": [0.5, 0.3, 0.2, 0.4, 0.9, 0.1],
    "confusion": [-0.2, 0.4, -0.3, -0.8, 0.1, 0.6],
    "curiosity": [0.3, 0.6, 0.2, -0.2, 0.3, 0.9],
    "desire": [0.4, 0.6, 0.3, 0.3, 0.4, 0.4],
    "disappointment": [-0.6, 0.3, -0.3, 0.4, 0.0, -0.3],
    "disapproval": [-0.5, 0.4, 0.3, 0.5, -0.4, -0.2],
    "disgust": [-0.7, 0.5, 0.3, 0.6, -0.6, -0.3],
    "embarrassment": [-0.5, 0.5, -0.5, 0.3, 0.2, 0.3],
    "excitement": [0.7, 0.9, 0.4, 0.3, 0.4, 0.7],
    "fear": [-0.7, 0.8, -0.6, -0.5, 0.1, 0.5],
    "gratitude": [0.7, 0.4, 0.2, 0.6, 0.8, 0.1],
    "grief": [-0.8, 0.3, -0.5, 0.4, 0.2, -0.4],
    "joy": [0.9, 0.7, 0.6, 0.7, 0.5, 0.3],
    "love": [0.8, 0.5, 0.3, 0.5, 0.9, 0.2],
    "nervousness": [-0.4, 0.7, -0.5, -0.6, 0.1, 0.4],
    "neutral": NEUTRAL_VECTOR,
    "optimism": [0.6, 0.5, 0.4, 0.5, 0.4, 0.4],
    "pride": [0.6, 0.5, 0.7, 0.6, 0.3, 0.2],
    "realization": [0.2, 0.4, 0.3, 0.7, 0.1, 0.8],
    "relief": [0.5, -0.3, 0.4, 0.6, 0.2, 0.0],
    "remorse": [-0.6, 0.3, -0.3, 0.5, 0.4, -0.2],
    "sadness": [-0.7, -0.3, -0.5, 0.4, 0.2, -0.4],
    "surprise": [0.1, 0.8, -0.1, -0.6, 0.2, 0.9],
}

HEURISTIC_RULES = [
    ("gratitude", ("thank", "thanks", "appreciate", "grateful")),
    ("joy", ("great", "awesome", "happy", "glad", "love this")),
    ("excitement", ("excited", "amazing", "lets go", "can't wait")),
    ("fear", ("afraid", "scared", "worried", "terrified")),
    ("nervousness", ("anxious", "nervous", "deadline", "asap", "urgent")),
    ("anger", ("angry", "furious", "outraged")),
    ("annoyance", ("annoying", "frustrating", "frustrated", "irritating")),
    ("sadness", ("sad", "down", "upset", "heartbroken")),
    ("disappointment", ("disappointed", "let down", "failed", "mistake")),
    ("confusion", ("confused", "unclear", "don't understand", "what happened")),
    ("curiosity", ("curious", "interesting", "tell me more", "wondering")),
    ("surprise", ("surprised", "unexpected", "wow", "didn't expect")),
]


@dataclass(frozen=True)
class ClassificationResult:
    emotion_vector: list[float]
    label: str
    confidence: float


class EmotionClassifier:
    def __init__(
        self,
        mode: str = "heuristic",
        model_name: str = DEFAULT_MODEL_NAME,
        device: str = "cpu",
        top_k: int = 5,
    ) -> None:
        self.mode = mode.strip().lower() or "heuristic"
        self.model_name = model_name.strip() or DEFAULT_MODEL_NAME
        self.device = device.strip() or "cpu"
        self.top_k = max(1, top_k)
        self._pipe: Any | None = None

        if self.mode == "transformers":
            self._pipe = self._build_transformers_pipeline()
        elif self.mode != "heuristic":
            raise ValueError(f"unsupported classifier mode: {self.mode}")

    def classify(self, text: str) -> ClassificationResult:
        normalized = text.strip()
        if not normalized:
            return ClassificationResult(
                emotion_vector=NEUTRAL_VECTOR.copy(),
                label="neutral",
                confidence=0.0,
            )

        if self.mode == "transformers":
            return self._classify_with_transformers(normalized)
        return self._classify_with_heuristics(normalized)

    def _build_transformers_pipeline(self) -> Any:
        try:
            from transformers import pipeline
        except ImportError as exc:
            raise RuntimeError(
                "transformers mode requires optional dependencies. Install python-ml with the 'ml' extra."
            ) from exc

        return pipeline(
            "text-classification",
            model=self.model_name,
            top_k=self.top_k,
            device=_resolve_transformers_device(self.device),
        )

    def _classify_with_transformers(self, text: str) -> ClassificationResult:
        if self._pipe is None:
            raise RuntimeError("transformers pipeline is not initialized")

        raw_results = self._pipe(text)
        if isinstance(raw_results, list) and raw_results and isinstance(raw_results[0], list):
            results = raw_results[0]
        else:
            results = raw_results

        weighted_scores: list[tuple[str, float]] = []
        for item in results[: self.top_k]:
            label = str(item.get("label", "")).strip().lower()
            score = float(item.get("score", 0.0))
            if label in GOEMOTIONS_TO_VECTOR and score > 0:
                weighted_scores.append((label, score))

        if not weighted_scores:
            return ClassificationResult(
                emotion_vector=NEUTRAL_VECTOR.copy(),
                label="neutral",
                confidence=0.0,
            )

        return _combine_weighted_labels(weighted_scores)

    def _classify_with_heuristics(self, text: str) -> ClassificationResult:
        lowered = text.lower()
        weighted_scores: list[tuple[str, float]] = []

        for label, keywords in HEURISTIC_RULES:
            score = 0.0
            for keyword in keywords:
                if keyword in lowered:
                    score += 1.0
            if score > 0:
                weighted_scores.append((label, score))

        if not weighted_scores:
            return ClassificationResult(
                emotion_vector=NEUTRAL_VECTOR.copy(),
                label="neutral",
                confidence=0.55,
            )

        return _combine_weighted_labels(weighted_scores)


def classify_text(text: str) -> ClassificationResult:
    classifier = EmotionClassifier()
    return classifier.classify(text)


def _combine_weighted_labels(weighted_scores: list[tuple[str, float]]) -> ClassificationResult:
    total_weight = sum(score for _, score in weighted_scores)
    top_label, top_score = max(weighted_scores, key=lambda item: item[1])

    vector = [0.0] * 6
    for label, score in weighted_scores:
        label_vector = GOEMOTIONS_TO_VECTOR.get(label, NEUTRAL_VECTOR)
        for index, component in enumerate(label_vector):
            vector[index] += component * score

    if total_weight > 0:
        vector = [max(-1.0, min(1.0, component / total_weight)) for component in vector]

    confidence = min(1.0, top_score / total_weight) if total_weight > 0 else 0.0
    return ClassificationResult(
        emotion_vector=vector,
        label=top_label,
        confidence=confidence,
    )


def _resolve_transformers_device(raw_device: str) -> int:
    normalized = raw_device.strip().lower()
    if normalized in {"cpu", "-1"}:
        return -1
    if normalized == "cuda":
        return 0
    if normalized.startswith("cuda:"):
        return int(normalized.split(":", 1)[1])
    return int(normalized)
