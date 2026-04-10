# FIPS 140-3 Compliance

QuicBaaS supports FIPS 140-3 compliance mode for deployments requiring federal cryptographic standards. This document covers configuration, approved algorithms, and validation requirements.

## Overview

QuicBaaS leverages Go 1.24+'s native FIPS 140-3 support via the `crypto/fips140` package. When FIPS mode is enabled:

- The Go crypto library automatically restricts to FIPS 140-3 approved algorithms
- Non-approved cryptographic operations will fail at runtime
- TLS automatically uses only approved cipher suites
- Key sizes are validated automatically

**Note:** Algorithm validation is handled automatically by the Go runtime - no manual validation is required in application code.

## Configuration

### Enable FIPS Mode

```yaml
security:
  fips_mode: true
  encryption_algorithm: "aes256"    # FIPS-approved

  # TLS configuration for FIPS
  tls_enabled: true
  tls_min_version: "1.2"            # TLS 1.2 minimum
  tls_cipher_suites:
    - TLS_AES_128_GCM_SHA256        # TLS 1.3
    - TLS_AES_256_GCM_SHA384        # TLS 1.3
    - TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
    - TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
    - TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
```

### Build with FIPS Support

```bash
# Build FIPS-compliant binary with Go 1.24+
make build-fips

# Manual build:
GOFIPS140=v1.0.0 go build -o bin/quicbaas-fips ./cmd/quicbaas
```

### Check FIPS Status

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/fips"

// Check if FIPS mode is enabled
if fips.Enabled() {
    fmt.Println("Running in FIPS 140-3 compliant mode")
}

// Get detailed status for diagnostics
status := fips.GetStatus()
fmt.Printf("FIPS Enabled: %v\n", status.Enabled)
fmt.Printf("Kernel FIPS Mode: %v\n", status.KernelFIPSMode)
fmt.Printf("Kernel Cmdline: %v\n", status.KernelCmdline)
fmt.Printf("Crypto Provider: %s\n", status.CryptoProvider)

// Log status for audit purposes
fips.LogStatus()
```

## Approved Algorithms

When Go FIPS mode is enabled, only these algorithms are available. Non-approved algorithms will fail automatically.

### Symmetric Encryption

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| AES-128-GCM | 128 bits | AES-128-GCM authenticated encryption |
| AES-256-GCM | 256 bits | AES-256-GCM authenticated encryption |

### Hash Functions

| Algorithm | Output Size | Description |
|-----------|-------------|-------------|
| SHA-256 | 256 bits | SHA-256 |
| SHA-384 | 384 bits | SHA-384 |
| SHA-512 | 512 bits | SHA-512 |

### Digital Signatures

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| ECDSA P-256 | 256 bits | ECDSA with P-256 curve |
| ECDSA P-384 | 384 bits | ECDSA with P-384 curve |
| ECDSA P-521 | 521 bits | ECDSA with P-521 curve |
| RSA-2048 | 2048 bits | RSA (minimum for FIPS) |
| RSA-3072 | 3072 bits | RSA |
| RSA-4096 | 4096 bits | RSA |

### Key Agreement

| Algorithm | Key Size | Description |
|-----------|----------|-------------|
| ECDH P-256 | 256 bits | ECDH with P-256 curve |
| ECDH P-384 | 384 bits | ECDH with P-384 curve |
| ECDH P-521 | 521 bits | ECDH with P-521 curve |

### Key Derivation

| Algorithm | Description |
|-----------|-------------|
| HKDF | HMAC-based Key Derivation Function |
| PBKDF2 | Password-Based Key Derivation Function 2 |

### Message Authentication

| Algorithm | Description |
|-----------|-------------|
| HMAC-SHA-256 | HMAC with SHA-256 |
| HMAC-SHA-384 | HMAC with SHA-384 |
| HMAC-SHA-512 | HMAC with SHA-512 |

## TLS Requirements

### Minimum TLS Version

FIPS 140-3 requires TLS 1.2 or higher. QuicBaaS enforces TLS 1.3 by default in FIPS mode.

### Approved Cipher Suites

TLS 1.3 cipher suites:

| Cipher Suite | Description |
|--------------|-------------|
| TLS_AES_128_GCM_SHA256 | AES-128-GCM with SHA-256 |
| TLS_AES_256_GCM_SHA384 | AES-256-GCM with SHA-384 |

TLS 1.2 cipher suites:

| Cipher Suite | Description |
|--------------|-------------|
| TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256 | ECDHE-RSA with AES-128-GCM |
| TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384 | ECDHE-RSA with AES-256-GCM |
| TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256 | ECDHE-ECDSA with AES-128-GCM |
| TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384 | ECDHE-ECDSA with AES-256-GCM |

## gRPC FIPS Validation

QuicBaaS includes gRPC interceptors that validate FIPS compliance for every request.

### Validation Checks

The FIPS interceptor validates:

1. **TLS Enabled**: Connection must use TLS encryption
2. **TLS Version**: Must be TLS 1.3 or higher (TLS 1.2 allowed with warning)
3. **Cipher Suite**: Must use a FIPS-approved cipher suite

Non-compliant connections receive:

```
rpc error: code = FailedPrecondition desc = FIPS compliance validation failed: insufficient TLS version
```

## Linux Kernel FIPS Mode

QuicBaaS can detect and integrate with Linux kernel FIPS mode.

### Check Kernel FIPS Mode

```bash
# Check if kernel FIPS mode is enabled
cat /proc/sys/crypto/fips_enabled
# 1 = enabled, 0 = disabled

