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
"""The device layer: the only module in the sidecar that imports OpenVINO.

Everything above this file works on numpy arrays and can be tested without a
driver, a device, or the vendor runtime present. That boundary is deliberate --
it is the same reason the Go daemon talks to this service over HTTP instead of
linking OpenVINO via cgo.

Three device facts drive the implementation:

* **Static shapes are mandatory.** Compiling a dynamic ``[?, ?]`` model for the
  NPU fails with ``ZE_RESULT_ERROR_INVALID_ARGUMENT``. The same model reshaped
  to ``[8, 128]`` compiles immediately. Every shape is therefore reshaped to
  fixed dimensions before compilation, and each distinct shape is a separate
  compiled model held in a cache.

* **The level-zero loader does not auto-discover the NPU.** Unless
  ``ZE_ENABLE_ALT_DRIVERS`` points at ``libze_intel_npu.so.1``,
  ``Core().available_devices`` simply omits ``NPU`` and the service would
  silently serve from the CPU forever. The variable belongs to this service's
  unit file, not to a developer's shell, so :func:`npu_visible` reports the
  outcome and the health endpoint publishes it.

* **The NPU can refuse a shape the CPU accepts.** Rather than fail a request,
  the engine retries that shape on the CPU and records which device served it.
  Fallback is per shape, not per process, so one awkward shape does not demote
  the whole service.
"""

from __future__ import annotations

import os
import threading
from typing import Protocol, runtime_checkable

import numpy as np

from npu_embedding.errors import (
    DeviceUnavailableError,
    InferenceError,
    ModelLoadError,
    OpenVINOUnavailableError,
    ShapeCompileError,
)

# Device identifiers as OpenVINO names them.
DEVICE_NPU = "NPU"
DEVICE_CPU = "CPU"

# The loader hint the Intel NPU needs before it will enumerate the device.
ALT_DRIVERS_ENV = "ZE_ENABLE_ALT_DRIVERS"
DEFAULT_NPU_DRIVER = "/usr/lib/x86_64-linux-gnu/libze_intel_npu.so.1"


def select_candidates(
    prefer_device: str,
    available: list[str],
    *,
    allow_cpu_fallback: bool = True,
) -> list[str]:
    """Return the devices to try, in order, for compiling a shape.

    The preferred device leads when it was enumerated. The CPU is appended as a
    last resort when fallback is permitted, which is what lets the sidecar
    always answer even on a host where the NPU never appeared.

    Raises :class:`DeviceUnavailableError` when nothing is left to try, because
    a sidecar that cannot reach any device should fail at startup rather than
    per request.
    """
    candidates: list[str] = []
    if prefer_device in available:
        candidates.append(prefer_device)
    if allow_cpu_fallback and DEVICE_CPU in available and DEVICE_CPU not in candidates:
        candidates.append(DEVICE_CPU)

    if not candidates:
        detail = f"OpenVINO reports {available}"
        if prefer_device == DEVICE_NPU:
            detail += (
                f"; if the NPU should be present, check that {ALT_DRIVERS_ENV} is set "
                f"in the service environment"
            )
        raise DeviceUnavailableError(
            f"no usable device for preference {prefer_device!r}: {detail}"
        )
    return candidates


def compile_with_fallback(
    candidates: list[str],
    compile_fn,
    batch: int,
    length: int,
):
    """Compile a static shape on the first device that accepts it.

    The NPU genuinely refuses shapes the CPU accepts, so a refusal is an
    expected outcome to move past rather than an error to report. Only when
    every candidate has refused does this raise.

    Returns the ``(compiled_model, device)`` pair that succeeded.
    """
    failures: list[str] = []
    for device in candidates:
        try:
            return compile_fn(device), device
        except RuntimeError as exc:
            failures.append(f"{device}: {exc}")

    raise ShapeCompileError(
        f"no available device could compile shape [{batch}, {length}]: "
        + "; ".join(failures),
        batch=batch,
        length=length,
    )


@runtime_checkable
class Engine(Protocol):
    """Runs a fixed-shape forward pass and reports what it can do."""

    def infer(
        self, input_ids: np.ndarray, attention_mask: np.ndarray
    ) -> np.ndarray:
        """Run one forward pass, returning ``(batch, length, hidden)``."""
        ...

    def ensure_shape(self, batch: int, length: int) -> str:
        """Compile ``(batch, length)`` if needed; return the serving device."""
        ...

    @property
    def device(self) -> str:
        """Return the device that served the most recent inference."""
        ...

    @property
    def hidden_size(self) -> int:
        """Return the model's hidden width."""
        ...

    def compiled_shapes(self) -> list[tuple[int, int]]:
        """Return the ``(batch, length)`` shapes currently compiled."""
        ...


