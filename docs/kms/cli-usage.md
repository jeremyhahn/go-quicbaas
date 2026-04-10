# KMS CLI Usage

## Command Structure

The KMS CLI is organized into logical command groups:

```
quicbaas kms key       # User-facing encryption keys
quicbaas kms encrypt   # Encrypt data
quicbaas kms decrypt   # Decrypt data
quicbaas kms backend   # Backend info (read-only)
quicbaas kms safety    # AEAD safety monitoring
```

## Key Management (`quicbaas kms key`)

Master keys for signing, key agreement, and DEK protection. Keys support hierarchical paths for multi-tenant isolation.

### Generate Key

```bash
# Generate with default algorithm (ED25519 - asymmetric)
quicbaas kms key generate my-key

# Generate with specific asymmetric algorithm
quicbaas kms key generate my-key --algorithm ECDSA-P256
quicbaas kms key generate my-key --algorithm RSA-2048

# Generate with symmetric algorithm
quicbaas kms key generate my-key --algorithm AES-256-GCM

# Generate with description
quicbaas kms key generate my-key --description "Production signing key"

# Generate exportable key (for BYOK scenarios)
quicbaas kms key generate transport-key --exportable

# Generate in tenant namespace
quicbaas kms key generate tenant-123/prod/signing-key
quicbaas kms key generate tenant-123/staging/api-key

# Generate with metadata
quicbaas kms key generate high-volume-key \
  --description "High volume signing key" \
  --exportable
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--algorithm` | Key algorithm (case-insensitive) | ED25519 |
| `--description` | Key description | - |
| `--exportable` | Allow key export | false |

**Supported Algorithms:**
| Algorithm | Type | Use Case |
|-----------|------|----------|
| `ED25519` | Asymmetric | Default - fast signing, key agreement |
| `ECDSA-P256` | Asymmetric | Signing, NIST compliance |
| `ECDSA-P384` | Asymmetric | Signing, higher security |
| `ECDSA-P521` | Asymmetric | Signing, maximum security |
| `RSA-2048` | Asymmetric | Signing, encryption, legacy compat |
| `RSA-3072` | Asymmetric | Signing, encryption, balanced security |
| `RSA-4096` | Asymmetric | Signing, encryption, max security |
| `AES-128-GCM` | Symmetric | Fast encryption |
| `AES-192-GCM` | Symmetric | Balanced encryption |
| `AES-256-GCM` | Symmetric | Strong encryption (recommended) |
| `CHACHA20-POLY1305` | Symmetric | Modern stream cipher |
| `XCHACHA20-POLY1305` | Symmetric | Extended nonce stream cipher |

### Get Key

```bash
# Get key info
quicbaas kms key get my-key

# Get specific version
quicbaas kms key get my-key --version 2
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--version` | Key version (0 for current) | 0 |

**Output:**
```
Key ID:       my-key
Name:         my-key
Algorithm:    Ed25519
Version:      3
Status:       enabled
Exportable:   false
Created:      2024-01-15T10:30:00Z
Updated:      2024-02-15T14:22:00Z
```

### List Keys

```bash
# List all keys
quicbaas kms key list

# List keys with prefix
quicbaas kms key list tenant-123/

# Limit results
quicbaas kms key list --limit 50
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--limit` | Maximum number of keys to return | 100 |

**Output:**
```
KEY ID                        ALGORITHM            VERSION  STATUS           CREATED
------------------------------  --------------------  -------  ---------------  --------------------
my-key                        Ed25519              3        enabled          2024-01-15T10:30:00Z
tenant-123/prod/signing       Ed25519              1        enabled          2024-01-20T09:15:00Z
tenant-123/prod/ecdsa-key     ECDSA-P256           2        enabled          2024-02-01T12:00:00Z
tenant-123/staging/rsa-key    RSA-2048             1        disabled         2024-02-10T08:30:00Z

Total: 4 keys
```

### Rotate Key

```bash
# Rotate key (creates new version)
quicbaas kms key rotate my-key
```

Old versions are retained for decryption of existing ciphertexts.

