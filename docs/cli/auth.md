# quicbaas auth

Authentication commands for managing QuicBaaS sessions (passwordless).

## Commands

### login

Authenticate with a QuicBaaS server and obtain an access token using a hardware authenticator.

```bash
quicbaas auth login <username>
```

**Authentication Methods** (choose one):
- **WebAuthn (Passkey)**: Use a platform authenticator (Touch ID, Windows Hello, biometric) or a roaming security key (YubiKey, Titan)
- **PIV Smartcard**: Insert a smartcard and enter PIN (if PIV is enabled)
- **mTLS**: Provide a TLS client certificate (for service accounts)

#### Flags
- `--token-file string`: Custom token file location (default: ~/.quicbaas/token)
- `--no-store`: Don't store token, print to stdout instead
- `--method string`: Auth method: `webauthn` or `smartcard` (auto-detected if not specified)

#### Examples

```bash
# Login with WebAuthn (passkey/security key)
quicbaas auth login alice@example.com

# Login with smartcard (PIV)
quicbaas auth login alice@example.com --method smartcard

# Store token in custom location
quicbaas auth login alice@example.com --token-file /path/to/token

# Print token to stdout without storing
quicbaas auth login alice@example.com --no-store

# Login to specific server
quicbaas auth login alice@example.com --address quicbaas.example.com:50051

# Login with mTLS client certificate (service account)
quicbaas auth login --address quicbaas.example.com:50051 \
  --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
```

### logout

Invalidate the current authentication token and remove it from local storage.

```bash
quicbaas auth logout
```

#### Flags
- `--token-file string`: Custom token file location (default: ~/.quicbaas/token)

#### Examples

```bash
# Logout and remove token
quicbaas auth logout

# Logout with custom token file
quicbaas auth logout --token-file /path/to/token

# Logout from specific server
quicbaas auth logout --address quicbaas.example.com:50051
```

### status

Display information about the current authentication session.

```bash
quicbaas auth status
```

#### Flags
- `--token-file string`: Custom token file location (default: ~/.quicbaas/token)
- `--output string`: Output format (json, table) (default: "table")

#### Examples

```bash
# Show authentication status
quicbaas auth status

# Show status with custom token file
quicbaas auth status --token-file /path/to/token

# Show status as JSON
quicbaas auth status --output json
```

## Notes

- Tokens are securely stored in ~/.quicbaas/token by default
- Tokens are automatically used for subsequent CLI commands that require authentication
- The token file contains access token, refresh token, token type, and expiration time
