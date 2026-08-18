# Credentials — CLI

> **Status: NORMATIVE** · Last verified: 2026-08-19
> Every verb and flag, with runnable examples. Feature folder: `docs/credentials/`.

Complete reference for the QuicBaaS credentials management CLI commands.

## Command Overview

```
quicbaas credentials <command> [subcommand] [flags]
```

| Command | Description |
|---------|-------------|
| `cert issue` | Issue a new certificate |
| `cert sign` | Sign a certificate signing request |
| `cert verify` | Verify a certificate |
| `cert revoke` | Revoke a certificate |
| `cert ca` | Get CA certificate |
| `cert list` | List issued certificates |

## Global Options

All credentials commands support:

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--output` | string | `table` | Output format: `json`, `yaml`, `table` |

---

## cert issue

Issue a new certificate with automatic key generation.

### Usage

```bash
quicbaas credentials cert issue <common-name> [flags]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `common-name` | Yes | Certificate common name (e.g., `my-service`) |

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--usage` | string | No | `client` | Certificate usage: `client`, `server`, `both` |
| `--validity` | int | No | 365 | Validity in days |
| `--dns` | string | No | - | DNS SANs (comma-separated) |
| `--ip` | string | No | - | IP SANs (comma-separated) |
| `--email` | string | No | - | Email SANs (comma-separated) |
| `--uri` | string | No | - | URI SANs (comma-separated) |
| `--org` | string | No | - | Organization |
| `--ou` | string | No | - | Organizational unit |
| `--country` | string | No | - | Country code (2 letters) |
| `--province` | string | No | - | State or province |
| `--locality` | string | No | - | City or locality |
| `--algorithm` | string | No | `ecdsa` | Key algorithm: `ecdsa`, `rsa` |
| `--key-size` | int | No | 256 | Key size (256/384/521 for ECDSA, 2048/4096 for RSA) |
| `--out-cert` | string | No | - | Write certificate to file |
| `--out-key` | string | No | - | Write private key to file |
| `--out-chain` | string | No | - | Write full chain to file |

### Examples

```bash
# Issue client certificate
quicbaas credentials cert issue my-client --usage client --validity 30

# Issue server certificate with DNS names
quicbaas credentials cert issue api-server \
  --usage server \
  --dns "api.example.com,api.internal.example.com" \
  --validity 90

# Issue certificate for both client and server auth
quicbaas credentials cert issue mtls-service \
  --usage both \
  --dns "service.local" \
  --ip "10.0.0.5,192.168.1.100"

# Issue with full subject
quicbaas credentials cert issue web-server \
  --usage server \
  --org "Acme Corp" \
  --ou "Engineering" \
  --country "US" \
  --province "California" \
  --locality "San Francisco" \
  --dns "www.acme.com"

# Issue with RSA key
quicbaas credentials cert issue legacy-client \
  --usage client \
  --algorithm rsa \
  --key-size 2048

# Write to files
quicbaas credentials cert issue my-service \
  --usage both \
  --out-cert ./service.crt \
  --out-key ./service.key \
  --out-chain ./chain.pem

# Output as JSON
quicbaas credentials cert issue my-client --output json
```

### Output

**Table format:**
```
SERIAL NUMBER    COMMON NAME    NOT BEFORE              NOT AFTER               USAGE
7890123456789    my-client      2024-01-15T10:30:00Z    2025-01-15T10:30:00Z    client

