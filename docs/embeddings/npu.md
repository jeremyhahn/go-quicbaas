# NPU embedding provider

> **Status: REFERENCE** · Last verified: 2026-09-29
> RAG embeddings on an Intel NPU through an OpenVINO sidecar.

Runs RAG embeddings on an Intel NPU through an OpenVINO sidecar, so embedding
work stops competing with builds, tests and peer review for the CPU cores and
the GPU.

- **Go provider**: `pkg/embedding/npu.go`, registered as `npu`
- **Sidecar**: `sidecar/npu-embedding/` (Python, OpenVINO)
- **Issue**: #858

## Read this first: it is not a speedup

Measured on gpu-host at sequence length 128: <!-- lab-string-ok: measurement provenance -->

| Device | Throughput |
| --- | --- |
| NPU, batch 8 | **264 embeddings/s** |
| NPU, batch 1 | 207 embeddings/s |
| CPU | 224 embeddings/s |
| GPU (RTX) | 144 embeddings/s |

The NPU is roughly **CPU-equivalent**. If you adopt this expecting throughput,
you will be disappointed and you will rip it out.

The actual reason to use it: the host's 24 CPU cores stay free for compilation
and tests, and the RTX stays free for peer review, which runs continuously.
Embedding is a steady background load from RAG ingest, so moving it to an
otherwise-idle accelerator pays continuously even at equal speed.

## Why a sidecar and not cgo

1. The NPU is on gpu-host while `quicbaasd` runs on vm-host-4 <!-- lab-string-ok: measurement provenance -->
   — a network boundary already exists.
2. `CLAUDE.md` forbids hardcoding vendor SDKs in the central daemon.
3. cgo would bind the whole daemon build to OpenVINO being installed on every
   host that compiles it.
4. It matches the existing precedent: `pkg/embedding/ollama.go` already reaches
   a local inference engine over a configured `APIURL`.

## Hardware and software baseline

Verified on gpu-host, 2026-09-12: <!-- lab-string-ok: measurement provenance -->

- Intel Arrow Lake NPU, `DEVICE_ARCHITECTURE 3720`, 2 tiles, at
  `/dev/accel/accel0`, owned by group `render`
- Intel NPU userspace driver **v1.38.0** — `intel-driver-compiler-npu`,
  `intel-level-zero-npu`, `intel-fw-npu` — plus Ubuntu's `libze1` loader
- **OpenVINO 2026.3.1** with `optimum-intel`

> The 2026 release line **removed the `openvino.runtime` submodule**. Use
> `import openvino as ov`; `Core` and `PartialShape` are top-level.

## The two things that silently break this

### 1. `ZE_ENABLE_ALT_DRIVERS`

The level-zero loader **does not auto-discover the NPU**. Without

```
ZE_ENABLE_ALT_DRIVERS=/usr/lib/x86_64-linux-gnu/libze_intel_npu.so.1
```

`Core().available_devices` simply omits `NPU`. Because the sidecar falls back to
the CPU transparently so it always answers, the result is a service that looks
perfectly healthy while never touching the accelerator.

Set it in the **unit file**, not in a shell profile — it is deliberately not set
globally on the workstation.

### 2. The `render` group

`/dev/accel/accel0` is owned by group `render`. The service user needs
`SupplementaryGroups=render` or the device cannot be opened.

### How to check

```sh
curl -s localhost:8077/v1/health | jq '{device, npu_available}'
```

`npu_available: false` means you are on the CPU. The Go provider exposes the same
fact through `NPUProvider.LastDevice()`, which reads `unknown` until the first
successful request.

`fallback_shapes` lists every `(batch, length, device)` that compiled somewhere
other than the preferred device. It exists because `device` alone cannot describe
a **partial** fallback, where the NPU accepts some shapes and refuses others.

The device is recorded when a shape is *compiled*, not only after an inference.
That matters because warmup compiles the whole bucket set before the first
request arrives: if the NPU enumerates but refuses those shapes, the fallback
happens entirely inside warmup, and reporting it only after some later inference
would leave health claiming `NPU` while the CPU did all the work — exactly the
silence this endpoint exists to break.

## The static-shape constraint, and why bucketing is free

The NPU requires **fully static** input shapes. A dynamic `[?, ?]` model fails to
compile with `ZE_RESULT_ERROR_INVALID_ARGUMENT`; reshaped to `[8, 128]` it
compiles immediately.

