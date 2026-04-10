# KMS API Reference

## REST API

Base path: `/api/v1/kms`

Path parameters use URL encoding for hierarchical paths (e.g., `tenant-123%2Fprod%2Fdb-key`).

### Keys

#### Generate Key

```http
POST /api/v1/kms/keys
Content-Type: application/json
Authorization: Bearer {token}

{
  "name": "api-key",
  "description": "API encryption key",
  "key_attributes": {
    "algorithm": "AES-256-GCM",
    "key_usage": ["encrypt", "decrypt"],
    "exportable": false
  },
  "tags": {"env": "prod"},
  "owner": "team-security"
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | Human-readable key name |
| `description` | string | No | Key description |
| `key_attributes.algorithm` | string | Yes | AES-256-GCM, RSA-2048, ECDSA-P256, ED25519, etc. |
| `key_attributes.key_usage` | []string | Yes | encrypt, decrypt, sign, verify |
| `key_attributes.exportable` | bool | No | Allow key export (default: false) |
| `tags` | map | No | Key-value pairs for organization |
| `owner` | string | No | Key owner identifier |

**Response (201 Created):**
```json
{
  "key": {
    "key_id": "key-api-key-1704067200000000000",
    "version": 1,
    "algorithm": "AES-256-GCM",
    "status": "enabled",
    "created_at": 1704067200
  },
  "metadata": {
    "key_id": "key-api-key-1704067200000000000",
    "name": "api-key",
    "description": "API encryption key",
    "algorithm": "AES-256-GCM",
    "status": "enabled",
    "usage": ["encrypt", "decrypt"],
    "version": 1,
    "exportable": false,
    "deletion_protection": false,
    "created_at": 1704067200,
    "updated_at": 1704067200,
    "tags": {"env": "prod"},
    "owner": "team-security"
  },
  "timestamp": 1704067200
}
```

#### Get Key

```http
GET /api/v1/kms/keys/{name}
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "key": {
    "key_id": "key-api-key-1704067200000000000",
    "version": 3,
    "algorithm": "AES-256-GCM",
    "status": "enabled",
    "created_at": 1704067200
  },
  "metadata": {
    "key_id": "key-api-key-1704067200000000000",
    "name": "api-key",
    "algorithm": "AES-256-GCM",
    "status": "enabled",
    "version": 3,
    "exportable": false,
    "created_at": 1704067200,
    "updated_at": 1704153600
  },
  "timestamp": 1704153600
}
```

#### List Keys

```http
GET /api/v1/kms/keys
GET /api/v1/kms/keys?prefix=tenant-&limit=50&cursor={cursor}
Authorization: Bearer {token}
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `prefix` | string | - | Filter keys by ID prefix |
| `limit` | int | 100 | Max keys to return (1-1000) |
| `cursor` | string | - | Pagination cursor |

**Response (200 OK):**
```json
{
  "keys": [
    {
      "key_id": "key-api-key-1704067200000000000",
      "version": 3,
      "algorithm": "AES-256-GCM",
      "status": "enabled",
      "created_at": 1704067200
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "total_count": 1,
  "timestamp": 1704153600
}
```

#### Rotate Key

```http
POST /api/v1/kms/keys/{name}/rotate
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "old_version": 3,
  "new_version": 4,
  "timestamp": 1704240000
}
```

#### Delete Key

```http
DELETE /api/v1/kms/keys/{name}
Content-Type: application/json
Authorization: Bearer {token}

{
  "force": false,
  "pending_window_days": 7
}
```

**Request Fields:**
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `force` | bool | false | Skip disable step |
| `pending_window_days` | int | 7 | Days before permanent deletion |

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "status": "pending_deletion",
  "deletion_date": 1704844800,
  "timestamp": 1704240000
}
```

#### Update Key Status

```http
PUT /api/v1/kms/keys/{name}/status
Content-Type: application/json
Authorization: Bearer {token}

