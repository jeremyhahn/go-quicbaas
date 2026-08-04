# Credentials Configuration

Complete configuration reference for the QuicBaaS Credentials Engine.

## Configuration Overview

```yaml
credentials:
  # Certificate credentials
  certificates:
    enabled: true
    provider: builtin  # builtin or external

    # Built-in provider settings
    builtin:
      auto_generate: true
      ca_cert_file: ""
      ca_key_file: ""
      subject:
        common_name: "QuicBaaS CA"
        organization: ""
        organizational_unit: ""
        country: ""
        province: ""
        locality: ""
      defaults:
        validity_days: 365
        key_algorithm: ecdsa
        key_size: 256

    # External provider settings
    external:
      type: ""
      address: ""
      ca_bundle: ""
      client_cert: ""
      client_key: ""

  # Database credentials
  database:
    enabled: false
    postgres:
      enabled: false
      default_ttl: 1h
    mysql:
      enabled: false
      default_ttl: 1h
```

## Certificate Configuration

### certificates.enabled

| | |
|---|---|
| Type | boolean |
| Default | `false` |
| Required | No |

Enable the certificate credentials engine.

```yaml
credentials:
  certificates:
    enabled: true
```

---

### certificates.provider

| | |
|---|---|
| Type | string |
| Default | `builtin` |
| Options | `builtin`, `external` |

Select the certificate provider implementation.

```yaml
credentials:
  certificates:
    provider: builtin  # Use built-in lightweight CA
```

---

## Built-in Provider Configuration

### builtin.auto_generate

| | |
|---|---|
| Type | boolean |
| Default | `true` |

Automatically generate a self-signed CA on startup if no CA files are provided.

```yaml
credentials:
  certificates:
    builtin:
      auto_generate: true
```

---

### builtin.ca_cert_file

| | |
|---|---|
| Type | string |
| Default | `""` |

Path to existing CA certificate file (PEM format).

```yaml
credentials:
  certificates:
    builtin:
      auto_generate: false
      ca_cert_file: /etc/quicbaas/ca.crt
      ca_key_file: /etc/quicbaas/ca.key
```

---

### builtin.ca_key_file

| | |
|---|---|
| Type | string |
| Default | `""` |

Path to existing CA private key file (PEM format).

---

### builtin.subject

Subject fields for auto-generated CA certificate.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `common_name` | string | `"QuicBaaS CA"` | CA common name |
| `organization` | string | `""` | Organization name |
| `organizational_unit` | string | `""` | Organizational unit |
| `country` | string | `""` | Two-letter country code |
| `province` | string | `""` | State or province |
| `locality` | string | `""` | City or locality |

```yaml
credentials:
  certificates:
    builtin:
      subject:
        common_name: "My Company CA"
        organization: "My Company Inc."
        organizational_unit: "Engineering"
        country: "US"
        province: "California"
        locality: "San Francisco"
```

---

### builtin.defaults

Default settings for issued certificates.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `validity_days` | int | `365` | Default certificate validity |
| `key_algorithm` | string | `ecdsa` | Key algorithm: `ecdsa` or `rsa` |
| `key_size` | int | `256` | Key size (see table below) |

**Key Size Values:**

| Algorithm | key_size | Description |
|-----------|----------|-------------|
| ecdsa | 256 | P-256 curve (recommended) |
| ecdsa | 384 | P-384 curve |
| ecdsa | 521 | P-521 curve |
| rsa | 2048 | RSA 2048-bit |
| rsa | 4096 | RSA 4096-bit |

```yaml
credentials:
  certificates:
    builtin:
      defaults:
        validity_days: 90
        key_algorithm: ecdsa
        key_size: 256
```

---

## External Provider Configuration

### external.type

| | |
|---|---|
| Type | string |
| Options | `xkms` (go-xkms TrustedCA) |

Type of external certificate provider.

```yaml
credentials:
  certificates:
    provider: external
    external:
      type: xkms   # go-xkms TrustedCA
```

---

### external.address

| | |
|---|---|
| Type | string |
| Example | `localhost:8443` |

Address of the external CA service.

---

### external.ca_bundle

