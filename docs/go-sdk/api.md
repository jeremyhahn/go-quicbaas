# Go SDK — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/go-sdk/`.

This document provides a complete API reference for the QuicBaaS Go SDK.

## Installation

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go"
```

## Client

### NewClient

Creates a new QuicBaaS client connected to the specified address.

```go
func NewClient(address string, opts ...Option) (Client, error)
```

**Parameters:**

| Name | Type | Description |
|------|------|-------------|
| address | string | Server address (TCP, Unix socket, or HTTP URL) |
| opts | ...Option | Optional configuration options |

**Address Formats:**

- TCP: `"localhost:50051"`
- Unix socket: `"/var/run/quicbaas.sock"` (auto-detected)
- Unix socket (explicit): `"unix:///var/run/quicbaas.sock"`
- Unix abstract: `"unix-abstract://quicbaas"`
- HTTP/HTTPS: `"http://localhost:8080"` (triggers REST transport)

**Example:**

```go
client, err := quicbaas.NewClient("localhost:50051",
    quicbaas.WithTimeout(10*time.Second),
    quicbaas.WithTLS(quicbaas.TLSConfig{
        CertFile: "/path/to/client.crt",
        KeyFile:  "/path/to/client.key",
        CAFile:   "/path/to/ca.crt",
    }),
)
if err != nil {
    log.Fatal(err)
}
defer client.Close()
```

---

### Client Interface

```go
type Client interface {
    // Core data operations
    KV() KVClient
    Batch() BatchClient
    Transaction() TransactionClient
    Query() QueryClient

    // Cluster operations
    Cluster() ClusterClient
    Health() HealthClient

    // Security operations
    Auth() AuthClient
    Seal() SealClient
    KMS() KMSClient
    Secrets() SecretsClient
    Crypto() CryptoClient

    // Identity and Access Management
    IAM() IAMClient

    // Specialized data operations
    Vector() VectorClient
    TimeSeries() TimeSeriesClient
    Backup() BackupClient

    // Connection management
    Transport() Transport
    Close() error
}
```

---

## Client Options

### WithTimeout

Sets the request timeout.

```go
func WithTimeout(d time.Duration) Option
```

**Default:** 30 seconds

### WithKeepalive

Sets the keepalive parameters.

```go
func WithKeepalive(time, timeout time.Duration) Option
```

**Default:** time=10s, timeout=5s

### WithRetry

Configures retry behavior.

```go
func WithRetry(maxRetries int, backoff time.Duration) Option
```

**Default:** maxRetries=3, backoff=100ms

### WithMaxRetryBackoff

Sets the maximum retry backoff duration.

```go
func WithMaxRetryBackoff(d time.Duration) Option
```

**Default:** 5 seconds

### WithTLS

Configures mTLS authentication using certificate files.

```go
func WithTLS(cfg TLSConfig) Option
```

**TLSConfig:**

| Field | Type | Description |
|-------|------|-------------|
| CertFile | string | Path to client certificate file |
| KeyFile | string | Path to client private key file |
| CAFile | string | Path to CA certificate file |
| ServerName | string | Override server name for verification |
| InsecureSkipVerify | bool | Disable server certificate verification |

### WithTLSConfig

Sets a custom tls.Config directly.

```go
func WithTLSConfig(cfg *tls.Config) Option
```

### WithInsecure

Disables TLS (development only).

```go
func WithInsecure() Option
```

### WithConnectionPool

Enables connection pooling (gRPC only).

```go
func WithConnectionPool(minSize, maxSize int) Option
```

**Default:** minSize=5, maxSize=50

### WithPoolMaxAge

Sets the maximum age of pooled connections.

```go
func WithPoolMaxAge(d time.Duration) Option
```

**Default:** 1 hour

### WithPoolHealthInterval

Sets the health check interval for pooled connections.

```go
func WithPoolHealthInterval(d time.Duration) Option
```

**Default:** 30 seconds

### WithREST

Configures the client to use REST transport.

```go
func WithREST() Option
```

### WithQUIC

Configures the client to use QUIC transport.

```go
func WithQUIC() Option
```

---

## KVClient

Provides key-value storage operations.

### Put

Stores a key-value pair.

```go
func Put(ctx context.Context, req *PutRequest) (*PutResponse, error)
```

**PutRequest:**

| Field | Type | Description |
|-------|------|-------------|
| EntityType | string | Entity type/namespace (required) |
| Key | string | Key identifier (required) |
| Value | []byte | Value to store |
| TTL | time.Duration | Optional time-to-live |

**PutResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Revision | uint64 | Revision number after the operation |

**Example:**

```go
resp, err := client.KV().Put(ctx, &quicbaas.PutRequest{
    EntityType: "users",
    Key:        "user-123",
    Value:      []byte(`{"name": "John"}`),
    TTL:        24 * time.Hour,
})
```

### Get

Retrieves a value by key.

```go
func Get(ctx context.Context, req *GetRequest) (*GetResponse, error)
```

**GetRequest:**

| Field | Type | Description |
|-------|------|-------------|
| EntityType | string | Entity type/namespace (required) |
| Key | string | Key identifier (required) |
| Revision | uint64 | Optional: get value at specific revision |

**GetResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Found | bool | Whether the key was found |
| Value | []byte | The stored value |
| Revision | uint64 | Current revision number |

**Example:**

```go
resp, err := client.KV().Get(ctx, &quicbaas.GetRequest{
    EntityType: "users",
    Key:        "user-123",
})
if resp.Found {
    fmt.Printf("Value: %s\n", resp.Value)
}
```

### Delete

Removes a key.

```go
func Delete(ctx context.Context, req *DeleteRequest) (*DeleteResponse, error)
```

**DeleteRequest:**

| Field | Type | Description |
|-------|------|-------------|
| EntityType | string | Entity type/namespace (required) |
| Key | string | Key identifier (required) |

**DeleteResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Deleted | bool | Whether the key was deleted |
| Revision | uint64 | Revision number after the operation |

### List

Lists keys matching a prefix.

```go
func List(ctx context.Context, req *ListRequest) (*ListResponse, error)
```

**ListRequest:**

| Field | Type | Description |
|-------|------|-------------|
| EntityType | string | Entity type/namespace (required) |
| Prefix | string | Key prefix filter |
| Limit | int | Maximum number of keys to return |
| Cursor | string | Pagination cursor |

**ListResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Keys | []string | List of matching keys |
| NextCursor | string | Cursor for next page |
| HasMore | bool | Whether more results exist |

**Example:**

```go
resp, err := client.KV().List(ctx, &quicbaas.ListRequest{
    EntityType: "users",
    Prefix:     "user-",
    Limit:      100,
})
for _, key := range resp.Keys {
    fmt.Println(key)
}
```

### Watch

Watches a specific key for changes.

```go
func Watch(ctx context.Context, key string, opts ...WatchOption) (Watcher, error)
```

**WatchOptions:**

| Option | Description |
|--------|-------------|
| WithStartRevision(rev uint64) | Start watching from a specific revision |
| WithPrevKV() | Include previous value in events |
| WithProgressNotify() | Enable periodic progress notifications |
| WithFilterPut() | Filter out PUT events |
| WithFilterDelete() | Filter out DELETE events |

**Example:**

```go
watcher, err := client.KV().Watch(ctx, "config/settings",
    quicbaas.WithPrevKV(),
)
if err != nil {
    log.Fatal(err)
}
defer watcher.Close()

