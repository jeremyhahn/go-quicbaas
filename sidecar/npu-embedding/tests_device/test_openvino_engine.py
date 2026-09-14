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
"""Tests that run the real OpenVINO runtime.

This is a separate tier, not a skipped part of the unit suite: it needs the
``device`` extra installed (``pip install -e '.[device,dev]'``) and is run by
``make test-npu-sidecar-device``. Nothing here is conditional -- if it is
invoked, it must pass.

It runs on the CPU plugin, so it verifies everything about the engine except NPU
behaviour itself: that the 2026 API calls are correct, that a dynamic model is
really reshaped to a static one before compilation, that the compile cache works,
that inputs are fed by name, and that the 3-D output is found. On the NPU host
the same suite runs with ``NPU_EMBEDDING_TEST_DEVICE=NPU`` to cover the device.

Most valuably, it asserts the padding-invariance property against a real
inference runtime rather than a deterministic double.
"""

from __future__ import annotations

import os

import numpy as np
import pytest

from npu_embedding.engine import DEVICE_CPU, OpenVINOEngine
from npu_embedding.errors import InferenceError, ModelLoadError
from npu_embedding.pooling import cosine_similarity, l2_normalize, masked_mean_pool
from npu_embedding.service import EmbeddingService, ServiceConfig

from tiny_model import HIDDEN_SIZE, build_tiny_ir

# Set to NPU on the NPU host to run this suite against the device.
TEST_DEVICE = os.environ.get("NPU_EMBEDDING_TEST_DEVICE", DEVICE_CPU)


@pytest.fixture(scope="module")
def model_xml(tmp_path_factory: pytest.TempPathFactory) -> str:
    """Write the tiny dynamic IR once for the module."""
    directory = tmp_path_factory.mktemp("ir")
    return build_tiny_ir(str(directory / "tiny.xml"))


@pytest.fixture
def engine(model_xml: str) -> OpenVINOEngine:
    """Return an engine over the tiny IR on the device under test."""
    return OpenVINOEngine(model_xml, prefer_device=TEST_DEVICE)


class WordTokenizer:
    """A tokenizer whose ids stay inside the tiny model's vocabulary."""

    vocab_size = 400

    def encode_body(self, text: str) -> list[int]:
        out = []
        for word in text.split():
            acc = 0
            for ch in word:
                acc = (acc * 31 + ord(ch)) % self.vocab_size
            out.append(3 + acc)
        return out

    @property
    def cls_id(self) -> int:
        return 1

    @property
    def sep_id(self) -> int:
        return 2

    @property
    def pad_id(self) -> int:
        return 0


# -----------------------------------------------------------------------------
# Construction
# -----------------------------------------------------------------------------


def test_hidden_size_is_read_from_the_model(engine: OpenVINOEngine) -> None:
    """The engine discovers the vector width from the IR's 3-D output."""
    assert engine.hidden_size == HIDDEN_SIZE


def test_missing_model_raises_a_typed_error() -> None:
    with pytest.raises(ModelLoadError) as excinfo:
        OpenVINOEngine("/nonexistent/model.xml", prefer_device=DEVICE_CPU)
    assert excinfo.value.code == "model_load_failed"


def test_device_is_reported_before_any_inference(engine: OpenVINOEngine) -> None:
    assert engine.device == TEST_DEVICE


# -----------------------------------------------------------------------------
# Static reshaping and the compile cache
# -----------------------------------------------------------------------------


def test_a_dynamic_model_is_compiled_at_a_static_shape(engine: OpenVINOEngine) -> None:
    """The reshape step is what makes the model compilable at all.

    On the NPU a dynamic ``[?, ?]`` model fails with
    ZE_RESULT_ERROR_INVALID_ARGUMENT; the fixture IR is deliberately dynamic on
    disk so this asserts the reshape really happened.
    """
    assert engine.ensure_shape(4, 32) == TEST_DEVICE
    assert (4, 32) in engine.compiled_shapes()


def test_compile_cache_is_reused(engine: OpenVINOEngine) -> None:
    """Compiling the same shape twice would waste seconds of device time."""
    engine.ensure_shape(1, 16)
    engine.ensure_shape(1, 16)
    engine.ensure_shape(1, 16)

    assert engine.compiled_shapes() == [(1, 16)]


def test_distinct_shapes_are_cached_separately(engine: OpenVINOEngine) -> None:
    engine.ensure_shape(1, 16)
    engine.ensure_shape(8, 16)
    engine.ensure_shape(1, 32)

    assert sorted(engine.compiled_shapes()) == [(1, 16), (1, 32), (8, 16)]


