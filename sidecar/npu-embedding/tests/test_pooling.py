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
"""Tests for masked mean pooling, normalisation and chunk pooling.

The padding-invariance tests here are the justification for the entire bucket
design: if padding changed an embedding, routing arbitrary text through a
handful of fixed shapes would be a silent accuracy compromise instead of a free
one.
"""

from __future__ import annotations

import numpy as np
import pytest

from npu_embedding.pooling import (
    cosine_similarity,
    l2_normalize,
    masked_mean_pool,
    pool_chunks,
)

from conftest import POISON


def build_hidden(token_vectors: list[np.ndarray], pad_to: int) -> np.ndarray:
    """Lay real token vectors into a padded row, poisoning the pad positions.

    The poison is the point: a correct pool never reads those positions, so any
    implementation that does will fail loudly rather than drift.
    """
    hidden = np.full((1, pad_to, token_vectors[0].shape[0]), POISON, dtype=np.float32)
    for i, vector in enumerate(token_vectors):
        hidden[0, i, :] = vector
    return hidden


def build_mask(real: int, pad_to: int) -> np.ndarray:
    """Return a (1, pad_to) mask with `real` leading ones."""
    mask = np.zeros((1, pad_to), dtype=np.int64)
    mask[0, :real] = 1
    return mask


# -----------------------------------------------------------------------------
# The invariance property
# -----------------------------------------------------------------------------


@pytest.mark.parametrize("pad_to", [8, 16, 32, 64, 128, 256])
def test_masked_mean_pool_is_invariant_to_padding(pad_to: int) -> None:
    """The same tokens pooled at any padded length give the same vector.

    Measured on the real model this holds to cosine 1.000000 across pad lengths
    32/64/128/256; here it holds exactly, because masked positions contribute to
    neither the numerator nor the denominator of the mean.
    """
    rng = np.random.default_rng(seed=1234)
    tokens = [rng.normal(size=16).astype(np.float32) for _ in range(5)]

    baseline = masked_mean_pool(build_hidden(tokens, 5), build_mask(5, 5))
    padded = masked_mean_pool(build_hidden(tokens, pad_to), build_mask(5, pad_to))

    np.testing.assert_allclose(padded, baseline, rtol=0, atol=1e-6)
    assert cosine_similarity(padded[0], baseline[0]) == pytest.approx(1.0, abs=1e-9)


def test_masked_mean_pool_equals_hand_computed_mean() -> None:
    """Pooling is exactly the arithmetic mean of the unmasked token vectors."""
    tokens = [
        np.array([1.0, 2.0, 3.0, 4.0], dtype=np.float32),
        np.array([3.0, 4.0, 5.0, 6.0], dtype=np.float32),
    ]
    hidden = build_hidden(tokens, pad_to=10)
    pooled = masked_mean_pool(hidden, build_mask(2, 10))

    expected = np.array([[2.0, 3.0, 4.0, 5.0]], dtype=np.float32)
    np.testing.assert_allclose(pooled, expected, rtol=0, atol=1e-6)


def test_batch_padding_rows_do_not_affect_real_rows() -> None:
    """Filler rows in a batch bucket cannot perturb the real rows.

    Self-attention never crosses batch rows, so padding the batch dimension up
    to a bucket is as exactly invariant as padding the sequence dimension.
    """
    rng = np.random.default_rng(seed=99)
    tokens = [rng.normal(size=16).astype(np.float32) for _ in range(4)]

    alone = masked_mean_pool(build_hidden(tokens, 8), build_mask(4, 8))

    # The same row inside a batch of 8, with 7 fully-masked poison rows.
    hidden = np.full((8, 8, 16), POISON, dtype=np.float32)
    mask = np.zeros((8, 8), dtype=np.int64)
    for i, vector in enumerate(tokens):
        hidden[0, i, :] = vector
    mask[0, :4] = 1

    batched = masked_mean_pool(hidden, mask)
    np.testing.assert_allclose(batched[0:1], alone, rtol=0, atol=1e-6)


def test_fully_masked_row_pools_to_zero_not_nan() -> None:
    """A batch-filler row yields zeros rather than dividing by zero."""
    hidden = np.full((1, 16, 8), POISON, dtype=np.float32)
    pooled = masked_mean_pool(hidden, np.zeros((1, 16), dtype=np.int64))

    assert np.all(np.isfinite(pooled)), "a fully masked row must not produce NaN or inf"
    np.testing.assert_allclose(pooled, np.zeros((1, 8), dtype=np.float32))


# -----------------------------------------------------------------------------
# Error handling
# -----------------------------------------------------------------------------


def test_masked_mean_pool_rejects_wrong_rank() -> None:
    """A 2-D hidden state is a programming error, not something to coerce."""
    with pytest.raises(ValueError, match="last_hidden_state must be"):
        masked_mean_pool(np.zeros((4, 8), dtype=np.float32), np.ones((4, 8), dtype=np.int64))


def test_masked_mean_pool_rejects_wrong_mask_rank() -> None:
    with pytest.raises(ValueError, match="attention_mask must be"):
        masked_mean_pool(
            np.zeros((1, 8, 4), dtype=np.float32), np.ones((1, 8, 1), dtype=np.int64)
        )


def test_masked_mean_pool_rejects_mismatched_shapes() -> None:
    """A mask that disagrees with the hidden state would misalign the mean."""
    with pytest.raises(ValueError, match="disagree"):
        masked_mean_pool(
            np.zeros((2, 8, 4), dtype=np.float32), np.ones((2, 16), dtype=np.int64)
        )


