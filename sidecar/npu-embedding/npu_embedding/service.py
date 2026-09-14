# Copyright (c) 2025 Jeremy Hahn
# Copyright (c) 2025 Automate The Things, LLC
#
# This file is part of go-quicbaas.
#
# go-quicbaas is dual-licensed:
#
# 1. GNU Affero General Public License v3.0 (AGPL-3.0)
#    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
#
# 2. Commercial License
#    Contact licensing@automatethethings.com for commercial licensing options.
"""Orchestration: arbitrary text in, fixed-shape inference underneath.

This is the layer that makes the sidecar's promise true -- callers submit any
text at any batch size and never learn that the device only runs a handful of
static shapes. It owns the sequence:

    tokenise -> chunk over-long text -> group into buckets -> pad -> infer
             -> masked mean pool -> pool chunks per text -> normalise

and nothing else in the service needs to know that order.
"""

from __future__ import annotations

from dataclasses import dataclass, field

import numpy as np

from npu_embedding import bucket, pooling
from npu_embedding.bucket import Chunk
from npu_embedding.engine import Engine
from npu_embedding.errors import EmptyTextError, InvalidRequestError
from npu_embedding.tokenizer import Tokenizer


@dataclass(slots=True)
class ServiceConfig:
    """Tunables for the embedding service."""

    # Identifier reported back to callers; also what the Go provider sends.
    model_name: str = "all-MiniLM-L6-v2"

    batch_buckets: tuple[int, ...] = bucket.DEFAULT_BATCH_BUCKETS
    length_buckets: tuple[int, ...] = bucket.DEFAULT_LENGTH_BUCKETS

    # The model's positional ceiling, not the device's.
    max_length: int = bucket.DEFAULT_MAX_LENGTH

    chunk_overlap: int = bucket.DEFAULT_CHUNK_OVERLAP

    # Unit-length vectors let the index use a dot product for cosine.
    normalize: bool = True


@dataclass(slots=True)
class EmbedResult:
    """The outcome of one embed call."""

    embeddings: list[list[float]]

    # The device that served the final group: "NPU" or "CPU". Reported because
    # fallback is transparent, so this is the only way a caller can tell.
    device: str

    # Per-text flag: True when the text exceeded the model ceiling and was
    # embedded as several pooled chunks.
    chunked: list[bool] = field(default_factory=list)

    total_tokens: int = 0

    # The static shapes this call actually executed, for diagnostics.
    shapes: list[tuple[int, int]] = field(default_factory=list)


