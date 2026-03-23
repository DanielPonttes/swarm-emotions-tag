from __future__ import annotations

from concurrent.futures import ThreadPoolExecutor, as_completed
import json
import re
from dataclasses import dataclass
from typing import Any
from urllib import error, request


NEUTRAL_VECTOR = [0.0, 0.0, 0.0, 0.5, 0.0, 0.0]
DEFAULT_MODEL_NAME = "monologg/bert-base-cased-goemotions-original"
DEFAULT_OLLAMA_BASE_URL = "http://127.0.0.1:11434"
DEFAULT_REQUEST_TIMEOUT_SEC = 90.0
DEFAULT_OLLAMA_NUM_PREDICT = 160
DEFAULT_OLLAMA_MAX_CONCURRENCY = 8

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
GOEMOTIONS_LABELS = tuple(GOEMOTIONS_TO_VECTOR.keys())

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
        batch_size: int = 8,
        ollama_base_url: str = DEFAULT_OLLAMA_BASE_URL,
        request_timeout_sec: float = DEFAULT_REQUEST_TIMEOUT_SEC,
        ollama_max_concurrency: int = DEFAULT_OLLAMA_MAX_CONCURRENCY,
    ) -> None:
        self.mode = mode.strip().lower() or "heuristic"
        self.model_name = model_name.strip() or DEFAULT_MODEL_NAME
        self.device = device.strip() or "cpu"
        self.top_k = max(1, top_k)
        self.batch_size = max(1, batch_size)
        self.ollama_base_url = ollama_base_url.strip().rstrip("/") or DEFAULT_OLLAMA_BASE_URL
        self.request_timeout_sec = max(1.0, request_timeout_sec)
        self.ollama_max_concurrency = max(1, ollama_max_concurrency)
        self._pipe: Any | None = None
        self._ollama_model_name = _normalize_ollama_model_name(self.model_name)

        if self.mode == "transformers":
            self._pipe = self._build_transformers_pipeline()
        elif self.mode == "ollama":
            self._verify_ollama_model()
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
        if self.mode == "ollama":
            return self._classify_with_ollama(normalized)
        return self._classify_with_heuristics(normalized)

    def classify_many(self, texts: list[str]) -> list[ClassificationResult]:
        if not texts:
            return []

        normalized_texts = [text.strip() for text in texts]
        if self.mode == "transformers":
            return self._classify_many_with_transformers(normalized_texts)
        if self.mode == "ollama":
            return self._classify_many_with_ollama(normalized_texts)
        return [self._classify_with_heuristics(text) for text in normalized_texts]

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
        return self._classification_from_transformers_output(raw_results)

    def _classify_many_with_transformers(self, texts: list[str]) -> list[ClassificationResult]:
        if self._pipe is None:
            raise RuntimeError("transformers pipeline is not initialized")

        outputs: list[ClassificationResult] = [
            ClassificationResult(
                emotion_vector=NEUTRAL_VECTOR.copy(),
                label="neutral",
                confidence=0.0,
            )
            for _ in texts
        ]
        active_indexes: list[int] = []
        active_texts: list[str] = []

        for index, text in enumerate(texts):
            if text:
                active_indexes.append(index)
                active_texts.append(text)

        if not active_texts:
            return outputs

        raw_results = self._pipe(active_texts, batch_size=self.batch_size)
        if not isinstance(raw_results, list) or len(raw_results) != len(active_texts):
            raise RuntimeError("unexpected transformers batch output shape")

        for index, raw_result in zip(active_indexes, raw_results):
            outputs[index] = self._classification_from_transformers_output(raw_result)

        return outputs

    def _classification_from_transformers_output(self, raw_results: Any) -> ClassificationResult:
        results = _normalize_transformers_results(raw_results)

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

    def _classify_many_with_ollama(self, texts: list[str]) -> list[ClassificationResult]:
        outputs: list[ClassificationResult] = [
            ClassificationResult(
                emotion_vector=NEUTRAL_VECTOR.copy(),
                label="neutral",
                confidence=0.0,
            )
            for _ in texts
        ]
        active_items = [(index, text) for index, text in enumerate(texts) if text]
        if not active_items:
            return outputs

        max_workers = min(self.batch_size, self.ollama_max_concurrency, len(active_items))
        if max_workers <= 1:
            for index, text in active_items:
                outputs[index] = self._classify_with_ollama(text)
            return outputs

        with ThreadPoolExecutor(max_workers=max_workers) as executor:
            future_to_index = {
                executor.submit(self._classify_with_ollama, text): index
                for index, text in active_items
            }
            for future in as_completed(future_to_index):
                outputs[future_to_index[future]] = future.result()
        return outputs

    def _classify_with_ollama(self, text: str) -> ClassificationResult:
        raw_output = self._ollama_chat(text)
        weighted_scores = _weighted_scores_from_llm_output(raw_output, self.top_k)
        if weighted_scores:
            return _combine_weighted_labels(weighted_scores)
        return self._classify_with_heuristics(text)

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

    def _verify_ollama_model(self) -> None:
        payload = self._ollama_json_request("/api/tags", method="GET")
        models = payload.get("models", [])
        available_models = {
            str(item.get("model") or item.get("name") or "").strip().lower()
            for item in models
            if isinstance(item, dict)
        }
        if self._ollama_model_name.strip().lower() not in available_models:
            raise RuntimeError(
                f"ollama model {self._ollama_model_name!r} is not available at {self.ollama_base_url}"
            )

    def _ollama_chat(self, text: str) -> str:
        payload = self._ollama_json_request(
            "/api/chat",
            payload={
                "model": self._ollama_model_name,
                "stream": False,
                "think": False,
                "messages": [
                    {
                        "role": "system",
                        "content": _ollama_system_prompt(self.top_k),
                    },
                    {
                        "role": "user",
                        "content": _ollama_user_prompt(text, self.top_k),
                    },
                ],
                "options": {
                    "temperature": 0,
                    "top_p": 0.1,
                    "top_k": 10,
                    "num_predict": DEFAULT_OLLAMA_NUM_PREDICT,
                },
            },
        )

        message = payload.get("message", {})
        content = str(message.get("content", "")).strip()
        if content:
            return content

        content = str(payload.get("response", "")).strip()
        if content:
            return content
        raise RuntimeError("ollama classifier returned empty content")

    def _ollama_json_request(
        self,
        path: str,
        payload: dict[str, Any] | None = None,
        method: str = "POST",
    ) -> dict[str, Any]:
        body: bytes | None = None
        headers = {"Accept": "application/json"}
        if payload is not None:
            body = json.dumps(payload).encode("utf-8")
            headers["Content-Type"] = "application/json"

        req = request.Request(
            self.ollama_base_url + path,
            data=body,
            headers=headers,
            method=method,
        )

        try:
            with request.urlopen(req, timeout=self.request_timeout_sec) as response:
                raw_body = response.read().decode("utf-8")
        except error.HTTPError as exc:
            raw_error = exc.read().decode("utf-8", errors="replace").strip()
            raise RuntimeError(
                f"ollama request failed with status {exc.code}: {raw_error or exc.reason}"
            ) from exc
        except error.URLError as exc:
            raise RuntimeError(f"ollama request failed: {exc.reason}") from exc

        try:
            decoded = json.loads(raw_body or "{}")
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"decode ollama response: {exc}") from exc
        if not isinstance(decoded, dict):
            raise RuntimeError("unexpected ollama response shape")
        if str(decoded.get("error", "")).strip():
            raise RuntimeError(f"ollama request failed: {decoded['error']}")
        return decoded


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


