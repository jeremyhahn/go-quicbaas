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
"""Entrypoint: build the real engine, warm the shape cache, serve.

Run as ``python -m npu_embedding``. Every knob is an environment variable so the
systemd unit is the single place deployment is described.

The one variable that is not optional in practice is ``ZE_ENABLE_ALT_DRIVERS``:
without it the level-zero loader never enumerates the NPU and the service serves
from the CPU while reporting itself healthy. :func:`ensure_alt_drivers` sets it
as a backstop when the environment has not, and ``/v1/health`` publishes the
outcome either way.
"""

from __future__ import annotations

import argparse
import logging
import os
import sys

from npu_embedding.app import (
    allow_insecure_bind_from_env,
    auth_token_from_env,
    create_app,
    require_auth_for_bind,
)
from npu_embedding.engine import DEVICE_CPU, DEVICE_NPU, OpenVINOEngine, ensure_alt_drivers
from npu_embedding.errors import SidecarError
from npu_embedding.service import EmbeddingService, ServiceConfig
from npu_embedding.tokenizer import HuggingFaceTokenizer

LOG = logging.getLogger("npu_embedding")

DEFAULT_MODEL_DIR = os.path.expanduser("~/ov-models/all-MiniLM-L6-v2")
DEFAULT_HOST = "0.0.0.0"
DEFAULT_PORT = 8077


def parse_args(argv: list[str] | None = None) -> argparse.Namespace:
    """Parse command-line arguments, defaulting from the environment."""
    parser = argparse.ArgumentParser(prog="npu-embedding")
    parser.add_argument(
        "--model-dir",
        default=os.environ.get("NPU_EMBEDDING_MODEL_DIR", DEFAULT_MODEL_DIR),
        help="directory holding the exported OpenVINO IR and its tokenizer",
    )
    parser.add_argument(
        "--model-name",
        default=os.environ.get("NPU_EMBEDDING_MODEL_NAME", "all-MiniLM-L6-v2"),
        help="model identifier reported to callers",
    )
    parser.add_argument(
        "--host",
        default=os.environ.get("NPU_EMBEDDING_HOST", DEFAULT_HOST),
        help="address to bind",
    )
    parser.add_argument(
        "--port",
        type=int,
        default=int(os.environ.get("NPU_EMBEDDING_PORT", DEFAULT_PORT)),
        help="port to bind",
    )
    parser.add_argument(
        "--device",
        default=os.environ.get("NPU_EMBEDDING_DEVICE", DEVICE_NPU),
        help=f"preferred device ({DEVICE_NPU} or {DEVICE_CPU})",
    )
    parser.add_argument(
        "--no-cpu-fallback",
        action="store_true",
        default=os.environ.get("NPU_EMBEDDING_NO_CPU_FALLBACK", "") not in ("", "0"),
        help="refuse to start unless the preferred device is available",
    )
    parser.add_argument(
        "--no-warmup",
        action="store_true",
        default=os.environ.get("NPU_EMBEDDING_NO_WARMUP", "") not in ("", "0"),
        help="skip precompiling the bucket set at startup",
    )
    return parser.parse_args(argv)


def build_service(args: argparse.Namespace) -> EmbeddingService:
    """Construct the tokenizer, engine and service from parsed arguments."""
    model_xml = os.path.join(args.model_dir, "openvino_model.xml")

    tokenizer = HuggingFaceTokenizer(args.model_dir)
    engine = OpenVINOEngine(
        model_xml,
        prefer_device=args.device,
        allow_cpu_fallback=not args.no_cpu_fallback,
    )
    return EmbeddingService(
        tokenizer, engine, ServiceConfig(model_name=args.model_name)
    )


def main(argv: list[str] | None = None) -> int:
    """Start the sidecar. Returns a process exit status."""
    logging.basicConfig(
        level=logging.INFO, format="%(asctime)s %(levelname)s %(name)s %(message)s"
    )
    args = parse_args(argv)

    if ensure_alt_drivers():
        LOG.info("level-zero alt driver in place; the NPU should enumerate")
    else:
        LOG.warning(
            "no level-zero NPU driver found via ZE_ENABLE_ALT_DRIVERS; "
            "the service will serve from the CPU. Set it in the unit file."
        )

    # Refuse an unauthenticated listener reachable off-box before doing the
    # expensive work of loading a model.
    token = auth_token_from_env()
    try:
        require_auth_for_bind(
            args.host, token, allow_insecure=allow_insecure_bind_from_env()
        )
    except SidecarError as exc:
        LOG.error("could not start: [%s] %s", exc.code, exc.message)
        return 1

    try:
        service = build_service(args)
    except SidecarError as exc:
        LOG.error("could not start: [%s] %s", exc.code, exc.message)
        return 1

    LOG.info(
        "model=%s dimensions=%d device=%s",
        args.model_name,
        service.dimensions,
        service.device,
    )

    if not args.no_warmup:
        # Roughly 7.8s for the default bucket set, paid once so that the first
        # real request does not absorb a multi-second compile.
        compiled = service.warmup()
        LOG.info("precompiled %d static shapes: %s", len(compiled), compiled)

    try:
        import uvicorn
    except ImportError:
        LOG.error("uvicorn is required to serve; install the sidecar's dependencies")
        return 1

    app = create_app(service, auth_token=token)
    uvicorn.run(app, host=args.host, port=args.port, log_level="info")
    return 0


if __name__ == "__main__":
    sys.exit(main())
