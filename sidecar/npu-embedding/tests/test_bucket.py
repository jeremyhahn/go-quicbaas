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
"""Tests for shape selection, padding and chunk planning."""

from __future__ import annotations

import numpy as np
import pytest

from npu_embedding.bucket import (
    DEFAULT_BATCH_BUCKETS,
    DEFAULT_LENGTH_BUCKETS,
    DEFAULT_MAX_LENGTH,
    Chunk,
    Group,
    Shape,
    build_inputs,
    chunk_tokens,
    plan_groups,
    precompile_shapes,
    select_batch,
    select_length,
)

CLS = 101
SEP = 102
PAD = 0


# -----------------------------------------------------------------------------
# Length selection
# -----------------------------------------------------------------------------


@pytest.mark.parametrize(
    ("needed", "expected"),
    [
        (1, 128),
        (127, 128),
        (128, 128),
        (129, 256),
        (256, 256),
        (257, 512),
        (512, 512),
    ],
)
def test_select_length_picks_smallest_fitting_bucket(needed: int, expected: int) -> None:
    assert select_length(needed) == expected


def test_select_length_clamps_to_the_model_ceiling() -> None:
    """Beyond 512 the model itself cannot run, so the request is clamped.

    512 is all-MiniLM-L6-v2's max_position_embeddings; a length of 1024 fails
    inside the model's own shape broadcast, on any device.
    """
    assert select_length(4096) == DEFAULT_MAX_LENGTH


def test_select_length_returns_exact_length_on_bucket_miss() -> None:
    """With no bucket large enough the exact length is returned for lazy compile.

    Truncating instead would silently discard real tokens.
    """
    assert select_length(200, length_buckets=(64, 128), max_length=512) == 200


def test_select_length_rejects_nonpositive() -> None:
    with pytest.raises(ValueError, match="must be positive"):
        select_length(0)


# -----------------------------------------------------------------------------
# Batch selection
# -----------------------------------------------------------------------------


def test_select_batch_uses_one_for_a_single_chunk() -> None:
    """A single text must not pad seven empty rows to reach batch 8."""
    assert select_batch(1) == 1


@pytest.mark.parametrize("count", [2, 3, 7, 8])
def test_select_batch_promotes_to_the_next_bucket(count: int) -> None:
    assert select_batch(count) == 8


def test_select_batch_saturates_at_the_largest_bucket() -> None:
    """Counts above every bucket saturate; plan_groups never emits them."""
    assert select_batch(100) == 8


def test_select_batch_rejects_nonpositive() -> None:
    with pytest.raises(ValueError, match="must be positive"):
        select_batch(0)


# -----------------------------------------------------------------------------
# Chunking
# -----------------------------------------------------------------------------


def test_chunk_tokens_short_text_is_one_window_with_specials() -> None:
    windows = chunk_tokens([5, 6, 7], max_length=128, cls_id=CLS, sep_id=SEP)
    assert windows == [(CLS, 5, 6, 7, SEP)]


def test_chunk_tokens_empty_body_still_yields_one_window() -> None:
    """A text that tokenises to nothing must still produce a vector.

    Dropping it would shift every later result by one and misalign the batch.
    """
    windows = chunk_tokens([], max_length=128, cls_id=CLS, sep_id=SEP)
    assert windows == [(CLS, SEP)]


def test_chunk_tokens_splits_over_long_text() -> None:
    """Text beyond the ceiling is split into windows that each fit."""
    body = list(range(1000, 1000 + 1200))
    windows = chunk_tokens(body, max_length=DEFAULT_MAX_LENGTH, cls_id=CLS, sep_id=SEP)

    # Window body is max_length - 2 = 510 tokens: 1200 -> 510 + 510 + 180.
    assert len(windows) == 3
    assert [len(w) for w in windows] == [512, 512, 182]

    for window in windows:
        assert len(window) <= DEFAULT_MAX_LENGTH
        assert window[0] == CLS
        assert window[-1] == SEP


def test_chunk_tokens_covers_every_token_exactly_once_without_overlap() -> None:
    """With no overlap the windows partition the body, losing nothing."""
    body = list(range(2000, 2000 + 1100))
    windows = chunk_tokens(body, max_length=DEFAULT_MAX_LENGTH, cls_id=CLS, sep_id=SEP)

    recovered: list[int] = []
    for window in windows:
        recovered.extend(window[1:-1])
    assert recovered == body