An embedding API that only accepted one fixed shape would be useless, so the
sidecar keeps a small set of precompiled shapes and routes arbitrary input into
them. **No bucket, shape, chunk or device decision is ever visible to a caller.**

That hiding is exact, not approximate:

> The same text padded to 32, 64, 128 and 256 tokens produces **cosine
> 1.000000** against every other padding, under masked mean pooling.

The reason is structural. The sentence embedding is the mean of the token
vectors over *unmasked* positions only, so a padded position contributes to
neither the numerator nor the denominator. Batch padding is invariant for the
same kind of reason: self-attention operates within a sequence and never across
batch rows, so the filler rows that top a batch up to its bucket cannot perturb
the real ones.

This is why bucketing is honest rather than a compromise — and it is asserted as
a test, not left as a comment:

- `sidecar/npu-embedding/tests/test_pooling.py` — invariance at the pooling
  level, with masked positions filled with deliberately enormous values so that
  including even one of them fails loudly instead of drifting
- `tests/test_service.py::test_embedding_is_identical_across_bucket_sets` —
  end to end
- `tests_device/test_openvino_engine.py::test_padding_invariance_through_the_real_runtime`
  — through a real OpenVINO forward pass

### Bucket set and compile cost

| Shape | Compile time |
| --- | --- |
| `[1, any L]` | ~0.05s |
| `[8, 256]` | 2.9s |
| `[8, 512]` | 4.7s |
| **Default set**: batch {1, 8} x length {128, 256, 512} | **7.8s, once** |

The sidecar precompiles the whole set at startup, so no request pays a
multi-second compile. A shape outside the set is compiled lazily rather than
refused.

Chunks are assigned to their **length bucket first** and only then split into
batches, so a 5-token query is never padded to 512 merely because a long
document arrived in the same request.

### 512 is the model's limit, not the NPU's

`all-MiniLM-L6-v2` declares `max_position_embeddings: 512`; a length of 1024
fails inside the model's own shape broadcast, on **any** device. Text beyond 512
tokens must be chunked on CPU and GPU too.

The sidecar chunks it: the body is split into windows of `max_length - 2` tokens,
each wrapped in `[CLS]`/`[SEP]`, and the per-chunk vectors are combined with a
**token-count-weighted** mean so a 510-token chunk counts for more than the
12-token remainder after it. `chunked` in the response reports which inputs were
split.

## Transparent CPU fallback

The NPU can refuse a shape the CPU accepts. Rather than fail a request, the
engine recompiles that shape on the CPU and records which device served it.

- Fallback is **per shape**, not per process, so one awkward shape does not
  demote the whole service.
- Only a refusal by *every* candidate device is an error (`compile_failed`).
- Because fallback always answers, it is invisible without looking. `/v1/health`
  and `LastDevice()` exist precisely so an operator can tell.

Set `NPU_EMBEDDING_NO_CPU_FALLBACK=1` to make a missing NPU a startup failure
instead, for a host where silently using the CPU would be worse than not
starting.

## Build tags: the provider only exists in vector builds

`pkg/embedding`'s real `Provider` interface sits behind
`//go:build vector && qrdb_vector`. Without **both** tags, `stub.go` supplies a
stub whose `RegisterProvider` returns `ErrVectorNotEnabled` and whose
`NewNPUProvider` returns the same.

This bites at 2am. If the provider appears to do nothing, check the tags first.

```sh
make test-embedding      # sets vector qrdb_vector for you
make coverage-embedding
make bench-embedding
```

## Wire protocol

### `POST /v1/embed`

Arbitrary texts, arbitrary batch size.

```json
{ "texts": ["any length", "any batch size"], "model": "all-MiniLM-L6-v2" }
```

```json
{
  "embeddings": [[0.1, "..."]],
  "model": "all-MiniLM-L6-v2",
  "dimensions": 384,
  "device": "NPU",
  "total_tokens": 24,
  "chunked": [false, false]
}
```

### `GET /v1/health`

```json
{
  "status": "ok",
  "device": "NPU",
  "model": "all-MiniLM-L6-v2",
  "dimensions": 384,
  "max_tokens": 512,
  "compiled_buckets": [[1, 128], [1, 256], [1, 512], [8, 128], [8, 256], [8, 512]],
  "npu_available": true,
  "fallback_shapes": []
}
```

### Errors

```json
{ "error": { "code": "compile_failed", "message": "NPU refused shape [8,512]" } }
```

