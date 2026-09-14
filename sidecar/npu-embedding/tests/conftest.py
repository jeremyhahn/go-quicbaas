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
"""Shared fixtures.

The engine and tokenizer here are *real implementations of their protocols*, not
mocks of the code under test. What is under test is the sidecar's bucketing,
padding, pooling and chunking; a neural network is not needed to check any of
those, and using one would make the properties unverifiable rather than more
convincing.

One detail in :class:`DeterministicEngine` is load-bearing: at masked positions
it emits deliberately enormous values. A correct masked mean pool ignores them
entirely, so the padding-invariance tests only pass if padded positions really
are excluded. If someone later pools over the whole tensor, those values make
the test explode rather than drift quietly.
"""

from __future__ import annotations

import numpy as np
import pytest

from npu_embedding.service import EmbeddingService, ServiceConfig

HIDDEN_SIZE = 16

# The value emitted at masked positions. Large enough that including even one
# such position in a mean would move the result far outside any tolerance.
POISON = 1.0e6


class WordTokenizer:
    """A deterministic whitespace tokenizer implementing the Tokenizer protocol.

    Token ids are derived from a stable hash of each word, so the same text
    always produces the same ids across runs and processes.
    """

    cls_token = 101
    sep_token = 102
    pad_token = 0

    # Ids are mapped into this range, above the special tokens.
    vocab_floor = 1000
    vocab_size = 8000

    def encode_body(self, text: str) -> list[int]:
        """Return ids for each whitespace-separated word, without specials."""
        out: list[int] = []
        for word in text.split():
            # A simple, stable, dependency-free hash.
            acc = 0
            for ch in word:
                acc = (acc * 31 + ord(ch)) % self.vocab_size
            out.append(self.vocab_floor + acc)
        return out

    @property
    def cls_id(self) -> int:
        return self.cls_token

    @property
    def sep_id(self) -> int:
        return self.sep_token

    @property
    def pad_id(self) -> int:
        return self.pad_token


class DeterministicEngine:
    """An Engine whose output depends only on the token at each position.

    This mirrors the property a real transformer has under correct attention
    masking -- padded positions cannot influence real ones -- which is exactly
    the property that makes shape bucketing exact.
    """

    def __init__(
        self,
        hidden_size: int = HIDDEN_SIZE,
        device: str = "NPU",
        npu_available: bool = True,
        prefer_device: str = "NPU",
        refuse_shapes: set[tuple[int, int]] | None = None,
        fallback_device: str = "CPU",
    ) -> None:
        self._hidden_size = hidden_size
        self._device = device
        self.npu_available = npu_available
        self._prefer_device = prefer_device

        # Shapes the preferred device refuses, which therefore compile on
        # fallback_device. This is fault injection on the real seam, not a
        # simulation of the logic under test.
        self._refuse_shapes = refuse_shapes or set()
        self._fallback_device = fallback_device
        self._shape_device: dict[tuple[int, int], str] = {}

        self._compiled: set[tuple[int, int]] = set()

        # The device last compiled or served, mirroring OpenVINOEngine.
        self._device_reported = device

        # Recorded so tests can assert which shapes were executed and how often
        # compilation actually happened.
        self.infer_shapes: list[tuple[int, int]] = []
        self.compile_calls: list[tuple[int, int]] = []

    def ensure_shape(self, batch: int, length: int) -> str:
        key = (batch, length)
        if key not in self._compiled:
            self._compiled.add(key)
            self.compile_calls.append(key)
            self._shape_device[key] = (
                self._fallback_device if key in self._refuse_shapes else self._device
            )
        # Mirrors OpenVINOEngine: the reported device is updated at compile time,
        # not only after an inference, so a fallback during warmup is visible
        # before any request arrives.
        self._device_reported = self._shape_device[key]
        return self._shape_device[key]

    def infer(self, input_ids: np.ndarray, attention_mask: np.ndarray) -> np.ndarray:
        batch, length = int(input_ids.shape[0]), int(input_ids.shape[1])
        self.ensure_shape(batch, length)
        self.infer_shapes.append((batch, length))

        out = np.full((batch, length, self._hidden_size), POISON, dtype=np.float32)
        for b in range(batch):
            for i in range(length):
                if attention_mask[b, i] == 1:
                    out[b, i, :] = self._token_vector(int(input_ids[b, i]))
        return out

    def _token_vector(self, token_id: int) -> np.ndarray:
        """Map a token id to a stable vector."""
        idx = np.arange(self._hidden_size, dtype=np.float32)
        return np.sin(token_id * 0.001 + idx * 0.1).astype(np.float32)

    @property
    def device(self) -> str:
        return self._device_reported

    @property
    def hidden_size(self) -> int:
        return self._hidden_size

    def compiled_shapes(self) -> list[tuple[int, int]]:
        return sorted(self._compiled)

    def fallback_shapes(self) -> list[tuple[int, int, str]]:
        return sorted(
            (batch, length, device)
            for (batch, length), device in self._shape_device.items()
            if device != self._prefer_device
        )


@pytest.fixture
def tokenizer() -> WordTokenizer:
    """Return a deterministic tokenizer."""
    return WordTokenizer()


@pytest.fixture
def engine() -> DeterministicEngine:
    """Return a deterministic engine reporting the NPU."""
    return DeterministicEngine()


@pytest.fixture
def service(tokenizer: WordTokenizer, engine: DeterministicEngine) -> EmbeddingService:
    """Return a service over the deterministic tokenizer and engine."""
    return EmbeddingService(tokenizer, engine, ServiceConfig())


def make_service(
    *,
    device: str = "NPU",
    npu_available: bool = True,
    config: ServiceConfig | None = None,
) -> tuple[EmbeddingService, DeterministicEngine]:
    """Build a service and hand back its engine for assertions."""
    eng = DeterministicEngine(device=device, npu_available=npu_available)
    return EmbeddingService(WordTokenizer(), eng, config or ServiceConfig()), eng
