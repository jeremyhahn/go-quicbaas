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
"""OpenVINO-backed embedding sidecar for go-quicbaas.

The sidecar exists because the Intel NPU requires *static* input shapes: a
dynamic ``[?, ?]`` model fails to compile with
``ZE_RESULT_ERROR_INVALID_ARGUMENT``, while the same model reshaped to
``[8, 128]`` compiles immediately. An embedding API that only accepted a fixed
shape would be useless, so this service accepts arbitrary text and arbitrary
batch sizes and hides the fixed shapes behind a bucket set.

That hiding is exact rather than approximate. With masked mean pooling, padding
a sequence to 32, 64, 128 or 256 tokens yields **cosine 1.000000** against every
other padding of the same text, because the padded positions are masked out of
the mean and batch rows do not attend to one another. Bucketing therefore costs
no accuracy at all -- see ``pooling.py`` and the invariance tests.

Layering:

* ``bucket.py``  -- shape selection, padding and chunk planning (pure)
* ``pooling.py`` -- masked mean pooling and normalisation (pure numpy)
* ``tokenizer.py`` -- the Tokenizer protocol and its transformers implementation
* ``engine.py``  -- the Engine protocol and the only OpenVINO import
* ``service.py`` -- orchestration tying the above together
* ``app.py``     -- the HTTP surface

Only ``engine.py`` touches a vendor runtime, so everything above it is testable
without an NPU present.
"""

from npu_embedding.errors import (
    DeviceUnavailableError,
    EmptyTextError,
    InferenceError,
    InsecureBindError,
    InvalidRequestError,
    ModelLoadError,
    ModelMismatchError,
    OpenVINOUnavailableError,
    ShapeCompileError,
    SidecarError,
    TokenizerLoadError,
)

__all__ = [
    "DeviceUnavailableError",
    "EmptyTextError",
    "InferenceError",
    "InsecureBindError",
    "InvalidRequestError",
    "ModelLoadError",
    "ModelMismatchError",
    "OpenVINOUnavailableError",
    "ShapeCompileError",
    "SidecarError",
    "TokenizerLoadError",
]