def _normalize_transformers_results(raw_results: Any) -> list[dict[str, Any]]:
    if isinstance(raw_results, dict):
        return [raw_results]
    if isinstance(raw_results, list):
        if raw_results and isinstance(raw_results[0], list):
            return raw_results[0]
        return raw_results
    return []


def _ollama_system_prompt(top_k: int) -> str:
    return (
        "You are an emotion classifier for EmotionRAG. "
        "Return strict JSON only, with no markdown and no prose. "
        f"Choose up to {max(1, top_k)} labels from the allowed label set. "
        "Scores must be positive, sorted descending, and sum approximately to 1.0."
    )


def _ollama_user_prompt(text: str, top_k: int) -> str:
    schema = '{"labels":[{"label":"joy","score":0.7},{"label":"gratitude","score":0.3}]}'
    return (
        "Classify the emotional content of the text below.\n"
        f"Allowed labels: {', '.join(GOEMOTIONS_LABELS)}.\n"
        f"Use at most {max(1, top_k)} labels.\n"
        "If the text is neutral, use the label neutral.\n"
        f"Return exactly one JSON object in this schema: {schema}\n"
        f"Text: {json.dumps(text, ensure_ascii=True)}"
    )


def _weighted_scores_from_llm_output(raw_output: str, top_k: int) -> list[tuple[str, float]]:
    payload = _parse_llm_json_payload(raw_output)
    if isinstance(payload, dict):
        weighted_scores = _weighted_scores_from_payload(payload)
        if weighted_scores:
            return weighted_scores[: max(1, top_k)]

    scanned_label = _scan_first_known_label(raw_output)
    if scanned_label:
        return [(scanned_label, 1.0)]
    return []


