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
"""Static-shape selection, padding and chunk planning.

The NPU will only run a model whose input shapes are fully static, so the
sidecar keeps a small set of precompiled ``(batch, length)`` shapes and routes
every request into them. This module decides which shape a given piece of work
runs under and builds the padded tensors. It is pure: no OpenVINO, no model, no
I/O, which is why the properties below can be tested directly.

Two measured facts shape the design:

* Compilation is the expensive part, not selection. ``[1, any L]`` compiles in
  about 0.05s, ``[8, 256]`` in 2.9s and ``[8, 512]`` in 4.7s. The default bucket
  set of batch {1, 8} x length {128, 256, 512} precompiles in 7.8s, once, at
  startup. A bucket miss is therefore survivable but worth avoiding.
* Padding is exactly invariant under masked mean pooling, so widening a
  sequence to the next bucket cannot change its embedding. Bucketing is free in
  accuracy terms, not a tradeoff. See ``pooling.py``.

512 is the *model's* ceiling (all-MiniLM-L6-v2 has
``max_position_embeddings: 512``; a length of 1024 fails inside the model's own
shape broadcast, not in the driver). Longer text must be chunked on any device,
CPU and GPU included.
"""

from __future__ import annotations

from dataclasses import dataclass

import numpy as np

# The default bucket set. Batch 1 serves interactive single-text queries; batch
# 8 serves ingest. Lengths follow the model's 512-token ceiling.
DEFAULT_BATCH_BUCKETS: tuple[int, ...] = (1, 8)
DEFAULT_LENGTH_BUCKETS: tuple[int, ...] = (128, 256, 512)

# The model's positional limit, not the device's.
DEFAULT_MAX_LENGTH = 512

# Tokens of overlap between consecutive chunks of an over-long text. Zero keeps
# the chunk mean unbiased: with overlap, tokens in the shared window would be
# counted twice and pull the pooled vector toward the seam.
DEFAULT_CHUNK_OVERLAP = 0


@dataclass(frozen=True, slots=True)
class Shape:
    """A static ``(batch, length)`` shape the engine can compile."""

    batch: int
    length: int

    def as_tuple(self) -> tuple[int, int]:
        """Return the shape as a plain tuple, for wire and cache keys."""
        return (self.batch, self.length)


@dataclass(frozen=True, slots=True)
class Chunk:
    """One tokenised window of one input text.

    A text short enough to fit the model produces exactly one chunk. An
    over-long text produces several, whose embeddings are pooled back together
    by weight.
    """

    # text_index is the position of the source text in the caller's batch, so
    # results can be reassembled in request order after length-sorted grouping.
    text_index: int

    # token_ids already includes whatever special tokens the model expects.
    token_ids: tuple[int, ...]

    @property
    def length(self) -> int:
        """Return the real token count, excluding any later padding."""
        return len(self.token_ids)


@dataclass(frozen=True, slots=True)
class Group:
    """A set of chunks executed together under one static shape."""

    shape: Shape

    # Indices into the flat chunk list this group was planned from.
    chunk_indices: tuple[int, ...]


def select_length(
    needed: int,
    length_buckets: tuple[int, ...] = DEFAULT_LENGTH_BUCKETS,
    max_length: int = DEFAULT_MAX_LENGTH,
) -> int:
    """Return the sequence length to run a ``needed``-token group under.

    The smallest bucket that fits wins. When nothing fits -- which means the
    caller configured buckets below the model ceiling -- the exact length is
    returned so the engine can lazily compile it rather than truncate real
    tokens. ``needed`` is clamped to ``max_length`` because beyond that the
    model itself cannot run.
    """
    if needed <= 0:
        raise ValueError("needed must be positive")

    capped = min(needed, max_length)
    for bucket in sorted(length_buckets):
        if bucket >= capped:
            return bucket
    return capped


def select_batch(
    count: int,
    batch_buckets: tuple[int, ...] = DEFAULT_BATCH_BUCKETS,
) -> int:
    """Return the batch size to run ``count`` chunks under.

    The smallest bucket that fits wins, so a single text runs at batch 1 rather
    than padding seven empty rows. When the count exceeds every bucket the
    largest is returned and the caller is expected to have split the work
    already; :func:`plan_groups` guarantees that.
    """
    if count <= 0:
        raise ValueError("count must be positive")

    for bucket in sorted(batch_buckets):
        if bucket >= count:
            return bucket
    return max(batch_buckets)


