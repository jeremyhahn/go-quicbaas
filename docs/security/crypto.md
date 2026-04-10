# Crypto Service

Encryption as a Service - encrypt, decrypt, sign, and verify data without exposing keys.

## Status

Complete - Full implementation with CLI commands, gRPC/REST APIs, and service layer.

## Overview

Crypto provides:
- Encrypt/decrypt data without key exposure
- Sign and verify data
- Generate HMACs
- Generate secure random data
- Generate data encryption keys
- Key derivation

## CLI Usage

### Encryption/Decryption

```bash
# Encrypt data
quicbaas crypto encrypt mykey --data "sensitive"

# Encrypt with context
quicbaas crypto encrypt mykey --data "sensitive" --context "user-123"

# Decrypt data
quicbaas crypto decrypt mykey --data "ciphertext..."

# Re-wrap with latest key version
quicbaas crypto rewrap mykey --data "old-ciphertext..."
```

### Signing/Verification

```bash
# Sign data
quicbaas crypto sign signing-key --data "message"

# Sign with specific hash
quicbaas crypto sign signing-key --data "message" --algorithm SHA512

# Verify signature
quicbaas crypto verify signing-key \
  --data "message" \
  --signature "base64-signature"
```

### Random Data

```bash
# Generate 32 random bytes (default)
quicbaas crypto random

# Generate specific size
quicbaas crypto random --bytes 64 --format hex
```

### Data Encryption Keys

```bash
# Generate 256-bit AES key (default)
quicbaas crypto datakey mykey

# Generate 128-bit AES key
quicbaas crypto datakey mykey --bits 128
```

## Supported Algorithms

### Encryption
- AES-128-GCM
- AES-192-GCM
- AES-256-GCM (recommended)
- ChaCha20-Poly1305

### Signing
- RSA-2048, RSA-4096
- ECDSA-P256, ECDSA-P384
- Ed25519

### Hashing
- SHA-256, SHA-384, SHA-512

## Go SDK

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/service"

// Encrypt data
encResp, err := cryptoService.Encrypt(ctx, &service.EncryptRequest{
    KeyID:     "mykey",
    Plaintext: []byte("sensitive"),
})

// Decrypt data
decResp, err := cryptoService.Decrypt(ctx, &service.DecryptRequest{
    KeyID:      "mykey",
    Ciphertext: encResp.Ciphertext,
})

// Sign data
signResp, err := cryptoService.Sign(ctx, &service.SignRequest{
    KeyID:   "signing-key",
    Message: []byte("data"),
})

// Verify signature
verifyResp, err := cryptoService.Verify(ctx, &service.VerifyRequest{
    KeyID:     "signing-key",
    Message:   []byte("data"),
    Signature: signResp.Signature,
})

// Generate random bytes
randomResp, err := cryptoService.GenerateRandom(ctx, &service.GenerateRandomRequest{
    NumBytes: 32,
})

// Generate data key
dekResp, err := cryptoService.GenerateDataKey(ctx, &service.GenerateDataKeyRequest{
    KeyID:   "mykey",
    KeySpec: service.KeySpec{Algorithm: service.KeyAlgAES256GCM},
})
```

## Security Features

1. **Key Isolation**: Crypto keys never leave QuicBaaS
2. **No Key Exposure**: Applications never see encryption keys
3. **Versioning**: All operations versioned
4. **Audit Trail**: Every operation can be logged
5. **Rate Limiting**: Protection against abuse
6. **Access Control**: Fine-grained permissions per key

## Use Cases

### Application-Level Encryption

```bash
# Encrypt before storing
ENCRYPTED=$(quicbaas crypto encrypt mykey --data "SSN: 123-45-6789")

# Decrypt when retrieved
quicbaas crypto decrypt mykey --data "$ENCRYPTED"
```

### Envelope Encryption

```bash
# Generate wrapped data key
quicbaas crypto datakey master-key --bits 256
# Use plaintext key to encrypt data
# Store ciphertext blob with encrypted data
```

### Digital Signatures

```bash
# Sign message
SIG=$(quicbaas crypto sign signing-key --data "contract text")

# Verify later
quicbaas crypto verify signing-key --data "contract text" --signature "$SIG"
```

## See Also

- [KMS](kms.md) - Key management
- [Secrets](secrets.md) - Secret storage
- [Seal/Unseal](seal.md) - Master key protection
- [Security Overview](README.md)