for event := range watcher.Events() {
    fmt.Printf("Event: %s Key: %s\n", event.Type, event.Key)
}
```

### WatchPrefix

Watches all keys with a given prefix.

```go
func WatchPrefix(ctx context.Context, prefix string, opts ...WatchOption) (Watcher, error)
```

Uses the same options as Watch.

---

### Watcher Interface

```go
type Watcher interface {
    Events() <-chan *WatchEvent
    Err() error
    Close() error
}
```

**WatchEvent:**

| Field | Type | Description |
|-------|------|-------------|
| Type | EventType | EventPut or EventDelete |
| Key | string | The affected key |
| Value | []byte | New value (for PUT events) |
| PrevValue | []byte | Previous value (if WithPrevKV used) |
| Revision | uint64 | Event revision number |
| Timestamp | time.Time | Event timestamp |

---

## BatchClient

Provides atomic batch operations.

### Execute

Executes multiple operations atomically.

```go
func Execute(ctx context.Context, req *BatchRequest) (*BatchResponse, error)
```

**BatchRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Operations | []BatchOperation | List of operations to execute |

**BatchOperation:**

| Field | Type | Description |
|-------|------|-------------|
| Type | BatchOpType | BatchOpPut or BatchOpDelete |
| EntityType | string | Entity type/namespace |
| Key | string | Key identifier |
| Value | []byte | Value (for PUT operations) |

**BatchResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Revision | uint64 | Revision after batch execution |
| Results | []BatchResult | Result for each operation |

**Example:**

```go
resp, err := client.Batch().Execute(ctx, &quicbaas.BatchRequest{
    Operations: []quicbaas.BatchOperation{
        {Type: quicbaas.BatchOpPut, EntityType: "users", Key: "user-1", Value: []byte("data1")},
        {Type: quicbaas.BatchOpPut, EntityType: "users", Key: "user-2", Value: []byte("data2")},
        {Type: quicbaas.BatchOpDelete, EntityType: "users", Key: "user-old"},
    },
})
```

---

## TransactionClient

Provides compare-and-swap transactions.

### Execute

Executes a conditional transaction.

```go
func Execute(ctx context.Context, req *TxnRequest) (*TxnResponse, error)
```

**TxnRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Compare | []Compare | Conditions that must be true |
| Success | []TxnOp | Operations if all conditions pass |
| Failure | []TxnOp | Operations if any condition fails |

**Compare:**

| Field | Type | Description |
|-------|------|-------------|
| Key | string | Key to compare |
| Target | CompareTarget | CompareVersion, CompareValue, CompareModRevision, CompareCreateRevision |
| Result | CompareResult | CompareEqual, CompareNotEqual, CompareLess, CompareGreater |
| Value | []byte | Value for comparison |
| Revision | uint64 | Revision for comparison |

**TxnOp:**

| Field | Type | Description |
|-------|------|-------------|
| Type | TxnOpType | TxnOpPut, TxnOpGet, TxnOpDelete |
| EntityType | string | Entity type/namespace |
| Key | string | Key identifier |
| Value | []byte | Value (for PUT operations) |

**TxnResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Succeeded | bool | Whether conditions were satisfied |
| Revision | uint64 | Revision after transaction |
| Responses | []TxnOpResponse | Results of executed operations |

**Example:**

```go
resp, err := client.Transaction().Execute(ctx, &quicbaas.TxnRequest{
    Compare: []quicbaas.Compare{
        {Key: "counter", Target: quicbaas.CompareValue, Result: quicbaas.CompareEqual, Value: []byte("5")},
    },
    Success: []quicbaas.TxnOp{
        {Type: quicbaas.TxnOpPut, Key: "counter", Value: []byte("6")},
    },
    Failure: []quicbaas.TxnOp{
        {Type: quicbaas.TxnOpGet, Key: "counter"},
    },
})
```

---

## QueryClient

Provides secondary index queries.

### Execute

Executes a query against secondary indexes.

```go
func Execute(ctx context.Context, req *QueryRequest) (*QueryResponse, error)
```

**QueryRequest:**

| Field | Type | Description |
|-------|------|-------------|
| EntityType | string | Entity type to query |
| Index | string | Index name |
| Value | interface{} | Value to match |
| Operator | QueryOperator | Comparison operator |
| Limit | int | Maximum results |
| Cursor | string | Pagination cursor |

**QueryOperator values:**

- QueryEqual
- QueryNotEqual
- QueryLessThan
- QueryLessThanOrEqual
- QueryGreaterThan
- QueryGreaterThanOrEqual
- QueryPrefix
- QueryRange

**QueryResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Results | []QueryResult | Matching results |
| NextCursor | string | Cursor for next page |
| HasMore | bool | Whether more results exist |

---

## ClusterClient

Provides cluster management operations.

### Status

Returns the current cluster status.

```go
func Status(ctx context.Context) (*ClusterStatus, error)
```

**ClusterStatus:**

| Field | Type | Description |
|-------|------|-------------|
| ClusterID | string | Unique cluster identifier |
| LeaderID | string | Current leader node ID |
| Members | []ClusterMember | List of cluster members |
| Healthy | bool | Overall cluster health |

### Nodes

Returns information about all cluster nodes.

```go
func Nodes(ctx context.Context) (*NodesResponse, error)
```

**NodesResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Nodes | []NodeInfo | List of node information |

**NodeInfo:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Node identifier |
| Name | string | Node name |
| Address | string | Node address |
| State | NodeState | NodeStateHealthy, NodeStateUnhealthy, NodeStateLeaving |

### AddNode

Adds a new node to the cluster.

```go
func AddNode(ctx context.Context, req *AddNodeRequest) error
```

**AddNodeRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Address | string | Address of the new node |
| Name | string | Optional node name |

### RemoveNode

Removes a node from the cluster.

```go
func RemoveNode(ctx context.Context, req *RemoveNodeRequest) error
```

**RemoveNodeRequest:**

| Field | Type | Description |
|-------|------|-------------|
| NodeID | string | ID of the node to remove |

---

## HealthClient

Provides health check operations.

### Ping

Checks basic connectivity.

```go
func Ping(ctx context.Context) error
```

Returns nil if the server is reachable.

### Liveness

Returns liveness probe status.

```go
func Liveness(ctx context.Context) (*HealthResponse, error)
```

### Readiness

Returns readiness probe status.

```go
func Readiness(ctx context.Context) (*HealthResponse, error)
```

**HealthResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Healthy | bool | Whether the check passed |
| Status | string | Status message |
| Details | map[string]interface{} | Additional details |

---

## AuthClient

Provides OAuth2 / OIDC authentication, WebAuthn/FIDO2 for users, and mTLS for service accounts.

**Note:** QuicBaaS is passwordless. Users authenticate via WebAuthn or smartcard (see Passwordless Authentication). Service accounts use OAuth2 client credentials with mTLS. Direct `Login` method is deprecated.

### ClientCredentials

Obtain an access token for a service account via OAuth2 client credentials flow (RFC 6749 §4.4). The client must present an mTLS certificate matching the enrolled `cnf.x5t#S256`.

```go
func ClientCredentials(ctx context.Context, req *ClientCredentialsRequest) (*TokenResponse, error)
```

**ClientCredentialsRequest:**

| Field | Type | Description |
|-------|------|-------------|
| ClientID | string | Service account client ID |
| Scope | string | Optional scopes (space-separated) |

**TokenResponse:**

| Field | Type | Description |
|-------|------|-------------|
| AccessToken | string | JWT access token (bound to mTLS certificate via `cnf.x5t#S256`) |
| TokenType | string | Always `Bearer` |
| ExpiresIn | int | Token expiration in seconds |
| Scope | string | Granted scopes |
| CNF | *CNF | Certificate binding info (`x5t#S256`) |

**Example:**

