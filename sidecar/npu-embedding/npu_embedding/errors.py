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
"""Typed errors for the NPU embedding sidecar.

Every failure mode has its own class carrying a stable machine-readable ``code``
that the Go provider surfaces as ``NPUSidecarError.Code``. No bare ``Exception``
is ever raised from this package.
"""

from __future__ import annotations


class SidecarError(Exception):
    """Base class for every sidecar error.

    ``code`` is the stable identifier crossing the wire; ``status`` is the HTTP
    status the API layer maps it to.
    """

    code = "sidecar_error"
    status = 500

    def __init__(self, message: str) -> None:
        super().__init__(message)
        self.message = message

    def payload(self) -> dict[str, dict[str, str]]:
        """Return the wire shape the Go provider decodes."""
        return {"error": {"code": self.code, "message": self.message}}


class InvalidRequestError(SidecarError):
    """A caller supplied a request the sidecar cannot honour."""

    code = "invalid_request"
    status = 400


class EmptyTextError(InvalidRequestError):
    """A submitted text was empty or whitespace-only."""

    code = "empty_text"

    def __init__(self, index: int) -> None:
        super().__init__(f"text at index {index} is empty")
        self.index = index


class ModelMismatchError(InvalidRequestError):
    """A request named a model other than the one this sidecar has loaded.

    Answering anyway would hand back vectors from a different model. When the
    widths happen to agree, nothing downstream would notice until a vector index
    had been populated with mutually incomparable vectors -- so this is refused
    rather than served.
    """

    code = "model_mismatch"

    def __init__(self, requested: str, loaded: str) -> None:
        super().__init__(
            f"this sidecar serves {loaded!r}, not {requested!r}; "
            f"point the caller at a sidecar for the model it wants"
        )
        self.requested = requested
        self.loaded = loaded


class InsecureBindError(SidecarError):
    """The service was asked to expose a non-loopback address with no token.

    Refusing is the safe default. An embedding endpoint is a compute amplifier:
    anyone who can reach it can spend the NPU and the CPU at will, and on this
    host that means contending with the builds and peer review the accelerator
    was introduced to keep clear of.
    """

    code = "insecure_bind"
    status = 500

    def __init__(self, host: str, token_env: str, override_env: str) -> None:
        super().__init__(
            f"refusing to bind {host} without authentication: set {token_env} to "
            f"require a bearer token, bind a loopback address, or set "
            f"{override_env}=1 to accept an unauthenticated listener"
        )
        self.host = host


class ModelLoadError(SidecarError):
    """The model IR could not be read from disk."""

    code = "model_load_failed"
    status = 503


class TokenizerLoadError(SidecarError):
    """The tokenizer could not be constructed for the configured model."""

    code = "tokenizer_load_failed"
    status = 503


class ShapeCompileError(SidecarError):
    """A static shape could not be compiled on any available device.

    The sidecar falls back from the NPU to the CPU before raising this, so it
    means both devices refused the shape.
    """

    code = "compile_failed"
    status = 503

    def __init__(self, message: str, batch: int, length: int) -> None:
        super().__init__(message)
        self.batch = batch
        self.length = length


class InferenceError(SidecarError):
    """Inference failed on a shape that had compiled successfully."""

    code = "inference_failed"
    status = 500


class OpenVINOUnavailableError(SidecarError):
    """The OpenVINO runtime could not be imported.

    The sidecar cannot serve embeddings at all in this state, so it is a
    startup error rather than something to fall back from.
    """

    code = "openvino_unavailable"
    status = 503


class DeviceUnavailableError(SidecarError):
    """Neither the NPU nor the CPU could be initialised."""

    code = "device_unavailable"
    status = 503