def test_chunk_tokens_boundary_exactly_at_the_window() -> None:
    """A body of exactly window length stays a single chunk."""
    body = list(range(510))
    windows = chunk_tokens(body, max_length=512, cls_id=CLS, sep_id=SEP)
    assert len(windows) == 1
    assert len(windows[0]) == 512


def test_chunk_tokens_one_past_the_window_splits() -> None:
    body = list(range(511))
    windows = chunk_tokens(body, max_length=512, cls_id=CLS, sep_id=SEP)
    assert len(windows) == 2


def test_chunk_tokens_overlap_repeats_tokens_at_the_seam() -> None:
    body = list(range(20))
    windows = chunk_tokens(body, max_length=12, cls_id=CLS, sep_id=SEP, overlap=3)

    # Window body is 10 with a stride of 7.
    assert windows[0][1:-1] == tuple(range(0, 10))
    assert windows[1][1:-1] == tuple(range(7, 17))


def test_chunk_tokens_rejects_impossible_configuration() -> None:
    with pytest.raises(ValueError, match="special tokens"):
        chunk_tokens([1, 2], max_length=2, cls_id=CLS, sep_id=SEP)

    with pytest.raises(ValueError, match="overlap must not be negative"):
        chunk_tokens([1, 2], max_length=128, cls_id=CLS, sep_id=SEP, overlap=-1)

    with pytest.raises(ValueError, match="smaller than the usable window"):
        chunk_tokens([1, 2], max_length=12, cls_id=CLS, sep_id=SEP, overlap=10)


# -----------------------------------------------------------------------------
# Group planning
# -----------------------------------------------------------------------------


def make_chunks(lengths: list[int]) -> list[Chunk]:
    """Build chunks of the given token lengths, one per source text."""
    return [
        Chunk(text_index=i, token_ids=tuple(range(length)))
        for i, length in enumerate(lengths)
    ]


def test_plan_groups_empty_input() -> None:
    assert plan_groups([]) == []


def test_plan_groups_single_chunk_uses_batch_one() -> None:
    groups = plan_groups(make_chunks([10]))
    assert len(groups) == 1
    assert groups[0].shape == Shape(batch=1, length=128)
    assert groups[0].chunk_indices == (0,)


def test_plan_groups_splits_beyond_the_largest_batch_bucket() -> None:
    """A batch larger than any bucket is split into executable groups."""
    groups = plan_groups(make_chunks([10] * 20))

    # 20 chunks at a max batch of 8: 8 + 8 + 4.
    assert [len(g.chunk_indices) for g in groups] == [8, 8, 4]
    assert all(g.shape.batch == 8 for g in groups)


def test_plan_groups_covers_every_chunk_exactly_once() -> None:
    """No chunk may be dropped or executed twice, or results misalign."""
    chunks = make_chunks([5, 300, 20, 400, 100, 7, 260, 130, 90])
    groups = plan_groups(chunks)

    seen = [i for g in groups for i in g.chunk_indices]
    assert sorted(seen) == list(range(len(chunks)))
    assert len(seen) == len(set(seen))


def test_plan_groups_batches_similar_lengths_together() -> None:
    """Short and long chunks must not share a bucket and inflate each other.

    Grouping by length is what keeps a 5-token query from being padded to 512
    because something long happened to arrive alongside it.
    """
    chunks = make_chunks([5, 500, 6, 490, 7, 480, 8, 470, 9])
    groups = plan_groups(chunks)

    # The group holding the shortest chunks should use the smallest bucket.
    shortest = min(groups, key=lambda g: g.shape.length)
    assert shortest.shape.length == 128
    lengths_in_shortest = {chunks[i].length for i in shortest.chunk_indices}
    assert max(lengths_in_shortest) <= 128


def test_plan_groups_shares_a_shape_only_within_one_bucket() -> None:
    """Chunks sharing a bucket share a group; chunks in different ones do not."""
    groups = plan_groups(make_chunks([100, 120, 127]))
    assert len(groups) == 1
    assert groups[0].shape.length == 128

    # A 100-token chunk must not be padded to 256 just because a 200-token
    # chunk arrived with it; they belong to different buckets.
    groups = plan_groups(make_chunks([100, 200]))
    assert sorted(g.shape.length for g in groups) == [128, 256]
    for group in groups:
        assert len(group.chunk_indices) == 1