{
  "status": "disabled"
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `status` | string | Yes | enabled or disabled |

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "status": "disabled",
  "timestamp": 1704240000
}
```

#### List Key Versions

```http
GET /api/v1/kms/keys/{name}/versions
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "versions": [
    {
      "version": 3,
      "status": "enabled",
      "algorithm": "AES-256-GCM",
      "created_at": 1704153600
    }
  ],
  "next_cursor": "",
  "has_more": false,
  "timestamp": 1704240000
}
```

#### Import Key

```http
POST /api/v1/kms/keys/import
Content-Type: application/json
Authorization: Bearer {token}

{
  "key_material": "base64-encoded-key-material",
  "wrapping_key_id": "transport-key-id",
  "key_attributes": {
    "algorithm": "AES-256-GCM"
  },
  "name": "imported-key",
  "description": "Imported from external system"
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `key_material` | bytes | Yes | Base64-encoded key material |
| `wrapping_key_id` | string | Yes | ID of key used to wrap material |
| `key_attributes.algorithm` | string | No | Algorithm (default: AES-256-GCM) |
| `name` | string | No | Human-readable name |
| `description` | string | No | Key description |

**Response (201 Created):**
Same format as Generate Key response.

### Crypto Operations

#### Encrypt

```http
POST /api/v1/kms/encrypt/{name}
Content-Type: application/json
Authorization: Bearer {token}

{
  "plaintext": "base64-encoded-plaintext",
  "context": "optional-additional-authenticated-data"
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `plaintext` | bytes | Yes | Base64-encoded data to encrypt |
| `context` | bytes | No | Additional authenticated data (AAD) |

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "key_version": 3,
  "ciphertext": "base64-encoded-ciphertext"
}
```

#### Decrypt

```http
POST /api/v1/kms/decrypt/{name}
Content-Type: application/json
Authorization: Bearer {token}

{
  "ciphertext": "base64-encoded-ciphertext",
  "context": "optional-additional-authenticated-data"
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `ciphertext` | bytes | Yes | Base64-encoded ciphertext |
| `context` | bytes | No | AAD used during encryption |

**Response (200 OK):**
```json
{
  "key_id": "key-api-key-1704067200000000000",
  "key_version": 3,
  "plaintext": "base64-encoded-plaintext"
}
```

### DEKs (Data Encryption Keys)

#### Create DEK

```http
POST /api/v1/kms/deks
Content-Type: application/json
Authorization: Bearer {token}

{
  "path": "tenant-123/prod/",
  "algorithm": "AES-256-GCM",
  "description": "Tenant 123 production DEK",
  "safety": {
    "max_operations": 10000000000,
    "max_bytes": 1099511627776,
    "warning_percent": 75
  }
}
```

**Request Fields:**
| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `path` | string | Yes | Hierarchical DEK path |
| `algorithm` | string | No | AES-256-GCM (default), AES-128-GCM |
| `description` | string | No | DEK description |
| `safety.max_operations` | uint64 | No | AEAD operation limit |
| `safety.max_bytes` | uint64 | No | Total bytes encrypted limit |
| `safety.warning_percent` | uint32 | No | Warning threshold (default: 80) |

**Response (201 Created):**
```json
{
  "path": "tenant-123/prod/",
  "algorithm": "AES-256-GCM",
  "version": 1,
  "is_default": false,
  "created_at": "2024-01-01T00:00:00Z"
}
```

#### Get DEK

```http
GET /api/v1/kms/deks/{path}
GET /api/v1/kms/deks/{path}?version=2
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "algorithm": "AES-256-GCM",
  "version": 2,
  "is_default": true,
  "usage": {
    "operations": 50234567,
    "bytes": 134789234688,
    "max_operations": 4294967296,
    "max_bytes": 375809638400,
    "usage_percent": 35.86
  },
  "created_at": "2024-01-01T00:00:00Z",
  "rotated_at": "2024-02-01T12:00:00Z"
}
```

#### List DEKs

```http
GET /api/v1/kms/deks
GET /api/v1/kms/deks?prefix=tenant-123/&recursive=true&limit=50
Authorization: Bearer {token}
```

**Query Parameters:**
| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `prefix` | string | - | Filter by path prefix |
| `recursive` | bool | false | Include nested paths |
| `limit` | int | 100 | Max DEKs to return |
| `cursor` | string | - | Pagination cursor |

**Response (200 OK):**
```json
{
  "deks": [
    {
      "path": "tenant-123/prod/",
      "algorithm": "AES-256-GCM",
      "version": 2,
      "is_default": true
    }
  ],
  "next_cursor": "",
  "has_more": false
}
```

#### Rotate DEK

```http
POST /api/v1/kms/deks/{path}/rotate
Content-Type: application/json
Authorization: Bearer {token}