```go
resp, err := client.Auth().ClientCredentials(ctx, &quicbaas.ClientCredentialsRequest{
    ClientID: "svc_abc123xyz",
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("Access Token: %s (expires in %d seconds)\n", resp.AccessToken, resp.ExpiresIn)
```

### RefreshToken

Refreshes an access token using a refresh token.

```go
func RefreshToken(ctx context.Context, refreshToken string) (*RefreshTokenResponse, error)
```

**RefreshTokenResponse:**

| Field | Type | Description |
|-------|------|-------------|
| AccessToken | string | New JWT access token |
| RefreshToken | string | New refresh token (if rotation enabled) |
| ExpiresAt | time.Time | New token expiration |

### ValidateToken

Validates an access token and returns its claims.

```go
func ValidateToken(ctx context.Context, accessToken string) (*TokenClaims, error)
```

**TokenClaims:**

| Field | Type | Description |
|-------|------|-------------|
| Valid | bool | Whether the token is valid |
| Subject | string | User ID |
| Username | string | Username |
| Roles | []string | User's roles |
| Permissions | []string | User's permissions |
| ExpiresAt | time.Time | Token expiration |
| IssuedAt | time.Time | Token issue time |

### Logout

Invalidates an access token.

```go
func Logout(ctx context.Context, accessToken string) error
```

### BeginFederatedLogin

Initiates an OIDC/OAuth2 federated login flow.

```go
func BeginFederatedLogin(ctx context.Context, req *BeginFederatedLoginRequest) (*BeginFederatedLoginResponse, error)
```

**BeginFederatedLoginRequest:**

| Field | Type | Description |
|-------|------|-------------|
| TenantID | string | Tenant identifier |
| ProviderID | string | Identity provider ID (e.g., "google", "okta") |
| RedirectURI | string | Callback URL after authentication |
| Scopes | []string | OAuth2 scopes to request |
| State | string | Optional state parameter (auto-generated if empty) |

**BeginFederatedLoginResponse:**

| Field | Type | Description |
|-------|------|-------------|
| AuthorizationURL | string | URL to redirect user to |
| State | string | State parameter for verification |

### HandleFederatedCallback

Handles the callback from an identity provider.

```go
func HandleFederatedCallback(ctx context.Context, req *HandleFederatedCallbackRequest) (*HandleFederatedCallbackResponse, error)
```

**HandleFederatedCallbackRequest:**

| Field | Type | Description |
|-------|------|-------------|
| TenantID | string | Tenant identifier |
| ProviderID | string | Identity provider ID |
| Code | string | Authorization code from callback |
| State | string | State parameter for verification |
| RedirectURI | string | Callback URL (must match begin request) |

**HandleFederatedCallbackResponse:**

| Field | Type | Description |
|-------|------|-------------|
| AccessToken | string | JWT access token |
| RefreshToken | string | JWT refresh token |
| ExpiresAt | time.Time | Token expiration |
| UserID | string | User identifier |
| NewUser | bool | Whether this is a new user |

### ListProviders

Lists available identity providers for a tenant.

```go
func ListProviders(ctx context.Context, tenantID string) (*ListProvidersResponse, error)
```

**ListProvidersResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Providers | []Provider | List of available providers |

### GetProvider

Gets details about a specific identity provider.

```go
func GetProvider(ctx context.Context, tenantID, providerID string) (*Provider, error)
```

**Provider:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Provider identifier |
| Name | string | Display name |
| Type | string | Provider type (oidc, oauth2, saml) |
| Enabled | bool | Whether provider is enabled |
| IssuerURL | string | OIDC issuer URL |
| AuthURL | string | Authorization endpoint |
| Scopes | []string | Supported scopes |

### GetSession

Retrieves details about a specific session.

```go
func GetSession(ctx context.Context, sessionID string) (*Session, error)
```

**Session:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Session identifier |
| UserID | string | User identifier |
| IPAddress | string | Client IP address |
| UserAgent | string | Client user agent |
| CreatedAt | time.Time | Session creation time |
| LastActive | time.Time | Last activity time |
| ExpiresAt | time.Time | Session expiration |
| MFAVerified | bool | Whether MFA was completed |

### ListSessions

Lists all active sessions for the current user.

```go
func ListSessions(ctx context.Context) (*ListSessionsResponse, error)
```

**ListSessionsResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Sessions | []Session | List of active sessions |

### RevokeSession

Revokes a specific session.

```go
func RevokeSession(ctx context.Context, sessionID string) error
```

### RevokeAllSessions

Revokes all sessions for the current user.

```go
func RevokeAllSessions(ctx context.Context) error
```

### IntrospectToken

Introspects a token (RFC 7662).

```go
func IntrospectToken(ctx context.Context, token string) (*TokenIntrospection, error)
```

**TokenIntrospection:**

| Field | Type | Description |
|-------|------|-------------|
| Active | bool | Whether the token is active |
| Scope | string | Token scope |
| ClientID | string | Client identifier |
| Username | string | Username |
| TokenType | string | Token type (Bearer) |
| ExpiresAt | time.Time | Token expiration |
| IssuedAt | time.Time | Token issue time |
| Subject | string | Subject (user ID) |
| Audience | string | Token audience |
| Issuer | string | Token issuer |

### GetJWKS

Retrieves the JSON Web Key Set for token verification.

```go
func GetJWKS(ctx context.Context) (*JWKS, error)
```

**JWKS:**

| Field | Type | Description |
|-------|------|-------------|
| Keys | []JWK | List of public keys |

**JWK:**

| Field | Type | Description |
|-------|------|-------------|
| KTY | string | Key type (RSA, EC) |
| Use | string | Key use (sig) |
| KID | string | Key identifier |
| ALG | string | Algorithm |
| N | string | RSA modulus (for RSA keys) |
| E | string | RSA exponent (for RSA keys) |
| CRV | string | Curve name (for EC keys) |
| X | string | X coordinate (for EC keys) |
| Y | string | Y coordinate (for EC keys) |

### GetOpenIDConfiguration

Retrieves the OpenID Connect discovery document.

```go
func GetOpenIDConfiguration(ctx context.Context) (*OpenIDConfiguration, error)
```

**OpenIDConfiguration:**

| Field | Type | Description |
|-------|------|-------------|
| Issuer | string | Authorization server issuer |
| AuthorizationEndpoint | string | OAuth2 authorization endpoint |
| TokenEndpoint | string | OAuth2 token endpoint |
| UserinfoEndpoint | string | OIDC userinfo endpoint |
| JWKSURI | string | JSON Web Key Set URL |
| ScopesSupported | []string | Supported OAuth2 scopes |
| ResponseTypesSupported | []string | Supported response types |
| GrantTypesSupported | []string | Supported grant types |
| TokenEndpointAuthMethodsSupported | []string | Supported token auth methods |
| ClaimsSupported | []string | Supported claims |
| MFASupported | bool | Whether MFA is supported |

---

## SealClient

Provides seal/unseal operations for encryption key management.

### Status

Returns the current seal status.

```go
func Status(ctx context.Context) (*SealStatus, error)
```

**SealStatus:**

| Field | Type | Description |
|-------|------|-------------|
| Sealed | bool | Whether the database is sealed |
| Initialized | bool | Whether initialization has occurred |
| Progress | int | Number of unseal keys provided |
| Required | int | Number of keys required to unseal |
| ClusterName | string | Cluster name |
| ClusterVersion | string | Cluster version |

### Init

Initializes the seal with Shamir secret sharing.

```go
func Init(ctx context.Context, req *InitRequest) (*InitResponse, error)
```

**InitRequest:**

| Field | Type | Description |
|-------|------|-------------|
| SecretShares | int | Total number of key shares to generate |
| SecretThreshold | int | Number of shares required to unseal |

**InitResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Keys | []string | Generated unseal keys (store securely) |
| RootToken | string | Initial root token |

