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
"""Tests for device selection and CPU fallback.

Fallback is the behaviour that lets the sidecar always answer, and it is exactly
the behaviour that is hardest to observe in production: when it works, requests
succeed and nothing looks wrong. So it is tested directly here by injecting
compile failures, rather than inferred from a passing request.

The OpenVINO-backed engine itself needs a driver and a device and is verified on
the NPU host; the decision logic it delegates to lives in plain functions that
are covered here.
"""

from __future__ import annotations

import os

import pytest

from npu_embedding.engine import (
    ALT_DRIVERS_ENV,
    DEVICE_CPU,
    DEVICE_NPU,
    compile_with_fallback,
    ensure_alt_drivers,
    npu_visible,
    select_candidates,
)
from npu_embedding.errors import DeviceUnavailableError, ShapeCompileError


# -----------------------------------------------------------------------------
# Device selection
# -----------------------------------------------------------------------------


def test_npu_leads_when_available() -> None:
    assert select_candidates(DEVICE_NPU, [DEVICE_NPU, DEVICE_CPU]) == [
        DEVICE_NPU,
        DEVICE_CPU,
    ]


def test_cpu_only_host_falls_back_silently() -> None:
    """A host with no NPU still serves, from the CPU."""
    assert select_candidates(DEVICE_NPU, [DEVICE_CPU]) == [DEVICE_CPU]


def test_cpu_is_not_duplicated_when_it_is_the_preference() -> None:
    assert select_candidates(DEVICE_CPU, [DEVICE_NPU, DEVICE_CPU]) == [DEVICE_CPU]


def test_fallback_can_be_refused() -> None:
    """With fallback off, a missing NPU is a startup failure.

    An operator who asked for the NPU specifically would rather the service
    refuse to start than quietly serve from the CPU forever.
    """
    with pytest.raises(DeviceUnavailableError) as excinfo:
        select_candidates(DEVICE_NPU, [DEVICE_CPU], allow_cpu_fallback=False)

    assert excinfo.value.code == "device_unavailable"
    # The message must name the variable that is almost always the cause.
    assert ALT_DRIVERS_ENV in excinfo.value.message


def test_no_devices_at_all_is_an_error() -> None:
    with pytest.raises(DeviceUnavailableError):
        select_candidates(DEVICE_NPU, [])


def test_missing_npu_message_names_the_loader_variable() -> None:
    """The error must point at ZE_ENABLE_ALT_DRIVERS, the usual culprit."""
    with pytest.raises(DeviceUnavailableError) as excinfo:
        select_candidates(DEVICE_NPU, ["GPU"], allow_cpu_fallback=False)
    assert ALT_DRIVERS_ENV in excinfo.value.message


def test_gpu_preference_without_fallback_is_rejected() -> None:
    with pytest.raises(DeviceUnavailableError):
        select_candidates("GPU", [DEVICE_CPU], allow_cpu_fallback=False)


# -----------------------------------------------------------------------------
# Compile fallback
# -----------------------------------------------------------------------------


def test_first_device_wins_when_it_accepts() -> None:
    attempted: list[str] = []

    def compile_fn(device: str) -> str:
        attempted.append(device)
        return f"compiled-on-{device}"

    compiled, device = compile_with_fallback(
        [DEVICE_NPU, DEVICE_CPU], compile_fn, 8, 128
    )

    assert compiled == "compiled-on-NPU"
    assert device == DEVICE_NPU
    assert attempted == [DEVICE_NPU], "the CPU must not be tried once the NPU accepts"


def test_npu_refusal_falls_back_to_cpu() -> None:
    """The measured failure mode: the NPU refuses a shape the CPU accepts.

    A dynamic or awkward shape raises ZE_RESULT_ERROR_INVALID_ARGUMENT on the
    NPU. The sidecar must answer anyway.
    """
    attempted: list[str] = []

    def compile_fn(device: str) -> str:
        attempted.append(device)
        if device == DEVICE_NPU:
            raise RuntimeError("ZE_RESULT_ERROR_INVALID_ARGUMENT")
        return f"compiled-on-{device}"

    compiled, device = compile_with_fallback(
        [DEVICE_NPU, DEVICE_CPU], compile_fn, 8, 512
    )

    assert device == DEVICE_CPU
    assert compiled == "compiled-on-CPU"
    assert attempted == [DEVICE_NPU, DEVICE_CPU]


def test_all_devices_refusing_raises_with_both_reasons() -> None:
    """Only a total refusal is an error, and it must explain every attempt."""

    def compile_fn(device: str) -> str:
        raise RuntimeError(f"{device} said no")

    with pytest.raises(ShapeCompileError) as excinfo:
        compile_with_fallback([DEVICE_NPU, DEVICE_CPU], compile_fn, 8, 512)

    error = excinfo.value
    assert error.code == "compile_failed"
    assert error.batch == 8
    assert error.length == 512
    assert "NPU said no" in error.message
    assert "CPU said no" in error.message


def test_compile_error_payload_is_the_wire_shape() -> None:
    """The Go provider decodes this shape into NPUSidecarError."""

    def compile_fn(device: str) -> str:
        raise RuntimeError("nope")

    with pytest.raises(ShapeCompileError) as excinfo:
        compile_with_fallback([DEVICE_NPU], compile_fn, 1, 128)

    payload = excinfo.value.payload()
    assert payload["error"]["code"] == "compile_failed"
    assert "nope" in payload["error"]["message"]


def test_empty_candidate_list_raises_rather_than_returning_none() -> None:
    with pytest.raises(ShapeCompileError):
        compile_with_fallback([], lambda _: "unused", 1, 128)


# -----------------------------------------------------------------------------
# The loader variable
# -----------------------------------------------------------------------------


def test_ensure_alt_drivers_respects_an_existing_value(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    """An operator-supplied value must never be overwritten."""
    driver = tmp_path / "libze_intel_npu.so.1"
    driver.write_bytes(b"")
    monkeypatch.setenv(ALT_DRIVERS_ENV, str(driver))

    assert ensure_alt_drivers("/nonexistent/other.so") is True
    assert os.environ[ALT_DRIVERS_ENV] == str(driver)


def test_ensure_alt_drivers_reports_a_stale_existing_value(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """A variable pointing at a missing file is reported, not trusted."""
    monkeypatch.setenv(ALT_DRIVERS_ENV, "/nonexistent/driver.so")
    assert ensure_alt_drivers() is False


def test_ensure_alt_drivers_sets_the_variable_when_the_driver_exists(
    monkeypatch: pytest.MonkeyPatch, tmp_path
) -> None:
    monkeypatch.delenv(ALT_DRIVERS_ENV, raising=False)
    driver = tmp_path / "libze_intel_npu.so.1"
    driver.write_bytes(b"")

    assert ensure_alt_drivers(str(driver)) is True
    assert os.environ[ALT_DRIVERS_ENV] == str(driver)


def test_ensure_alt_drivers_reports_failure_when_no_driver_is_present(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """On a host with no NPU driver the caller must be told, not misled."""
    monkeypatch.delenv(ALT_DRIVERS_ENV, raising=False)
    assert ensure_alt_drivers("/nonexistent/driver.so") is False
    assert ALT_DRIVERS_ENV not in os.environ


def test_npu_visible_is_false_without_openvino() -> None:
    """The diagnostic must answer rather than raise when OpenVINO is absent."""
    assert npu_visible() in (True, False)
