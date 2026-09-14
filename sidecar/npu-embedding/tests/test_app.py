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
"""Tests for the HTTP surface.

These drive the real ASGI application through Starlette's test transport, so the
routing, validation, status codes and JSON bodies exercised here are the ones the
Go provider will meet. The wire contract is shared between two languages, so it
is asserted on both sides: these tests and ``pkg/embedding/npu_test.go`` must
agree on every field name.
"""

from __future__ import annotations

import numpy as np
import pytest
from fastapi.testclient import TestClient

from npu_embedding.app import (
    ALLOW_INSECURE_BIND_ENV,
    AUTH_TOKEN_ENV,
    allow_insecure_bind_from_env,
    auth_token_from_env,
    create_app,
    is_loopback,
    require_auth_for_bind,
)
from npu_embedding.errors import InsecureBindError
from npu_embedding.service import ServiceConfig

from conftest import make_service


def client(**kwargs) -> TestClient:
    """Build a test client over a service with a deterministic engine."""
    service, _ = make_service(**kwargs)
    return TestClient(create_app(service))


# -----------------------------------------------------------------------------
# The embed endpoint
# -----------------------------------------------------------------------------


def test_embed_returns_the_contracted_body() -> None:
    """Every field the Go provider decodes must be present and correct."""
    response = client().post("/v1/embed", json={"texts": ["hello world"]})

    assert response.status_code == 200
    body = response.json()

    assert set(body) == {
        "embeddings",
        "model",
        "dimensions",
        "device",
        "total_tokens",
        "chunked",
    }
    assert len(body["embeddings"]) == 1
    assert len(body["embeddings"][0]) == body["dimensions"]
    assert body["model"] == "all-MiniLM-L6-v2"
    assert body["device"] == "NPU"
    assert body["chunked"] == [False]
    assert body["total_tokens"] == 4  # two words plus CLS and SEP


def test_embed_accepts_an_arbitrary_batch_size() -> None:
    """No bucket is visible to a caller: 50 texts is a legal request."""
    texts = [f"text number {i}" for i in range(50)]
    response = client().post("/v1/embed", json={"texts": texts})

    assert response.status_code == 200
    assert len(response.json()["embeddings"]) == 50


def test_embed_accepts_text_past_the_model_ceiling() -> None:
    """Over-long text is chunked server-side and flagged, never rejected."""
    long_text = " ".join(f"w{i}" for i in range(1500))
    response = client().post("/v1/embed", json={"texts": [long_text]})

    assert response.status_code == 200
    body = response.json()
    assert body["chunked"] == [True]
    assert len(body["embeddings"][0]) == body["dimensions"]


def test_embed_empty_batch_is_valid() -> None:
    response = client().post("/v1/embed", json={"texts": []})

    assert response.status_code == 200
    assert response.json()["embeddings"] == []


def test_embed_rejects_empty_text_with_the_error_shape() -> None:
    """The error body is what NPUSidecarError.Code is decoded from."""
    response = client().post("/v1/embed", json={"texts": ["fine", "  "]})

    assert response.status_code == 400
    body = response.json()
    assert body["error"]["code"] == "empty_text"
    assert "index 1" in body["error"]["message"]


def test_embed_rejects_a_missing_texts_field() -> None:
    """An absent field defaults to an empty batch rather than a 500."""
    response = client().post("/v1/embed", json={})
    assert response.status_code == 200
    assert response.json()["embeddings"] == []


def test_embed_rejects_a_non_list_texts_field() -> None:
    """Pydantic rejects the wrong type before the service ever sees it."""
    response = client().post("/v1/embed", json={"texts": "not a list"})
    assert response.status_code == 422


def test_embed_rejects_non_string_elements() -> None:
    response = client().post("/v1/embed", json={"texts": ["ok", 42]})
    assert response.status_code == 422


def test_embed_vectors_are_unit_length() -> None:
    response = client().post("/v1/embed", json={"texts": ["normalise me please"]})
    vector = np.array(response.json()["embeddings"][0], dtype=np.float32)
    assert np.linalg.norm(vector) == pytest.approx(1.0, abs=1e-5)


