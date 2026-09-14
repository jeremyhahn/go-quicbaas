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
"""Builds a tiny real OpenVINO IR for exercising :class:`OpenVINOEngine`.

This is not a stand-in for all-MiniLM-L6-v2. It is a genuine OpenVINO model with
the same *interface shape* as the exported sentence transformer -- dynamic
``[?, ?]`` integer inputs named ``input_ids`` and ``attention_mask``, and a
``(batch, length, hidden)`` float output. That is exactly what the engine's
plumbing depends on: reshaping to a static shape, compiling, feeding by input
name, and picking the 3-D output back out.

Being dynamic on disk matters. A dynamic model is what fails to compile for the
NPU with ``ZE_RESULT_ERROR_INVALID_ARGUMENT``, so a fixture that was already
static would not prove the reshape step does anything.
"""

from __future__ import annotations

import numpy as np
import openvino as ov
from openvino import opset13 as ops

VOCAB_SIZE = 512
HIDDEN_SIZE = 8


def build_tiny_ir(destination: str, *, hidden_size: int = HIDDEN_SIZE) -> str:
    """Write a dynamic-shaped embedding-style IR and return its .xml path.

    The graph is a token-embedding lookup masked by ``attention_mask``, which
    gives every position a value determined solely by its token -- the same
    property a correctly masked transformer has, and the one masked mean pooling
    relies on.
    """
    input_ids = ops.parameter([-1, -1], dtype=np.int64, name="input_ids")
    input_ids.output(0).get_tensor().set_names({"input_ids"})

    attention_mask = ops.parameter([-1, -1], dtype=np.int64, name="attention_mask")
    attention_mask.output(0).get_tensor().set_names({"attention_mask"})

    # A deterministic embedding table, so results are reproducible across runs.
    rng = np.random.default_rng(seed=20260912)
    table = ops.constant(
        rng.standard_normal((VOCAB_SIZE, hidden_size)).astype(np.float32)
    )

    # (batch, length) -> (batch, length, hidden)
    hidden = ops.gather(table, input_ids, ops.constant(0, dtype=np.int64))

    # Zero the padded positions, mirroring a masked model.
    mask = ops.unsqueeze(
        ops.convert(attention_mask, destination_type="f32"),
        ops.constant([-1], dtype=np.int64),
    )
    masked = ops.multiply(hidden, mask)

    result = ops.result(masked)
    result.output(0).get_tensor().set_names({"last_hidden_state"})

    model = ov.Model([result], [input_ids, attention_mask], "tiny_embedder")
    ov.save_model(model, destination, compress_to_fp16=False)
    return destination