| Code | Status | Meaning |
| --- | --- | --- |
| `empty_text` | 400 | A submitted text was empty or whitespace-only |
| `invalid_request` | 400 | Malformed request body |
| `compile_failed` | 503 | Every device refused a shape |
| `model_load_failed` | 503 | The IR could not be read |
| `tokenizer_load_failed` | 503 | The tokenizer could not be built |
| `openvino_unavailable` | 503 | The OpenVINO runtime is not importable |
| `device_unavailable` | 503 | No usable device |
| `inference_failed` | 500 | A compiled shape failed at run time |
| `unauthorized` | 401 | Bearer token missing or wrong |
| `model_mismatch` | 400 | The request named a model this sidecar does not serve |
| `insecure_bind` | 500 | Refused to expose a non-loopback address with no token |

## Deploying the sidecar

On the host that owns the NPU — **not** the host running `quicbaasd`.

```sh
python -m venv /opt/quicbaas/npu-embedding
/opt/quicbaas/npu-embedding/bin/pip install -e 'sidecar/npu-embedding[device]'
```

Export the model IR once:

```sh
optimum-cli export openvino \
  --model sentence-transformers/all-MiniLM-L6-v2 \
  /var/lib/quicbaas/ov-models/all-MiniLM-L6-v2
```

Then install the unit from
`sidecar/npu-embedding/deploy/npu-embedding.service`:

```sh
sudo cp sidecar/npu-embedding/deploy/npu-embedding.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now npu-embedding
curl -s localhost:8077/v1/health | jq '{device, npu_available}'
```

### Environment

| Variable | Default | Purpose |
| --- | --- | --- |
| `ZE_ENABLE_ALT_DRIVERS` | — | **Required** for the NPU to be enumerated |
| `NPU_EMBEDDING_MODEL_DIR` | `~/ov-models/all-MiniLM-L6-v2` | IR + tokenizer |
| `NPU_EMBEDDING_MODEL_NAME` | `all-MiniLM-L6-v2` | Reported to callers |
| `NPU_EMBEDDING_HOST` | `0.0.0.0` | Bind address |
| `NPU_EMBEDDING_PORT` | `8077` | Bind port |
| `NPU_EMBEDDING_DEVICE` | `NPU` | Preferred device |
| `NPU_EMBEDDING_NO_CPU_FALLBACK` | unset | Refuse to start without the NPU |
| `NPU_EMBEDDING_NO_WARMUP` | unset | Skip precompiling the bucket set |
| `NPU_EMBEDDING_TOKEN` | unset | Require a bearer token |
| `NPU_EMBEDDING_ALLOW_INSECURE_BIND` | unset | Accept an unauthenticated off-box listener |

### The sidecar refuses to start exposed without a token

An embedding endpoint is a compute amplifier: anyone who can reach it can spend
the NPU and the CPU at will, which on this host means contending with the builds
and the peer review that the accelerator exists to keep clear of. So an open LAN
listener is a resource-exhaustion vector, not merely an information one.

The service therefore **refuses to start** on a non-loopback address with no
token. Choose one:

- set `NPU_EMBEDDING_TOKEN` (the shipped unit binds `0.0.0.0`, so this is the
  normal path — use an `EnvironmentFile` with mode `0600` rather than an inline
  value);
- bind `127.0.0.1` and reach it over a tunnel or mesh interface, which needs no
  token;
- set `NPU_EMBEDDING_ALLOW_INSECURE_BIND=1` to accept an unauthenticated
  listener, only on a genuinely trusted segment.

A loud failure at install time beats an unauthenticated inference endpoint nobody
noticed.

### One sidecar serves exactly one model

A request naming a different model than the loaded one is refused with
`model_mismatch`, and the Go provider independently rejects a response whose
`model` differs from its configuration.

Neither check is belt-and-braces pedantry. A vector's meaning depends on the
model that produced it, and two models of the same width yield mutually
incomparable vectors — so serving the wrong one has no visible symptom until
retrieval quality quietly collapses. Run one sidecar per model.

## Pointing a tenant at it

> **Do not set `rag.embedding.provider: npu` yet.** go-qrdb rejects `npu` as a
> manager default, so that config does not fall back gracefully — it disables
> embedding support entirely. The daemon now detects this and logs a specific
> error naming the cause instead of a generic "provider unavailable" warning, but
> the right configuration today is below. See the limitation section.

Keep an accepted provider as the configured default and declare the NPU under
`additional`, which is what registers it:

