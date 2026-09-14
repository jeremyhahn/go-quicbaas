# NPU embedding sidecar

An OpenVINO-backed embedding service that runs `all-MiniLM-L6-v2` on an Intel
NPU. It is consumed by go-quicbaas's `npu` embedding provider
(`pkg/embedding/npu.go`) over HTTP.

Full documentation, including every measured number and the deployment
procedure, is in [`docs/embeddings/npu.md`](../../docs/embeddings/npu.md).

## Why it exists as a sidecar

The NPU lives on one host while `quicbaasd` runs on another, so a network
boundary already exists. Keeping OpenVINO out of the Go daemon also means the
daemon's build does not depend on a vendor runtime being installed everywhere,
which `CLAUDE.md` requires. This mirrors `pkg/embedding/ollama.go`, which
likewise reaches a local inference engine through a configured URL.

## Why it is not a speedup

Measured at sequence length 128: **NPU 264/s** (batch 8) and 207/s (batch 1),
against **CPU 224/s** and **GPU 144/s**. The NPU is roughly CPU-equivalent. The
point is that embedding work stops competing with builds, tests and peer review
for the CPU cores and the GPU — not that it goes faster.

## Layout

| File | Responsibility |
| --- | --- |
| `bucket.py` | Static-shape selection, padding, chunk planning. Pure. |
| `pooling.py` | Masked mean pooling, normalisation, chunk pooling. Pure numpy. |
| `tokenizer.py` | The `Tokenizer` protocol and its transformers implementation. |
| `engine.py` | The `Engine` protocol and the **only** OpenVINO import. |
| `service.py` | Orchestration: text in, embeddings out. |
| `app.py` | The two HTTP endpoints. |

Only `engine.py` touches a vendor runtime, so everything above it is tested
without an NPU, a driver, or OpenVINO present.

## Install

Logic and tests only — no device needed:

```sh
pip install -e '.[dev]'
```

On the NPU host, add the runtime:

```sh
pip install -e '.[device,dev]'
```

## Run

```sh
ZE_ENABLE_ALT_DRIVERS=/usr/lib/x86_64-linux-gnu/libze_intel_npu.so.1 \
  python -m npu_embedding --model-dir ~/ov-models/all-MiniLM-L6-v2
```

**`ZE_ENABLE_ALT_DRIVERS` is not optional.** Without it the level-zero loader
never enumerates the NPU, `Core().available_devices` omits it, and the service
serves from the CPU while reporting itself healthy. The process also needs
membership of the `render` group to open `/dev/accel/accel0`.

Confirm which device is actually serving:

```sh
curl -s localhost:8077/v1/health | jq '{device, npu_available}'
```

## API

Neither endpoint exposes a bucket, a shape, a chunk or a device decision.

```sh
curl -s localhost:8077/v1/embed \
  -H 'content-type: application/json' \
  -d '{"texts": ["any length", "any batch size"]}'
```

Errors return `{"error": {"code": ..., "message": ...}}`, which the Go provider
decodes into a typed `NPUSidecarError`.

## Tests

```sh
make test-npu-sidecar          # logic; no device required
make test-npu-sidecar-device   # real OpenVINO; needs the `device` extra
```

The device tier is a separate suite rather than a skipped part of the first, so
nothing is ever silently not run. On the NPU host, set
`NPU_EMBEDDING_TEST_DEVICE=NPU` to exercise the device itself.