{
  "background": true
}
```

**Request Fields:**
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `background` | bool | false | Run re-encryption in background |

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "old_version": 2,
  "new_version": 3,
  "rotated_at": "2024-03-15T10:00:00Z"
}
```

#### Delete DEK

```http
DELETE /api/v1/kms/deks/{path}
Content-Type: application/json
Authorization: Bearer {token}

{
  "force": true,
  "migrate_to": "other-path/"
}
```

**Request Fields:**
| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `force` | bool | false | Force deletion |
| `migrate_to` | string | - | Migrate keys to this DEK path |

**Response (200 OK):**
```json
{
  "path": "tenant-123/old/",
  "status": "deleted"
}
```

#### Get DEK Status

```http
GET /api/v1/kms/deks/{path}/status
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "algorithm": "AES-256-GCM",
  "version": 2,
  "status": "active",
  "usage": {
    "operations": 50234567,
    "bytes": 134789234688,
    "max_operations": 4294967296,
    "max_bytes": 375809638400,
    "usage_percent": 35.86,
    "warning_percent": 80
  },
  "reencryption": {
    "in_progress": false,
    "progress_percent": 0,
    "keys_processed": 0,
    "keys_total": 0
  },
  "keys_using_dek": 1234
}
```

#### Set Default DEK

```http
POST /api/v1/kms/deks/{path}/set-default
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "is_default": true
}
```

#### Set DEK Safety

```http
PUT /api/v1/kms/deks/{path}/safety
Content-Type: application/json
Authorization: Bearer {token}

{
  "max_operations": 10000000000,
  "max_bytes": 1099511627776,
  "warning_percent": 75
}
```

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "safety": {
    "max_operations": 10000000000,
    "max_bytes": 1099511627776,
    "warning_percent": 75
  }
}
```

#### Clear DEK Safety

```http
DELETE /api/v1/kms/deks/{path}/safety
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "path": "tenant-123/prod/",
  "safety_cleared": true
}
```

### Backend

#### Get Backend Status

```http
GET /api/v1/kms/backend/status
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "backend_type": "software",
  "status": "healthy",
  "capabilities": {
    "encrypt": true,
    "decrypt": true,
    "sign": true,
    "verify": true,
    "generate": true,
    "import": true,
    "rotate": true,
    "data_key": true,
    "key_wrap": true,
    "key_unwrap": true,
    "multi_party": false
  },
  "timestamp": 1704240000
}
```

#### Test Backend

```http
POST /api/v1/kms/backend/test
Authorization: Bearer {token}
```

**Response (200 OK):**
```json
{
  "success": true,
  "duration_ns": 1234567,
  "message": "backend test completed successfully",
  "timestamp": 1704240000
}
```

## gRPC API

Service: `quicbaas.kms.v1.KMSService`

```protobuf
service KMSService {
  // Keys
  rpc CreateKey(CreateKeyRequest) returns (CreateKeyResponse);
  rpc GetKey(GetKeyRequest) returns (GetKeyResponse);
  rpc ListKeys(ListKeysRequest) returns (ListKeysResponse);
  rpc RotateKey(RotateKeyRequest) returns (RotateKeyResponse);
  rpc DeleteKey(DeleteKeyRequest) returns (DeleteKeyResponse);
  rpc EnableKey(EnableKeyRequest) returns (EnableKeyResponse);
  rpc DisableKey(DisableKeyRequest) returns (DisableKeyResponse);
  rpc ListKeyVersions(ListKeyVersionsRequest) returns (ListKeyVersionsResponse);
  rpc ImportKey(ImportKeyRequest) returns (ImportKeyResponse);

  // Crypto
  rpc Encrypt(EncryptRequest) returns (EncryptResponse);
  rpc Decrypt(DecryptRequest) returns (DecryptResponse);

  // DEKs
  rpc CreateDEK(CreateDEKRequest) returns (CreateDEKResponse);
  rpc GetDEK(GetDEKRequest) returns (GetDEKResponse);
  rpc ListDEKs(ListDEKsRequest) returns (ListDEKsResponse);
  rpc RotateDEK(RotateDEKRequest) returns (RotateDEKResponse);
  rpc DeleteDEK(DeleteDEKRequest) returns (DeleteDEKResponse);
  rpc GetDEKStatus(GetDEKStatusRequest) returns (GetDEKStatusResponse);
  rpc SetDefaultDEK(SetDefaultDEKRequest) returns (SetDefaultDEKResponse);
  rpc SetDEKSafety(SetDEKSafetyRequest) returns (SetDEKSafetyResponse);
  rpc ClearDEKSafety(ClearDEKSafetyRequest) returns (ClearDEKSafetyResponse);

  // Backend
  rpc GetBackendStatus(GetBackendStatusRequest) returns (GetBackendStatusResponse);
}
```

### Key Messages

```protobuf
message CreateKeyRequest {
  string name = 1;
  string description = 2;
  KeyAttributes key_attributes = 3;
  map<string, string> tags = 4;
  string owner = 5;
}