```yaml
rag:
  enabled: true
  embedding:
    provider: ollama          # the manager default; must be one qrdb accepts
    api_url: http://127.0.0.1:11434
    model: nomic-embed-text
    dimensions: 768
    timeout: 30

    additional:
      - provider: npu
        api_url: http://npu-host.internal:8077
        model: all-MiniLM-L6-v2
        dimensions: 384
        timeout: 30
```

**The `additional` entry is not optional.** Only the configured default used to be
registered, so without it `npu` exists in the binary and is unreachable: both
selection paths below fail with provider-not-found. A provider listed here is
registered under its own name and can then be selected or promoted.

`dimensions` is shown above for clarity but may be omitted: an `additional`
entry that states no width gets its provider's native width, which for `npu` is
the 384 of `all-MiniLM-L6-v2`. Providers whose width follows the model the
operator names — Ollama, LM Studio, OpenAI, Voyage — keep the generic 768.
This distinction matters because the NPU provider cross-checks the width the
sidecar declares against its own configured width and rejects any response that
disagrees, so an entry defaulted to the wrong width would register cleanly and
then fail every single request.

A failure to register an `additional` provider is logged and skipped, never
fatal — an unreachable sidecar must not take down the embedding subsystem that
the default provider is serving perfectly well.

Then either select `npu` per request:

```json
{ "text": "embed me on the accelerator", "provider": "npu" }
```

or switch the service default, which applies to every request that does not name
a provider:

```
POST /api/v1/embeddings/providers/default   { "provider": "npu" }
```

Both paths are live: `GenerateEmbeddingRequest.Provider` is honoured, and
`SetDefaultProvider` actually takes effect. (Both were previously inert — the
first was recorded for metrics and ignored, the second returned success without
changing anything — which meant no non-default provider was reachable at all.)

`npu` is selectable on every transport. It has a gRPC enum value
(`EMBEDDING_PROVIDER_NPU`), so gRPC and SDK-over-gRPC callers can name it; REST
and MCP pass the provider as a string and needed no change beyond the MCP tool
descriptions. Without the enum value the conversion tables would have mapped
`npu` to `UNSPECIFIED`, which the service reads as "use the default" — so a
caller asking for the NPU would have been served by Ollama instead, silently.
The SDK guard is stricter still and rejects any non-empty provider that maps to
`UNSPECIFIED`, which would have made `npu` unreachable before a request even
left the client.

A request routed to a named provider bypasses the manager's cache, because the
cache belongs to the manager and is keyed for its default. Such a request is
always reported as a cache miss.

To take the NPU out of service — the sidecar is down, the model is being
re-exported — disable it:

```
POST /api/v1/embeddings/providers/status   { "name": "npu", "enabled": false }
```

A disabled provider is refused with `ProviderDisabledError`, which matches
`embedding.ErrProviderUnavailable`. It is **not** silently replaced by another
provider: a caller that asked for a specific provider gets an error rather than
a vector from somewhere else, because silently swapping the model behind an
embedding would poison a vector index with incomparable vectors. Requests that
name no provider are unaffected and keep using the default.

Disabling a provider also refuses making it the default, since it would be
excluded from routing the moment it was selected.

**Disabling the configured default works too.** Unnamed requests route through
the manager's cached default path, which names no provider, so the disabled check
has to resolve that name explicitly — otherwise disabling the provider that is
currently serving would record the intent and change nothing, which is the one
case an operator most needs during an outage. The lookup is skipped entirely when
nothing is disabled, so the common path costs nothing.

(`SetProviderStatus` previously recorded the disabled set without anything ever
reading it, so disabling a provider did nothing at all.)

Credentials should use an `inference.SecretRef` rather than a literal key:

```go
cfg := embedding.DefaultNPUConfig()
cfg.BaseURL = "http://npu-host.internal:8077"
cfg.APIKeyRef = inference.SecretRef{
    Engine: "secrets",
    Path:   "tenants/acme/inference/npu",
    Key:    "token",
}
cfg.Secrets = resolver

provider, err := embedding.NewNPUProvider(ctx, cfg)
```

### Current limitation: `npu` cannot be named in configuration

**Ollama remains the configured default** (`pkg/config/rag_config.go`), and today
that is not merely a preference.

go-qrdb's `validateManagerConfig` (`pkg/embedding/manager.go:83` in go-qrdb)
allowlists only `openai`, `ollama`, `lmstudio` and `voyage`, so
`embedding.NewManager(Config{Provider: "npu"})` fails with `ErrProviderInvalid`
*before* any provider is registered. Note the asymmetry:
`Manager.RegisterProvider` applies no such allowlist, so `npu` registers fine —
only naming it in configuration is blocked.

