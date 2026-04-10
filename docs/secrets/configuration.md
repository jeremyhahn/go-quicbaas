# Secrets Configuration

## Configuration File

```yaml
# quicbaas.yaml
secrets:
  enabled: true

  # Encryption settings
  encryption:
    # Default encryption algorithm
    # Options: AES-128-GCM, AES-192-GCM, AES-256-GCM,
    #          ChaCha20-Poly1305, XChaCha20-Poly1305
    default_algorithm: AES-128-GCM

    # Default encryption key ID (go-xkms key reference)
    default_key_id: quicbaas-secrets-key

    # Backend for encryption keys
    # Options: software, tpm2, pkcs11, awskms, azurekv, gcpkms
    backend: software

  # Versioning settings
  versioning:
    enabled: true
    # Maximum versions to retain (0 = unlimited)
    max_versions: 10

  # Soft delete settings
  soft_delete:
    enabled: true
    # Days to retain deleted secrets before permanent removal
    retention_days: 30

  # Access control
  access:
    # Require authentication for all operations
    require_auth: true
    # Default policy for unauthenticated access
    default_deny: true
```

## Backend Configuration

### Software Backend (Default)

```yaml
secrets:
  encryption:
    backend: software
    software:
      # Directory for key storage
      key_dir: /var/lib/quicbaas/keys
      # Key file permissions (octal)
      key_perms: 0600
```

### TPM2 Backend

```yaml
secrets:
  encryption:
    backend: tpm2
    tpm2:
      # TPM device path
      device: /dev/tpmrm0
      # PCR banks to use for sealing
      pcr_banks: [0, 7, 9]
      # Owner auth password (optional)
      owner_auth: ""
```

### PKCS#11 Backend (HSM)

```yaml
secrets:
  encryption:
    backend: pkcs11
    pkcs11:
      # PKCS#11 library path
      library: /usr/lib/softhsm/libsofthsm2.so
      # Token label
      token_label: quicbaas
      # Slot ID (alternative to label)
      slot_id: 0
      # PIN for token access
      pin: "${PKCS11_PIN}"
```

### AWS KMS Backend

```yaml
secrets:
  encryption:
    backend: awskms
    aws_kms:
      # AWS region
      region: us-west-2
      # KMS key ID or ARN
      key_id: arn:aws:kms:us-west-2:123456789:key/abcd1234
      # Optional: assume role ARN
      role_arn: ""
```

### Azure Key Vault Backend

```yaml
secrets:
  encryption:
    backend: azurekv
    azure_kv:
      # Key Vault URL
      vault_url: https://myquicbaas.vault.azure.net
      # Key name
      key_name: quicbaas-secrets
      # Key version (optional, uses latest if empty)
      key_version: ""
```

### GCP Cloud KMS Backend

```yaml
secrets:
  encryption:
    backend: gcpkms
    gcp_kms:
      # Project ID
      project_id: my-project
      # Location
      location: us-west1
      # Key ring name
      key_ring: quicbaas
      # Crypto key name
      crypto_key: secrets-key
```

## Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `QUICBAAS_SECRETS_ENABLED` | Enable secrets engine | `false` |
| `QUICBAAS_SECRETS_KEY_ID` | Default encryption key ID | `quicbaas-secrets-key` |
| `QUICBAAS_SECRETS_ALGORITHM` | Default algorithm | `AES-128-GCM` |
| `PKCS11_PIN` | PKCS#11 token PIN | - |
| `AWS_KMS_KEY_ID` | AWS KMS key ID | - |

## CLI Configuration

### Set Default Key

```bash
quicbaas config set secrets.encryption.default_key_id my-key
```

### Set Default Algorithm

```bash
quicbaas config set secrets.encryption.default_algorithm ChaCha20-Poly1305
```

### View Configuration

```bash
quicbaas config get secrets
```

## Runtime Configuration

### Update via API

```bash
# Update default algorithm
curl -X PUT http://localhost:8080/api/v1/config/secrets \
  -H 'Content-Type: application/json' \
  -d '{
    "encryption": {
      "default_algorithm": "AES-256-GCM"
    }
  }'
```

## Performance Tuning

### High-Throughput Settings

```yaml
secrets:
  # Batch encryption for multiple secrets
  batch:
    enabled: true
    max_batch_size: 100
    flush_interval: 10ms

  # Caching for decrypted secrets (use with caution)
  cache:
    enabled: false  # Default: disabled for security
    ttl: 60s
    max_size: 1000
```

### Low-Latency Settings

```yaml
secrets:
  encryption:
    # Use AES-GCM on systems with AES-NI
    default_algorithm: AES-128-GCM

  # Disable versioning for simple use cases
  versioning:
    enabled: false
```