def _parse_llm_json_payload(raw_output: str) -> dict[str, Any] | None:
    normalized = raw_output.strip()
    if not normalized:
        return None

    candidates = [normalized]
    extracted = _extract_json_object(normalized)
    if extracted and extracted not in candidates:
        candidates.append(extracted)

    for candidate in candidates:
        try:
            decoded = json.loads(_strip_code_fences(candidate))
        except json.JSONDecodeError:
            continue
        if isinstance(decoded, dict):
            return decoded
    return None


def _weighted_scores_from_payload(payload: dict[str, Any]) -> list[tuple[str, float]]:
    weighted_scores: list[tuple[str, float]] = []

    list_candidates = [
        payload.get("labels"),
        payload.get("emotions"),
        payload.get("results"),
    ]
    for candidate in list_candidates:
        if isinstance(candidate, list):
            for item in candidate:
                label, score = _payload_item_to_label_score(item)
                if label and score > 0:
                    weighted_scores.append((label, score))

    single_label, single_score = _payload_item_to_label_score(payload)
    if single_label and single_score > 0:
        weighted_scores.append((single_label, single_score))

    primary_label = str(payload.get("primary_label", "")).strip().lower()
    primary_score = _coerce_float(
        payload.get("primary_score", payload.get("confidence", payload.get("score", 0.0)))
    )
    if primary_label in GOEMOTIONS_TO_VECTOR and primary_score > 0:
        weighted_scores.append((primary_label, primary_score))

    deduped: dict[str, float] = {}
    for label, score in weighted_scores:
        if label not in GOEMOTIONS_TO_VECTOR or score <= 0:
            continue
        deduped[label] = max(deduped.get(label, 0.0), score)
    return sorted(deduped.items(), key=lambda item: item[1], reverse=True)


def _payload_item_to_label_score(item: Any) -> tuple[str, float]:
    if not isinstance(item, dict):
        return "", 0.0

    label = str(item.get("label") or item.get("emotion") or item.get("name") or "").strip().lower()
    score = _coerce_float(item.get("score", item.get("confidence", item.get("weight", 0.0))))
    if label not in GOEMOTIONS_TO_VECTOR:
        return "", 0.0
    return label, score


def _coerce_float(value: Any) -> float:
    try:
        return float(value)
    except (TypeError, ValueError):
        return 0.0


def _strip_code_fences(text: str) -> str:
    stripped = text.strip()
    if stripped.startswith("```"):
        stripped = re.sub(r"^```[a-zA-Z0-9_-]*\s*", "", stripped)
        stripped = re.sub(r"\s*```$", "", stripped)
    return stripped.strip()


def _extract_json_object(text: str) -> str:
    stripped = _strip_code_fences(text)
    start = stripped.find("{")
    if start < 0:
        return ""

    depth = 0
    for index in range(start, len(stripped)):
        char = stripped[index]
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
            if depth == 0:
                return stripped[start : index + 1]
    return ""


def _scan_first_known_label(text: str) -> str:
    lowered = text.lower()
    for label in sorted(GOEMOTIONS_LABELS, key=len, reverse=True):
        if re.search(rf"\b{re.escape(label)}\b", lowered):
            return label
    return ""


def _normalize_ollama_model_name(model: str) -> str:
    trimmed = model.strip()
    lower = trimmed.lower()

    if lower in {"qwen/qwen3.5-27b", "qwen3.5-27b", "qwen 3.5 27b", "qwen3.5 27b"}:
        return "qwen3.5:27b"
    if lower.startswith("qwen/") and "3.5" in lower and "27b" in lower:
        return "qwen3.5:27b"
    return trimmed
