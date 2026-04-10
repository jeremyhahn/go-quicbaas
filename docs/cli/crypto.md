# quicbaas crypto

Crypto encryption and signing commands for encryption-as-a-service operations.

## Overview

These commands provide cryptographic operations without exposing key material:
- Encrypt/decrypt data
- Sign/verify data
- Generate HMACs
- Generate random bytes
- Hash data
- Generate data encryption keys

## Commands

### encrypt

Encrypt data using a named crypto key.

```bash
quicbaas crypto encrypt <key-name>
```

#### Flags
- `--data string`: Data to encrypt (required)
- `--context string`: Additional context data
- `--base64`: Data is base64 encoded

#### Examples

```bash
# Encrypt string data
quicbaas crypto encrypt mykey --data "sensitive data"

# Encrypt with context
quicbaas crypto encrypt mykey --data "data" --context "user-123"

# Encrypt base64 data
quicbaas crypto encrypt mykey --data SGVsbG8= --base64
```

### decrypt

Decrypt data using a named crypto key.

```bash
quicbaas crypto decrypt <key-name>
```

#### Flags
- `--data string`: Data to decrypt (hex or base64) (required)
- `--context string`: Additional context data
- `--base64`: Data is base64 encoded (default: hex)

#### Examples

```bash
# Decrypt hex-encoded ciphertext
quicbaas crypto decrypt mykey --data "abcd1234..."

# Decrypt with context
quicbaas crypto decrypt mykey --data "abcd..." --context "user-123"

# Decrypt base64 ciphertext
quicbaas crypto decrypt mykey --data SGVsbG8= --base64
```

### rewrap

Re-encrypt data with a new key version.

```bash
quicbaas crypto rewrap <key-name>
```

#### Flags
- `--data string`: Ciphertext to rewrap (required)
- `--context string`: Additional context data
- `--base64`: Data is base64 encoded (default: hex)

#### Examples

```bash
# Rewrap ciphertext with latest key version
quicbaas crypto rewrap mykey --data "abcd1234..."

# Rewrap with context
quicbaas crypto rewrap mykey --data "abcd..." --context "user-123"
```

### sign

Generate a cryptographic signature for data.

```bash
quicbaas crypto sign <key-name>
```

#### Flags
- `--data string`: Data to sign (required)
- `--algorithm string`: Signature algorithm (default: key's algorithm)
- `--base64`: Data is base64 encoded

#### Examples

```bash
# Sign data
quicbaas crypto sign signing-key --data "message to sign"

# Sign with specific algorithm
quicbaas crypto sign signing-key --data "message" --algorithm ECDSA-P256

# Sign base64 data
quicbaas crypto sign signing-key --data SGVsbG8= --base64
```

### verify

Verify a cryptographic signature.

```bash
quicbaas crypto verify <key-name>
```

#### Flags
- `--data string`: Original data (required)
- `--signature string`: Signature to verify (hex or base64) (required)
- `--algorithm string`: Signature algorithm
- `--base64`: Data and signature are base64 encoded

#### Examples

```bash
# Verify signature
quicbaas crypto verify signing-key --data "message" --signature "abcd..."

# Verify with algorithm
quicbaas crypto verify signing-key --data "message" --signature "abcd..." --algorithm ECDSA-P256

# Verify base64 data
quicbaas crypto verify signing-key --data SGVsbG8= --signature c2lnbmF0dXJl --base64
```

### hash

Generate a cryptographic hash of data.

```bash
quicbaas crypto hash
```

#### Flags
- `--data string`: Data to hash (required)
- `--algorithm string`: Hash algorithm (SHA-256, SHA-512, SHA3-256, SHA3-512, BLAKE2b) (default: "SHA-256")
- `--format string`: Output format (hex, base64) (default: "hex")

#### Examples

```bash
# Hash with SHA-256 (default)
quicbaas crypto hash --data "data to hash"

# Hash with SHA-512
quicbaas crypto hash --data "data" --algorithm SHA-512

# Hash with base64 output
quicbaas crypto hash --data "data" --format base64
```

### random

Generate cryptographically secure random bytes.

```bash
quicbaas crypto random
```

#### Flags
- `--bytes int`: Number of random bytes to generate (required)
- `--format string`: Output format (hex, base64) (default: "hex")

#### Examples

```bash
# Generate 32 random bytes (hex)
quicbaas crypto random --bytes 32

# Generate 16 random bytes (base64)
quicbaas crypto random --bytes 16 --format base64
```

### datakey

Generate a data encryption key.

```bash
quicbaas crypto datakey <key-name>
```

#### Flags
- `--bits int`: Key size in bits (128, 256, 512) (default: 256)

#### Examples

```bash
# Generate 256-bit data key
quicbaas crypto datakey mykey --bits 256

# Generate 128-bit data key
quicbaas crypto datakey mykey --bits 128
```

## Key Context

Context provides additional authenticated data (AAD) for encryption operations:
- Binds ciphertext to specific context (e.g., user ID, session)
- Must be provided during both encryption and decryption
- Enhances security without exposing in ciphertext
- Common uses: tenant isolation, access control

## Signature Algorithms

- **RSA-2048**: RSA with 2048-bit key
- **RSA-4096**: RSA with 4096-bit key
- **ECDSA-P256**: ECDSA with P-256 curve
- **ECDSA-P384**: ECDSA with P-384 curve
- **Ed25519**: Edwards-curve Digital Signature Algorithm

## Hash Algorithms

- **SHA-256**: 256-bit SHA-2 hash
- **SHA-512**: 512-bit SHA-2 hash
- **SHA3-256**: 256-bit SHA-3 hash
- **SHA3-512**: 512-bit SHA-3 hash
- **BLAKE2b**: BLAKE2b hash function

## Notes

- Crypto keys never leave the server
- Encrypted data includes key version for automatic key rotation
- Rewrap allows updating ciphertext without decrypting plaintext
- Data keys are encrypted with the crypto key before return
- Random bytes use a cryptographically secure random number generator
- Hash operations are stateless and don't require stored keys