def chunk_tokens(
    body_ids: list[int] | tuple[int, ...],
    *,
    max_length: int = DEFAULT_MAX_LENGTH,
    cls_id: int,
    sep_id: int,
    overlap: int = DEFAULT_CHUNK_OVERLAP,
) -> list[tuple[int, ...]]:
    """Split a text's body tokens into windows that fit the model.

    ``body_ids`` carries the text's tokens *without* special tokens; each
    returned window is wrapped in ``cls_id``/``sep_id``, so the window itself is
    two tokens shorter than ``max_length``. A text that already fits yields a
    single window. An empty body still yields one window, so that an input which
    tokenises to nothing produces a vector rather than vanishing from the batch
    and misaligning results.
    """
    if max_length < 3:
        raise ValueError("max_length must leave room for both special tokens")
    if overlap < 0:
        raise ValueError("overlap must not be negative")

    window = max_length - 2
    if overlap >= window:
        raise ValueError("overlap must be smaller than the usable window")

    body = tuple(body_ids)
    if len(body) <= window:
        return [(cls_id, *body, sep_id)]

    stride = window - overlap
    out: list[tuple[int, ...]] = []
    for start in range(0, len(body), stride):
        # range() never yields a start at or past len(body), so every slice
        # here holds at least one token.
        piece = body[start : start + window]
        out.append((cls_id, *piece, sep_id))
        if start + window >= len(body):
            break
    return out


def plan_groups(
    chunks: list[Chunk],
    *,
    batch_buckets: tuple[int, ...] = DEFAULT_BATCH_BUCKETS,
    length_buckets: tuple[int, ...] = DEFAULT_LENGTH_BUCKETS,
    max_length: int = DEFAULT_MAX_LENGTH,
) -> list[Group]:
    """Group chunks into executable batches, each under one static shape.

    Chunks are assigned to their length bucket *first* and only then split into
    batches, so a short query is never padded to 512 merely because a long
    document arrived in the same request. Sorting by length alone is not enough:
    with a fixed batch stride the group boundary can still straddle a jump from
    9 tokens to 470, dragging the short sequences up with it.

    Regrouping is safe because every chunk carries its ``text_index`` and batch
    rows are independent under self-attention, so results are reassembled by
    index afterwards.
    """
    if not chunks:
        return []

    max_batch = max(batch_buckets)

    by_bucket: dict[int, list[int]] = {}
    for index, chunk in enumerate(chunks):
        length = select_length(chunk.length, length_buckets, max_length)
        by_bucket.setdefault(length, []).append(index)

    groups: list[Group] = []
    for length in sorted(by_bucket):
        members = by_bucket[length]
        for start in range(0, len(members), max_batch):
            indices = tuple(members[start : start + max_batch])
            groups.append(
                Group(
                    shape=Shape(
                        batch=select_batch(len(indices), batch_buckets),
                        length=length,
                    ),
                    chunk_indices=indices,
                )
            )
    return groups


def build_inputs(
    chunks: list[Chunk],
    group: Group,
    *,
    pad_id: int,
) -> tuple[np.ndarray, np.ndarray]:
    """Build the padded ``(input_ids, attention_mask)`` pair for one group.

    Rows beyond the group's real chunk count are left fully masked. Those
    padding rows cannot affect the real ones: self-attention operates within a
    sequence, never across batch rows, which is what makes batch padding as
    exactly invariant as sequence padding.

    Both tensors are ``int64`` because that is what the optimum-exported IR
    declares for its integer inputs.
    """
    shape = group.shape
    input_ids = np.full((shape.batch, shape.length), pad_id, dtype=np.int64)
    attention_mask = np.zeros((shape.batch, shape.length), dtype=np.int64)

    if len(group.chunk_indices) > shape.batch:
        raise ValueError(
            f"group holds {len(group.chunk_indices)} chunks, above its batch of {shape.batch}"
        )

    for row, chunk_index in enumerate(group.chunk_indices):
        ids = chunks[chunk_index].token_ids[: shape.length]
        input_ids[row, : len(ids)] = ids
        attention_mask[row, : len(ids)] = 1

    return input_ids, attention_mask


def precompile_shapes(
    batch_buckets: tuple[int, ...] = DEFAULT_BATCH_BUCKETS,
    length_buckets: tuple[int, ...] = DEFAULT_LENGTH_BUCKETS,
) -> list[Shape]:
    """Return the full bucket cross-product, for warming the shape cache.

    Compiling all of the default set costs about 7.8s once at startup, which is
    paid to keep the first real request off the compile path.
    """
    return [
        Shape(batch=b, length=length)
        for b in sorted(batch_buckets)
        for length in sorted(length_buckets)
    ]