def test_embed_is_order_preserving_over_http() -> None:
    """Results must line up with the submitted order, not the batching order."""
    texts = ["tiny", " ".join(f"big{i}" for i in range(900)), "small again"]
    body = client().post("/v1/embed", json={"texts": texts}).json()

    assert body["chunked"] == [False, True, False]

    singles = [
        client().post("/v1/embed", json={"texts": [text]}).json()["embeddings"][0]
        for text in texts
    ]
    for i, expected in enumerate(singles):
        np.testing.assert_allclose(
            np.array(body["embeddings"][i], dtype=np.float32),
            np.array(expected, dtype=np.float32),
            rtol=0,
            atol=1e-6,
        )


# -----------------------------------------------------------------------------
# The health endpoint
# -----------------------------------------------------------------------------


def test_health_reports_the_device() -> None:
    body = client().get("/v1/health").json()

    assert body["status"] == "ok"
    assert body["device"] == "NPU"
    assert body["npu_available"] is True
    assert body["max_tokens"] == 512
    assert body["dimensions"] > 0
    assert isinstance(body["compiled_buckets"], list)


def test_health_exposes_cpu_fallback() -> None:
    """The whole point of the endpoint: fallback is otherwise invisible."""
    body = client(device="CPU", npu_available=False).get("/v1/health").json()

    assert body["status"] == "ok"
    assert body["device"] == "CPU"
    assert body["npu_available"] is False


def test_health_lists_warmed_shapes() -> None:
    service, _ = make_service()
    service.warmup()
    body = TestClient(create_app(service)).get("/v1/health").json()

    assert [1, 128] in body["compiled_buckets"]
    assert [8, 512] in body["compiled_buckets"]


# -----------------------------------------------------------------------------
# Routing
# -----------------------------------------------------------------------------


def test_unknown_path_is_a_404() -> None:
    assert client().get("/v1/nope").status_code == 404


def test_embed_rejects_get() -> None:
    assert client().get("/v1/embed").status_code == 405


# -----------------------------------------------------------------------------
# Authentication
# -----------------------------------------------------------------------------


def test_token_is_required_when_configured() -> None:
    service, _ = make_service()
    authed = TestClient(create_app(service, auth_token="s3cr3t"))

    assert authed.post("/v1/embed", json={"texts": ["x"]}).status_code == 401
    assert authed.get("/v1/health").status_code == 401


def test_correct_token_is_accepted() -> None:
    service, _ = make_service()
    authed = TestClient(create_app(service, auth_token="s3cr3t"))

    headers = {"Authorization": "Bearer s3cr3t"}
    assert authed.post("/v1/embed", json={"texts": ["x"]}, headers=headers).status_code == 200
    assert authed.get("/v1/health", headers=headers).status_code == 200


def test_wrong_token_is_rejected_with_the_error_shape() -> None:
    service, _ = make_service()
    authed = TestClient(create_app(service, auth_token="s3cr3t"))

    response = authed.post(
        "/v1/embed", json={"texts": ["x"]}, headers={"Authorization": "Bearer wrong"}
    )
    assert response.status_code == 401
    assert response.json()["error"]["code"] == "unauthorized"


def test_malformed_authorization_header_is_rejected() -> None:
    service, _ = make_service()
    authed = TestClient(create_app(service, auth_token="s3cr3t"))

    for header in ("s3cr3t", "Basic s3cr3t", "Bearer", ""):
        response = authed.get("/v1/health", headers={"Authorization": header})
        assert response.status_code == 401, f"header {header!r} must not authorise"


def test_no_token_configured_means_open_access() -> None:
    """The sidecar is deployed on a trusted LAN segment by default."""
    assert client().get("/v1/health").status_code == 200