**Example:**

```go
resp, err := client.Seal().Init(ctx, &quicbaas.InitRequest{
    SecretShares:    5,
    SecretThreshold: 3,
})
// Store keys securely - they cannot be retrieved again
```

### Unseal

Provides an unseal key to progress toward unsealing.

```go
func Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error)
```

**UnsealRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Key | string | One unseal key |
| Reset | bool | Reset unseal progress |

**UnsealResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Sealed | bool | Whether still sealed |
| Progress | int | Current unseal progress |
| Required | int | Keys required to unseal |

### Seal

Seals the database, requiring unseal keys to access data.

```go
func Seal(ctx context.Context) error
```

---

## KMSClient

Provides key management operations.

### GenerateKey

Generates a new encryption key.

```go
func GenerateKey(ctx context.Context, req *GenerateKeyRequest) (*GenerateKeyResponse, error)
```

**GenerateKeyRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Key name |
| Algorithm | KeyAlgorithm | Key algorithm |
| Usage | []KeyUsage | Permitted key usages |

**KeyAlgorithm values:**

- AlgorithmAES256GCM
- AlgorithmChaCha20Poly1305
- AlgorithmRSA2048
- AlgorithmRSA4096
- AlgorithmECDSAP256
- AlgorithmECDSAP384
- AlgorithmED25519

**KeyUsage values:**

- KeyUsageEncrypt
- KeyUsageDecrypt
- KeyUsageSign
- KeyUsageVerify
- KeyUsageWrap
- KeyUsageUnwrap

**GenerateKeyResponse:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Generated key identifier |
| Version | int | Key version (starts at 1) |
| Algorithm | KeyAlgorithm | Algorithm used |

### GetKey

Retrieves key metadata.

```go
func GetKey(ctx context.Context, keyID string) (*KeyInfo, error)
```

**KeyInfo:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| Name | string | Key name |
| Algorithm | KeyAlgorithm | Key algorithm |
| Version | int | Current version |
| Usage | []KeyUsage | Permitted usages |
| CreatedAt | time.Time | Creation timestamp |
| RotatedAt | time.Time | Last rotation timestamp |

### ListKeys

Lists all keys.

```go
func ListKeys(ctx context.Context) (*ListKeysResponse, error)
```

**ListKeysResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Keys | []KeyInfo | List of key metadata |

### RotateKey

Rotates a key to a new version.

```go
func RotateKey(ctx context.Context, keyID string) (*RotateKeyResponse, error)
```

**RotateKeyResponse:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| NewVersion | int | New version number |

### DeleteKey

Deletes a key.

```go
func DeleteKey(ctx context.Context, keyID string) error
```

---

## SecretsClient

Provides secrets management operations.

### Store

Stores a secret.

```go
func Store(ctx context.Context, req *StoreSecretRequest) (*StoreSecretResponse, error)
```

**StoreSecretRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Path | string | Secret path |
| Data | map[string]interface{} | Secret data |
| Metadata | map[string]string | Optional metadata |

**StoreSecretResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Version | int | Secret version |

**Example:**

```go
resp, err := client.Secrets().Store(ctx, &quicbaas.StoreSecretRequest{
    Path: "database/postgres",
    Data: map[string]interface{}{
        "username": "admin",
        "password": "secret123",
    },
})
```

### Get

Retrieves a secret.

```go
func Get(ctx context.Context, path string, opts ...SecretOption) (*Secret, error)
```

**SecretOption:**

| Option | Description |
|--------|-------------|
| WithSecretVersion(version int) | Retrieve a specific version |

**Secret:**

| Field | Type | Description |
|-------|------|-------------|
| Path | string | Secret path |
| Data | map[string]interface{} | Secret data |
| Metadata | map[string]string | Associated metadata |
| Version | int | Secret version |
| CreatedAt | time.Time | Creation timestamp |

### List

Lists secrets under a path prefix.

```go
func List(ctx context.Context, prefix string) (*ListSecretsResponse, error)
```

**ListSecretsResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Paths | []string | List of secret paths |

### Delete

Deletes a secret.

```go
func Delete(ctx context.Context, path string) error
```

---

## CryptoClient

Provides encryption/decryption as a service.

### Encrypt

Encrypts data using a managed key.

```go
func Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error)
```

**EncryptRequest:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| Plaintext | []byte | Data to encrypt |
| Context | []byte | Optional additional authenticated data (AAD) |

**EncryptResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Ciphertext | []byte | Encrypted data |
| KeyVersion | int | Key version used |

### Decrypt

Decrypts data using a managed key.

```go
func Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error)
```

**DecryptRequest:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| Ciphertext | []byte | Data to decrypt |
| Context | []byte | AAD used during encryption |

**DecryptResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Plaintext | []byte | Decrypted data |

### Sign

Signs data using a managed key.

```go
func Sign(ctx context.Context, req *SignRequest) (*SignResponse, error)
```

**SignRequest:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| Data | []byte | Data to sign |
| HashAlg | HashAlgorithm | HashSHA256, HashSHA384, or HashSHA512 |

**SignResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Signature | []byte | Digital signature |
| KeyVersion | int | Key version used |

### Verify

Verifies a signature.

```go
func Verify(ctx context.Context, req *VerifyRequest) (*VerifyResponse, error)
```

**VerifyRequest:**

| Field | Type | Description |
|-------|------|-------------|
| KeyID | string | Key identifier |
| Data | []byte | Original data |
| Signature | []byte | Signature to verify |
| HashAlg | HashAlgorithm | Hash algorithm used |

**VerifyResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Valid | bool | Whether signature is valid |

---

## VectorClient

Provides vector similarity search operations.

### Insert

Inserts a vector into a collection.

```go
func Insert(ctx context.Context, req *VectorInsertRequest) (*VectorInsertResponse, error)
```

**VectorInsertRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Collection | string | Collection name |
| ID | string | Vector identifier |
| Vector | []float32 | Vector embedding |
| Metadata | map[string]interface{} | Optional metadata |

**VectorInsertResponse:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Inserted vector ID |

**Example:**

```go
resp, err := client.Vector().Insert(ctx, &quicbaas.VectorInsertRequest{
    Collection: "documents",
    ID:         "doc-123",
    Vector:     []float32{0.1, 0.2, 0.3, ...},
    Metadata: map[string]interface{}{
        "title": "Example Document",
    },
})
```

### Search

Searches for similar vectors.

```go
func Search(ctx context.Context, req *VectorSearchRequest) (*VectorSearchResponse, error)
```

**VectorSearchRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Collection | string | Collection name |
| Vector | []float32 | Query vector |
| TopK | int | Number of results to return |
| Filter | map[string]interface{} | Optional metadata filter |

**VectorSearchResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Results | []VectorSearchResult | Matching vectors |

**VectorSearchResult:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Vector identifier |
| Score | float32 | Similarity score |
| Vector | []float32 | The vector |
| Metadata | map[string]interface{} | Associated metadata |

**Example:**

```go
resp, err := client.Vector().Search(ctx, &quicbaas.VectorSearchRequest{
    Collection: "documents",
    Vector:     queryVector,
    TopK:       10,
})
for _, result := range resp.Results {
    fmt.Printf("ID: %s, Score: %f\n", result.ID, result.Score)
}
```

### Delete

Deletes a vector from a collection.

```go
func Delete(ctx context.Context, req *VectorDeleteRequest) error
```

**VectorDeleteRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Collection | string | Collection name |
| ID | string | Vector identifier |

---

## TimeSeriesClient

Provides time-series data operations.

### Write

Writes a time-series data point.

```go
func Write(ctx context.Context, req *TimeSeriesWriteRequest) error
```

**TimeSeriesWriteRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Metric | string | Metric name (required) |
| Tags | map[string]string | Optional key-value tags |
| Timestamp | time.Time | Data point timestamp (required) |
| Value | float64 | Numeric value |

**Example:**

```go
err := client.TimeSeries().Write(ctx, &quicbaas.TimeSeriesWriteRequest{
    Metric:    "cpu_usage",
    Tags:      map[string]string{"host": "server-1"},
    Timestamp: time.Now(),
    Value:     75.5,
})
```

### Query

Queries time-series data with optional aggregation.

```go
func Query(ctx context.Context, req *TimeSeriesQueryRequest) (*TimeSeriesQueryResponse, error)
```

**TimeSeriesQueryRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Metric | string | Metric name (required) |
| Tags | map[string]string | Optional tag filters |
| Start | time.Time | Start of time range (required) |
| End | time.Time | End of time range (required) |
| Aggregate | AggregateFunction | Aggregation function |
| Interval | time.Duration | Aggregation bucket size |

**AggregateFunction values:**

- AggregateNone - Raw data points
- AggregateSum - Sum of values
- AggregateAvg - Average of values
- AggregateMin - Minimum value
- AggregateMax - Maximum value
- AggregateCount - Count of values

**TimeSeriesQueryResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Series | []TimeSeriesPoint | Resulting data points |

**TimeSeriesPoint:**

| Field | Type | Description |
|-------|------|-------------|
| Timestamp | time.Time | Data point timestamp |
| Value | float64 | Numeric value |

**Example:**

```go
resp, err := client.TimeSeries().Query(ctx, &quicbaas.TimeSeriesQueryRequest{
    Metric:    "cpu_usage",
    Tags:      map[string]string{"host": "server-1"},
    Start:     time.Now().Add(-1 * time.Hour),
    End:       time.Now(),
    Aggregate: quicbaas.AggregateAvg,
    Interval:  5 * time.Minute,
})
for _, point := range resp.Series {
    fmt.Printf("%s: %f\n", point.Timestamp, point.Value)
}
```

---

## BackupClient

Provides backup and restore operations.

### Create

Creates a new backup.

```go
func Create(ctx context.Context, req *BackupRequest) (*BackupResponse, error)
```

**BackupRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Destination | string | Backup destination path (required) |
| Compress | bool | Enable compression |
| Encrypt | bool | Enable encryption |

**BackupResponse:**

| Field | Type | Description |
|-------|------|-------------|
| BackupID | string | Unique backup identifier |
| Size | int64 | Backup size in bytes |
| Duration | time.Duration | Time taken to create backup |
| CreatedAt | time.Time | Creation timestamp |

**Example:**

```go
resp, err := client.Backup().Create(ctx, &quicbaas.BackupRequest{
    Destination: "s3://my-bucket/backups/",
    Compress:    true,
    Encrypt:     true,
})
fmt.Printf("Backup created: %s (%d bytes)\n", resp.BackupID, resp.Size)
```

### Restore

Restores data from a backup.

```go
func Restore(ctx context.Context, req *RestoreRequest) (*RestoreResponse, error)
```

**RestoreRequest:**

| Field | Type | Description |
|-------|------|-------------|
| BackupID | string | Backup ID to restore |
| Source | string | Alternative: source path |

**RestoreResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Restored | bool | Whether restore succeeded |
| Duration | time.Duration | Time taken to restore |

### List

Lists all available backups.

```go
func List(ctx context.Context) (*ListBackupsResponse, error)
```

**ListBackupsResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Backups | []BackupInfo | List of available backups |

**BackupInfo:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Backup identifier |
| Size | int64 | Backup size in bytes |
| CreatedAt | time.Time | Creation timestamp |
| Encrypted | bool | Whether backup is encrypted |

---

## IAMClient

The IAMClient provides Identity and Access Management (IAM) with Attribute-Based Access Control (ABAC).

```go
iam := client.Identity().IAM()
```

### IAMClient Interface

```go
type IAMClient interface {
    Tenant() TenantClient   // Multi-tenant management
    User() UserClient       // User CRUD and role assignment
    Role() RoleClient       // Role CRUD with permissions
    Policy() PolicyClient   // ABAC policy management
    PEP() PEPClient         // Policy Enforcement Point (authorization)
    App() AppClient         // OAuth2 application management
    Session() SessionClient // Session management
    Audit() AuditClient     // Audit log queries
    GDPR() GDPRClient       // GDPR compliance (consent, export, deletion)
    Admin() AdminClient     // Delegated administration
}
```

---

### TenantClient

Manages multi-tenant operations.

```go
tenant := client.Identity().IAM().Tenant()
```

#### Create

Creates a new tenant.

```go
func Create(ctx context.Context, req *CreateTenantRequest) (*Tenant, error)
```

**CreateTenantRequest:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Tenant identifier (optional; generated when empty) |
| Name | string | Tenant name |
| DisplayName | string | Human-readable display name (optional) |
| Description | string | Free-form description (optional) |
| Settings | map[string]string | Tenant settings |
| Metadata | map[string]string | Custom metadata |
| IsolationType | TenantIsolationType | Engine isolation strategy. **Create-only** |
| EngineQuotas | *EngineQuotas | Engine-level resource limits applied at creation |
| EngineEntitlements | *EngineEntitlements | Engine-level feature entitlements applied at creation |

The last three are the engine-side provisioning knobs. They are optional and
default to the engine's configured behavior, so existing callers are
unaffected; they travel identically on every transport (embedded, gRPC, Unix,
REST, QUIC, MCP).

`IsolationType` is create-only because the engine binds a tenant's isolation
when the tenant is created — there is no update path, since changing strategy
would mean relocating the tenant's data. `TenantIsolationPrefix` is the
default; `TenantIsolationBarrier`, `TenantIsolationVolume` and
`TenantIsolationRaft` give successively stronger separation.

A nil `EngineQuotas`/`EngineEntitlements` means "leave the engine's configured
defaults alone". That is deliberately distinct from a non-nil empty struct,
which sets every limit to the system default and denies every feature.

`EngineQuotas` is a **limits-only** view of the engine's `Quotas`, not an alias
of it. The engine's struct also carries the `Current*` usage counters it
maintains as consumption changes; those are outputs, and the gRPC create
request models limits only. Naming just the limits makes the accepted
create-time field set identical on every transport by construction: a Go caller
cannot set a counter, and strict decoding rejects `current_*` on the wire.
`(*EngineQuotas).Quotas()` widens it to the engine struct with the counters at
zero. Correct a counter with `UpdateQuotas`, never with tenant create.

`EngineEntitlements` stays a direct alias of the engine's `Entitlements`: every
field on it is a grant, so there is nothing output-only to exclude.

```go
tenant, err := client.Identity().IAM().Tenant().Create(ctx, &iam.CreateTenantRequest{
    ID:            "acme-corp",
    Name:          "Acme Corporation",
    IsolationType: iam.TenantIsolationBarrier,
    EngineQuotas: &iam.EngineQuotas{
        MaxDatabases:    20,
        MaxStorageBytes: 100 << 30,
    },
    EngineEntitlements: &iam.EngineEntitlements{
        AllowedIsolationModes: []string{"prefix", "barrier"},
        AllowDedicatedRaft:    true,
    },
})
```

All three names are exported by `sdk/go/client/iam`, so a consumer that may
only import `quicbaas/sdk/go/*` can name them. `TenantIsolationType` and
`EngineEntitlements` are aliases of the engine's own types and are therefore
assignable to and from them; `EngineQuotas` is a distinct limits-only struct
(see above) — convert with `(*EngineQuotas).Quotas()` when an engine `Quotas`
value is needed.

