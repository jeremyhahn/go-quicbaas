# quicbaas auth

Authentication commands for managing QuicBaaS sessions.

## Commands

### login

Authenticate with a QuicBaaS server and obtain an access token.

```bash
quicbaas auth login <username>
```

#### Flags
- `--password string`: Password (not recommended, use interactive prompt instead)
- `--token-file string`: Custom token file location (default: ~/.quicbaas/token)
- `--no-store`: Don't store token, print to stdout instead

#### Examples

```bash
# Login with interactive password prompt
quicbaas auth login admin

# Login with password from environment variable
QUICBAAS_PASSWORD=secret quicbaas auth login admin

# Login and store token in custom location
quicbaas auth login admin --token-file /path/to/token

# Login without storing token (print to stdout)
quicbaas auth login admin --no-store

# Login to specific server
quicbaas auth login admin --address quicbaas.example.com:50051

# Login with TLS
quicbaas auth login admin --tls --tls-cert client.crt --tls-key client.key --tls-ca ca.crt
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
