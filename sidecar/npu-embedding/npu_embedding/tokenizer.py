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
"""Tokenisation, behind a protocol so the rest of the sidecar stays testable.

The sidecar does its own padding and chunking, so it needs *unpadded,
untruncated* body tokens and the model's special-token ids. It deliberately does
not ask the tokenizer to pad or truncate: padding is chosen per bucket in
``bucket.py``, and truncating here would silently discard the tail of a long
document instead of chunking it.
"""

from __future__ import annotations

from typing import Protocol, runtime_checkable

from npu_embedding.errors import TokenizerLoadError


@runtime_checkable
class Tokenizer(Protocol):
    """Turns text into body token ids and names the model's special tokens."""

    def encode_body(self, text: str) -> list[int]:
        """Return token ids for ``text`` *without* special tokens."""
        ...

    @property
    def cls_id(self) -> int:
        """Return the id of the sequence-start token."""
        ...

    @property
    def sep_id(self) -> int:
        """Return the id of the sequence-end token."""
        ...

    @property
    def pad_id(self) -> int:
        """Return the id used to fill padded positions."""
        ...


class HuggingFaceTokenizer:
    """A :class:`Tokenizer` backed by a ``transformers`` fast tokenizer.

    The tokenizer is loaded from the same directory as the exported IR, so its
    vocabulary is guaranteed to match the model's embedding table.
    """

    def __init__(self, model_dir: str) -> None:
        try:
            # Imported lazily: the pure modules and their tests must not require
            # transformers to be installed.
            from transformers import AutoTokenizer
        except ImportError as exc:
            raise TokenizerLoadError(
                f"transformers is required to load a tokenizer: {exc}"
            ) from exc

        try:
            self._inner = AutoTokenizer.from_pretrained(model_dir)
        except (OSError, ValueError) as exc:
            raise TokenizerLoadError(
                f"could not load a tokenizer from {model_dir}: {exc}"
            ) from exc

        cls_id = self._inner.cls_token_id
        sep_id = self._inner.sep_token_id
        pad_id = self._inner.pad_token_id

        if cls_id is None or sep_id is None:
            raise TokenizerLoadError(
                f"tokenizer at {model_dir} does not define both CLS and SEP tokens"
            )

        self._cls_id = int(cls_id)
        self._sep_id = int(sep_id)
        # A model without an explicit pad token pads with zeros; those positions
        # are masked out regardless, so the id only has to be in-vocabulary.
        self._pad_id = int(pad_id) if pad_id is not None else 0

    def encode_body(self, text: str) -> list[int]:
        """Return token ids for ``text``, excluding special tokens."""
        return list(
            self._inner.encode(text, add_special_tokens=False, truncation=False)
        )

    @property
    def cls_id(self) -> int:
        """Return the id of the sequence-start token."""
        return self._cls_id

    @property
    def sep_id(self) -> int:
        """Return the id of the sequence-end token."""
        return self._sep_id

    @property
    def pad_id(self) -> int:
        """Return the id used to fill padded positions."""
        return self._pad_id