def test_shape_device_is_none_before_compilation(engine: OpenVINOEngine) -> None:
    assert engine.shape_device(2, 64) is None
    engine.ensure_shape(2, 64)
    assert engine.shape_device(2, 64) == TEST_DEVICE


# -----------------------------------------------------------------------------
# Inference
# -----------------------------------------------------------------------------


def build_batch(
    rows: list[list[int]], length: int, pad_id: int = 0
) -> tuple[np.ndarray, np.ndarray]:
    """Pad token rows into a fixed (batch, length) pair."""
    batch = len(rows)
    input_ids = np.full((batch, length), pad_id, dtype=np.int64)
    attention_mask = np.zeros((batch, length), dtype=np.int64)
    for r, tokens in enumerate(rows):
        input_ids[r, : len(tokens)] = tokens
        attention_mask[r, : len(tokens)] = 1
    return input_ids, attention_mask


def test_infer_returns_the_expected_tensor(engine: OpenVINOEngine) -> None:
    input_ids, attention_mask = build_batch([[5, 6, 7], [9, 10]], length=16)
    hidden = engine.infer(input_ids, attention_mask)

    assert hidden.shape == (2, 16, HIDDEN_SIZE)
    assert hidden.dtype == np.float32
    assert np.all(np.isfinite(hidden))


def test_infer_compiles_on_demand(engine: OpenVINOEngine) -> None:
    """A shape outside the warmed set is compiled lazily rather than refused."""
    assert engine.compiled_shapes() == []
    input_ids, attention_mask = build_batch([[5, 6]], length=48)
    engine.infer(input_ids, attention_mask)
    assert (1, 48) in engine.compiled_shapes()


def test_infer_rejects_mismatched_inputs(engine: OpenVINOEngine) -> None:
    with pytest.raises(InferenceError, match="must agree"):
        engine.infer(
            np.zeros((1, 16), dtype=np.int64), np.zeros((1, 32), dtype=np.int64)
        )


# -----------------------------------------------------------------------------
# The invariance property, against a real runtime
# -----------------------------------------------------------------------------


@pytest.mark.parametrize("length", [8, 16, 32, 64, 128, 256])
def test_padding_invariance_through_the_real_runtime(
    engine: OpenVINOEngine, length: int
) -> None:
    """The measured property, reproduced against real inference.

    On the real model the same text padded to 32/64/128/256 gave cosine
    1.000000 pairwise. Here the same holds through an actual OpenVINO forward
    pass: masked positions are excluded from the mean, so the padded length
    cannot move the vector.
    """
    tokens = [5, 17, 42, 108, 7]

    baseline_ids, baseline_mask = build_batch([tokens], length=len(tokens))
    baseline = l2_normalize(
        masked_mean_pool(engine.infer(baseline_ids, baseline_mask), baseline_mask)
    )

    padded_ids, padded_mask = build_batch([tokens], length=length)
    padded = l2_normalize(
        masked_mean_pool(engine.infer(padded_ids, padded_mask), padded_mask)
    )

    assert cosine_similarity(padded[0], baseline[0]) == pytest.approx(1.0, abs=1e-6)
    np.testing.assert_allclose(padded, baseline, rtol=0, atol=1e-5)


def test_batch_padding_invariance_through_the_real_runtime(
    engine: OpenVINOEngine,
) -> None:
    """A row's vector is unaffected by the other rows sharing its batch."""
    tokens = [11, 22, 33]

    alone_ids, alone_mask = build_batch([tokens], length=32)
    alone = masked_mean_pool(engine.infer(alone_ids, alone_mask), alone_mask)

    crowded_ids, crowded_mask = build_batch(
        [tokens, [99] * 30, [7, 8], [400 % 400 + 3] * 12], length=32
    )
    crowded = masked_mean_pool(engine.infer(crowded_ids, crowded_mask), crowded_mask)

    np.testing.assert_allclose(crowded[0:1], alone, rtol=0, atol=1e-5)


# -----------------------------------------------------------------------------
# The whole pipeline over a real runtime
# -----------------------------------------------------------------------------


def test_service_end_to_end_over_real_openvino(model_xml: str) -> None:
    """The full sidecar pipeline, with nothing simulated below it."""
    engine = OpenVINOEngine(model_xml, prefer_device=TEST_DEVICE)
    service = EmbeddingService(
        WordTokenizer(),
        engine,
        ServiceConfig(length_buckets=(32, 64, 128), max_length=128),
    )

    result = service.embed(["hello world", "a much longer piece of text here"])

    assert len(result.embeddings) == 2
    for vector in result.embeddings:
        assert len(vector) == HIDDEN_SIZE
        assert np.linalg.norm(np.array(vector, dtype=np.float32)) == pytest.approx(
            1.0, abs=1e-5
        )
    assert result.device == TEST_DEVICE