Because `startEmbedding` treats a manager-construction failure as "embedding is
optional" and continues, a config naming `npu` would have left embeddings
silently off, looking like an unreachable sidecar. The daemon now preflights the
provider name and logs the real cause, pointing at this section.

What this does **not** block: the provider still registers, and both runtime
selection paths above work, so the NPU is fully usable today. Only
`rag.embedding.provider: npu` waits on **go-qrdb#528**, which asks for that
allowlist to validate against the registry instead of a literal list.

## What happens when the sidecar is unreachable

Construction never dials, so **the daemon starts normally with the sidecar
down**. Failures surface per request as typed errors:

| Condition | Error | `errors.Is` |
| --- | --- | --- |
| Sidecar down, DNS failure, timeout | `*NPUUnavailableError` | `ErrNPUUnavailable` |
| Non-200 from the sidecar | `*NPUSidecarError` | `ErrNPUSidecar` |
| Undecodable body | `*NPUMalformedResponseError` | `ErrNPUMalformedResponse` |
| Wrong vector width | `*NPUDimensionsMismatchError` | `ErrDimensionsMismatch` |
| Wrong number of vectors | `*NPUResultCountError` | `ErrNPUMalformedResponse` |
| Empty or whitespace text | `*NPUInvalidInputError` | `ErrInvalidInput` |
| Bad configuration | `*NPUConfigError` | `ErrNPUEndpointRequired`, … |
| Credential resolution failed | `*NPUSecretResolutionError` | `ErrNPUSecretResolution` |
| Sidecar served a different model | `*NPUModelMismatchError` | `ErrNPUModelMismatch` |

An unreachable sidecar and a 5xx from one both also match
`embedding.ErrProviderUnavailable`, and a 429 matches
`embedding.ErrRateLimitExceeded`. That is deliberate: the service layer maps
those onto `ErrUnavailable` and `ErrRateLimited`, so a sidecar that is down or
shedding load is reported as a backend condition rather than as an internal
error. Without it a stopped sidecar would surface to API callers as a 500 and
mislead monitoring.

`NPUUnavailableError` unwraps to *both* its sentinel and the underlying
transport cause, so `errors.Is(err, context.Canceled)` works on a cancelled
request.

Empty input is deliberately asymmetric: `EmbedBatch(nil)` is valid and returns an
empty result **without contacting the sidecar**, while an empty *string* is
rejected before any request is made.

## Tests

```sh
make test-embedding            # Go provider, vector build, -race
make coverage-embedding        # 95.2% of pkg/embedding
make bench-embedding           # provider overhead only, excludes the device
make test-npu-sidecar          # 169 Python tests; no device needed
make coverage-npu-sidecar       # unit tier alone; combine with the device tier for 91%
make test-npu-sidecar-device   # 23 tests, real OpenVINO; installs the `device` extra
```

The sidecar targets bootstrap their own virtualenv at
`sidecar/npu-embedding/.venv` on first use, so a clean checkout needs no
Python packages installed by hand.

The device tier is a **separate suite**, not a skipped part of the first, so
nothing is ever silently not run. It passes on the CPU plugin, which verifies
every OpenVINO API call, the dynamic-to-static reshape, the compile cache, feeding
by input name and output selection. On the NPU host, run it with
`NPU_EMBEDDING_TEST_DEVICE=NPU` to cover the device itself.

Sidecar coverage is **91%** across both tiers: every pure module
(`bucket.py`, `pooling.py`, `service.py`, `app.py`, `errors.py`) is at 100%, and
`engine.py` at 89%. The unit tier alone reports 79%, because the OpenVINO paths
in `engine.py` are only reachable from the device tier — quote the combined
figure, not the first one.

The residual gaps are `__main__.py`'s serve loop and
`HuggingFaceTokenizer`'s body, which need `transformers` and a real model
directory; both have their failure contracts covered.

## Not in scope: generative models on the NPU

Deliberately excluded. Autoregressive decoding grows a KV cache by one position
per token, so cache tensors change shape every step — which sits badly with
mandatory static shapes, and none of the workarounds (pad-to-maximum, bucketed
cache lengths with recompiles, or OpenVINO stateful models) has been measured on
the NPU plugin.

Generation on this host is already served well by ollama on the RTX. See **#867**
for the measurement task that has to come before any implementation.
