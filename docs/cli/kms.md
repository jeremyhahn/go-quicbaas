# quicbaas kms

Key Management Service commands for managing cryptographic keys.

## Overview

These commands allow you to generate, import, rotate, and delete encryption keys, as well as perform encryption and decryption operations using those keys.

## Commands

### generate

Generate a new cryptographic key with the specified algorithm.

```bash
quicbaas kms generate
```

#### Flags
- `--name string`: Key name (required)
- `--algorithm string`: Key algorithm (default: "AES-256-GCM")
- `--description string`: Key description
- `--exportable`: Allow key export
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Generate AES-256-GCM key
quicbaas kms generate --name mykey --algorithm AES-256-GCM

# Generate exportable ChaCha20-Poly1305 key
quicbaas kms generate --name mykey --algorithm CHACHA20-POLY1305 --exportable

# Generate RSA-4096 key with description
quicbaas kms generate --name signing-key --algorithm RSA-4096 --description "Production signing key"
```

### import

Import an external cryptographic key.

```bash
quicbaas kms import
```

#### Flags
- `--name string`: Key name (required)
- `--material string`: Key material file path or base64: prefixed string (required)
- `--algorithm string`: Key algorithm (required) (default: "AES-256-GCM")
- `--description string`: Key description
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Import key from file
quicbaas kms import --name imported-key --material /path/to/keyfile --algorithm AES-256-GCM

# Import key from base64 string
quicbaas kms import --name imported-key --material base64:SGVsbG8gV29ybGQ= --algorithm AES-256-GCM
```

### get

Retrieve metadata for a cryptographic key.

```bash
quicbaas kms get <key-name>
```

#### Flags
- `--version uint64`: Key version (0 for current)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Get current key version
quicbaas kms get mykey

# Get specific key version
quicbaas kms get mykey --version 2

# Get key as JSON
quicbaas kms get mykey --output json
```

### list

List all cryptographic keys with optional filtering.

```bash
quicbaas kms list
```

#### Flags
- `--prefix string`: Filter by key name prefix
- `--limit int`: Maximum number of keys to return (default: 100)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# List all keys
quicbaas kms list

# List with name prefix filter
quicbaas kms list --prefix prod-

# List with limit
quicbaas kms list --limit 50

# List as JSON
quicbaas kms list --output json
```

### versions

List all versions of a cryptographic key.

```bash
quicbaas kms versions <key-name>
```

#### Flags
- `--limit int`: Maximum number of versions to return (0 for all)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# List all versions
quicbaas kms versions mykey

# List with limit
quicbaas kms versions mykey --limit 10

# List as JSON
quicbaas kms versions mykey --output json
```

### rotate

Generate a new version of a cryptographic key.

```bash
quicbaas kms rotate <key-name>
```

#### Flags
- `--grace-period string`: Grace period duration (e.g., 24h, 7d, 30m) after which old version will be automatically disabled
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Rotate a key
quicbaas kms rotate mykey

# Rotate with 24-hour grace period
quicbaas kms rotate mykey --grace-period 24h

# Rotate with 7-day grace period
quicbaas kms rotate mykey --grace-period 7d

# Rotate with 30-minute grace period (for testing)
quicbaas kms rotate mykey --grace-period 30m

# Rotate and view as JSON
quicbaas kms rotate mykey --output json
```

**Grace Period Behavior:**
- When `--grace-period` is specified, a scheduler job is created to automatically disable the old key version after the specified duration
- The old key version remains enabled during the grace period, allowing time for services to transition to the new version
- After the grace period expires, the old version is automatically disabled
- If no grace period is specified, the old version remains enabled indefinitely

### enable

Enable a disabled cryptographic key or key version.

```bash
quicbaas kms enable <key-name>
```

#### Flags
- `--version uint64`: Key version to enable (0 for all versions)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Enable all versions of a key
quicbaas kms enable mykey

# Enable specific key version
quicbaas kms enable mykey --version 2

# Enable and view as JSON
quicbaas kms enable mykey --output json
```

### disable

Disable a cryptographic key or key version temporarily.

```bash
quicbaas kms disable <key-name>
```

#### Flags
- `--version uint64`: Key version to disable (0 for all versions)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Disable all versions of a key
quicbaas kms disable mykey

# Disable specific key version
quicbaas kms disable mykey --version 2

# Disable and view as JSON
quicbaas kms disable mykey --output json
```

**Note:** Disabled keys cannot be used for encryption but can still be used for decryption of existing data. To re-enable a key, use the `enable` command.

### delete

Schedule a cryptographic key for deletion.

```bash
quicbaas kms delete <key-name>
```

#### Flags
- `--force`: Bypass deletion protection
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Schedule key for deletion
quicbaas kms delete mykey

# Force delete (bypass protection)
quicbaas kms delete mykey --force
```

### encrypt

Encrypt data using a key.

```bash
quicbaas kms encrypt <key-name>
```

#### Flags
- `--data string`: Data to encrypt (required)
- `--aad string`: Additional authenticated data
- `--base64`: Data is base64 encoded
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Encrypt string data
quicbaas kms encrypt mykey --data "sensitive data"

# Encrypt with additional authenticated data
quicbaas kms encrypt mykey --data "sensitive" --aad "context"

# Encrypt base64 data
quicbaas kms encrypt mykey --data SGVsbG8= --base64
```

### decrypt

Decrypt data using a key.

```bash
quicbaas kms decrypt <key-name>
```

#### Flags
- `--data string`: Data to decrypt (hex or base64) (required)
- `--aad string`: Additional authenticated data
- `--base64`: Data is base64 encoded (default: hex)
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Decrypt hex-encoded ciphertext
quicbaas kms decrypt mykey --data "abcd1234..."

# Decrypt with additional authenticated data
quicbaas kms decrypt mykey --data "abcd1234..." --aad "context"

# Decrypt base64 ciphertext
quicbaas kms decrypt mykey --data SGVsbG8= --base64
```

## Supported Algorithms

- AES-128-GCM
- AES-256-GCM
- CHACHA20-POLY1305
- RSA-2048
- RSA-4096
- ECDSA-P256
- ECDSA-P384
- Ed25519

## Notes

- Keys are scheduled for deletion with a 30-day window by default
- Old key versions are retained for decryption after rotation
- Exportable keys can be exported from the system
- Additional authenticated data (AAD) provides additional security context
- Encrypted data includes the key version used for encryption