**Output:**
```
Key ID:       my-key
Old Version:  2
New Version:  3
Timestamp:    2024-02-15T14:22:00Z
```

### Delete Key

```bash
# Delete key (30-day pending deletion)
quicbaas kms key delete my-key

# Skip confirmation
quicbaas kms key delete my-key --force

# Immediate deletion (bypasses pending period)
quicbaas kms key delete my-key --force --purge
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--force` | Skip confirmation | false |
| `--purge` | Immediate deletion (requires --force) | false |

### Enable/Disable Key

```bash
# Disable key (prevents encrypt/decrypt)
quicbaas kms key disable my-key

# Re-enable key
quicbaas kms key enable my-key
```

**Output:**
```
Key ID:       my-key
Old Status:   enabled
New Status:   disabled
Timestamp:    2024-02-15T14:22:00Z
```

### List Versions

```bash
# List all versions
quicbaas kms key versions my-key

# Limit results
quicbaas kms key versions my-key --limit 10
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--limit` | Maximum number of versions to return | 100 |

**Output:**
```
VERSION  STATUS           ALGORITHM            CREATED
-------  ---------------  --------------------  --------------------
3        current          Ed25519              2024-02-15T14:22:00Z
2        rotated          Ed25519              2024-01-20T09:15:00Z
1        rotated          Ed25519              2024-01-15T10:30:00Z

Total: 3 versions
```

### Import Key (BYOK)

```bash
# Import from file
quicbaas kms key import my-imported-key \
  --material /path/to/key.bin \
  --algorithm AES-256-GCM

# Import from base64 string
quicbaas kms key import my-imported-key \
  --material base64:SGVsbG8gV29ybGQh... \
  --algorithm AES-256-GCM

# Import with wrapping key (for encrypted key material)
quicbaas kms key import my-imported-key \
  --material /path/to/wrapped-key.bin \
  --algorithm AES-256-GCM \
  --wrapping-key transport-key
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--material` | Key material file path or base64: prefixed string | (required) |
| `--algorithm` | Key algorithm | (required) |
| `--description` | Key description | - |
| `--wrapping-key` | Wrapping key ID for encrypted key material | - |

### Export Key

Export a key wrapped with another key for secure transport.

```bash
# Export wrapped with AES key
quicbaas kms key export my-key \
  --wrapping-key transport-key \
  --wrapping-algorithm AES-KW

# Export wrapped with RSA public key
quicbaas kms key export my-key \
  --wrapping-key rsa-transport \
  --wrapping-algorithm RSA-OAEP-SHA256

# Export as JWK format
quicbaas kms key export my-key \
  --wrapping-key transport \
  --wrapping-algorithm AES-KW \
  --format jwk \
  --output key.jwk
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--wrapping-key` | Wrapping key ID | (required) |
| `--wrapping-algorithm` | Wrapping algorithm | AES-KW |
| `--format` | Export format (raw, jwk) | raw |
| `--output` | Output file path (optional, default: stdout) | - |

**Wrapping Algorithms:**
| Algorithm | Description | Wrapping Key Type |
|-----------|-------------|-------------------|
| `AES-KW` | RFC 3394 AES Key Wrap | AES symmetric |
| `AES-KWP` | RFC 5649 AES Key Wrap with Padding | AES symmetric |
| `AES-GCM` | AES-GCM envelope encryption | AES symmetric |
| `RSA-OAEP-SHA256` | RSA-OAEP with SHA-256 | RSA public key |
| `RSA-OAEP-SHA384` | RSA-OAEP with SHA-384 | RSA public key |
| `RSA-OAEP-SHA512` | RSA-OAEP with SHA-512 | RSA public key |
| `ECDH-ES+A256KW` | ECDH-ES with AES-256 Key Wrap | ECDH key pair |

### Unwrap Key

Import a previously exported (wrapped) key.

```bash
quicbaas kms key unwrap imported-key \
  --wrapping-key transport-key \
  --wrapping-algorithm AES-KW \
  --material /path/to/wrapped-key.bin \
  --algorithm AES-256-GCM
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--wrapping-key` | Wrapping key ID | (required) |
| `--wrapping-algorithm` | Wrapping algorithm | AES-KW |
| `--material` | Wrapped key material file path | (required) |
| `--algorithm` | Key algorithm | (required) |

