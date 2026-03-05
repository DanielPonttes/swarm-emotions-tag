def embed_text(text: str, dimension: int = 6) -> list[float]:
    if dimension <= 0:
        raise ValueError("dimension must be greater than zero")

    if not text.strip():
        return [0.0] * dimension

    value = min(len(text.strip()) / 100.0, 1.0)
    return [value] + [0.0] * (dimension - 1)

