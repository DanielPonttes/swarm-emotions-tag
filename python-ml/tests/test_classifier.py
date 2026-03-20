import unittest

from app.classifier import EmotionClassifier, NEUTRAL_VECTOR


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


if __name__ == "__main__":
    unittest.main()