#### Get

Retrieves a tenant by ID.

```go
func Get(ctx context.Context, tenantID string) (*Tenant, error)
```

#### Update

Updates an existing tenant.

```go
func Update(ctx context.Context, tenantID string, req *UpdateTenantRequest) (*Tenant, error)
```

#### Delete

Deletes a tenant.

```go
func Delete(ctx context.Context, tenantID string) error
```

#### List

Lists all tenants.

```go
func List(ctx context.Context, opts ...ListOption) (*ListTenantsResponse, error)
```

---

### UserClient

Manages user operations and role assignments.

```go
user := client.Identity().IAM().User()
```

#### Create

Creates a new user.

```go
func Create(ctx context.Context, req *CreateUserRequest) (*User, error)
```

**CreateUserRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Username | string | Unique username |
| Email | string | Email address |
| FirstName | string | First name |
| LastName | string | Last name |
| Password | string | Initial password |
| TenantID | string | Tenant ID |
| Enabled | bool | Whether user is enabled |

#### Get

Retrieves a user by ID.

```go
func Get(ctx context.Context, userID string) (*User, error)
```

#### Update

Updates an existing user.

```go
func Update(ctx context.Context, userID string, req *UpdateUserRequest) (*User, error)
```

#### Delete

Deletes a user.

```go
func Delete(ctx context.Context, userID string) error
```

#### List

Lists users with pagination.

```go
func List(ctx context.Context, opts ...ListOption) (*ListUsersResponse, error)
```

#### AssignRole

Assigns a role to a user.

```go
func AssignRole(ctx context.Context, userID, roleID string) error
```

#### RemoveRole

Removes a role from a user.

```go
func RemoveRole(ctx context.Context, userID, roleID string) error
```

#### ListRoles

Lists roles assigned to a user.

```go
func ListRoles(ctx context.Context, userID string) (*ListRolesResponse, error)
```

---

### RoleClient

Manages role operations with permission bundles.

```go
role := client.Identity().IAM().Role()
```

#### Create

Creates a new role.

```go
func Create(ctx context.Context, req *CreateRoleRequest) (*Role, error)
```

**CreateRoleRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Role name |
| Description | string | Description |
| Permissions | []string | Permission strings |
| TenantID | string | Tenant ID |

#### Get

Retrieves a role by ID.

```go
func Get(ctx context.Context, roleID string) (*Role, error)
```

#### Update

Updates an existing role.

```go
func Update(ctx context.Context, roleID string, req *UpdateRoleRequest) (*Role, error)
```

#### Delete

Deletes a role.

```go
func Delete(ctx context.Context, roleID string) error
```

#### List

Lists roles with pagination.

```go
func List(ctx context.Context, opts ...ListOption) (*ListRolesResponse, error)
```

#### AddPermission

Adds a permission to a role.

```go
func AddPermission(ctx context.Context, roleID, permission string) error
```

#### RemovePermission

Removes a permission from a role.

```go
func RemovePermission(ctx context.Context, roleID, permission string) error
```

#### ListPermissions

Lists permissions in a role.

```go
func ListPermissions(ctx context.Context, roleID string) (*ListPermissionsResponse, error)
```

---

### PolicyClient

Manages ABAC (Attribute-Based Access Control) policies.

```go
policy := client.Identity().IAM().Policy()
```

#### Create

Creates a new ABAC policy.

```go
func Create(ctx context.Context, req *CreatePolicyRequest) (*Policy, error)
```

**CreatePolicyRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Policy name |
| Description | string | Description |
| Effect | string | "allow" or "deny" |
| Priority | int | Evaluation priority (higher first) |
| Subjects | PolicySubjects | Who the policy applies to |
| Actions | []string | Actions to match |
| Resources | []string | Resources to match |
| Conditions | []PolicyCondition | Conditions for evaluation |
| TenantID | string | Tenant ID |

**PolicySubjects:**

| Field | Type | Description |
|-------|------|-------------|
| Users | []string | User IDs |
| Groups | []string | Names matched against the subject's `groups` claim (the names of its roles) |
| Roles | []string | Role IDs |
| Attributes | map[string]string | Attribute requirements |

**PolicyCondition:**

| Field | Type | Description |
|-------|------|-------------|
| Field | string | Attribute field to check |
| Operator | string | Comparison operator (eq, ne, in, contains, etc.) |
| Value | interface{} | Value to compare against |

#### Get

Retrieves a policy by ID.

```go
func Get(ctx context.Context, policyID string) (*Policy, error)
```

#### Update

Updates an existing policy.

```go
func Update(ctx context.Context, policyID string, req *UpdatePolicyRequest) (*Policy, error)
```

#### Delete

Deletes a policy.

```go
func Delete(ctx context.Context, policyID string) error
```

#### List

Lists policies with pagination.

```go
func List(ctx context.Context, opts ...ListOption) (*ListPoliciesResponse, error)
```

#### Validate

Validates a policy for correctness without saving.

```go
func Validate(ctx context.Context, policy *Policy) (*ValidationResult, error)
```

#### Test

Tests a policy against sample authorization requests.

```go
func Test(ctx context.Context, policyID string, testCases []PolicyTestCase) (*PolicyTestResult, error)
```

**Example - Creating an ABAC Policy:**

```go
policy, err := client.Identity().IAM().Policy().Create(ctx, &iam.CreatePolicyRequest{
    Name:        "engineering-data-access",
    Description: "Allow engineers to read data resources",
    Effect:      "allow",
    Priority:    100,
    Subjects: iam.PolicySubjects{
        Roles: []string{"engineer"},
    },
    Actions:   []string{"read", "list"},
    Resources: []string{"data/*"},
    Conditions: []iam.PolicyCondition{
        {
            Field:    "request.ip",
            Operator: "in_cidr",
            Value:    "10.0.0.0/8",
        },
        {
            Field:    "time.hour",
            Operator: "between",
            Value:    []int{9, 17}, // Business hours
        },
    },
    TenantID: "tenant-123",
})
```

---

### PEPClient (Policy Enforcement Point)

Provides sub-5μs authorization checks for enforcing ABAC policies.

```go
pep := client.Identity().IAM().PEP()
```

#### Check

Checks if a single authorization request is allowed.

```go
func Check(ctx context.Context, req *CheckRequest) (*CheckResponse, error)
```

**CheckRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Subject | string | User or service making the request |
| Action | string | Action being performed |
| Resource | string | Resource being accessed |
| Context | map[string]interface{} | Additional context attributes |

**CheckResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Allowed | bool | Whether access is allowed |
| Reason | string | Explanation of decision |
| PolicyID | string | ID of matching policy |
| EvaluationTime | time.Duration | Time to evaluate |

#### CheckBatch

Checks multiple authorization requests in a single call.

```go
func CheckBatch(ctx context.Context, requests []*CheckRequest) ([]*CheckResponse, error)
```

**Example - Authorization Check:**

```go
resp, err := client.Identity().IAM().PEP().Check(ctx, &iam.CheckRequest{
    Subject:  "user-456",
    Action:   "read",
    Resource: "data/customer-records",
    Context: map[string]interface{}{
        "ip":         "10.0.1.5",
        "department": "engineering",
    },
})
if err != nil {
    log.Fatal(err)
}

if resp.Allowed {
    fmt.Println("Access granted")
} else {
    fmt.Printf("Access denied: %s\n", resp.Reason)
}
```

---

### AppClient

Manages OAuth2 application registration for client credentials flow.

```go
app := client.Identity().IAM().App()
```

#### Create

Registers a new OAuth2 application.

```go
func Create(ctx context.Context, req *CreateAppRequest) (*App, error)
```

**CreateAppRequest:**