message KeyAttributes {
  string algorithm = 1;
  repeated string key_usage = 2;
  bool exportable = 3;
}

message CreateKeyResponse {
  KeyHandle key = 1;
  KeyMetadata metadata = 2;
  int64 timestamp = 3;
}

message KeyHandle {
  string key_id = 1;
  int32 version = 2;
  string algorithm = 3;
  string status = 4;
  int64 created_at = 5;
}

message KeyMetadata {
  string key_id = 1;
  string name = 2;
  string description = 3;
  string algorithm = 4;
  string status = 5;
  repeated string usage = 6;
  int32 version = 7;
  bool exportable = 8;
  bool deletion_protection = 9;
  int64 created_at = 10;
  int64 updated_at = 11;
  int64 last_used_at = 12;
  map<string, string> tags = 13;
  string owner = 14;
}

message GetKeyRequest {
  string key_id = 1;
}

message ListKeysRequest {
  string prefix = 1;
  int32 limit = 2;
  string cursor = 3;
}

message ListKeysResponse {
  repeated KeyHandle keys = 1;
  string next_cursor = 2;
  bool has_more = 3;
  int32 total_count = 4;
  int64 timestamp = 5;
}

message RotateKeyRequest {
  string key_id = 1;
}

message RotateKeyResponse {
  string key_id = 1;
  int32 old_version = 2;
  int32 new_version = 3;
  int64 timestamp = 4;
}

message DeleteKeyRequest {
  string key_id = 1;
  bool force = 2;
  int32 pending_window_days = 3;
}

message UpdateKeyStatusRequest {
  string key_id = 1;
  string status = 2;  // enabled or disabled
}
```

### Crypto Messages

```protobuf
message EncryptRequest {
  string key_id = 1;
  bytes plaintext = 2;
  bytes context = 3;
}

message EncryptResponse {
  string key_id = 1;
  int32 key_version = 2;
  bytes ciphertext = 3;
}

message DecryptRequest {
  string key_id = 1;
  bytes ciphertext = 2;
  bytes context = 3;
}

message DecryptResponse {
  string key_id = 1;
  int32 key_version = 2;
  bytes plaintext = 3;
}
```

### DEK Messages

```protobuf
message SafetyConfig {
  uint64 max_operations = 1;
  uint64 max_bytes = 2;
  uint32 warning_percent = 3;
}

