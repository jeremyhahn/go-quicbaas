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
"""Tests that load a real ``transformers`` tokenizer from disk.

The unit tier only ever sees tokenizer doubles, because ``transformers`` is in
the ``device`` extra. This tier runs :class:`HuggingFaceTokenizer` against a
genuine fast tokenizer saved by :mod:`tiny_tokenizer`, so the load path, the
encode call and the special-token lookup are the ones production runs.

It also pins the contract the rest of the sidecar relies on: the tokenizer
returns unpadded, untruncated body tokens, and the bucket layer -- not the
tokenizer -- windows them at the model's ceiling and pads them under a mask.
"""

from __future__ import annotations

import pytest

from npu_embedding import bucket
from npu_embedding.bucket import Chunk
from npu_embedding.errors import TokenizerLoadError
from npu_embedding.tokenizer import HuggingFaceTokenizer, Tokenizer

from tiny_tokenizer import (
    CLS_ID,
    MODEL_MAX_LENGTH,
    PAD_ID,
    SEP_ID,
    UNK_ID,
    VOCAB,
    build_tiny_tokenizer,
)


@pytest.fixture(scope="module")
def tokenizer_dir(tmp_path_factory: pytest.TempPathFactory) -> str:
    """Write the tiny tokenizer once for the module."""
    return build_tiny_tokenizer(str(tmp_path_factory.mktemp("tokenizer")))


@pytest.fixture
def tokenizer(tokenizer_dir: str) -> HuggingFaceTokenizer:
    return HuggingFaceTokenizer(tokenizer_dir)


# -----------------------------------------------------------------------------
# Loading
# -----------------------------------------------------------------------------


def test_load_produces_a_tokenizer(tokenizer: HuggingFaceTokenizer) -> None:
    assert isinstance(tokenizer, Tokenizer)


def test_missing_directory_raises_a_typed_error(tmp_path) -> None:
    missing = tmp_path / "no-such-model"

    with pytest.raises(TokenizerLoadError) as excinfo:
        HuggingFaceTokenizer(str(missing))

    assert excinfo.value.code
    assert str(missing) in excinfo.value.message


def test_malformed_directory_raises_a_typed_error(tmp_path) -> None:
    """A truncated export must fail at startup, not on the first request."""
    (tmp_path / "tokenizer_config.json").write_text("{ this is not json")
    (tmp_path / "tokenizer.json").write_text("{ nor is this")

    with pytest.raises(TokenizerLoadError) as excinfo:
        HuggingFaceTokenizer(str(tmp_path))

    assert str(tmp_path) in excinfo.value.message


def test_a_tokenizer_without_cls_and_sep_is_refused(tmp_path) -> None:
    """Every chunk is wrapped in CLS/SEP, so a tokenizer lacking them is unusable."""
    build_tiny_tokenizer(str(tmp_path), with_cls_sep=False)

    with pytest.raises(TokenizerLoadError, match="CLS and SEP"):
        HuggingFaceTokenizer(str(tmp_path))


# -----------------------------------------------------------------------------
# Accessors
# -----------------------------------------------------------------------------


def test_special_token_ids_come_from_the_vocabulary(
    tokenizer: HuggingFaceTokenizer,
) -> None:
    assert tokenizer.cls_id == CLS_ID
    assert tokenizer.sep_id == SEP_ID
    assert tokenizer.pad_id == PAD_ID


def test_pad_id_defaults_to_zero_without_a_pad_token(tmp_path) -> None:
    """Padded positions are masked, so any in-vocabulary id will do."""
    build_tiny_tokenizer(str(tmp_path), with_pad=False)

    tokenizer = HuggingFaceTokenizer(str(tmp_path))

    assert tokenizer.pad_id == 0
    assert tokenizer.cls_id == CLS_ID
    assert tokenizer.sep_id == SEP_ID


# -----------------------------------------------------------------------------
# Encoding
# -----------------------------------------------------------------------------


def test_known_string_encodes_to_its_vocabulary_ids(
    tokenizer: HuggingFaceTokenizer,
) -> None:
    ids = tokenizer.encode_body("The quick brown fox")

    assert ids == [VOCAB["the"], VOCAB["quick"], VOCAB["brown"], VOCAB["fox"]]


def test_encoding_excludes_special_tokens(tokenizer: HuggingFaceTokenizer) -> None:
    """The bucket layer adds CLS/SEP per window; the tokenizer must not."""
    ids = tokenizer.encode_body("lazy dog")

    assert CLS_ID not in ids
    assert SEP_ID not in ids


def test_words_split_into_wordpieces(tokenizer: HuggingFaceTokenizer) -> None:
    assert tokenizer.encode_body("embeddings") == [
        VOCAB["em"],
        VOCAB["##bed"],
        VOCAB["##ding"],
        VOCAB["##s"],
    ]


def test_out_of_vocabulary_words_map_to_unk(tokenizer: HuggingFaceTokenizer) -> None:
    assert tokenizer.encode_body("zebra") == [UNK_ID]


def test_text_beyond_the_model_maximum_is_not_truncated(
    tokenizer: HuggingFaceTokenizer,
) -> None:
    """Truncating here would silently drop the tail of a long document."""
    text = " ".join(["the quick brown fox"] * 10)

    ids = tokenizer.encode_body(text)

    assert len(ids) == 40
    assert len(ids) > MODEL_MAX_LENGTH


def test_long_text_is_windowed_at_the_model_maximum(
    tokenizer: HuggingFaceTokenizer,
) -> None:
    """The model's ceiling is enforced by chunking, and no token is lost."""
    body = tokenizer.encode_body(" ".join(["jumps over the lazy dog"] * 6))

    windows = bucket.chunk_tokens(
        body,
        max_length=MODEL_MAX_LENGTH,
        cls_id=tokenizer.cls_id,
        sep_id=tokenizer.sep_id,
    )

    assert len(windows) > 1
    for window in windows:
        assert len(window) <= MODEL_MAX_LENGTH
        assert window[0] == CLS_ID
        assert window[-1] == SEP_ID
    rejoined = [token for window in windows for token in window[1:-1]]
    assert rejoined == body


def test_padded_input_is_masked_exactly_over_real_tokens(
    tokenizer: HuggingFaceTokenizer,
) -> None:
    texts = ["the quick brown fox jumps", "lazy dog"]
    chunks = [
        Chunk(
            text_index=index,
            token_ids=bucket.chunk_tokens(
                tokenizer.encode_body(text),
                max_length=MODEL_MAX_LENGTH,
                cls_id=tokenizer.cls_id,
                sep_id=tokenizer.sep_id,
            )[0],
        )
        for index, text in enumerate(texts)
    ]
    (group,) = bucket.plan_groups(
        chunks, batch_buckets=(4,), length_buckets=(MODEL_MAX_LENGTH,)
    )

    input_ids, attention_mask = bucket.build_inputs(
        chunks, group, pad_id=tokenizer.pad_id
    )

    assert input_ids.shape == attention_mask.shape == (4, MODEL_MAX_LENGTH)
    for row, chunk in enumerate(chunks):
        real = chunk.length
        assert attention_mask[row].tolist() == [1] * real + [0] * (
            MODEL_MAX_LENGTH - real
        )
        assert input_ids[row, :real].tolist() == list(chunk.token_ids)
        assert set(input_ids[row, real:].tolist()) == {PAD_ID}
    # The batch-filling rows carry no real tokens at all.
    assert not attention_mask[len(chunks) :].any()
