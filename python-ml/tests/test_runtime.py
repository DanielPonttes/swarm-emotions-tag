import unittest

from app.runtime import collect_runtime_info, runtime_info_dict


class RuntimeInfoTests(unittest.TestCase):
    def test_collect_runtime_info_has_stable_shape(self) -> None:
        payload = runtime_info_dict(collect_runtime_info())

        self.assertIn("torch_version", payload)
        self.assertIn("cuda_available", payload)
        self.assertIn("cuda_device_count", payload)
        self.assertIn("cuda_devices", payload)
        self.assertIsInstance(payload["cuda_devices"], list)


if __name__ == "__main__":
    unittest.main()