class EmbeddingService:
    """Turns arbitrary text into embeddings over a fixed-shape engine."""

    def __init__(
        self,
        tokenizer: Tokenizer,
        engine: Engine,
        config: ServiceConfig | None = None,
    ) -> None:
        self._tokenizer = tokenizer
        self._engine = engine
        self._config = config or ServiceConfig()

        # The static shapes the most recent embed call executed, for diagnostics.
        self._last_shapes: list[tuple[int, int]] = []

    @property
    def config(self) -> ServiceConfig:
        """Return the service configuration."""
        return self._config

    @property
    def dimensions(self) -> int:
        """Return the width of the vectors this service produces."""
        return self._engine.hidden_size

    @property
    def device(self) -> str:
        """Return the device that served the most recent inference."""
        return self._engine.device

    def warmup(self) -> list[tuple[int, int]]:
        """Precompile the whole bucket set before serving traffic.

        The default set costs about 7.8s in total, paid once, which keeps the
        first real request off the multi-second compile path. Shapes the device
        refuses are compiled on the CPU instead, exactly as during serving, so a
        warmup never prevents startup.
        """
        compiled: list[tuple[int, int]] = []
        for shape in bucket.precompile_shapes(
            self._config.batch_buckets, self._config.length_buckets
        ):
            self._engine.ensure_shape(shape.batch, shape.length)
            compiled.append(shape.as_tuple())
        return compiled

    def embed(self, texts: list[str]) -> EmbedResult:
        """Embed any number of texts of any length.

        Raises :class:`InvalidRequestError` when ``texts`` is not a list of
        non-empty strings. An empty list is valid and returns an empty result
        without touching the device.
        """
        if not isinstance(texts, list):
            raise InvalidRequestError("texts must be a list of strings")
        if not texts:
            return EmbedResult(embeddings=[], device=self._engine.device)

        for index, text in enumerate(texts):
            if not isinstance(text, str):
                raise InvalidRequestError(f"texts[{index}] must be a string")
            if not text.strip():
                raise EmptyTextError(index)

        chunks, chunked_flags = self._build_chunks(texts)
        chunk_vectors = self._infer_chunks(chunks)
        embeddings = self._pool_by_text(texts, chunks, chunk_vectors)

        return EmbedResult(
            embeddings=[vector.tolist() for vector in embeddings],
            device=self._engine.device,
            chunked=chunked_flags,
            total_tokens=sum(chunk.length for chunk in chunks),
            shapes=self._last_shapes,
        )

    def _build_chunks(self, texts: list[str]) -> tuple[list[Chunk], list[bool]]:
        """Tokenise every text and split whatever exceeds the model ceiling."""
        chunks: list[Chunk] = []
        chunked_flags: list[bool] = []

        for index, text in enumerate(texts):
            body = self._tokenizer.encode_body(text)
            windows = bucket.chunk_tokens(
                body,
                max_length=self._config.max_length,
                cls_id=self._tokenizer.cls_id,
                sep_id=self._tokenizer.sep_id,
                overlap=self._config.chunk_overlap,
            )
            chunked_flags.append(len(windows) > 1)
            for window in windows:
                chunks.append(Chunk(text_index=index, token_ids=window))

        return chunks, chunked_flags

    def _infer_chunks(self, chunks: list[Chunk]) -> np.ndarray:
        """Run every chunk through the engine, returning one vector per chunk.

        Chunks are grouped into buckets by length, so the returned array is
        reordered back into chunk order before it is handed on.
        """
        groups = bucket.plan_groups(
            chunks,
            batch_buckets=self._config.batch_buckets,
            length_buckets=self._config.length_buckets,
            max_length=self._config.max_length,
        )

        out = np.zeros((len(chunks), self._engine.hidden_size), dtype=np.float32)
        shapes: list[tuple[int, int]] = []

        for group in groups:
            input_ids, attention_mask = bucket.build_inputs(
                chunks, group, pad_id=self._tokenizer.pad_id
            )
            hidden = self._engine.infer(input_ids, attention_mask)
            pooled = pooling.masked_mean_pool(hidden, attention_mask)

            # Only the leading rows carry real chunks; the rest are the padding
            # rows that fill the batch bucket and are discarded here.
            for row, chunk_index in enumerate(group.chunk_indices):
                out[chunk_index] = pooled[row]

            shapes.append(group.shape.as_tuple())

        self._last_shapes = shapes
        return out

    def _pool_by_text(
        self,
        texts: list[str],
        chunks: list[Chunk],
        chunk_vectors: np.ndarray,
    ) -> list[np.ndarray]:
        """Combine each text's chunk vectors into that text's single vector."""
        rows_by_text: list[list[int]] = [[] for _ in texts]
        for chunk_index, chunk in enumerate(chunks):
            rows_by_text[chunk.text_index].append(chunk_index)

        pooled: list[np.ndarray] = []
        for rows in rows_by_text:
            vectors = chunk_vectors[rows]
            weights = np.array([chunks[i].length for i in rows], dtype=np.float32)
            pooled.append(pooling.pool_chunks(vectors, weights))

        stacked = np.vstack(pooled)
        if self._config.normalize:
            stacked = pooling.l2_normalize(stacked)
        return [row for row in stacked]

    def health(self) -> dict[str, object]:
        """Return the sidecar's self-report for the health endpoint."""
        npu_available = getattr(self._engine, "npu_available", None)
        if npu_available is None:
            npu_available = self._engine.device == "NPU"

        # A partial fallback -- some shapes on the NPU, some refused to the CPU --
        # cannot be described by `device` alone, so list the exceptions.
        fallback = getattr(self._engine, "fallback_shapes", None)
        fallback_shapes = [list(entry) for entry in fallback()] if fallback else []

        return {
            "status": "ok",
            "device": self._engine.device,
            "model": self._config.model_name,
            "dimensions": self._engine.hidden_size,
            "max_tokens": self._config.max_length,
            "compiled_buckets": [list(s) for s in self._engine.compiled_shapes()],
            "npu_available": bool(npu_available),
            "fallback_shapes": fallback_shapes,
        }