| Field | Type | Description |
|-------|------|-------------|
| Name | string | Application name |
| RedirectURIs | []string | Allowed redirect URIs |
| Scopes | []string | Allowed scopes |
| TenantID | string | Tenant ID |

**App Response:**

| Field | Type | Description |
|-------|------|-------------|
| ClientID | string | OAuth2 client ID |
| ClientSecret | string | OAuth2 client secret (returned once) |

#### Get

Retrieves an application by ID.

```go
func Get(ctx context.Context, appID string) (*App, error)
```

#### Update

Updates an existing application.

```go
func Update(ctx context.Context, appID string, req *UpdateAppRequest) (*App, error)
```

#### Delete

Deletes an application.

```go
func Delete(ctx context.Context, appID string) error
```

#### List

Lists applications with pagination.

```go
func List(ctx context.Context, opts ...ListOption) (*ListAppsResponse, error)
```

#### RotateSecret

Rotates the client secret for an application.

```go
func RotateSecret(ctx context.Context, appID string) (*RotateSecretResponse, error)
```

---

### GDPRClient

Provides GDPR compliance features.

```go
gdpr := client.Identity().IAM().GDPR()
```

#### RecordConsent

Records user consent for data processing.

```go
func RecordConsent(ctx context.Context, req *RecordConsentRequest) error
```

**RecordConsentRequest:**

| Field | Type | Description |
|-------|------|-------------|
| UserID | string | User ID |
| Purpose | string | Purpose of consent |
| Granted | bool | Whether consent was granted |
| ExpiresAt | time.Time | Consent expiration |

#### GetConsent

Retrieves consent status for a user.

```go
func GetConsent(ctx context.Context, userID, purpose string) (*Consent, error)
```

#### RevokeConsent

Revokes previously granted consent.

```go
func RevokeConsent(ctx context.Context, userID, purpose string) error
```

#### ExportData

Exports all user data (data portability).

```go
func ExportData(ctx context.Context, userID string) (*DataExport, error)
```

#### DeleteData

Deletes all user data (right to be forgotten).

```go
func DeleteData(ctx context.Context, userID string) error
```

#### ListConsents

Lists all consent records for a user.

```go
func ListConsents(ctx context.Context, userID string) (*ListConsentsResponse, error)
```

---

### AuditClient

Queries audit logs for compliance and security.

```go
audit := client.Identity().IAM().Audit()
```

#### Query

Queries audit log entries.

```go
func Query(ctx context.Context, req *AuditQueryRequest) (*AuditQueryResponse, error)
```

**AuditQueryRequest:**

| Field | Type | Description |
|-------|------|-------------|
| StartTime | time.Time | Query start time |
| EndTime | time.Time | Query end time |
| Actor | string | Filter by actor |
| Action | string | Filter by action |
| Resource | string | Filter by resource |
| Limit | int | Max results |
| Cursor | string | Pagination cursor |

**AuditEntry:**

| Field | Type | Description |
|-------|------|-------------|
| ID | string | Entry ID |
| Timestamp | time.Time | When event occurred |
| Actor | string | Who performed action |
| Action | string | What action |
| Resource | string | On what resource |
| Success | bool | Whether action succeeded |
| Details | map[string]interface{} | Additional details |

#### GetEntry

Retrieves a specific audit entry.

```go
func GetEntry(ctx context.Context, entryID string) (*AuditEntry, error)
```

---

### AdminClient

Provides delegated administration and impersonation.

```go
admin := client.Identity().IAM().Admin()
```

#### CanManage

Checks if an admin can manage a target user.

```go
func CanManage(ctx context.Context, adminID, targetUserID string) (*CanManageResponse, error)
```

#### AssignAdmin

Assigns admin rights to a user for a scope.

```go
func AssignAdmin(ctx context.Context, req *AssignAdminRequest) error
```

**AssignAdminRequest:**

| Field | Type | Description |
|-------|------|-------------|
| UserID | string | User to grant admin rights |
| Scope | string | Scope of admin rights |
| Permissions | []string | Specific permissions |

#### RemoveAdmin

Removes admin rights from a user.

```go
func RemoveAdmin(ctx context.Context, userID, scope string) error
```

#### ListAdmins

Lists users with admin rights for a scope.

```go
func ListAdmins(ctx context.Context, scope string) (*ListAdminsResponse, error)
```

#### Impersonate

Creates an impersonation token to act as another user.

```go
func Impersonate(ctx context.Context, req *ImpersonateRequest) (*ImpersonateResponse, error)
```

**ImpersonateRequest:**

| Field | Type | Description |
|-------|------|-------------|
| TargetUserID | string | User to impersonate |
| Reason | string | Reason for impersonation |
| Duration | time.Duration | Token validity |

**ImpersonateResponse:**

| Field | Type | Description |
|-------|------|-------------|
| Token | string | Impersonation token |
| ExpiresAt | time.Time | Token expiration |

---

## Error Handling

The SDK returns typed errors that can be inspected for specific error conditions:

```go
import "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"

resp, err := client.KV().Get(ctx, req)
if err != nil {
    var dragonErr *errors.Error
    if errors.As(err, &dragonErr) {
        switch dragonErr.Code {
        case errors.CodeNotFound:
            // Handle not found
        case errors.CodeTimeout:
            // Handle timeout
        case errors.CodeUnavailable:
            // Handle server unavailable
        default:
            // Handle other errors
        }
    }
}
```

---

## Transport Types

The SDK supports multiple transport protocols:

| Type | Description |
|------|-------------|
| TransportGRPC | gRPC protocol (default) |
| TransportREST | REST/HTTP protocol |
| TransportQUIC | QUIC protocol |

Select transport using options:

```go
// gRPC (default)
client, _ := quicbaas.NewClient("localhost:50051")

// REST
client, _ := quicbaas.NewClient("http://localhost:8080", quicbaas.WithREST())

// QUIC
client, _ := quicbaas.NewClient("localhost:50051", quicbaas.WithQUIC())
```

---

## Mounting a host ServiceModule

A downstream host that embeds a QuicBaaS node (go-trusted-platform) mounts its
own capability plane on that node as a go-qrdb `ServiceModule`. One
registration is served over gRPC, unix, REST and MCP under the node's single
authorizer, alongside go-quicbaas's own module and go-xkms's.

The stack-owned types are re-exported here, so a module never imports
`go-qrdb`, `go-xkms` or `go-quicbaas/pkg`. Transport-specific **third-party**
types are not re-exported wholesale: `RegisterGRPC` still needs
`google.golang.org/grpc` for the module's generated `pb.Register<X>Server`,
`RegisterMCP` builds tools with `mcp-go`'s own types, and a test that
constructs a capture registrar supplies its own `chi.Router`.

| SDK symbol | Role |
|------------|------|
| `HostModule` | the interface a host implements (`Name`, `RegisterGRPC`, `RegisterHTTP`, `RegisterMCP`, `Start`, `Stop`) |
| `BaseHostModule` | embed for no-op defaults on every hook except `Name` |
| `ModuleHTTPRegistrar` | the permission-recording registrar `RegisterHTTP` receives |
| `NewModuleHTTPRegistrar` | build a capture registrar in module tests |
| `Permission`, `PermissionIdentityGated` | the `resource:action` token declared at each route |
| `ModuleGRPCRegistrar`, `ModuleMCPServer` | the hook argument types for `RegisterGRPC` / `RegisterMCP` |
| `ModuleMCPToolPermissions` | optional: declare each MCP tool's permission |
| `ModuleGRPCPublicMethods` | optional: gRPC/unix counterpart of `HandlePublic` |
| `ModuleAuthFallback`, `ModuleIdentity` | optional: authenticate the module's own session tokens |
| `ServiceModuleName`, `XKMSServiceModuleName` | the two reserved module names |
| `WithModule` | the `ServerOption` that mounts the module |

