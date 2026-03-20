from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class RuntimeInfo:
    torch_version: str | None
    cuda_available: bool
    cuda_device_count: int
    cuda_devices: list[str]


def collect_runtime_info() -> RuntimeInfo:
    try:
        import torch
    except Exception:
        return RuntimeInfo(
            torch_version=None,
            cuda_available=False,
            cuda_device_count=0,
            cuda_devices=[],
        )

    cuda_available = bool(torch.cuda.is_available())
    cuda_devices: list[str] = []
    cuda_device_count = 0

    if cuda_available:
        try:
            cuda_device_count = int(torch.cuda.device_count())
        except Exception:
            cuda_device_count = 0

        for index in range(cuda_device_count):
            try:
                cuda_devices.append(str(torch.cuda.get_device_name(index)))
            except Exception:
                cuda_devices.append(f"cuda:{index}")

    return RuntimeInfo(
        torch_version=str(getattr(torch, "__version__", "")) or None,
        cuda_available=cuda_available,
        cuda_device_count=cuda_device_count,
        cuda_devices=cuda_devices,
    )


def runtime_info_dict(info: RuntimeInfo) -> dict[str, object]:
    return {
        "torch_version": info.torch_version,
        "cuda_available": info.cuda_available,
        "cuda_device_count": info.cuda_device_count,
        "cuda_devices": list(info.cuda_devices),
    }
