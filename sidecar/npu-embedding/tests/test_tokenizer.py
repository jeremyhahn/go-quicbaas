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
"""Tests for the tokenizer layer.

The transformers-backed implementation needs its model directory present, so what
is covered here is the failure contract -- a bad directory or a missing
dependency must surface as TokenizerLoadError with a code, not as whatever
exception the library happened to raise.

The protocol itself is exercised throughout the service tests by the
deterministic WordTokenizer.
"""

from __future__ import annotations

import pytest

from npu_embedding.errors import TokenizerLoadError
from npu_embedding.tokenizer import HuggingFaceTokenizer, Tokenizer

from conftest import WordTokenizer


def test_missing_model_directory_raises_a_typed_error() -> None:
    """Whether transformers is installed or not, the error is typed."""
    with pytest.raises(TokenizerLoadError) as excinfo:
        HuggingFaceTokenizer("/nonexistent/model/directory")

    assert excinfo.value.code == "tokenizer_load_failed"
    assert excinfo.value.status == 503


def test_word_tokenizer_satisfies_the_protocol() -> None:
    """The test tokenizer must really implement the protocol it stands in for."""
    assert isinstance(WordTokenizer(), Tokenizer)


def test_word_tokenizer_is_deterministic() -> None:
    a = WordTokenizer().encode_body("the quick brown fox")
    b = WordTokenizer().encode_body("the quick brown fox")
    assert a == b
    assert len(a) == 4


def test_word_tokenizer_distinguishes_different_words() -> None:
    ids = WordTokenizer().encode_body("alpha beta")
    assert ids[0] != ids[1]


def test_word_tokenizer_special_tokens_are_distinct() -> None:
    tok = WordTokenizer()
    assert len({tok.cls_id, tok.sep_id, tok.pad_id}) == 3
    # Special ids must not collide with body ids.
    body = tok.encode_body("some words here")
    assert tok.cls_id not in body
    assert tok.sep_id not in body
    assert tok.pad_id not in body


def test_word_tokenizer_empty_text_is_empty_body() -> None:
    assert WordTokenizer().encode_body("   ") == []