def test_auth_token_from_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(AUTH_TOKEN_ENV, raising=False)
    assert auth_token_from_env() is None

    monkeypatch.setenv(AUTH_TOKEN_ENV, "  ")
    assert auth_token_from_env() is None, "whitespace must not count as a token"

    monkeypatch.setenv(AUTH_TOKEN_ENV, "tok")
    assert auth_token_from_env() == "tok"


# -----------------------------------------------------------------------------
# Configuration passthrough
# -----------------------------------------------------------------------------


def test_model_name_is_reported_from_configuration() -> None:
    service, _ = make_service(config=ServiceConfig(model_name="custom-model"))
    body = TestClient(create_app(service)).post(
        "/v1/embed", json={"texts": ["x"]}
    ).json()

    assert body["model"] == "custom-model"


# -----------------------------------------------------------------------------
# The service refuses to expose itself unauthenticated
# -----------------------------------------------------------------------------

# An embedding endpoint is a compute amplifier: whoever reaches it can spend the
# NPU and the CPU freely, which on the NPU host means contending with the builds
# and peer review the accelerator exists to keep clear of. So an off-box bind
# without a token is refused at startup rather than quietly served.


@pytest.mark.parametrize("host", ["127.0.0.1", "::1", "localhost", ""])
def test_loopback_binds_need_no_token(host: str) -> None:
    """Reaching a loopback address already implies local access."""
    assert is_loopback(host) is True
    require_auth_for_bind(host, None)


@pytest.mark.parametrize("host", ["0.0.0.0", "198.51.100.7", "::", "npu-host.internal"])
def test_off_box_bind_without_a_token_is_refused(host: str) -> None:
    assert is_loopback(host) is False

    with pytest.raises(InsecureBindError) as excinfo:
        require_auth_for_bind(host, None)

    error = excinfo.value
    assert error.code == "insecure_bind"
    # The message must name every way out, or an operator is stuck.
    assert AUTH_TOKEN_ENV in error.message
    assert ALLOW_INSECURE_BIND_ENV in error.message
    assert "loopback" in error.message


def test_off_box_bind_with_a_token_is_allowed() -> None:
    require_auth_for_bind("0.0.0.0", "a-real-token")


def test_off_box_bind_can_be_explicitly_opted_into() -> None:
    """An explicit opt-in is honoured; it is just never the default."""
    require_auth_for_bind("0.0.0.0", None, allow_insecure=True)


def test_allow_insecure_bind_from_env(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv(ALLOW_INSECURE_BIND_ENV, raising=False)
    assert allow_insecure_bind_from_env() is False

    monkeypatch.setenv(ALLOW_INSECURE_BIND_ENV, "0")
    assert allow_insecure_bind_from_env() is False, '"0" must mean off'

    monkeypatch.setenv(ALLOW_INSECURE_BIND_ENV, "1")
    assert allow_insecure_bind_from_env() is True


# -----------------------------------------------------------------------------
# The loaded model is the only model served
# -----------------------------------------------------------------------------


def test_request_for_a_different_model_is_refused() -> None:
    """Serving the loaded model under another name would poison an index.

    Two models of the same width produce mutually incomparable vectors, so the
    mismatch would have no visible symptom until retrieval quality collapsed.
    """
    response = client().post(
        "/v1/embed", json={"texts": ["x"], "model": "some-other-model"}
    )

    assert response.status_code == 400
    body = response.json()
    assert body["error"]["code"] == "model_mismatch"
    assert "all-MiniLM-L6-v2" in body["error"]["message"]
    assert "some-other-model" in body["error"]["message"]


def test_request_naming_the_loaded_model_is_accepted() -> None:
    response = client().post(
        "/v1/embed", json={"texts": ["x"], "model": "all-MiniLM-L6-v2"}
    )
    assert response.status_code == 200
    assert response.json()["model"] == "all-MiniLM-L6-v2"


def test_request_omitting_the_model_accepts_whatever_is_loaded() -> None:
    response = client().post("/v1/embed", json={"texts": ["x"]})
    assert response.status_code == 200
    assert response.json()["model"] == "all-MiniLM-L6-v2"