Certificate written to: ./service.crt
Private key written to: ./service.key
```

**JSON format:**
```json
{
  "certificate_pem": "-----BEGIN CERTIFICATE-----\n...",
  "private_key_pem": "-----BEGIN EC PRIVATE KEY-----\n...",
  "ca_certificate_pem": "-----BEGIN CERTIFICATE-----\n...",
  "chain_pem": "-----BEGIN CERTIFICATE-----\n...",
  "serial_number": "7890123456789",
  "not_before": "2024-01-15T10:30:00Z",
  "not_after": "2025-01-15T10:30:00Z"
}
```

---

## cert sign

Sign a Certificate Signing Request (CSR).

### Usage

```bash
quicbaas credentials cert sign --csr <path> [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--csr` | string | Yes | - | Path to CSR file (PEM format) |
| `--usage` | string | No | `client` | Certificate usage: `client`, `server`, `both` |
| `--validity` | int | No | 365 | Validity in days |
| `--out` | string | No | - | Write certificate to file |

### Examples

```bash
# Sign a CSR
quicbaas credentials cert sign --csr ./request.csr --validity 30

# Sign for server usage
quicbaas credentials cert sign --csr ./server.csr --usage server --validity 90

# Sign and write to file
quicbaas credentials cert sign --csr ./request.csr --out ./signed.crt

# Output as JSON
quicbaas credentials cert sign --csr ./request.csr --output json
```

### Output

**Table format:**
```
SERIAL NUMBER    COMMON NAME      NOT BEFORE              NOT AFTER
1234567890123    requested-cn     2024-01-15T10:30:00Z    2024-04-15T10:30:00Z
```

---

## cert verify

Verify a certificate against the CA.

### Usage

```bash
quicbaas credentials cert verify --cert <path> [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--cert` | string | Yes | - | Path to certificate file (PEM format) |

### Examples

```bash
# Verify a certificate
quicbaas credentials cert verify --cert ./certificate.pem

# Output as JSON
quicbaas credentials cert verify --cert ./certificate.pem --output json
```

### Output

**Valid certificate:**
```
VALID    COMMON NAME    SERIAL NUMBER    EXPIRES
true     my-service     7890123456789    2025-01-15T10:30:00Z
```

**Invalid certificate:**
```
VALID    ERROR
false    certificate has been revoked
```

**JSON format:**
```json
{
  "valid": true
}
```

```json
{
  "valid": false,
  "error_message": "certificate has been revoked"
}
```

---

## cert revoke

Revoke a certificate by serial number.

### Usage

```bash
quicbaas credentials cert revoke --serial <serial-number> [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--serial` | string | Yes | - | Certificate serial number |

### Examples

```bash
# Revoke by serial number
quicbaas credentials cert revoke --serial 7890123456789

# Output as JSON
quicbaas credentials cert revoke --serial 7890123456789 --output json
```

### Output

**Table format:**
```
SERIAL NUMBER    STATUS
7890123456789    revoked
```

**JSON format:**
```json
{
  "success": true
}
```

---

## cert ca

Get the CA certificate.

### Usage

```bash
quicbaas credentials cert ca [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--bundle` | bool | No | `false` | Include full CA bundle/chain |
| `--out` | string | No | - | Write to file |

### Examples

```bash
# Get CA certificate
quicbaas credentials cert ca

# Get full CA bundle
quicbaas credentials cert ca --bundle

# Write to file
quicbaas credentials cert ca --out ./ca.crt

# Write bundle to file
quicbaas credentials cert ca --bundle --out ./ca-bundle.pem

# Output as JSON
quicbaas credentials cert ca --output json
```

### Output

**Table format:**
```
IDENTITY        SUBJECT
quicbaas-ca     CN=QuicBaaS CA,O=My Organization

-----BEGIN CERTIFICATE-----
MIIBkTCB+wIJAK...
-----END CERTIFICATE-----
```

**JSON format:**
```json
{
  "ca_certificate_pem": "-----BEGIN CERTIFICATE-----\n...",
  "ca_bundle_pem": "-----BEGIN CERTIFICATE-----\n...",
  "identity": "quicbaas-ca"
}
```

---

## cert list

List issued certificates (if tracking is enabled).

### Usage

```bash
quicbaas credentials cert list [flags]
```

### Flags

| Flag | Type | Required | Default | Description |
|------|------|----------|---------|-------------|
| `--include-revoked` | bool | No | `false` | Include revoked certificates |
| `--limit` | int | No | 100 | Maximum number of results |

### Examples

```bash
# List all active certificates
quicbaas credentials cert list

# Include revoked certificates
quicbaas credentials cert list --include-revoked

# Limit results
quicbaas credentials cert list --limit 10

# Output as JSON
quicbaas credentials cert list --output json
```

### Output

**Table format:**
```
SERIAL NUMBER      COMMON NAME       USAGE     NOT AFTER               STATUS
7890123456789      api-server        server    2025-01-15T10:30:00Z    active
1234567890123      my-client         client    2024-06-15T10:30:00Z    active
9876543210987      old-service       both      2024-01-01T00:00:00Z    revoked
```

---

## Workflow Examples

### Generate mTLS Certificates for a Service

```bash
# 1. Get the CA certificate for trust
quicbaas credentials cert ca --out ./ca.crt

# 2. Issue server certificate
quicbaas credentials cert issue my-service-server \
  --usage server \
  --dns "my-service.local,my-service.svc.cluster.local" \
  --ip "10.0.0.5" \
  --validity 90 \
  --out-cert ./server.crt \
  --out-key ./server.key

# 3. Issue client certificate
quicbaas credentials cert issue my-service-client \
  --usage client \
  --validity 30 \
  --out-cert ./client.crt \
  --out-key ./client.key
```

### Sign External CSR

```bash
# 1. Generate CSR externally (e.g., with openssl)
openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 \
  -nodes -keyout client.key -out client.csr \
  -subj "/CN=external-client/O=External Org"

# 2. Sign with QuicBaaS CA
quicbaas credentials cert sign \
  --csr ./client.csr \
  --usage client \
  --validity 30 \
  --out ./client.crt

# 3. Verify the signed certificate
quicbaas credentials cert verify --cert ./client.crt
```

### Certificate Rotation

```bash
# 1. Issue new certificate before old one expires
quicbaas credentials cert issue my-service \
  --usage server \
  --dns "my-service.local" \
  --validity 90 \
  --out-cert ./new-server.crt \
  --out-key ./new-server.key

# 2. Deploy new certificate to service

# 3. Revoke old certificate (after confirming new one is working)
quicbaas credentials cert revoke --serial <old-serial-number>
```


---

## quicbaas credential

Post-init credential operations: submit, list, rotate, retrieve,
versions, delete, gc.

### Overview

The `credential` verb tree manages QuicBaaS's named credential
service. All seven verbs delegate to `Security().Credentials()` over
the active SDK transport — every transport (REST, gRPC, QUIC, MCP,
Unix, embedded) ships a real-wire implementation. There is no
`PendingCredentialBackendError` or `NotImplementedError` anywhere on
this path.

Source: [`cmd/quicbaas/credential.go`](../../cmd/quicbaas/credential.go).

For SDK-level docs (versioned-rotation semantics, grace period,
storage layout) see
[`docs/sdk/credentials.md`](engineering.md).

### Verb summary

| Verb | Versioned? | Purpose |
|------|------------|---------|
| `submit <name> --value <v>` | No | Non-versioned set/replace |
| `list` | n/a | Enumerate credential names (values never returned) |
| `rotate <name> --value <v> [--grace <dur>]` | Yes | Atomic versioned rotation |
| `retrieve <name> [--version N]` | Yes | Read current or specific version |
| `versions <name>` | Yes | Show metadata for current + non-expired grace versions |
| `delete <name>` | n/a | Wipe credential + all historical versions |
| `gc <name>` | Yes | Reap expired grace versions |

### submit

Submit a credential value under the given name. Non-versioned: replaces
the existing value (if any) immediately. The manual strategy notifies
any blocked retrievers; the barrier strategy persists the value via
the StorageBarrier.

```bash
quicbaas credential submit my-key --value s3cr3t
```

| Flag | Default | Description |
|------|---------|-------------|
| `--value <string>` | (required) | Credential value |

Output:

```
credential "my-key" submitted
```

For a versioned rotation that keeps the previous value retrievable,
use `rotate`.

### list

Enumerate every stored credential by name. Values are never returned.

```bash
quicbaas credential list
quicbaas credential list --output json
```

| Flag | Default | Description |
|------|---------|-------------|
| `--output <format>` | `table` | `json` or `table` |

Table output: one name per line, or `(no credentials)` for an empty
store.

Backends that cannot enumerate (future HSM-only strategies) return
an enumeration-not-supported error.

### rotate

Versioned rotation: atomically replace the current value of `<name>`
with `--value`. By default the previous value is wiped immediately.
Use `--grace` to keep it retrievable for a grace window before
automatic reaping.

```bash
quicbaas credential rotate my-key --value newSecret --grace 24h
```

| Flag | Default | Description |
|------|---------|-------------|
| `--value <string>` | (required) | New credential value |
| `--grace <duration>` | `0` (wipe immediately) | Grace period during which the previous version remains retrievable. Accepts Go duration strings (`30s`, `5m`, `24h`, `168h`). |
| `--output <format>` | `table` | `json` or `table` |

Output (table):

```
rotated my-key: previous v1 -> new v2 (previous version retrievable until 2026-05-26T14:32:11Z)
```

When `--grace 0` (or omitted) the parenthetical clause is dropped.

### retrieve

Read a credential value. Defaults to the current version; pass
`--version N` to read a specific historical version (must still be
within its grace window).

```bash
quicbaas credential retrieve my-key             # current value
quicbaas credential retrieve my-key --version 2 # historical v2
```

| Flag | Default | Description |
|------|---------|-------------|
| `--version <uint>` | `0` (current) | Specific version to retrieve |

Output: the raw value on its own line (so the CLI can be used in
pipelines: `quicbaas credential retrieve db-password | mysql -p...`).
Use the JSON output of `rotate` / `versions` for metadata.

### versions

Show every non-expired version of `<name>`. Output is metadata only —
no values.

```bash
quicbaas credential versions my-key
```

Table output:

```
VERSION    CURRENT   CREATED                   GRACE-UNTIL
2          yes       2026-05-25T14:32:11Z      -
1          no        2026-05-24T09:14:22Z      2026-05-26T14:32:11Z
```

Use `quicbaas credential retrieve --version N` to read a specific
value.

### delete

Delete a credential and all of its non-current historical versions.

```bash
quicbaas credential delete my-key
```

Idempotent — deleting a non-existent name is a no-op.

Output:

```
credential "my-key" deleted
```

### gc

Reap historical versions whose grace period has elapsed. Backends
also reap opportunistically during `retrieve` and `versions` traffic;
explicit `gc` exists so operators can force the wipe ahead of an
audit and confirm the count.

```bash
quicbaas credential gc my-key
```

Output:

```
reaped 2 expired version(s)
```

### Error types

| Type | When |
|------|------|
| `CredentialNameRequiredError` | Verb invoked without `<name>` |
| `CredentialValueRequiredError` | `submit` / `rotate` invoked without `--value` |
| (from qrdb) `CredentialNotFoundError` | `retrieve` / `delete` against an unknown name |
| (from qrdb) `CredentialVersionExpiredError` | `retrieve --version N` where N has expired |
| (from qrdb) `CredentialGraceExceededError` | `rotate --grace` above the server-side cap |

### Common patterns

#### Provision a service password

```bash
PASSWORD=$(openssl rand -base64 24)
quicbaas credential submit db-password --value "$PASSWORD"
```

#### Rolling rotation with grace

```bash
NEW_PASSWORD=$(openssl rand -base64 24)
quicbaas credential rotate db-password --value "$NEW_PASSWORD" --grace 24h

# ... later, after workers have drained the old value ...
quicbaas credential gc db-password
```

#### Audit current state

```bash
quicbaas credential list
quicbaas credential versions db-password --output json
```

#### Programmatic pipeline

```bash
PASS=$(quicbaas credential retrieve db-password)
echo "$PASS" | mysql -h db.internal -u service -p
```

### Transport coverage

All seven verbs work on every transport. See the matrix in
[`docs/sdk/credentials.md`](engineering.md#transport-coverage).

### See also

- [`docs/sdk/credentials.md`](engineering.md) — full SDK
  contract, including version metadata, storage layout, and the
  versioned-rotation lifecycle diagram.
- [`docs/sdk/security.md`](../ssh/engineering.md) —
  `Security().Credentials()` accessor.
- [`docs/credentials/README.md`](user.md) — the
  broader credentials engine (CA, database creds, dynamic secrets).