message UsageInfo {
  uint64 operations = 1;
  uint64 bytes = 2;
  uint64 max_operations = 3;
  uint64 max_bytes = 4;
  double usage_percent = 5;
  uint32 warning_percent = 6;
}

message CreateDEKRequest {
  string path = 1;
  string algorithm = 2;
  string description = 3;
  SafetyConfig safety = 4;
}

message CreateDEKResponse {
  string path = 1;
  string algorithm = 2;
  uint64 version = 3;
  bool is_default = 4;
  google.protobuf.Timestamp created_at = 5;
}

message GetDEKRequest {
  string path = 1;
  uint64 version = 2;  // optional, 0 = current
}

message GetDEKResponse {
  string path = 1;
  string algorithm = 2;
  uint64 version = 3;
  bool is_default = 4;
  UsageInfo usage = 5;
  google.protobuf.Timestamp created_at = 6;
  google.protobuf.Timestamp rotated_at = 7;
}

message ListDEKsRequest {
  string prefix = 1;
  bool recursive = 2;
  int32 limit = 3;
  string cursor = 4;
}

message RotateDEKRequest {
  string path = 1;
  bool background = 2;
}

message RotateDEKResponse {
  string path = 1;
  uint64 old_version = 2;
  uint64 new_version = 3;
  google.protobuf.Timestamp rotated_at = 4;
}

message DeleteDEKRequest {
  string path = 1;
  bool force = 2;
  string migrate_to = 3;
}

message GetDEKStatusRequest {
  string path = 1;
}

message GetDEKStatusResponse {
  string path = 1;
  string algorithm = 2;
  uint64 version = 3;
  string status = 4;
  UsageInfo usage = 5;
  ReencryptionInfo reencryption = 6;
  uint64 keys_using_dek = 7;
}

message ReencryptionInfo {
  bool in_progress = 1;
  double progress_percent = 2;
  uint64 keys_processed = 3;
  uint64 keys_total = 4;
}

message SetDEKSafetyRequest {
  string path = 1;
  SafetyConfig safety = 2;
}

message ClearDEKSafetyRequest {
  string path = 1;
}
```

## Error Codes

| Code | HTTP | Description |
|------|------|-------------|
| `KEY_NOT_FOUND` | 404 | Key does not exist |
| `KEY_EXISTS` | 409 | Key ID already in use |
| `DEK_NOT_FOUND` | 404 | DEK does not exist |
| `DEK_EXISTS` | 409 | DEK path already in use |
| `INVALID_ALGORITHM` | 400 | Unsupported algorithm |
| `INVALID_INPUT` | 400 | Invalid request parameters |
| `PERMISSION_DENIED` | 403 | Insufficient permissions |
| `BACKEND_ERROR` | 500 | Backend operation failed |
| `UNAVAILABLE` | 503 | Service unavailable |
| `TIMEOUT` | 504 | Operation timed out |
| `RESOURCE_EXHAUSTED` | 429 | Rate limit exceeded |
| `ROTATION_IN_PROGRESS` | 409 | Key/DEK rotation running |
| `REENCRYPTION_IN_PROGRESS` | 409 | DEK re-encryption running |

## Supported Algorithms

### Symmetric Encryption
- `AES-128-GCM` - 128-bit AES in GCM mode
- `AES-192-GCM` - 192-bit AES in GCM mode
- `AES-256-GCM` - 256-bit AES in GCM mode (default)
- `ChaCha20-Poly1305` - ChaCha20 with Poly1305 MAC
- `XChaCha20-Poly1305` - Extended nonce ChaCha20

### Asymmetric Signing
- `RSA-2048` - 2048-bit RSA
- `RSA-3072` - 3072-bit RSA
- `RSA-4096` - 4096-bit RSA
- `ECDSA-P256` - ECDSA with P-256 curve
- `ECDSA-P384` - ECDSA with P-384 curve
- `ECDSA-P521` - ECDSA with P-521 curve
- `ED25519` - Edwards-curve Digital Signature Algorithm
