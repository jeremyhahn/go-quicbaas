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
"""Builds a tiny real WordPiece tokenizer for exercising :class:`HuggingFaceTokenizer`.

This is not a stand-in for the all-MiniLM-L6-v2 vocabulary. It is a genuine
``transformers`` fast tokenizer, saved to disk the way an optimum export saves
one, so ``AutoTokenizer.from_pretrained`` takes exactly the path it takes in
production: BERT normalisation, BERT pre-tokenisation and greedy WordPiece
matching. Only the vocabulary is small, which keeps the tests fast and offline.

Every id stays below the tiny IR's ``VOCAB_SIZE``, so the same directory can
hold both the tokenizer and the model and serve as a complete model directory.
"""

from __future__ import annotations

from tokenizers import Tokenizer, models, normalizers, pre_tokenizers
from transformers import PreTrainedTokenizerFast

SPECIAL_TOKENS = ("[PAD]", "[UNK]", "[CLS]", "[SEP]", "[MASK]")
WORDS = (
    "the",
    "quick",
    "brown",
    "fox",
    "jumps",
    "over",
    "lazy",
    "dog",
    "em",
    "##bed",
    "##ding",
    "##s",
)
VOCAB = {token: index for index, token in enumerate(SPECIAL_TOKENS + WORDS)}

PAD_ID = VOCAB["[PAD]"]
UNK_ID = VOCAB["[UNK]"]
CLS_ID = VOCAB["[CLS]"]
SEP_ID = VOCAB["[SEP]"]

# Deliberately small, so a modest text already exceeds the model's ceiling.
MODEL_MAX_LENGTH = 16


def _wordpiece() -> Tokenizer:
    """Return a BERT-style WordPiece pipeline over :data:`VOCAB`."""
    tokenizer = Tokenizer(models.WordPiece(VOCAB, unk_token="[UNK]"))
    tokenizer.normalizer = normalizers.BertNormalizer(lowercase=True)
    tokenizer.pre_tokenizer = pre_tokenizers.BertPreTokenizer()
    return tokenizer


def build_tiny_tokenizer(
    destination: str,
    *,
    with_cls_sep: bool = True,
    with_pad: bool = True,
) -> str:
    """Save the tiny tokenizer into ``destination`` and return the directory.

    ``with_cls_sep`` and ``with_pad`` drop the corresponding special-token
    declarations, producing the incomplete tokenizers a bad export can leave
    behind.
    """
    special: dict[str, str] = {"unk_token": "[UNK]"}
    if with_cls_sep:
        special["cls_token"] = "[CLS]"
        special["sep_token"] = "[SEP]"
    if with_pad:
        special["pad_token"] = "[PAD]"

    PreTrainedTokenizerFast(
        tokenizer_object=_wordpiece(),
        model_max_length=MODEL_MAX_LENGTH,
        **special,
    ).save_pretrained(destination)
    return destination