### Write the module

```go
type deviceModule struct {
    quicbaas.BaseHostModule
}

func (m *deviceModule) Name() string { return "trusted-platform" }

func (m *deviceModule) RegisterHTTP(r *quicbaas.ModuleHTTPRegistrar) {
    // Handle declares the route's permission at the registration site;
    // HandlePublic is reserved for genuine first-contact surfaces. A route
    // registered straight onto the embedded chi.Router bypasses permission
    // recording entirely and is rejected when the module is mounted, with
    // HostModuleUnregisteredRouteError.
    r.Handle(http.MethodGet, "/api/v1/devices", quicbaas.Permission("device:read"), m.listDevices)
    r.HandlePublic(http.MethodGet, "/api/v1/devices/health", m.health)
}

func (m *deviceModule) MCPToolPermissions() map[string]quicbaas.Permission {
    return map[string]quicbaas.Permission{"device_list": "device:read"}
}
```

### Mount it

```go
srv, err := quicbaas.NewServer(cfg, quicbaas.WithModule(&deviceModule{}))
if err != nil {
    return err
}
// BEFORE the Start check — see "Embedded server lifecycle" below.
defer srv.Shutdown(context.Background())

if err := srv.Start(); err != nil {
    return err
}
```

`WithModule` is repeatable — one call per module, mounted in declaration
order after `quicbaas` and `xkms`. This is the **only** supported shape: the
SDK exposes no qrdb server constructor, because building the node (audit sink,
shard router, REST/MCP resolvers, the xkms module, unified TLS) is
go-quicbaas's job as the composer. See
[../composition/architecture.md](../composition/architecture.md) §
*Host module seam* for the reasoning.

### Failure modes

All of these fail construction with a typed error rather than a panic:

| Condition | Error | Raised by |
|-----------|-------|-----------|
| `WithModule(nil)` | `*NilHostModuleError` | `NewServer` |
| `Name()` returns `""` | `*UnnamedHostModuleError` | `NewServer` |
| `Name()` is `quicbaas`, `xkms`, or an earlier host module | `*HostModuleNameConflictError` | `NewServer` |
| The module claims a REST route another mounted module already owns | `*HostModuleRouteConflictError` (names both owners and both spellings) | qrdb node construction |
| The module registers a route straight onto the registrar's embedded `chi.Router` | `*HostModuleUnregisteredRouteError` | qrdb node construction |
| The module claims a route under `/v1` or one of go-qrdb's top-level planes | `*HostModuleReservedRouteError` | qrdb node construction |
| The module's router cannot be enumerated to check for the above | `*HostModuleRouterInspectionError` (fails closed) | qrdb node construction |
| The module registers a pattern chi refuses (no leading `/`, bad regex, unsupported method), or adds router middleware | `*HostModuleInvalidRouteError` | qrdb node construction |

```go
var conflict *quicbaas.HostModuleNameConflictError
if errors.As(err, &conflict) {
    log.Fatalf("module %q is already registered by %s", conflict.Name, conflict.Owner)
}
```

Route comparison runs the patterns through a shadow `chi.Mux` — the same
matcher the live router uses — rather than comparing pattern text. chi matches
a path parameter by position, so `/devices/{id}` and `/devices/{deviceID}` are
one and the same route: registering the second silently replaces the first. A
regex constraint *is* part of the match, so `{id:[0-9]+}` and `{name}` coexist.
Routing the patterns through chi itself means the guard cannot drift from those
rules. (go-qrdb's own registry keys on raw pattern text and misses the renamed
parameter case.)

### No router middleware

A host module mounts **after** `quicbaas` and `xkms`, so the shared router
already holds routes by the time your `RegisterHTTP` runs — and chi refuses any
further router-level middleware at that point (*"all middlewares must be
defined before routes on a mux"*). Calling `r.Use(...)` fails the mount with
`*HostModuleInvalidRouteError`. Wrap your own handlers instead.

### What the guard does not cover

Two residuals belong to go-qrdb, which owns the live registrar (tracked in
go-qrdb#194):

- **go-qrdb's built-in routes** are not checked — see below.
- **`RegisterHTTP` must be repeatable.** Validating a module's REST surface
  means asking it for that surface, so the seam calls `RegisterHTTP` once
  against a throwaway router and go-qrdb calls it again against the live one.
  Write it as a pure declaration of your route table: a module that mutates
  state, counts calls, or refuses a second registration will not survive that,
  and a module that stashed the live `chi.Router` and added routes later from
  `Start` would not be described by what was validated. This is a requirement
  of mounting through `WithModule`, not of go-qrdb's `ServiceModule` interface;
  `quicbaas`'s module and `xkms`'s both satisfy it, pinned by a test. Lifting
  it needs a registration-time introspection hook from go-qrdb (go-qrdb#194).

### Namespace your routes

On a unified node **modules register before built-ins**, and a built-in whose
`(method, pattern)` a module already claimed yields to the module silently
(go-qrdb `pkg/api/rest` `routeIf`). A host module claiming `GET /kv` would
therefore take over a core endpoint, under *its* authorization contract, with
nothing reporting it.

The mount refuses a host route inside the node's own namespaces, with
`*HostModuleReservedRouteError`:

- **`/v1`** — reserved wholesale. It is the node's API-version namespace
  (`/v1/databases`, `/v1/collections`, `/v1/credentials`, `/v1/tenants`, ...),
  and reserving the whole segment covers every route go-qrdb adds there in
  future rather than a list that goes stale.
- go-qrdb's bare top-level planes: `/kv`, `/admin`, `/ts`, `/vector`, `/users`,
  `/roles`, `/tx`, `/batch`, `/index`, `/lock`, `/lease`, `/certs`,
  `/membership`, `/watch`, `/status`, `/sql`, `/snapshot`, `/permissions`,
  `/peers`, `/metrics`, `/health`, `/drain`, `/capabilities`.

This costs you nothing: `/api/v1/<your-module>/...` — the shape shown above —
never touches either, and the check applies to host modules only. What remains
uncovered is a plane go-qrdb adds under a *new* top-level segment after this
snapshot; go-qrdb#194 asks for the built-in table itself, which removes the
drift. **Put every route under a prefix of your own.**

### Transport gaps

The module seam covers **gRPC, unix, REST and MCP**. It does **not** cover:

- **QUIC** — go-qrdb's QUIC transport is a bespoke frame protocol with
  method-keyed dispatch rather than HTTP-over-chi, so `RegisterHTTP` does not
  reach it. A module needing QUIC parity serves it itself until go-qrdb adds a
  `RegisterQUIC` hook.
- **The embedded (in-process) transport** — that surface is each capability
  owner's own SDK. A host calls its module's Go API directly in-process
  instead of routing through the node.

Both are upstream go-qrdb gaps. File them against go-qrdb rather than working
around them downstream.

## Embedded server lifecycle

`Start()` does **not** unwind on failure. It returns at the first component
that fails, and every component started before it stays up: the embedded qrdb
node, an open audit log, the cache-cleanup goroutine, the lease reaper, the
scheduler poll loop.

**A non-nil error from `Start()` MUST be followed by `Shutdown()`.** The
daemon is unaffected — it logs and exits, and process teardown collects
everything — but a host program embedding the SDK has no such backstop and
leaks every one of those resources for the lifetime of its process.

`Shutdown()` is safe on a server that never started, on one that started
fully, and on one that stopped anywhere in between, and it is idempotent. So
the deferred call belongs immediately after `NewServer`, before the `Start`
error check — as in the *Mount it* example above.
