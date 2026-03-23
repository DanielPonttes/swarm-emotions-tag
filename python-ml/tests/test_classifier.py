import json
import threading
import time
import unittest
from unittest import mock

from app.classifier import EmotionClassifier, NEUTRAL_VECTOR


class _FakeHTTPResponse:
    def __init__(self, payload: dict[str, object]) -> None:
        self._payload = payload

    def read(self) -> bytes:
        return json.dumps(self._payload).encode("utf-8")

    def __enter__(self) -> "_FakeHTTPResponse":
        return self

    def __exit__(self, exc_type, exc, tb) -> bool:
        return False


class EmotionClassifierBatchTests(unittest.TestCase):
    def test_classify_many_with_heuristics_preserves_order(self) -> None:
        classifier = EmotionClassifier(mode="heuristic", batch_size=16)

        results = classifier.classify_many(
            [
                "thanks for the quick fix",
                "",
                "this is urgent and I am worried about the deadline",
                "tell me more, I am curious about this design",
            ]
        )

        self.assertEqual(len(results), 4)
        self.assertEqual(results[0].label, "gratitude")
        self.assertEqual(results[1].label, "neutral")
        self.assertEqual(results[1].emotion_vector, NEUTRAL_VECTOR)
        self.assertEqual(results[2].label, "nervousness")
        self.assertEqual(results[3].label, "curiosity")

    def test_classify_many_empty_input(self) -> None:
        classifier = EmotionClassifier(mode="heuristic")

        self.assertEqual(classifier.classify_many([]), [])

    def test_ollama_mode_normalizes_qwen_model_and_parses_json_response(self) -> None:
        seen_requests: list[tuple[str, str, float, bytes | None]] = []

        def fake_urlopen(req, timeout: float = 0) -> _FakeHTTPResponse:
            seen_requests.append((req.full_url, req.get_method(), timeout, req.data))
            if req.full_url.endswith("/api/tags"):
                return _FakeHTTPResponse({"models": [{"name": "qwen3.5:27b"}]})
            if req.full_url.endswith("/api/chat"):
                return _FakeHTTPResponse(
                    {
                        "message": {
                            "content": (
                                "```json\n"
                                '{"labels":[{"label":"gratitude","score":0.75},'
                                '{"label":"joy","score":0.25}]}\n'
                                "```"
                            )
                        }
                    }
                )
            raise AssertionError(f"unexpected URL: {req.full_url}")

        with mock.patch("app.classifier.request.urlopen", side_effect=fake_urlopen):
            classifier = EmotionClassifier(
                mode="ollama",
                model_name="Qwen/Qwen3.5-27B",
                top_k=3,
                ollama_base_url="http://127.0.0.1:11434",
                request_timeout_sec=7.0,
            )
            result = classifier.classify("Thanks for the quick fix.")

        self.assertEqual(result.label, "gratitude")
        self.assertAlmostEqual(result.confidence, 0.75, places=2)
        self.assertEqual(len(result.emotion_vector), 6)
        self.assertEqual(len(seen_requests), 2)

        tags_url, tags_method, tags_timeout, _ = seen_requests[0]
        self.assertEqual(tags_url, "http://127.0.0.1:11434/api/tags")
        self.assertEqual(tags_method, "GET")
        self.assertEqual(tags_timeout, 7.0)

        chat_url, chat_method, chat_timeout, chat_body = seen_requests[1]
        self.assertEqual(chat_url, "http://127.0.0.1:11434/api/chat")
        self.assertEqual(chat_method, "POST")
        self.assertEqual(chat_timeout, 7.0)
        self.assertIsNotNone(chat_body)

        payload = json.loads(chat_body.decode("utf-8"))
        self.assertEqual(payload["model"], "qwen3.5:27b")
        self.assertFalse(payload["stream"])
        self.assertFalse(payload["think"])

    def test_ollama_mode_requires_available_model(self) -> None:
        with mock.patch(
            "app.classifier.request.urlopen",
            return_value=_FakeHTTPResponse({"models": [{"name": "llama3.2:3b"}]}),
        ):
            with self.assertRaises(RuntimeError):
                EmotionClassifier(
                    mode="ollama",
                    model_name="Qwen/Qwen3.5-27B",
                    ollama_base_url="http://127.0.0.1:11434",
                )

    def test_ollama_mode_falls_back_to_label_scan_when_model_skips_json(self) -> None:
        responses = iter(
            [
                _FakeHTTPResponse({"models": [{"name": "qwen3.5:27b"}]}),
                _FakeHTTPResponse({"message": {"content": "Primary emotion: nervousness"}}),
            ]
        )

        with mock.patch("app.classifier.request.urlopen", side_effect=lambda *args, **kwargs: next(responses)):
            classifier = EmotionClassifier(
                mode="ollama",
                model_name="Qwen/Qwen3.5-27B",
                ollama_base_url="http://127.0.0.1:11434",
            )
            result = classifier.classify("This deadline makes me anxious.")

        self.assertEqual(result.label, "nervousness")
        self.assertEqual(len(result.emotion_vector), 6)

    def test_classify_many_with_ollama_preserves_order_and_limits_concurrency(self) -> None:
        active_calls = 0
        max_active_calls = 0
        lock = threading.Lock()
        label_by_text = {
            "thanks for the quick fix": "gratitude",
            "this is urgent and I am worried about the deadline": "nervousness",
            "tell me more, I am curious about this design": "curiosity",
            "this is awesome and I am very happy with the result": "joy",
        }

        def fake_ollama_chat(text: str) -> str:
            nonlocal active_calls, max_active_calls
            with lock:
                active_calls += 1
                max_active_calls = max(max_active_calls, active_calls)
            try:
                time.sleep(0.03)
                return json.dumps(
                    {
                        "labels": [
                            {
                                "label": label_by_text[text],
                                "score": 1.0,
                            }
                        ]
                    }
                )
            finally:
                with lock:
                    active_calls -= 1

        with mock.patch.object(EmotionClassifier, "_verify_ollama_model"), mock.patch.object(
            EmotionClassifier,
            "_ollama_chat",
            side_effect=fake_ollama_chat,
        ):
            classifier = EmotionClassifier(
                mode="ollama",
                model_name="Qwen/Qwen3.5-27B",
                batch_size=8,
                ollama_max_concurrency=2,
            )
            results = classifier.classify_many(
                [
                    "thanks for the quick fix",
                    "",
                    "this is urgent and I am worried about the deadline",
                    "tell me more, I am curious about this design",
                    "this is awesome and I am very happy with the result",
                ]
            )

        self.assertEqual([result.label for result in results], ["gratitude", "neutral", "nervousness", "curiosity", "joy"])
        self.assertEqual(max_active_calls, 2)


if __name__ == "__main__":
    unittest.main()