**Output:**
```
Key ID:       imported-key
Version:      1
Algorithm:    AES-256-GCM
Status:       enabled
Timestamp:    2024-02-15T14:22:00Z
```

### Set Safety Configuration

Update AEAD safety limits for a key.

```bash
# Set per-key limits
quicbaas kms key set-safety my-key \
  --max-operations 500000000 \
  --max-bytes 50GB \
  --warning-percent 70

# Clear per-key config (revert to server defaults)
quicbaas kms key set-safety my-key --clear
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--max-operations` | Maximum number of operations | 0 (server default) |
| `--max-bytes` | Maximum bytes (e.g., 50GB, 1TB) | - |
| `--warning-percent` | Warning threshold percentage | 0 (server default) |
| `--clear` | Clear per-key safety config | false |

## Crypto Operations

### Encrypt

```bash
# Encrypt with key
quicbaas kms encrypt my-key --data "secret value"

# With additional authenticated data
quicbaas kms encrypt my-key --data "secret" --aad "context-info"

# Base64 input
quicbaas kms encrypt my-key --data "SGVsbG8=" --base64
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--data` | Data to encrypt | (required) |
| `--aad` | Additional authenticated data | - |
| `--base64` | Data is base64 encoded | false |

**Output (table format):**
```
Key ID:             my-key
Key Version:        3
Ciphertext (hex):   a1b2c3d4e5f6...
Ciphertext (base64): obLD1OX2...
```

### Decrypt

```bash
# Decrypt hex-encoded ciphertext
quicbaas kms decrypt my-key --data "a1b2c3d4..."

# With AAD (must match encrypt)
quicbaas kms decrypt my-key --data "a1b2c3d4..." --aad "context-info"

# Base64 ciphertext input
quicbaas kms decrypt my-key --data "SGVsbG8=" --base64
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--data` | Data to decrypt (hex or base64) | (required) |
| `--aad` | Additional authenticated data | - |
| `--base64` | Data is base64 encoded (default: hex) | false |

**Output (table format):**
```
Key ID:       my-key
Key Version:  3
Plaintext:    secret value
```

## Backend Operations (`quicbaas kms backend`)

Read-only information about the configured backend.

### Backend Status

```bash
quicbaas kms backend status
quicbaas kms backend status --output json
```

**Output (TPM2):**
```
Backend:        tpm2
Device:         /dev/tpmrm0
Status:         healthy
PCR Banks:      sha256
PCR Registers:  0, 7, 9
Capabilities:
  Seal:         true
  Sign:         true
  Encrypt:      true
```

**Output (AWS KMS):**
```
Backend:        awskms
Region:         us-west-2
Key ID:         arn:aws:kms:us-west-2:123456789:key/abcd1234
Status:         healthy
Capabilities:
  Seal:         true
  Sign:         false
  Encrypt:      true
```

### Test Backend

```bash
# Performs seal/unseal test cycle
quicbaas kms backend test
quicbaas kms backend test --output json
```

**Output:**
```
Success:   true
Duration:  125ms
```

## Safety Monitoring (`quicbaas kms safety`)

### Safety Check

Show keys and DEKs approaching AEAD limits.

```bash
quicbaas kms safety check
quicbaas kms safety check --output json
```

**Output:**
```
TYPE   PATH                    USAGE%   STATUS
----   ----------------------  -------  ---------------
KEY    tenant-123/             85.2%    WARNING (> 80%)
KEY    tenant-123/high-volume  92.1%    CRITICAL (> 90%)
KEY    fast-key                78.5%    OK

Total Checked: 15 keys, 3 DEKs
```

### Safety Report

Full usage report for all keys and DEKs.

```bash
# Table format
quicbaas kms safety report

# JSON format
quicbaas kms safety report --output json

# Prometheus metrics
quicbaas kms safety report --format prometheus
```

**Flags:**
| Flag | Description | Default |
|------|-------------|---------|
| `--format` | Report format (table, json, prometheus) | table |

