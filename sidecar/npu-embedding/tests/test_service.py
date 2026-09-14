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
"""End-to-end tests for the embedding service.

These run the real pipeline -- tokenise, chunk, bucket, pad, infer, pool,
normalise -- over a deterministic engine. The engine is not what is under test;
the orchestration is.
"""

from __future__ import annotations

import numpy as np
import pytest

from npu_embedding.bucket import DEFAULT_MAX_LENGTH
from npu_embedding.errors import EmptyTextError, InvalidRequestError
from npu_embedding.pooling import cosine_similarity
from npu_embedding.service import EmbeddingService, ServiceConfig

from conftest import DeterministicEngine, WordTokenizer, make_service


def words(count: int, *, prefix: str = "w") -> str:
    """Build a text of `count` distinct words."""
    return " ".join(f"{prefix}{i}" for i in range(count))


# -----------------------------------------------------------------------------
# The property that makes the arbitrary-size API honest
# -----------------------------------------------------------------------------


@pytest.mark.parametrize("length_buckets", [(128,), (256,), (512,), (128, 256, 512)])
def test_embedding_is_identical_across_bucket_sets(
    length_buckets: tuple[int, ...]
) -> None:
    """The same text embeds identically whatever shape it is routed through.

    This is the end-to-end form of the padding-invariance property. If it ever
    fails, the bucket set has become a source of silent drift and the
    arbitrary-size API is no longer honest -- so it must fail loudly here.
    """
    text = words(20)

    baseline, _ = make_service(config=ServiceConfig(length_buckets=(128, 256, 512)))
    expected = np.array(baseline.embed([text]).embeddings[0], dtype=np.float32)

    service, _ = make_service(config=ServiceConfig(length_buckets=length_buckets))
    actual = np.array(service.embed([text]).embeddings[0], dtype=np.float32)

    np.testing.assert_allclose(actual, expected, rtol=0, atol=1e-6)
    assert cosine_similarity(actual, expected) == pytest.approx(1.0, abs=1e-6)


def test_embedding_is_identical_alone_and_inside_a_batch(
    service: EmbeddingService,
) -> None:
    """A text's vector must not depend on what else shared its batch.

    Alone it runs at batch 1; among eight it runs at batch 8 with a different
    sequence length chosen by its neighbours. Both must agree.
    """
    target = words(15, prefix="target")
    neighbours = [words(400, prefix=f"n{i}") for i in range(7)]

    alone = np.array(service.embed([target]).embeddings[0], dtype=np.float32)

    batched_service, _ = make_service()
    result = batched_service.embed([target, *neighbours])
    batched = np.array(result.embeddings[0], dtype=np.float32)

    assert cosine_similarity(alone, batched) == pytest.approx(1.0, abs=1e-6)
    np.testing.assert_allclose(batched, alone, rtol=0, atol=1e-6)


def test_batch_order_is_preserved(service: EmbeddingService) -> None:
    """Results come back aligned with the request despite length-sorted batching."""
    texts = [words(n, prefix=f"t{n}") for n in (3, 400, 17, 250, 9, 600, 40)]

    batch = service.embed(texts)
    assert len(batch.embeddings) == len(texts)

    for i, text in enumerate(texts):
        single, _ = make_service()
        expected = np.array(single.embed([text]).embeddings[0], dtype=np.float32)
        actual = np.array(batch.embeddings[i], dtype=np.float32)
        assert cosine_similarity(actual, expected) == pytest.approx(
            1.0, abs=1e-6
        ), f"text {i} came back misaligned"


# -----------------------------------------------------------------------------
# Chunking beyond the model ceiling
# -----------------------------------------------------------------------------


def test_over_long_text_is_chunked_and_flagged(service: EmbeddingService) -> None:
    """Text past 512 tokens is embedded as pooled chunks, and says so."""
    long_text = words(1500)
    result = service.embed([long_text])

    assert result.chunked == [True]
    assert len(result.embeddings[0]) == service.dimensions
    assert np.all(np.isfinite(np.array(result.embeddings[0], dtype=np.float32)))


def test_short_text_is_not_flagged_as_chunked(service: EmbeddingService) -> None:
    result = service.embed([words(10)])
    assert result.chunked == [False]


def test_chunked_text_runs_several_shapes(service: EmbeddingService) -> None:
    """A 1500-word text needs more than one chunk, hence more than one row."""
    result = service.embed([words(1500)])

    # 1500 body tokens at a 510-token window is three chunks, which fit one
    # batch-8 group.
    assert result.total_tokens > DEFAULT_MAX_LENGTH
    assert len(result.shapes) >= 1


def test_chunk_boundary_is_not_a_cliff(service: EmbeddingService) -> None:
    """Just-fits and just-overflows texts both produce usable unit vectors."""
    fits = service.embed([words(500)])
    overflows = service.embed([words(520)])

    assert fits.chunked == [False]
    assert overflows.chunked == [True]

    for result in (fits, overflows):
        vector = np.array(result.embeddings[0], dtype=np.float32)
        assert np.linalg.norm(vector) == pytest.approx(1.0, abs=1e-5)