| | |
|---|---|
| Type | string |

Path to CA bundle for verifying the external CA's TLS certificate.

---

### external.client_cert

| | |
|---|---|
| Type | string |

Path to client certificate for mTLS with external CA.

---

### external.client_key

| | |
|---|---|
| Type | string |

Path to client private key for mTLS with external CA.

```yaml
credentials:
  certificates:
    provider: external
    external:
      type: xkms   # go-xkms TrustedCA
      address: "ca.internal:8443"
      ca_bundle: /etc/quicbaas/ca-bundle.pem
      client_cert: /etc/quicbaas/client.crt
      client_key: /etc/quicbaas/client.key
```

---

## Database Credentials Configuration

### database.enabled

| | |
|---|---|
| Type | boolean |
| Default | `false` |

Enable dynamic database credential generation.

---

### database.postgres

PostgreSQL credential generator configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable PostgreSQL credentials |
| `default_ttl` | duration | `1h` | Default credential TTL |

```yaml
credentials:
  database:
    enabled: true
    postgres:
      enabled: true
      default_ttl: 1h
```

---

### database.mysql

MySQL credential generator configuration.

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | boolean | `false` | Enable MySQL credentials |
| `default_ttl` | duration | `1h` | Default credential TTL |

```yaml
credentials:
  database:
    enabled: true
    mysql:
      enabled: true
      default_ttl: 30m
```

---

## Example Configurations

### Standalone with Auto-Generated CA

```yaml
credentials:
  certificates:
    enabled: true
    provider: builtin
    builtin:
      auto_generate: true
      subject:
        common_name: "QuicBaaS CA"
        organization: "My Organization"
      defaults:
        validity_days: 365
        key_algorithm: ecdsa
        key_size: 256
```

### Using Existing CA

```yaml
credentials:
  certificates:
    enabled: true
    provider: builtin
    builtin:
      auto_generate: false
      ca_cert_file: /etc/quicbaas/pki/ca.crt
      ca_key_file: /etc/quicbaas/pki/ca.key
      defaults:
        validity_days: 90
        key_algorithm: ecdsa
        key_size: 256
```

### External CA (go-xkms TrustedCA)

```yaml
credentials:
  certificates:
    enabled: true
    provider: external
    external:
      type: xkms   # go-xkms TrustedCA
      address: "ca.internal:8443"
      ca_bundle: /etc/quicbaas/pki/ca-bundle.pem
      client_cert: /etc/quicbaas/pki/quicbaas-client.crt
      client_key: /etc/quicbaas/pki/quicbaas-client.key
```

### Full Configuration with Database Credentials

```yaml
credentials:
  certificates:
    enabled: true
    provider: builtin
    builtin:
      auto_generate: true
      subject:
        common_name: "Production CA"
        organization: "Acme Corp"
        country: "US"
      defaults:
        validity_days: 30
        key_algorithm: ecdsa
        key_size: 256

  database:
    enabled: true
    postgres:
      enabled: true
      default_ttl: 1h
    mysql:
      enabled: true
      default_ttl: 30m
```

---

## Environment Variables

Configuration can be overridden via environment variables:

| Environment Variable | Config Path |
|---------------------|-------------|
| `QUICBAAS_CREDENTIALS_CERTIFICATES_ENABLED` | `credentials.certificates.enabled` |
| `QUICBAAS_CREDENTIALS_CERTIFICATES_PROVIDER` | `credentials.certificates.provider` |
| `QUICBAAS_CREDENTIALS_CERTIFICATES_BUILTIN_CA_CERT_FILE` | `credentials.certificates.builtin.ca_cert_file` |
| `QUICBAAS_CREDENTIALS_CERTIFICATES_BUILTIN_CA_KEY_FILE` | `credentials.certificates.builtin.ca_key_file` |
| `QUICBAAS_CREDENTIALS_DATABASE_ENABLED` | `credentials.database.enabled` |

```bash
export QUICBAAS_CREDENTIALS_CERTIFICATES_ENABLED=true
export QUICBAAS_CREDENTIALS_CERTIFICATES_PROVIDER=builtin
```
