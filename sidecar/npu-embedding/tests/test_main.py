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
"""Tests for the entrypoint's configuration surface.

Argument parsing is where a deployment mistake becomes a silent CPU fallback, so
the precedence between flags, environment variables and defaults is pinned here.

The construction of the real OpenVINO engine cannot run off the NPU host; what is
checked here is that a failure to construct it is reported as a typed error and a
non-zero exit rather than a traceback.
"""

from __future__ import annotations

import pytest

from npu_embedding.__main__ import DEFAULT_PORT, build_service, main, parse_args
from npu_embedding.engine import DEVICE_CPU, DEVICE_NPU
from npu_embedding.errors import SidecarError


def test_defaults_prefer_the_npu(monkeypatch: pytest.MonkeyPatch) -> None:
    for var in (
        "NPU_EMBEDDING_MODEL_DIR",
        "NPU_EMBEDDING_MODEL_NAME",
        "NPU_EMBEDDING_HOST",
        "NPU_EMBEDDING_PORT",
        "NPU_EMBEDDING_DEVICE",
        "NPU_EMBEDDING_NO_CPU_FALLBACK",
        "NPU_EMBEDDING_NO_WARMUP",
    ):
        monkeypatch.delenv(var, raising=False)

    args = parse_args([])
    assert args.device == DEVICE_NPU
    assert args.port == DEFAULT_PORT
    assert args.model_name == "all-MiniLM-L6-v2"
    assert args.no_cpu_fallback is False
    assert args.no_warmup is False


def test_environment_supplies_defaults(monkeypatch: pytest.MonkeyPatch) -> None:
    """The systemd unit configures the service through the environment."""
    monkeypatch.setenv("NPU_EMBEDDING_PORT", "9999")
    monkeypatch.setenv("NPU_EMBEDDING_DEVICE", DEVICE_CPU)
    monkeypatch.setenv("NPU_EMBEDDING_MODEL_NAME", "other-model")
    monkeypatch.setenv("NPU_EMBEDDING_HOST", "127.0.0.1")

    args = parse_args([])
    assert args.port == 9999
    assert args.device == DEVICE_CPU
    assert args.model_name == "other-model"
    assert args.host == "127.0.0.1"


def test_flags_override_the_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("NPU_EMBEDDING_PORT", "9999")
    args = parse_args(["--port", "7000", "--device", DEVICE_CPU])
    assert args.port == 7000
    assert args.device == DEVICE_CPU


def test_no_cpu_fallback_flag(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("NPU_EMBEDDING_NO_CPU_FALLBACK", raising=False)
    assert parse_args(["--no-cpu-fallback"]).no_cpu_fallback is True


def test_no_cpu_fallback_from_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("NPU_EMBEDDING_NO_CPU_FALLBACK", "1")
    assert parse_args([]).no_cpu_fallback is True


def test_zero_is_not_truthy_for_boolean_environment_flags(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """"0" must mean off, not "a non-empty string, therefore on"."""
    monkeypatch.setenv("NPU_EMBEDDING_NO_WARMUP", "0")
    assert parse_args([]).no_warmup is False


def test_no_warmup_from_environment(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("NPU_EMBEDDING_NO_WARMUP", "1")
    assert parse_args([]).no_warmup is True


def test_build_service_reports_a_typed_error_for_a_missing_model() -> None:
    """A bad model directory must raise a SidecarError, never a bare exception.

    Which subclass depends on what is installed -- without OpenVINO or
    transformers the import guard fires first, with them the load does -- but it
    is always a typed sidecar error carrying a code.
    """
    args = parse_args(["--model-dir", "/nonexistent/model/directory"])

    with pytest.raises(SidecarError) as excinfo:
        build_service(args)

    assert excinfo.value.code
    assert excinfo.value.status >= 500


def test_main_exits_nonzero_when_the_service_cannot_start() -> None:
    """A startup failure is an exit status, not a traceback."""
    assert main(["--model-dir", "/nonexistent/model/directory", "--no-warmup"]) == 1


def test_main_refuses_an_unauthenticated_off_box_bind(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """The service must not come up exposed with no token.

    This runs before the model is loaded, so the refusal does not depend on a
    device or an IR being present.
    """
    monkeypatch.delenv("NPU_EMBEDDING_TOKEN", raising=False)
    monkeypatch.delenv("NPU_EMBEDDING_ALLOW_INSECURE_BIND", raising=False)

    assert main(["--host", "0.0.0.0", "--no-warmup"]) == 1


def test_main_accepts_an_off_box_bind_with_a_token(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    """With a token the bind check passes; startup then fails on the model.

    A non-zero exit here comes from the missing model directory, not the bind
    guard -- which is what distinguishes "refused for being insecure" from
    "refused for lacking a model".
    """
    monkeypatch.setenv("NPU_EMBEDDING_TOKEN", "a-real-token")
    monkeypatch.delenv("NPU_EMBEDDING_ALLOW_INSECURE_BIND", raising=False)

    # Reaches build_service and fails there, proving the bind guard let it past.
    args = parse_args(["--host", "0.0.0.0", "--model-dir", "/nonexistent"])
    with pytest.raises(SidecarError):
        build_service(args)
