# Canonical `quicbaasd init` Matrix Integration Tests

`test/integration/init` is the canonical "init + start + transport" matrix
covering every supported combination of barrier strategy and listener
transport for the `quicbaasd` binary per
[`docs/08-bootstrap-auth-architecture.md`](../../docs/08-bootstrap-auth-architecture.md) §6.

## What it covers

| Axis              | Values                                       | Source of truth                          |
|-------------------|----------------------------------------------|------------------------------------------|
| Barrier strategy  | `none`, `software`, `shamir`                 | `cmd/quicbaasd/init.go`, `canonical.go`  |
| Transport         | `rest`, `grpc`, `quic`, `mcp`, `unix`        | `pkg/config/defaults.go` listener table  |

Total = **3 × 5 = 15** software-only combinations. Hardware strategies
(`pkcs11`, `tpm2`, `awskms`, `gcpkms`, `azurekv`, `vault`) are
deliberately out of scope here — they belong in dedicated env-gated
suites that require operator-supplied hardware. They are exercised at
the CLI surface only via `TestCanonicalInit_UnsupportedHardwareGracefulSkip`
to confirm the daemon emits a typed `UnsupportedBarrierStrategyError`.

## Per-cell flow

Every `(strategy, transport)` cell drives the daemon through the full
production code path:

1. **Init** — `quicbaasd init --barrier-strategy <s> --data-dir <tmpdir>`
   (PINs only for `software`/`shamir`; `none` rejects PINs).
2. **Marker assertion** — `<dataDir>/.quicbaasd_init_done` is read and
   compared against the expected strategy / officer count / shares /
   threshold tuple.
3. **Pending ceremony assertion** (shamir only) —
   `<dataDir>/pending_ceremony.json` is parsed and validated.
4. **Daemon start** — `quicbaasd start --data-dir <tmpdir>
   --http-port <p> ...` with per-subtest non-overlapping ports.
5. **Readiness** — poll `https://127.0.0.1:<rest>/api/v1/health/ready`
   AND probe the transport-under-test's listener (TCP open / TLS
   handshake / UDP write-read for QUIC / Unix socket connect). QUIC is
   probed on the REST port: the daemon serves it as REST over HTTP/3
   there and binds no other QUIC port. The
   daemon's CA chain must land on disk before the cell is considered
   ready.
6. **PKI post-conditions** — `<dataDir>/certs/ca-chain.crt` and
   `server.crt` must exist (proof the xkms-backed CA bootstrap ran).
7. **SDK over the selected transport** —
   `client.Transport().Request(ctx, "quicbaas.Health", "Liveness", ...)`
   round-trips. A successful response proves the SDK transport stack
   lines up with the daemon's listener.
8. **Cross-strategy marker invariants** — `credential_strategy` defaults
   to `barrier`; `completed_at` is non-zero.
9. **Clean stop** — `SIGTERM` the daemon, wait for graceful drain
   (force-kill after 20s). The data dir's PebbleDB lock is released
   before the next subtest runs.

## How to run

```bash
INTEGRATION_TEST=1 make integration-test-init
```

The Makefile target rebuilds `quicbaasd` with the correct tags
(`hashicorpmetrics local iam raft rest quic mcp`) and exports
`QUICBAASD_BINARY` so the test discovers the freshly-built binary
deterministically.

Direct `go test` invocation:

```bash
INTEGRATION_TEST=1 QUICBAASD_BINARY=$(pwd)/bin/quicbaasd \
    go test -v -tags integration -timeout 5m -count 1 \
    ./test/integration/init/...
```

### Environment variables

| Variable                          | Purpose                                                                            |
|-----------------------------------|------------------------------------------------------------------------------------|
| `INTEGRATION_TEST` (required)     | Project-wide opt-in gate. Tests fail fast when unset.                              |
| `QUICBAASD_BINARY` (optional)     | Absolute path to `quicbaasd`. Defaults to `<repo>/bin/quicbaasd`.                  |
| `QUICBAAS_TEST_TPM2_SOCKET`       | When set, `TestCanonicalInit_UnsupportedHardwareGracefulSkip` skips for TPM2.      |
| `QUICBAAS_TEST_SOFTHSM2_MODULE`   | When set, `TestCanonicalInit_UnsupportedHardwareGracefulSkip` skips for SoftHSM.   |

## Why a combinatorial matrix?

The init flow's contract is "drive the binary to a known operational
state from a single command, against any supported barrier strategy,
listening on any supported transport". Each `(strategy, transport)` cell
is its own integration boundary:

- Strategies select different bootstrap code paths inside the daemon
  (embedded-server-with-seal vs embedded-server-without-seal vs
  pending-ceremony-state). Coverage across all three rules out a class
  of bugs where a marker write only succeeds for one strategy.
- Transports each have an independent listener stack (TCP vs UDP vs
  AF_UNIX). A regression in any listener (e.g. the daemon accidentally
  binding a different port than env-configured) only surfaces when we
  drive it column-by-column.

The two axes multiply because they fail independently and no single
subset proves the combined behaviour.

## Hermeticity guarantees

- Every subtest gets its own `t.TempDir()` data directory. The
  PebbleDB instance, xkms key material, CA chain, server cert, and Unix
  socket all live under the per-subtest tmpdir.
- Ports are allocated via the kernel's "bind to 0" trick with a
  global de-duplication set so parallel siblings cannot collide.
- The Unix socket is the gRPC anchor for transports that do not carry
  gRPC natively. Path basename is `d.sock` so the absolute path stays
  under the AF_UNIX 108-byte limit.
- Daemons are reaped via `t.Cleanup` so panicking or fatal-failing
  cells cannot leak processes that would poison sibling subtests on
  the next run.

## File layout

| File                       | Responsibility                                                          |
|----------------------------|-------------------------------------------------------------------------|
| `init_matrix_test.go`      | The matrix itself (`TestCanonicalInit_Matrix`) + negative tests.        |
| `helpers.go`               | Daemon supervision, port allocation, transport probes, marker parsing.  |
| `sdk_verify.go`            | SDK-based per-transport `Health.Liveness` verification.                 |
| `officer_material.go`      | Synthetic CSR / cert / pubkey generators for the shamir rows.           |
| `exec_init.go`             | Raw `exec.Command` helpers shared between matrix and negative tests.    |