def test_plan_groups_assigns_each_chunk_to_its_own_bucket() -> None:
    """Every chunk lands in the smallest bucket that fits it."""
    chunks = make_chunks([10, 130, 300, 20])
    groups = plan_groups(chunks)

    bucket_of = {
        index: group.shape.length
        for group in groups
        for index in group.chunk_indices
    }
    assert bucket_of[0] == 128
    assert bucket_of[3] == 128
    assert bucket_of[1] == 256
    assert bucket_of[2] == 512


def test_plan_groups_respects_custom_buckets() -> None:
    groups = plan_groups(
        make_chunks([10] * 5),
        batch_buckets=(2, 4),
        length_buckets=(16, 32),
    )
    assert [len(g.chunk_indices) for g in groups] == [4, 1]
    assert groups[0].shape == Shape(batch=4, length=16)
    assert groups[1].shape == Shape(batch=2, length=16)


# -----------------------------------------------------------------------------
# Padding
# -----------------------------------------------------------------------------


def test_build_inputs_pads_sequence_and_batch() -> None:
    chunks = [
        Chunk(text_index=0, token_ids=(CLS, 7, 8, SEP)),
        Chunk(text_index=1, token_ids=(CLS, 9, SEP)),
    ]
    group = Group(shape=Shape(batch=8, length=16), chunk_indices=(0, 1))

    input_ids, attention_mask = build_inputs(chunks, group, pad_id=PAD)

    assert input_ids.shape == (8, 16)
    assert attention_mask.shape == (8, 16)
    assert input_ids.dtype == np.int64
    assert attention_mask.dtype == np.int64

    # Row 0: four real tokens then padding.
    np.testing.assert_array_equal(input_ids[0, :4], np.array([CLS, 7, 8, SEP]))
    np.testing.assert_array_equal(input_ids[0, 4:], np.full(12, PAD))
    np.testing.assert_array_equal(attention_mask[0], np.array([1] * 4 + [0] * 12))

    # Row 1: three real tokens.
    np.testing.assert_array_equal(attention_mask[1], np.array([1] * 3 + [0] * 13))

    # Rows 2..7 are batch filler and fully masked.
    assert attention_mask[2:].sum() == 0


def test_build_inputs_mask_counts_match_real_tokens() -> None:
    chunks = [Chunk(text_index=0, token_ids=tuple(range(37)))]
    group = Group(shape=Shape(batch=1, length=128), chunk_indices=(0,))

    _, attention_mask = build_inputs(chunks, group, pad_id=PAD)
    assert int(attention_mask.sum()) == 37


def test_build_inputs_rejects_overfull_group() -> None:
    """More chunks than rows would silently drop work."""
    chunks = make_chunks([4, 4, 4])
    group = Group(shape=Shape(batch=2, length=16), chunk_indices=(0, 1, 2))

    with pytest.raises(ValueError, match="above its batch"):
        build_inputs(chunks, group, pad_id=PAD)


def test_build_inputs_truncates_to_the_shape_length() -> None:
    """A chunk longer than its shape is clipped rather than overflowing.

    plan_groups never produces this, so it is a defensive bound.
    """
    chunks = [Chunk(text_index=0, token_ids=tuple(range(40)))]
    group = Group(shape=Shape(batch=1, length=16), chunk_indices=(0,))

    input_ids, attention_mask = build_inputs(chunks, group, pad_id=PAD)
    assert input_ids.shape == (1, 16)
    assert int(attention_mask.sum()) == 16


# -----------------------------------------------------------------------------
# Warmup set
# -----------------------------------------------------------------------------


def test_precompile_shapes_is_the_bucket_cross_product() -> None:
    """The default set is batch {1,8} x length {128,256,512} -- six shapes.

    Compiling all six costs about 7.8s once at startup.
    """
    shapes = precompile_shapes()
    assert len(shapes) == len(DEFAULT_BATCH_BUCKETS) * len(DEFAULT_LENGTH_BUCKETS)
    assert Shape(batch=1, length=128) in shapes
    assert Shape(batch=8, length=512) in shapes


def test_shape_as_tuple_round_trips() -> None:
    assert Shape(batch=8, length=256).as_tuple() == (8, 256)


def test_chunk_length_is_its_token_count() -> None:
    assert Chunk(text_index=0, token_ids=(1, 2, 3)).length == 3
