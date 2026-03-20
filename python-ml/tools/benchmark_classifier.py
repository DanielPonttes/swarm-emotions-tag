from __future__ import annotations

import argparse
import itertools
import json
import math
import time
from pathlib import Path

from app.classifier import DEFAULT_MODEL_NAME, EmotionClassifier
from app.runtime import collect_runtime_info, runtime_info_dict

DEFAULT_TEXTS = [
    "I am grateful for your help on this project.",
    "This is urgent and I am worried about the deadline.",
    "I am disappointed because the build failed again.",
    "Tell me more, I am curious about this design.",
    "This is awesome and I am very happy with the result.",
    "I am frustrated because the bug keeps coming back.",
    "I feel nervous about the production rollout tonight.",
    "Thanks, this fix was exactly what I needed.",
    "I did not expect this regression after the refactor.",
    "Can you explain why this architecture was chosen?",
]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Benchmark the Python emotion classifier.")
    parser.add_argument("--mode", default="transformers")
    parser.add_argument("--model-name", default=DEFAULT_MODEL_NAME)
    parser.add_argument("--device", default="cpu")
    parser.add_argument("--top-k", type=int, default=5)
    parser.add_argument("--batch-size", type=int, default=32)
    parser.add_argument("--warmup-batches", type=int, default=10)
    parser.add_argument("--duration-sec", type=float, default=300.0)
    parser.add_argument("--texts-file", default="")
    parser.add_argument("--expected-gpu-substring", default="")
    parser.add_argument("--output", default="")
    return parser.parse_args()


def load_texts(path: str) -> list[str]:
    if not path:
        return DEFAULT_TEXTS.copy()

    texts = [
        line.strip()
        for line in Path(path).read_text(encoding="utf-8").splitlines()
        if line.strip()
    ]
    if not texts:
        raise SystemExit(f"no texts found in {path!r}")
    return texts


def percentile(values: list[float], ratio: float) -> float:
    if not values:
        return 0.0
    sorted_values = sorted(values)
    index = max(0, math.ceil(len(sorted_values) * ratio) - 1)
    return sorted_values[min(index, len(sorted_values) - 1)]


def maybe_sync_cuda() -> None:
    try:
        import torch
    except Exception:
        return

    if torch.cuda.is_available():
        torch.cuda.synchronize()


def maybe_reset_cuda_peaks() -> None:
    try:
        import torch
    except Exception:
        return

    if torch.cuda.is_available():
        for index in range(torch.cuda.device_count()):
            torch.cuda.reset_peak_memory_stats(index)


def maybe_cuda_memory_stats() -> dict[str, object]:
    try:
        import torch
    except Exception:
        return {}

    if not torch.cuda.is_available():
        return {}

    devices: list[dict[str, object]] = []
    for index in range(torch.cuda.device_count()):
        devices.append(
            {
                "index": index,
                "name": torch.cuda.get_device_name(index),
                "memory_allocated_mb": round(torch.cuda.memory_allocated(index) / (1024 * 1024), 3),
                "memory_reserved_mb": round(torch.cuda.memory_reserved(index) / (1024 * 1024), 3),
                "max_memory_allocated_mb": round(torch.cuda.max_memory_allocated(index) / (1024 * 1024), 3),
                "max_memory_reserved_mb": round(torch.cuda.max_memory_reserved(index) / (1024 * 1024), 3),
            }
        )
    return {"cuda_memory": devices}


def next_batch(texts: list[str], batch_size: int, cursor: int) -> tuple[list[str], int]:
    batch = [texts[(cursor + index) % len(texts)] for index in range(batch_size)]
    return batch, (cursor + batch_size) % len(texts)


def validate_runtime(expected_gpu_substring: str, runtime: dict[str, object]) -> None:
    expected = expected_gpu_substring.strip().lower()
    if not expected:
        return

    devices = [str(name).lower() for name in runtime.get("cuda_devices", [])]
    if not devices:
        raise SystemExit("expected CUDA device but none were detected")
    if not any(expected in name for name in devices):
        raise SystemExit(
            f"expected GPU containing {expected_gpu_substring!r}, got {runtime.get('cuda_devices', [])!r}"
        )


def benchmark(args: argparse.Namespace) -> dict[str, object]:
    texts = load_texts(args.texts_file)
    classifier = EmotionClassifier(
        mode=args.mode,
        model_name=args.model_name,
        device=args.device,
        top_k=args.top_k,
        batch_size=args.batch_size,
    )
    runtime = runtime_info_dict(collect_runtime_info())
    validate_runtime(args.expected_gpu_substring, runtime)

    cursor = 0
    for _ in range(max(0, args.warmup_batches)):
        batch, cursor = next_batch(texts, args.batch_size, cursor)
        classifier.classify_many(batch)

    maybe_sync_cuda()
    maybe_reset_cuda_peaks()

    start = time.perf_counter()
    deadline = start + max(0.0, args.duration_sec)
    latencies_ms: list[float] = []
    completed_batches = 0
    completed_items = 0

    while time.perf_counter() < deadline:
        batch, cursor = next_batch(texts, args.batch_size, cursor)
        maybe_sync_cuda()
        batch_start = time.perf_counter()
        results = classifier.classify_many(batch)
        maybe_sync_cuda()
        latency_ms = (time.perf_counter() - batch_start) * 1000.0

        if len(results) != len(batch):
            raise SystemExit(f"unexpected result count: got {len(results)}, want {len(batch)}")

        latencies_ms.append(latency_ms)
        completed_batches += 1
        completed_items += len(batch)

    wall_time_sec = max(time.perf_counter() - start, 1e-9)
    avg_batch_latency_ms = sum(latencies_ms) / len(latencies_ms) if latencies_ms else 0.0
    avg_item_latency_ms = avg_batch_latency_ms / args.batch_size if args.batch_size > 0 else 0.0

    summary: dict[str, object] = {
        "mode": args.mode,
        "model_name": args.model_name,
        "device": args.device,
        "top_k": args.top_k,
        "batch_size": args.batch_size,
        "warmup_batches": args.warmup_batches,
        "requested_duration_sec": args.duration_sec,
        "wall_time_sec": wall_time_sec,
        "texts": len(texts),
        "completed_batches": completed_batches,
        "completed_items": completed_items,
        "items_per_sec": completed_items / wall_time_sec,
        "batches_per_sec": completed_batches / wall_time_sec,
        "avg_batch_latency_ms": avg_batch_latency_ms,
        "p95_batch_latency_ms": percentile(latencies_ms, 0.95),
        "p99_batch_latency_ms": percentile(latencies_ms, 0.99),
        "max_batch_latency_ms": max(latencies_ms) if latencies_ms else 0.0,
        "avg_item_latency_ms": avg_item_latency_ms,
        "runtime": runtime,
    }
    summary.update(maybe_cuda_memory_stats())
    return summary


def main() -> int:
    args = parse_args()
    summary = benchmark(args)

    payload = json.dumps(summary, ensure_ascii=True, indent=2)
    print(payload)

    if args.output:
        output_path = Path(args.output)
        output_path.parent.mkdir(parents=True, exist_ok=True)
        output_path.write_text(payload + "\n", encoding="utf-8")

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