# Check kernel command line
cat /proc/cmdline | grep fips
```

### Enable Kernel FIPS Mode

```bash
# Add to kernel boot parameters
fips=1

# On RHEL/CentOS
sudo fips-mode-setup --enable
sudo reboot

# On Ubuntu/Debian
sudo apt-get install fips-initramfs
sudo update-initramfs -u
# Add fips=1 to GRUB_CMDLINE_LINUX in /etc/default/grub
sudo update-grub
sudo reboot
```

### Automatic Detection

QuicBaaS reports kernel FIPS status in the status output:

```
=== FIPS 140-3 Status ===
Go FIPS Mode: true
Kernel FIPS Mode: true
Kernel Cmdline fips=1: true
Crypto Provider: Go FIPS 140-3
========================
```

## Go 1.24+ FIPS Support

Go 1.24+ includes native FIPS 140-3 support. Build with GOFIPS140 to enable.

### Build Requirements

```bash
# Requires Go 1.24+
export GOFIPS140=v1.0.0
go build ./cmd/quicbaas
```

### Verify FIPS Mode

Check if FIPS mode is active:

```go
import "github.com/jeremyhahn/go-quicbaas/pkg/fips"

status := fips.GetStatus()
if status.Enabled {
    fmt.Println("Go FIPS 140-3 Module active")
} else {
    fmt.Println("Using Go Standard Crypto")
}
```

## Compliance Checklist

### Build Requirements

- [ ] Go 1.24+
- [ ] Built with GOFIPS140 environment variable

### Configuration

- [ ] fips_mode: true in configuration
- [ ] TLS 1.2+ minimum version
- [ ] Only FIPS-approved cipher suites configured

### Runtime

- [ ] FIPS mode enabled at startup
- [ ] Kernel FIPS mode enabled (if applicable)
- [ ] gRPC FIPS interceptor enabled
- [ ] Non-approved algorithms rejected automatically

### Monitoring

- [ ] FIPS status logged at startup
- [ ] Non-compliant connections rejected

## Error Messages

| Error | Cause | Resolution |
|-------|-------|------------|
| crypto operation failed | Using non-approved algorithm | Use only FIPS-approved algorithms |
| TLS version below minimum | TLS 1.1 or lower | Configure TLS 1.2+ |
| cipher suite not FIPS-approved | Non-approved cipher | Configure approved cipher suite |

## See Also

- [Security README](README.md) - Security overview
- [TLS Setup](tls-setup.md) - TLS configuration guide
- [Hardening Guide](hardening-guide.md) - Production hardening
- [Go FIPS Documentation](https://go.dev/doc/fips) - Official Go FIPS docs
