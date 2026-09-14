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
"""The sidecar's HTTP surface.

Two endpoints, matching what ``pkg/embedding/npu.go`` expects:

``POST /v1/embed``
    Arbitrary texts at an arbitrary batch size. No bucket, shape, chunk or
    device decision is ever required of the caller.

``GET /v1/health``
    The device self-report. This endpoint exists because CPU fallback is
    transparent: embeddings keep succeeding when the NPU is missing, so
    ``npu_available`` here is the only way an operator learns the truth.

Every failure leaves as ``{"error": {"code": ..., "message": ...}}``, which is
the shape the Go provider decodes into ``NPUSidecarError``.
"""

from __future__ import annotations

import hmac
import os

from fastapi import APIRouter, FastAPI, Request
from fastapi.responses import JSONResponse
from pydantic import BaseModel, Field

from npu_embedding.errors import (
    InsecureBindError,
    ModelMismatchError,
    SidecarError,
)
from npu_embedding.service import EmbeddingService

# Environment variable holding the bearer token the sidecar requires, if any.
AUTH_TOKEN_ENV = "NPU_EMBEDDING_TOKEN"

# Set to 1 to accept an unauthenticated non-loopback listener. Off by default.
ALLOW_INSECURE_BIND_ENV = "NPU_EMBEDDING_ALLOW_INSECURE_BIND"

# Hosts that only the local machine can reach. Binding one of these needs no
# token, because reaching it already implies local access.
_LOOPBACK_HOSTS = frozenset({"127.0.0.1", "::1", "localhost", ""})


def is_loopback(host: str) -> bool:
    """Report whether binding ``host`` exposes the service beyond this machine."""
    return host.strip().lower() in _LOOPBACK_HOSTS


def require_auth_for_bind(
    host: str,
    auth_token: str | None,
    *,
    allow_insecure: bool = False,
) -> None:
    """Refuse an unauthenticated listener that is reachable off-box.

    An embedding endpoint is a compute amplifier: anyone who can reach it can
    spend the NPU and the CPU freely. On this host that means contending with the
    builds and the peer review that the accelerator exists to keep clear of, so
    an open LAN listener is a resource-exhaustion vector rather than merely an
    information one.

    Raises :class:`InsecureBindError` unless the bind is loopback, a token is
    configured, or the operator has explicitly opted in.
    """
    if is_loopback(host) or auth_token or allow_insecure:
        return
    raise InsecureBindError(host, AUTH_TOKEN_ENV, ALLOW_INSECURE_BIND_ENV)


def allow_insecure_bind_from_env() -> bool:
    """Report whether the operator opted in to an unauthenticated listener."""
    return os.environ.get(ALLOW_INSECURE_BIND_ENV, "").strip() not in ("", "0")


class EmbedRequest(BaseModel):
    """The request body for ``POST /v1/embed``."""

    texts: list[str] = Field(default_factory=list)

    # The sidecar serves one model per process, so a mismatch is refused rather
    # than silently honoured. Omit the field to accept whatever is loaded.
    model: str | None = None


class EmbedResponse(BaseModel):
    """The success body for ``POST /v1/embed``."""

    embeddings: list[list[float]]
    model: str
    dimensions: int
    device: str
    total_tokens: int
    chunked: list[bool]


def create_app(
    service: EmbeddingService,
    *,
    auth_token: str | None = None,
) -> FastAPI:
    """Build the ASGI application around an already-constructed service.

    The service is injected rather than built here so that tests drive the real
    HTTP stack without needing a device.
    """
    app = FastAPI(
        title="go-quicbaas NPU embedding sidecar",
        version="1.0.0",
        docs_url=None,
        redoc_url=None,
    )
    router = APIRouter(prefix="/v1")

    @app.exception_handler(SidecarError)
    async def handle_sidecar_error(
        _request: Request, exc: SidecarError
    ) -> JSONResponse:
        """Render a typed sidecar error in the wire's error shape."""
        return JSONResponse(status_code=exc.status, content=exc.payload())

    def authorized(request: Request) -> bool:
        """Check the bearer token when one is configured."""
        if not auth_token:
            return True
        header = request.headers.get("authorization", "")
        prefix = "Bearer "
        if not header.startswith(prefix):
            return False
        # Constant-time comparison: this guards a credential.
        return hmac.compare_digest(header[len(prefix) :], auth_token)

    def unauthorized() -> JSONResponse:
        return JSONResponse(
            status_code=401,
            content={
                "error": {
                    "code": "unauthorized",
                    "message": "a valid bearer token is required",
                }
            },
        )

    @router.post("/embed")
    async def embed(request: Request, body: EmbedRequest) -> JSONResponse:
        """Embed arbitrary texts at an arbitrary batch size."""
        if not authorized(request):
            return unauthorized()

        # A sidecar serves exactly one model. Honouring a request that names a
        # different one by quietly using the loaded model would return vectors
        # from the wrong model -- undetectable downstream whenever the widths
        # agree, and poisonous to a vector index.
        if body.model and body.model != service.config.model_name:
            raise ModelMismatchError(body.model, service.config.model_name)

        result = service.embed(body.texts)
        response = EmbedResponse(
            embeddings=result.embeddings,
            model=service.config.model_name,
            dimensions=service.dimensions,
            device=result.device,
            total_tokens=result.total_tokens,
            chunked=result.chunked,
        )
        return JSONResponse(status_code=200, content=response.model_dump())

    @router.get("/health")
    async def health(request: Request) -> JSONResponse:
        """Report the loaded model and which device is actually serving."""
        if not authorized(request):
            return unauthorized()
        return JSONResponse(status_code=200, content=service.health())

    app.include_router(router)
    return app


def auth_token_from_env() -> str | None:
    """Read the sidecar's bearer token from the environment, if set."""
    token = os.environ.get(AUTH_TOKEN_ENV, "").strip()
    return token or None