**Output (table format):**
```
Server Configuration:
  Enforce Limits:       true
  Max Operations:       4294967296
  Max Bytes:            350 GB
  Warning Threshold:    80%
  Auto-Rotate Threshold: 90%

Summary:
  Total Operations:     125,234,567
  Total Bytes Processed: 1.2 TB

TYPE   PATH                     OPERATIONS           BYTES               USAGE%
----   ----------------------   ---------------      ---------------     -------
KEY    tenant-123/high-volume   50,234,567 / 4.29B   125.5 GB / 350 GB   35.9%
KEY    fast-key                 75,000,000 / 4.29B   200 GB / 350 GB     57.1%
```

**Output (prometheus format):**
```
# HELP quicbaas_kms_total_operations Total cryptographic operations across all keys/DEKs
# TYPE quicbaas_kms_total_operations counter
quicbaas_kms_total_operations 125234567

# HELP quicbaas_kms_total_bytes_processed Total bytes processed across all keys/DEKs
# TYPE quicbaas_kms_total_bytes_processed counter
quicbaas_kms_total_bytes_processed 1288490188800

# HELP quicbaas_kms_item_operations Cryptographic operations per key/DEK
# TYPE quicbaas_kms_item_operations counter
quicbaas_kms_item_operations{type="key",path="tenant-123/high-volume"} 50234567
quicbaas_kms_item_operations{type="key",path="fast-key"} 75000000

# HELP quicbaas_kms_item_bytes_processed Bytes processed per key/DEK
# TYPE quicbaas_kms_item_bytes_processed counter
quicbaas_kms_item_bytes_processed{type="key",path="tenant-123/high-volume"} 134742343680
quicbaas_kms_item_bytes_processed{type="key",path="fast-key"} 214748364800

# HELP quicbaas_kms_item_usage_percent Usage percentage per key/DEK
# TYPE quicbaas_kms_item_usage_percent gauge
quicbaas_kms_item_usage_percent{type="key",path="tenant-123/high-volume"} 35.90
quicbaas_kms_item_usage_percent{type="key",path="fast-key"} 57.10
```

## Common Options

| Option | Description |
|--------|-------------|
| `--output` | Output format (json, yaml, table) |
| `--server` | QuicBaaS server address |
| `--token` | Authentication token |

## Examples

### Multi-Tenant Setup

```bash
# Generate tenant keys (with hierarchical paths)
quicbaas kms key generate tenant-acme/prod/db-key
quicbaas kms key generate tenant-acme/prod/api-key
quicbaas kms key generate tenant-globex/prod/secrets

# Encrypt tenant data
quicbaas kms encrypt tenant-acme/prod/db-key --data "connection-string"
```

### High-Security Setup

```bash
# Generate with strict AEAD limits
quicbaas kms key generate compliance-key \
  --algorithm AES-256-GCM

# Set safety limits
quicbaas kms key set-safety compliance-key \
  --max-operations 1000000 \
  --max-bytes 10GB \
  --warning-percent 50

# Generate exportable transport key
quicbaas kms key generate transport-key --exportable

# Export key for external backup
quicbaas kms key export compliance-key \
  --wrapping-key transport-key \
  --wrapping-algorithm AES-KW \
  --output backup.wrapped
```

### Monitoring Script

```bash
#!/bin/bash
# Check and rotate keys approaching limits

WARNINGS=$(quicbaas kms safety check --output json | jq -r '.items[] | select(.usage_percent > 90) | .id')

for KEY_PATH in $WARNINGS; do
  echo "Rotating key: $KEY_PATH"
  quicbaas kms key rotate "$KEY_PATH"
done
```

### Key Lifecycle Management

```bash
# Create a new key
quicbaas kms key generate production-key \
  --algorithm AES-256-GCM \
  --description "Production encryption key"

# Check key info
quicbaas kms key get production-key

# Rotate key periodically
quicbaas kms key rotate production-key

# List all versions
quicbaas kms key versions production-key

# Temporarily disable during maintenance
quicbaas kms key disable production-key

# Re-enable after maintenance
quicbaas kms key enable production-key

# Schedule deletion when no longer needed
quicbaas kms key delete production-key --force
```