def npu_visible() -> bool:
    """Report whether OpenVINO can currently enumerate the NPU.

    Returns ``False`` rather than raising when OpenVINO is absent, so that
    callers can use this for diagnostics without guarding the import.
    """
    try:
        import openvino as ov
    except ImportError:
        return False

    try:
        return DEVICE_NPU in ov.Core().available_devices
    except RuntimeError:
        return False


class OpenVINOEngine:
    """An :class:`Engine` backed by the OpenVINO runtime.

    One instance owns one model and a cache of compiled static shapes. It is
    safe for concurrent use: compilation is serialised by a lock, since
    compiling the same shape twice would waste seconds of device time.

    .. note::
       This is the only code path in the change that cannot be exercised
       without the NPU userspace driver and OpenVINO present, so it is kept as
       thin as possible and everything decision-shaped lives above it.
    """

    def __init__(
        self,
        model_path: str,
        *,
        prefer_device: str = DEVICE_NPU,
        allow_cpu_fallback: bool = True,
    ) -> None:
        try:
            # OpenVINO 2026 removed the openvino.runtime submodule; Core, and
            # PartialShape live at the top level.
            import openvino as ov
        except ImportError as exc:
            raise OpenVINOUnavailableError(
                f"the openvino package is required to serve embeddings: {exc}"
            ) from exc

        self._ov = ov
        self._prefer_device = prefer_device
        self._allow_cpu_fallback = allow_cpu_fallback

        try:
            self._core = ov.Core()
        except RuntimeError as exc:
            raise DeviceUnavailableError(
                f"could not initialise the OpenVINO core: {exc}"
            ) from exc

        self._available = list(self._core.available_devices)
        # Resolved once at construction so an unusable configuration fails at
        # startup rather than on the first request.
        self._candidates = select_candidates(
            prefer_device, self._available, allow_cpu_fallback=allow_cpu_fallback
        )

        try:
            self._model = self._core.read_model(model_path)
        except (RuntimeError, OSError) as exc:
            raise ModelLoadError(
                f"could not read the model IR at {model_path}: {exc}"
            ) from exc

        self._input_names = [inp.any_name for inp in self._model.inputs]
        self._hidden_size = self._detect_hidden_size()

        # Compiled models keyed by static shape, plus the device each was
        # compiled for so fallback is observable per shape.
        self._compiled: dict[tuple[int, int], object] = {}
        self._shape_device: dict[tuple[int, int], str] = {}
        self._lock = threading.Lock()
        self._last_device = self._candidates[0]

    def _detect_hidden_size(self) -> int:
        """Read the hidden width off the model's 3-D output.

        An optimum export usually emits ``last_hidden_state`` plus a pooled
        output; the 3-D one is the per-token tensor this engine pools itself.
        """
        for output in self._model.outputs:
            shape = output.get_partial_shape()
            if shape.rank.get_length() == 3:
                hidden = shape[2]
                if hidden.is_static:
                    return int(hidden.get_length())
        raise ModelLoadError(
            "could not determine the hidden width: the model exposes no output "
            "with a static 3-D shape"
        )

    @property
    def npu_available(self) -> bool:
        """Report whether the NPU was enumerated at construction."""
        return DEVICE_NPU in self._available

    @property
    def device(self) -> str:
        """Return the device that served the most recent inference."""
        return self._last_device

    @property
    def hidden_size(self) -> int:
        """Return the model's hidden width."""
        return self._hidden_size

    def compiled_shapes(self) -> list[tuple[int, int]]:
        """Return the static shapes currently held in the compile cache."""
        with self._lock:
            return sorted(self._compiled.keys())

    def shape_device(self, batch: int, length: int) -> str | None:
        """Return the device a shape compiled for, or ``None`` if uncompiled."""
        with self._lock:
            return self._shape_device.get((batch, length))

    def fallback_shapes(self) -> list[tuple[int, int, str]]:
        """Return the shapes that compiled on something other than the preference.

        ``device`` alone cannot describe a *partial* fallback, where the NPU took
        some shapes and refused others. This lists exactly which ones went
        elsewhere, so an operator sees the shape of the problem rather than a
        single summary value.
        """
        with self._lock:
            return sorted(
                (batch, length, device)
                for (batch, length), device in self._shape_device.items()
                if device != self._prefer_device
            )

    def ensure_shape(self, batch: int, length: int) -> str:
        """Compile ``(batch, length)`` if it is not cached; return its device.

        Tries the preferred device first and falls back to the CPU when it
        refuses, which is the behaviour that lets the service always answer.
        Raises :class:`ShapeCompileError` only when every candidate device
        refused the shape.
        """
        key = (batch, length)
        with self._lock:
            if key in self._compiled:
                device = self._shape_device[key]
                self._last_device = device
                return device

            static = self._reshaped(batch, length)

            compiled, device = compile_with_fallback(
                self._candidates,
                lambda target: self._core.compile_model(static, target),
                batch,
                length,
            )
            self._compiled[key] = compiled
            self._shape_device[key] = device

            # Record the device here, not only after an inference. Warmup
            # compiles every bucket before a single request arrives, so if the
            # NPU enumerates but refuses those shapes, the fallback happens
            # entirely inside warmup. Leaving _last_device at the preferred
            # device until some later inference would make /v1/health report
            # "NPU" while the CPU was doing all the work -- precisely the
            # silent fallback that endpoint exists to expose.
            self._last_device = device
            return device

    def _reshaped(self, batch: int, length: int):
        """Return a copy of the model with every input pinned to a static shape.

        A dynamic model cannot compile for the NPU at all, so this is not an
        optimisation.
        """
        static = self._model.clone()
        target = self._ov.PartialShape([batch, length])
        try:
            static.reshape({name: target for name in self._input_names})
        except RuntimeError as exc:
            raise ShapeCompileError(
                f"could not reshape the model to [{batch}, {length}]: {exc}",
                batch=batch,
                length=length,
            ) from exc
        return static

    def infer(
        self, input_ids: np.ndarray, attention_mask: np.ndarray
    ) -> np.ndarray:
        """Run one forward pass over an already-padded batch."""
        if input_ids.shape != attention_mask.shape:
            raise InferenceError(
                f"input_ids {input_ids.shape} and attention_mask "
                f"{attention_mask.shape} must agree"
            )

        batch, length = int(input_ids.shape[0]), int(input_ids.shape[1])
        device = self.ensure_shape(batch, length)
        compiled = self._compiled[(batch, length)]

        feed = self._build_feed(input_ids, attention_mask)
        try:
            result = compiled(feed)
        except RuntimeError as exc:
            raise InferenceError(
                f"inference failed on {device} for shape [{batch}, {length}]: {exc}"
            ) from exc

        self._last_device = device
        return self._select_hidden_state(result, batch, length)

    def _build_feed(
        self, input_ids: np.ndarray, attention_mask: np.ndarray
    ) -> dict[str, np.ndarray]:
        """Map the padded tensors onto whatever inputs the IR declares.

        BERT-family exports commonly declare ``token_type_ids`` as well; a
        single-segment input means those are all zero.
        """
        feed: dict[str, np.ndarray] = {}
        for name in self._input_names:
            if "attention" in name:
                feed[name] = attention_mask
            elif "token_type" in name:
                feed[name] = np.zeros_like(input_ids)
            else:
                feed[name] = input_ids
        return feed

    def _select_hidden_state(
        self, result, batch: int, length: int
    ) -> np.ndarray:
        """Pick the per-token output tensor out of the inference result."""
        for value in result.values():
            array = np.asarray(value)
            if array.ndim == 3 and array.shape[0] == batch and array.shape[1] == length:
                return array
        raise InferenceError(
            f"the model returned no (batch, length, hidden) output for shape "
            f"[{batch}, {length}]"
        )


def ensure_alt_drivers(driver_path: str = DEFAULT_NPU_DRIVER) -> bool:
    """Point the level-zero loader at the NPU driver if nothing else has.

    Without this the loader enumerates no NPU and the service falls back to the
    CPU while looking perfectly healthy. Setting it belongs to the unit file;
    this is the in-process backstop for anyone starting the service by hand, and
    it never overrides a value the environment already supplies.

    Returns ``True`` when the variable ends up set to an existing driver.
    """
    existing = os.environ.get(ALT_DRIVERS_ENV)
    if existing:
        return os.path.exists(existing)

    if not os.path.exists(driver_path):
        return False

    os.environ[ALT_DRIVERS_ENV] = driver_path
    return True
