# Cloud / Hardware Barrier Seal Integration Tests

End-to-end integration tests for the cloud- and hardware-backed barrier
sealing strategies that go-xkms supplies and quicbaas composes through the
build-tagged seal factories (`pkg/server/seal_factory_<backend>.go`).

Each test boots a fresh `quicbaasd` configured with one cloud strategy
pointed at an emulator, then drives the canonical barrier lifecycle through
the CLI:

```
barrier init  ->  barrier status  ->  barrier seal  ->  barrier unseal
```

and asserts the seal blob round-trips through the KMS (the barrier comes back
unsealed). The daemon binary under test MUST be built with the matching seal
build tag so the factory is compiled in — `make integration-test-seal` does
this (`-tags 'awskms vault'`).

## How a cloud strategy is selected at runtime

The CLI `barrier init --barrier-strategy` flag only accepts
`none|software|shamir` (client-side validation). Cloud strategies are
selected at the **daemon** level via config (`cfg.Seal.Strategy`), not the
CLI. The harness:

1. runs `quicbaasd init --barrier-strategy none` (cheap first-boot bootstrap;
   writes a `.quicbaasd_init_done` marker recording strategy=none), then
2. runs `quicbaasd start --config seal.yaml` with the seal YAML selecting the
   cloud strategy + `QUICBAAS_SEAL_STRATEGY=<id>` in the env. The env flips
   `cfg.Seal.StrategyExplicit` on (`resolveBarrierStrategy` precedence) so the
   configured cloud strategy wins over the "none" init marker.

The server's `BarrierInitialize(ctx, secret)` ignores the request strategy and
the secret; it uses whatever strategy the data-plane `StorageBarrier` was
built with (`bestStrategy()` picks the single registered cloud strategy). The
`CloudKMSStrategy` seals/unseals the root key autonomously via the KMS — no
passphrase.

## Backend status

| Backend  | Tag       | Emulator                         | Status |
|----------|-----------|----------------------------------|--------|
| awskms   | `awskms`  | Floci KMS                        | docker-emulatable |
| vault    | `vault`   | HashiCorp Vault dev server       | docker-emulatable |
| gcpkms   | `gcpkms`  | none faithful                    | needs real GCP |
| azurekv  | `azurekv` | none faithful                    | needs real Azure |
| pkcs11   | `pkcs11`  | SoftHSM2 (CGO)                   | hardware follow-up |
| tpm2     | (none)    | swtpm + tpm_simulator            | hardware follow-up |

### awskms — Floci KMS

`awskms_test.go` creates a symmetric AES-256 CMK in Floci via the AWS SDK,
then points `seal.awskms.endpoint` at Floci. Pure docker, no real
credentials (`AWS_ACCESS_KEY_ID=test`). Floci is MIT-licensed and needs no
auth token. Compose service: `floci`
(`SERVICES=kms`).

### vault — HashiCorp Vault dev server

`vault_test.go` enables the Transit engine and creates an `aes256-gcm96`
Transit key on a `vault server -dev` instance, then points `seal.vault.address`
at it. Pure docker, fixed root token. Compose service: `vault`.

### gcpkms — needs real GCP

There is no faithful local Google Cloud KMS emulator: `fake-gcs-server`
emulates **Cloud Storage object** operations only, not the **Cloud KMS**
`Encrypt`/`Decrypt` crypto-key operations the gcpkms seal backend invokes. To
test against real GCP:

1. Create a key ring + AES symmetric crypto key in Cloud KMS.
2. Provide ADC credentials (`GOOGLE_APPLICATION_CREDENTIALS` -> a service-
   account JSON with `cloudkms.cryptoKeyVersions.useToEncrypt/Decrypt`).
3. Set `seal.gcpkms.{project_id, location_id, key_ring_id, key_id}` and run
   the daemon with `-tags gcpkms`.

A `gcpkms_test.go` modeled on `awskms_test.go` would slot in once a real
project is wired into CI.

### azurekv — needs real Azure

Azurite emulates Azure **Blob/Queue/Table storage** only, not **Key Vault**
key-wrap operations. To test against real Azure Key Vault:

1. Create a Key Vault + an RSA/AES key.
2. Provide a service principal (`AZURE_TENANT_ID`/`AZURE_CLIENT_ID`/
   `AZURE_CLIENT_SECRET`) or a managed identity with `wrapKey`/`unwrapKey`.
3. Set `seal.azurekv.{vault_url, key_name}` and run with `-tags azurekv`.

### pkcs11 — SoftHSM2 (CGO, hardware follow-up)

The pkcs11 backend needs CGO + a PKCS#11 provider shared object. Exact image
additions for `Dockerfile.seal`:

```dockerfile
# Runtime libs:
RUN apk add --no-cache softhsm opensc      # (alpine) — or `softhsm2 opensc` on debian
# Build with CGO + the pkcs11 tag:
RUN CGO_ENABLED=1 go build -tags 'pkcs11 ...' -o bin/quicbaasd ./cmd/quicbaasd
```

Setup must initialise a token before the daemon starts:

```sh
softhsm2-util --init-token --slot 0 --label barrier --pin 1234 --so-pin 5678
```

then set `seal.pkcs11.{library: /usr/lib/softhsm/libsofthsm2.so,
token_label: barrier, pin: 1234, key_label: barrier-root}`.

### tpm2 — swtpm software TPM (hardware follow-up)

The tpm2 strategy is build-tag-free but the simulator path needs the go-xkms
`tpm_simulator` tag and a running software TPM. Exact additions:

```dockerfile
RUN apk add --no-cache swtpm tpm2-tools
# Build with the simulator tag:
RUN go build -tags 'tpm_simulator ...' -o bin/quicbaasd ./cmd/quicbaasd
```

Setup runs `swtpm socket --tpmstate dir=/tmp/swtpm --server type=tcp,port=2321
--ctrl type=tcp,port=2322 --tpm2 &`, provisions it, and sets
`seal.tpm2.{use_simulator: true, simulator_host: localhost, simulator_port:
2321}`.

## Running

```sh
make integration-test-seal
# or directly with emulators up:
go test -v -tags 'integration awskms vault' ./test/integration/seal/...
```

`make integration-test-seal` builds the seal-tagged daemon + CLI into the
`seal-test` image, starts the `floci` + `vault` emulators, and runs the
suite against them — no real cloud credentials required.

## KNOWN BLOCKER (cloud factories)

The cloud seal factories currently construct their go-xkms backend with a
**fresh, empty in-process key-metadata store** (`storage.New()`) and never
register the operator's pre-existing cloud key. At runtime,
`CloudKMSStrategy.SealRootKey -> Backend.Seal -> Backend.SymmetricEncrypter`
requires the key's metadata to be present in that in-process store, so
`barrier init` fails with `ErrKeyNotFound` before any KMS call. The unit
round-trip tests in `pkg/server` only pass because they explicitly call
`backend.GenerateSymmetricKey(attrs)` (vault) / `backend.Save(...)` (awskms)
first — a step the production factory omits.

These tests are therefore **written and ready** but will fail end-to-end until
the factory wiring is fixed (the fix is an architecture decision — auto-
provision vs. register-existing, and idempotency across restarts — and must be
approved before implementing). See the parent report for the exact one-call
fix per backend.