def test_service_invariance_across_bucket_sets_over_real_openvino(
    model_xml: str,
) -> None:
    """Bucket choice must not change an embedding, on a real runtime."""
    text = "the quick brown fox jumps over the lazy dog"

    def embed_with(length_buckets: tuple[int, ...]) -> np.ndarray:
        engine = OpenVINOEngine(model_xml, prefer_device=TEST_DEVICE)
        service = EmbeddingService(
            WordTokenizer(),
            engine,
            ServiceConfig(length_buckets=length_buckets, max_length=256),
        )
        return np.array(service.embed([text]).embeddings[0], dtype=np.float32)

    narrow = embed_with((16,))
    wide = embed_with((256,))
    mixed = embed_with((16, 64, 256))

    assert cosine_similarity(narrow, wide) == pytest.approx(1.0, abs=1e-6)
    assert cosine_similarity(narrow, mixed) == pytest.approx(1.0, abs=1e-6)


def test_service_chunks_over_long_text_over_real_openvino(model_xml: str) -> None:
    """Chunking and chunk pooling work against a real forward pass."""
    engine = OpenVINOEngine(model_xml, prefer_device=TEST_DEVICE)
    service = EmbeddingService(
        WordTokenizer(),
        engine,
        ServiceConfig(length_buckets=(32,), max_length=32),
    )

    long_text = " ".join(f"word{i}" for i in range(200))
    result = service.embed([long_text])

    assert result.chunked == [True]
    assert result.total_tokens > 32
    vector = np.array(result.embeddings[0], dtype=np.float32)
    assert np.all(np.isfinite(vector))
    assert np.linalg.norm(vector) == pytest.approx(1.0, abs=1e-5)


def test_warmup_compiles_every_bucket_over_real_openvino(model_xml: str) -> None:
    """Warmup must leave nothing for the first request to compile."""
    engine = OpenVINOEngine(model_xml, prefer_device=TEST_DEVICE)
    service = EmbeddingService(
        WordTokenizer(),
        engine,
        ServiceConfig(batch_buckets=(1, 4), length_buckets=(16, 32), max_length=32),
    )

    compiled = service.warmup()
    assert set(compiled) == {(1, 16), (1, 32), (4, 16), (4, 32)}
    assert set(engine.compiled_shapes()) == set(compiled)

    before = set(engine.compiled_shapes())
    service.embed(["short text", "another short one", "and a third"])
    assert set(engine.compiled_shapes()) == before, "a warmed engine must not recompile"


# -----------------------------------------------------------------------------
# Fallback visibility on the real engine
# -----------------------------------------------------------------------------

# On a host with no NPU, `prefer_device="NPU"` means every shape compiles on the
# CPU. That is exactly the transparent-fallback case, and the real engine must
# report it before any inference -- otherwise warmup hides it until the first
# request.


def test_fallback_is_reported_at_compile_time_not_inference_time(
    model_xml: str,
) -> None:
    """A fallback during warmup must be visible before any request arrives."""
    engine = OpenVINOEngine(model_xml, prefer_device="NPU")
    if engine.npu_available:
        pytest.fail(
            "this assertion is about a host without an NPU; on the NPU host run "
            "with NPU_EMBEDDING_TEST_DEVICE=NPU, where the device is genuinely NPU"
        )

    # Nothing compiled yet.
    assert engine.fallback_shapes() == []

    engine.ensure_shape(1, 32)

    assert engine.device == DEVICE_CPU, (
        "the engine reported %r after falling back at compile time" % engine.device
    )
    assert (1, 32, DEVICE_CPU) in engine.fallback_shapes()


def test_service_warmup_reports_fallback_through_health(model_xml: str) -> None:
    """The whole chain: warmup falls back, and /v1/health's payload says so."""
    engine = OpenVINOEngine(model_xml, prefer_device="NPU")
    if engine.npu_available:
        pytest.fail("run this on a host without an NPU, or set NPU_EMBEDDING_TEST_DEVICE=NPU")

    service = EmbeddingService(
        WordTokenizer(),
        engine,
        ServiceConfig(batch_buckets=(1,), length_buckets=(16, 32), max_length=32),
    )
    service.warmup()

    health = service.health()
    assert health["device"] == DEVICE_CPU
    assert health["npu_available"] is False
    assert sorted(health["fallback_shapes"]) == [[1, 16, "CPU"], [1, 32, "CPU"]]