def test_mixed_batch_of_short_and_over_long_texts(service: EmbeddingService) -> None:
    """Chunked and unchunked texts coexist in one request without misaligning."""
    texts = [words(5), words(1200), words(8)]
    result = service.embed(texts)

    assert result.chunked == [False, True, False]
    assert len(result.embeddings) == 3
    for vector in result.embeddings:
        assert len(vector) == service.dimensions


# -----------------------------------------------------------------------------
# Batch sizes larger than any compiled bucket
# -----------------------------------------------------------------------------


def test_batch_larger_than_every_bucket_is_served(service: EmbeddingService) -> None:
    """100 texts against a max bucket of 8 must all come back, once each."""
    texts = [words(10, prefix=f"u{i}") for i in range(100)]
    result = service.embed(texts)

    assert len(result.embeddings) == 100
    for vector in result.embeddings:
        assert len(vector) == service.dimensions

    # Distinct texts must not collapse to one vector.
    first = np.array(result.embeddings[0], dtype=np.float32)
    second = np.array(result.embeddings[1], dtype=np.float32)
    assert cosine_similarity(first, second) < 0.999999


def test_oversized_batch_only_uses_bucketed_shapes() -> None:
    """Every executed shape must come from the configured bucket set."""
    service, engine = make_service()
    service.embed([words(10, prefix=f"v{i}") for i in range(37)])

    allowed_batches = set(service.config.batch_buckets)
    allowed_lengths = set(service.config.length_buckets)
    for batch, length in engine.infer_shapes:
        assert batch in allowed_batches, f"batch {batch} is outside the bucket set"
        assert length in allowed_lengths, f"length {length} is outside the bucket set"


def test_repeated_calls_reuse_compiled_shapes() -> None:
    """The compile cache must not recompile a shape already held.

    Compilation costs seconds on the NPU, so a cache miss per request would
    dominate the workload.
    """
    service, engine = make_service()
    for _ in range(5):
        service.embed([words(10)])

    assert engine.compile_calls == [(1, 128)], (
        f"expected exactly one compile, got {engine.compile_calls}"
    )


# -----------------------------------------------------------------------------
# Empty and invalid input
# -----------------------------------------------------------------------------


def test_empty_batch_touches_no_device() -> None:
    service, engine = make_service()
    result = service.embed([])

    assert result.embeddings == []
    assert engine.infer_shapes == []
    assert engine.compile_calls == []


def test_empty_text_is_rejected_with_its_index(service: EmbeddingService) -> None:
    with pytest.raises(EmptyTextError) as excinfo:
        service.embed([words(4), "   ", words(6)])

    assert excinfo.value.index == 1
    assert excinfo.value.code == "empty_text"
    assert excinfo.value.status == 400


def test_non_string_input_is_rejected(service: EmbeddingService) -> None:
    with pytest.raises(InvalidRequestError, match=r"texts\[1\] must be a string"):
        service.embed([words(3), 42])


def test_non_list_input_is_rejected(service: EmbeddingService) -> None:
    with pytest.raises(InvalidRequestError, match="must be a list"):
        service.embed("not a list")


def test_text_that_tokenises_to_nothing_still_yields_a_vector() -> None:
    """Punctuation-only input must produce a vector, not vanish from the batch.

    The word tokenizer maps "!!!" to a single token, but a text whose body is
    empty after tokenisation still has to hold its slot so later results stay
    aligned.
    """
    service, _ = make_service()
    result = service.embed(["a", "b"])
    assert len(result.embeddings) == 2


# -----------------------------------------------------------------------------
# Device reporting
# -----------------------------------------------------------------------------


def test_device_is_reported_on_every_result() -> None:
    service, _ = make_service(device="NPU")
    assert service.embed([words(5)]).device == "NPU"


def test_cpu_fallback_is_reported_not_hidden() -> None:
    """Serving from the CPU must be visible in the result and in health.

    Fallback is transparent by design so the service always answers; that makes
    this report the only way an operator learns the NPU is not in use.
    """
    service, _ = make_service(device="CPU", npu_available=False)

    assert service.embed([words(5)]).device == "CPU"

    health = service.health()
    assert health["device"] == "CPU"
    assert health["npu_available"] is False


def test_health_reports_model_and_compiled_shapes() -> None:
    service, _ = make_service()
    service.embed([words(5)])

    health = service.health()
    assert health["status"] == "ok"
    assert health["model"] == "all-MiniLM-L6-v2"
    assert health["dimensions"] == service.dimensions
    assert health["max_tokens"] == DEFAULT_MAX_LENGTH
    assert health["npu_available"] is True
    assert [1, 128] in health["compiled_buckets"]


# -----------------------------------------------------------------------------
# Warmup
# -----------------------------------------------------------------------------


