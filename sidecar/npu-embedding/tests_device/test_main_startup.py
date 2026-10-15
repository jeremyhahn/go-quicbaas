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
"""Tests that run the real startup sequence of ``python -m npu_embedding``.

The unit tier can only check that a failed startup exits non-zero. Here
:func:`main` runs end to end over a complete model directory -- the tiny
OpenVINO IR beside the tiny tokenizer -- so the real tokenizer is loaded, the
real engine is built, and warmup really compiles the bucket set.

The one thing replaced is ``uvicorn.run``, which would otherwise bind a socket
and block. A recorder takes its place and the tests drive the application
``main`` handed it through FastAPI's in-process client, so what is asserted is
the service a deployment would actually serve.
"""

from __future__ import annotations

import logging
import os
import sys

import pytest
import uvicorn
from fastapi.testclient import TestClient

from npu_embedding.__main__ import main
from npu_embedding.app import ALLOW_INSECURE_BIND_ENV, AUTH_TOKEN_ENV
from npu_embedding.bucket import DEFAULT_BATCH_BUCKETS, DEFAULT_LENGTH_BUCKETS
from npu_embedding.engine import ALT_DRIVERS_ENV, DEVICE_CPU

from tiny_model import HIDDEN_SIZE, build_tiny_ir
from tiny_tokenizer import build_tiny_tokenizer

# Set to NPU on the NPU host to run this suite against the device.
TEST_DEVICE = os.environ.get("NPU_EMBEDDING_TEST_DEVICE", DEVICE_CPU)

# Every default bucket, in the order the health endpoint lists compiled shapes.
DEFAULT_SHAPES = sorted(
    [b, length] for b in DEFAULT_BATCH_BUCKETS for length in DEFAULT_LENGTH_BUCKETS
)


class ServeRecorder:
    """Takes the place of ``uvicorn.run``: records what would have been served."""

    def __init__(self) -> None:
        self.calls: list[tuple[object, dict[str, object]]] = []

    def __call__(self, app: object, **kwargs: object) -> None:
        self.calls.append((app, kwargs))

    def client(self, **kwargs: object) -> TestClient:
        """Return an in-process client over the single app that was served."""
        assert len(self.calls) == 1, "main must serve exactly once"
        app, _ = self.calls[0]
        return TestClient(app, **kwargs)


@pytest.fixture(scope="module")
def model_dir(tmp_path_factory: pytest.TempPathFactory) -> str:
    """A complete model directory: the IR and its tokenizer side by side."""
    directory = tmp_path_factory.mktemp("model")
    build_tiny_ir(str(directory / "openvino_model.xml"))
    return build_tiny_tokenizer(str(directory))


@pytest.fixture
def served(monkeypatch: pytest.MonkeyPatch, tmp_path) -> ServeRecorder:
    """Isolate main from the host's environment and capture what it serves."""
    driver = tmp_path / "libze_intel_npu.so.1"
    driver.write_bytes(b"")
    monkeypatch.setenv(ALT_DRIVERS_ENV, str(driver))
    monkeypatch.delenv(AUTH_TOKEN_ENV, raising=False)
    monkeypatch.delenv(ALLOW_INSECURE_BIND_ENV, raising=False)

    recorder = ServeRecorder()
    monkeypatch.setattr(uvicorn, "run", recorder)
    return recorder


def start(model_dir: str, *extra: str) -> int:
    return main(
        [
            "--model-dir",
            model_dir,
            "--model-name",
            "tiny",
            "--device",
            TEST_DEVICE,
            "--host",
            "127.0.0.1",
            "--port",
            "8123",
            *extra,
        ]
    )


def test_startup_warms_every_bucket_before_serving(
    model_dir: str, served: ServeRecorder
) -> None:
    """The first request must find the whole bucket set already compiled."""
    assert start(model_dir) == 0

    _, options = served.calls[0]
    assert options["host"] == "127.0.0.1"
    assert options["port"] == 8123

    health = served.client().get("/v1/health").json()
    assert health["model"] == "tiny"
    assert health["dimensions"] == HIDDEN_SIZE
    assert health["compiled_buckets"] == DEFAULT_SHAPES


def test_started_service_embeds_through_the_real_tokenizer(
    model_dir: str, served: ServeRecorder
) -> None:
    assert start(model_dir) == 0

    response = served.client().post(
        "/v1/embed", json={"texts": ["the quick brown fox"]}
    )

    assert response.status_code == 200
    body = response.json()
    assert len(body["embeddings"]) == 1
    assert len(body["embeddings"][0]) == HIDDEN_SIZE
    # Four WordPiece tokens plus CLS and SEP.
    assert body["total_tokens"] == 6


def test_no_warmup_serves_with_an_empty_compile_cache(
    model_dir: str, served: ServeRecorder
) -> None:
    assert start(model_dir, "--no-warmup") == 0

    health = served.client().get("/v1/health").json()
    assert health["compiled_buckets"] == []


def test_the_configured_token_guards_the_served_app(
    model_dir: str, served: ServeRecorder, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.setenv(AUTH_TOKEN_ENV, "a-real-token")

    assert start(model_dir, "--no-warmup") == 0

    client = served.client()
    assert client.get("/v1/health").status_code == 401
    authorized = client.get(
        "/v1/health", headers={"Authorization": "Bearer a-real-token"}
    )
    assert authorized.status_code == 200


def test_startup_fails_without_uvicorn(
    model_dir: str, served: ServeRecorder, monkeypatch: pytest.MonkeyPatch
) -> None:
    """A missing server dependency is an exit status, not a traceback."""
    monkeypatch.setitem(sys.modules, "uvicorn", None)

    assert start(model_dir, "--no-warmup") == 1
    assert served.calls == []


def test_startup_fails_when_the_model_ir_is_missing(
    tmp_path, served: ServeRecorder
) -> None:
    """A directory holding only a tokenizer loads it, then fails on the IR."""
    build_tiny_tokenizer(str(tmp_path))

    assert start(str(tmp_path), "--no-warmup") == 1
    assert served.calls == []


def test_startup_reports_a_present_npu_driver(
    model_dir: str, served: ServeRecorder, caplog: pytest.LogCaptureFixture
) -> None:
    with caplog.at_level(logging.INFO, logger="npu_embedding"):
        assert start(model_dir, "--no-warmup") == 0

    assert "level-zero alt driver in place" in caplog.text


def test_startup_warns_when_the_npu_driver_is_missing(
    model_dir: str,
    served: ServeRecorder,
    monkeypatch: pytest.MonkeyPatch,
    tmp_path,
    caplog: pytest.LogCaptureFixture,
) -> None:
    """Serving from the CPU must be announced, never silent."""
    monkeypatch.setenv(ALT_DRIVERS_ENV, str(tmp_path / "absent.so"))

    with caplog.at_level(logging.WARNING, logger="npu_embedding"):
        assert start(model_dir, "--no-warmup") == 0

    assert "no level-zero NPU driver found" in caplog.text
