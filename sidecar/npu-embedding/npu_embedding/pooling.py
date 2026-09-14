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
"""Masked mean pooling and vector normalisation.

This module is where the sidecar earns the right to bucket shapes at all.

``all-MiniLM-L6-v2`` is a sentence-transformer whose sentence embedding is the
*mean of its token embeddings*. Taking that mean over only the unmasked
positions makes the result independent of how much padding follows:

    mean_{i in real}(h_i)

contains no term for a padded position, so appending padding cannot change it.
Measured on the real model, the same text padded to 32, 64, 128 and 256 tokens
produced **cosine 1.000000** against every other padding. That is why routing
arbitrary-length text into a handful of fixed shapes costs nothing, and it is
asserted as a property test rather than left as a comment -- if a future change
starts including padded positions in the mean, the bucket set silently becomes
a source of drift, and that must fail loudly instead.

Pooling over *chunks* of an over-long text is the same operation one level up: a
token-count-weighted mean of the chunk vectors, so that a 500-token chunk counts
for more than the 12-token remainder that follows it.
"""

from __future__ import annotations

import numpy as np

# Guards division when a row is entirely masked, which happens for the padding
# rows that fill a batch bucket.
_EPSILON = 1e-9


def masked_mean_pool(
    last_hidden_state: np.ndarray,
    attention_mask: np.ndarray,
) -> np.ndarray:
    """Average token vectors over unmasked positions only.

    ``last_hidden_state`` is ``(batch, length, hidden)`` and ``attention_mask``
    is ``(batch, length)`` holding 1 for real tokens and 0 for padding. The
    result is ``(batch, hidden)``.

    Because masked positions contribute neither to the numerator nor to the
    denominator, the result for a given sequence is identical at every padded
    length. Fully masked rows -- the filler rows of a batch bucket -- pool to
    zero rather than dividing by zero.
    """
    if last_hidden_state.ndim != 3:
        raise ValueError(
            f"last_hidden_state must be (batch, length, hidden), got shape {last_hidden_state.shape}"
        )
    if attention_mask.ndim != 2:
        raise ValueError(
            f"attention_mask must be (batch, length), got shape {attention_mask.shape}"
        )
    if last_hidden_state.shape[:2] != attention_mask.shape:
        raise ValueError(
            "last_hidden_state and attention_mask disagree on (batch, length): "
            f"{last_hidden_state.shape[:2]} vs {attention_mask.shape}"
        )

    # float32 throughout: the IR emits float32 and the vector store consumes it.
    mask = attention_mask.astype(np.float32)[:, :, None]
    hidden = last_hidden_state.astype(np.float32, copy=False)

    summed = (hidden * mask).sum(axis=1)
    counts = mask.sum(axis=1)
    return summed / np.maximum(counts, _EPSILON)


def l2_normalize(vectors: np.ndarray) -> np.ndarray:
    """Scale each row to unit length.

    Cosine similarity over normalised vectors reduces to a dot product, which is
    what the vector index wants. Zero rows are left as zero rather than being
    turned into NaN.
    """
    if vectors.ndim != 2:
        raise ValueError(f"vectors must be (batch, hidden), got shape {vectors.shape}")

    norms = np.linalg.norm(vectors, axis=1, keepdims=True)
    return vectors / np.maximum(norms, _EPSILON)


def pool_chunks(
    chunk_vectors: np.ndarray,
    weights: np.ndarray,
) -> np.ndarray:
    """Combine the chunk vectors of one text into a single vector.

    ``weights`` is the real token count of each chunk, so a long chunk counts
    proportionally more than a short trailing remainder. A single-chunk text
    returns that chunk unchanged.
    """
    if chunk_vectors.ndim != 2:
        raise ValueError(
            f"chunk_vectors must be (chunks, hidden), got shape {chunk_vectors.shape}"
        )
    if chunk_vectors.shape[0] == 0:
        raise ValueError("chunk_vectors must not be empty")
    if weights.shape != (chunk_vectors.shape[0],):
        raise ValueError(
            f"weights must be ({chunk_vectors.shape[0]},), got shape {weights.shape}"
        )

    if chunk_vectors.shape[0] == 1:
        return chunk_vectors[0].astype(np.float32, copy=False)

    w = weights.astype(np.float32)[:, None]
    total = float(w.sum())
    if total <= 0.0:
        raise ValueError("chunk weights must sum to a positive value")

    return (chunk_vectors.astype(np.float32, copy=False) * w).sum(axis=0) / total


def cosine_similarity(a: np.ndarray, b: np.ndarray) -> float:
    """Return the cosine similarity of two 1-D vectors.

    Used by the invariance tests and by the sidecar's own self-check.
    """
    if a.ndim != 1 or b.ndim != 1:
        raise ValueError("cosine_similarity takes two 1-D vectors")
    if a.shape != b.shape:
        raise ValueError(f"vectors disagree on width: {a.shape} vs {b.shape}")

    denominator = float(np.linalg.norm(a)) * float(np.linalg.norm(b))
    if denominator <= _EPSILON:
        return 0.0
    return float(np.dot(a, b) / denominator)