# -----------------------------------------------------------------------------
# Normalisation
# -----------------------------------------------------------------------------


def test_l2_normalize_gives_unit_rows() -> None:
    vectors = np.array([[3.0, 4.0], [1.0, 0.0], [0.0, -2.0]], dtype=np.float32)
    normalized = l2_normalize(vectors)

    norms = np.linalg.norm(normalized, axis=1)
    np.testing.assert_allclose(norms, np.ones(3, dtype=np.float32), rtol=0, atol=1e-6)
    # Direction is preserved.
    np.testing.assert_allclose(normalized[0], np.array([0.6, 0.8], dtype=np.float32), atol=1e-6)


def test_l2_normalize_leaves_zero_rows_finite() -> None:
    """A zero row must stay zero rather than becoming NaN."""
    normalized = l2_normalize(np.zeros((2, 4), dtype=np.float32))
    assert np.all(np.isfinite(normalized))
    np.testing.assert_allclose(normalized, np.zeros((2, 4), dtype=np.float32))


def test_l2_normalize_rejects_wrong_rank() -> None:
    with pytest.raises(ValueError, match="vectors must be"):
        l2_normalize(np.zeros((2, 3, 4), dtype=np.float32))


def test_normalization_preserves_padding_invariance() -> None:
    """Normalising after pooling keeps the invariance property intact."""
    rng = np.random.default_rng(seed=7)
    tokens = [rng.normal(size=16).astype(np.float32) for _ in range(3)]

    short = l2_normalize(masked_mean_pool(build_hidden(tokens, 3), build_mask(3, 3)))
    long = l2_normalize(masked_mean_pool(build_hidden(tokens, 512), build_mask(3, 512)))

    assert cosine_similarity(short[0], long[0]) == pytest.approx(1.0, abs=1e-9)


# -----------------------------------------------------------------------------
# Chunk pooling
# -----------------------------------------------------------------------------


def test_pool_chunks_single_chunk_is_identity() -> None:
    vector = np.array([[1.0, 2.0, 3.0]], dtype=np.float32)
    pooled = pool_chunks(vector, np.array([42.0], dtype=np.float32))
    np.testing.assert_allclose(pooled, vector[0])


def test_pool_chunks_weights_by_token_count() -> None:
    """A long chunk must count for more than a short trailing remainder."""
    chunks = np.array([[1.0, 0.0], [0.0, 1.0]], dtype=np.float32)
    # 510 real tokens versus 10: the result should sit close to the first chunk.
    pooled = pool_chunks(chunks, np.array([510.0, 10.0], dtype=np.float32))

    expected = np.array([510.0 / 520.0, 10.0 / 520.0], dtype=np.float32)
    np.testing.assert_allclose(pooled, expected, rtol=0, atol=1e-6)
    assert pooled[0] > pooled[1]


def test_pool_chunks_equal_weights_is_plain_mean() -> None:
    chunks = np.array([[2.0, 4.0], [4.0, 8.0]], dtype=np.float32)
    pooled = pool_chunks(chunks, np.array([5.0, 5.0], dtype=np.float32))
    np.testing.assert_allclose(pooled, np.array([3.0, 6.0], dtype=np.float32), atol=1e-6)


def test_pool_chunks_rejects_empty() -> None:
    with pytest.raises(ValueError, match="must not be empty"):
        pool_chunks(np.zeros((0, 4), dtype=np.float32), np.zeros((0,), dtype=np.float32))


def test_pool_chunks_rejects_mismatched_weights() -> None:
    with pytest.raises(ValueError, match="weights must be"):
        pool_chunks(np.zeros((3, 4), dtype=np.float32), np.ones((2,), dtype=np.float32))


def test_pool_chunks_rejects_wrong_rank() -> None:
    with pytest.raises(ValueError, match="chunk_vectors must be"):
        pool_chunks(np.zeros((4,), dtype=np.float32), np.ones((1,), dtype=np.float32))


def test_pool_chunks_rejects_zero_weight_sum() -> None:
    with pytest.raises(ValueError, match="positive"):
        pool_chunks(
            np.ones((2, 3), dtype=np.float32), np.zeros((2,), dtype=np.float32)
        )


# -----------------------------------------------------------------------------
# Cosine helper
# -----------------------------------------------------------------------------


def test_cosine_similarity_identical_and_orthogonal() -> None:
    a = np.array([1.0, 2.0, 3.0], dtype=np.float32)
    assert cosine_similarity(a, a) == pytest.approx(1.0, abs=1e-6)

    x = np.array([1.0, 0.0], dtype=np.float32)
    y = np.array([0.0, 1.0], dtype=np.float32)
    assert cosine_similarity(x, y) == pytest.approx(0.0, abs=1e-6)

    assert cosine_similarity(x, -x) == pytest.approx(-1.0, abs=1e-6)


def test_cosine_similarity_zero_vector_is_zero_not_nan() -> None:
    a = np.zeros(4, dtype=np.float32)
    b = np.ones(4, dtype=np.float32)
    assert cosine_similarity(a, b) == 0.0


def test_cosine_similarity_rejects_bad_input() -> None:
    with pytest.raises(ValueError, match="two 1-D vectors"):
        cosine_similarity(np.zeros((2, 2), dtype=np.float32), np.zeros((2, 2), dtype=np.float32))

    with pytest.raises(ValueError, match="disagree on width"):
        cosine_similarity(np.zeros(3, dtype=np.float32), np.zeros(4, dtype=np.float32))
