# quicbaas seal

Seal management commands for QuicBaaS encryption seal.

## Overview

The seal protects the master encryption key using Shamir's Secret Sharing. QuicBaaS must be unsealed after startup before it can process requests.

## Commands

### status

Check the current seal status.

```bash
quicbaas seal status
```

#### Flags
- `--output string`: Output format (json, yaml, table) (default: "table")

#### Examples

```bash
# Check seal status
quicbaas seal status

# Check status as JSON
quicbaas seal status --output json
```

### init

Initialize the seal with Shamir's Secret Sharing.

```bash
quicbaas seal init
```

#### Flags
- `--shares int`: Number of unseal key shares (default: 5)
- `--threshold int`: Number of shares required to unseal (default: 3)
- `--recovery-shares int`: Number of recovery key shares
- `--recovery-threshold int`: Number of recovery keys required

#### Examples

```bash
# Initialize with 5 shares, 3 required to unseal
quicbaas seal init --shares 5 --threshold 3

# Initialize with recovery keys
quicbaas seal init --shares 5 --threshold 3 --recovery-shares 3 --recovery-threshold 2
```

### unseal

Provide an unseal key share to unseal QuicBaaS.

```bash
quicbaas seal unseal
```

#### Flags
- `--share string`: Unseal key share (base64 encoded)

#### Examples

```bash
# Provide share via stdin (recommended)
quicbaas seal unseal

# Provide share via flag (less secure)
quicbaas seal unseal --share base64:encoded-share
```

### seal

Seal QuicBaaS to prevent access to encrypted data.

```bash
quicbaas seal seal
```

#### Examples

```bash
# Seal the system (with confirmation)
quicbaas seal seal
```

### rekey

Generate new unseal keys while keeping the same master key.

```bash
quicbaas seal rekey
```

#### Flags
- `--shares int`: Number of new unseal key shares (default: 5)
- `--threshold int`: Number of new shares required to unseal (default: 3)

#### Examples

```bash
# Rekey with new configuration
quicbaas seal rekey --shares 7 --threshold 4
```

## Seal Process

### Initialization
1. Generate master encryption key
2. Split key into shares using Shamir's Secret Sharing
3. Generate root token
4. Optionally generate recovery keys
5. Return shares and token to operator

### Unsealing
1. Collect unseal shares from operators
2. Reconstruct master key when threshold is met
3. Decrypt encryption keys
4. Enable database operations

### Sealing
1. Clear master key from memory
2. Disable database operations
3. Require unsealing to resume

## Security Considerations

- Store unseal shares securely and separately
- Distribute shares among multiple operators
- Use recovery keys for disaster recovery
- Rekey periodically to rotate unseal keys
- Monitor seal status for security events

## Important Notes

- **Store unseal shares securely** - They cannot be recovered if lost
- Initialization can only be done once unless seal is destroyed
- Unsealing requires threshold number of shares (e.g., 3 of 5)
- Sealing immediately locks the database
- Rekeying generates new shares but keeps the same master key
- Recovery keys can be used if unseal shares are lost
- Shares are provided as base64-encoded strings