def test_warmup_precompiles_the_whole_bucket_set() -> None:
    """Warmup must cover every bucket so no request pays a compile."""
    service, engine = make_service()
    compiled = service.warmup()

    expected = {
        (b, length)
        for b in service.config.batch_buckets
        for length in service.config.length_buckets
    }
    assert set(compiled) == expected
    assert set(engine.compiled_shapes()) == expected


def test_requests_after_warmup_compile_nothing_new() -> None:
    service, engine = make_service()
    service.warmup()
    before = len(engine.compile_calls)

    service.embed([words(10), words(200), words(400)])
    assert len(engine.compile_calls) == before, "a warmed service must not recompile"


# -----------------------------------------------------------------------------
# Normalisation setting
# -----------------------------------------------------------------------------


def test_normalised_vectors_are_unit_length() -> None:
    service, _ = make_service(config=ServiceConfig(normalize=True))
    vector = np.array(service.embed([words(12)]).embeddings[0], dtype=np.float32)
    assert np.linalg.norm(vector) == pytest.approx(1.0, abs=1e-5)


def test_normalisation_can_be_disabled() -> None:
    """Unnormalised output is available for callers that scale themselves."""
    service, _ = make_service(config=ServiceConfig(normalize=False))
    vector = np.array(service.embed([words(12)]).embeddings[0], dtype=np.float32)
    assert np.linalg.norm(vector) != pytest.approx(1.0, abs=1e-3)


def test_total_tokens_counts_real_tokens_only() -> None:
    """Padding must not inflate the token count reported to callers."""
    service, _ = make_service()
    # Five words plus CLS and SEP.
    assert service.embed([words(5)]).total_tokens == 7


def test_dimensions_comes_from_the_engine() -> None:
    engine = DeterministicEngine(hidden_size=32)
    service = EmbeddingService(WordTokenizer(), engine, ServiceConfig())
    assert service.dimensions == 32
    assert len(service.embed([words(3)]).embeddings[0]) == 32


def test_service_device_property_tracks_the_engine() -> None:
    """The service reports the engine's device without a request first."""
    service, _ = make_service(device="CPU")
    assert service.device == "CPU"


class EngineWithoutNPUFlag:
    """An Engine that does not advertise npu_available.

    A third-party or future engine need not expose that attribute, so health
    must infer it from the device rather than raising AttributeError.
    """

    hidden_size = 8

    def __init__(self, device: str) -> None:
        self._device = device

    def ensure_shape(self, batch: int, length: int) -> str:
        return self._device

    def infer(self, input_ids, attention_mask):
        return np.zeros(
            (input_ids.shape[0], input_ids.shape[1], self.hidden_size), dtype=np.float32
        )

    @property
    def device(self) -> str:
        return self._device

    def compiled_shapes(self):
        return []


def test_health_infers_npu_availability_when_unadvertised() -> None:
    service = EmbeddingService(
        WordTokenizer(), EngineWithoutNPUFlag("NPU"), ServiceConfig()
    )
    assert service.health()["npu_available"] is True

    service = EmbeddingService(
        WordTokenizer(), EngineWithoutNPUFlag("CPU"), ServiceConfig()
    )
    assert service.health()["npu_available"] is False


# -----------------------------------------------------------------------------
# A fallback during warmup must be visible before any request arrives
# -----------------------------------------------------------------------------

# The device report exists precisely because fallback is transparent. Warmup
# compiles every bucket before the first request, so if the NPU enumerates but
# refuses those shapes, the fallback happens entirely inside warmup -- and health
# would have reported "NPU" until some later inference corrected it.


def test_warmup_fallback_is_reported_immediately() -> None:
    """Health must not claim the NPU when warmup fell back to the CPU."""
    engine = DeterministicEngine(
        refuse_shapes={(1, 128), (1, 256), (1, 512), (8, 128), (8, 256), (8, 512)}
    )
    service = EmbeddingService(WordTokenizer(), engine, ServiceConfig())

    service.warmup()

    health = service.health()
    assert health["device"] == "CPU", (
        "health reported %r after warmup fell back; the fallback must be visible "
        "before any request" % health["device"]
    )


def test_partial_warmup_fallback_lists_the_offending_shapes() -> None:
    """A partial fallback cannot be described by `device` alone."""
    engine = DeterministicEngine(refuse_shapes={(8, 512)})
    service = EmbeddingService(WordTokenizer(), engine, ServiceConfig())

    service.warmup()
    health = service.health()

    assert [8, 512, "CPU"] in health["fallback_shapes"]
    # The shapes the NPU did accept must not be listed.
    assert [1, 128, "CPU"] not in health["fallback_shapes"]
    assert len(health["fallback_shapes"]) == 1


def test_no_fallback_reports_an_empty_list() -> None:
    engine = DeterministicEngine()
    service = EmbeddingService(WordTokenizer(), engine, ServiceConfig())

    service.warmup()
    health = service.health()

    assert health["fallback_shapes"] == []
    assert health["device"] == "NPU"
