# Encryption Key Storage

Encryption keys from go-xkms can be persisted to any object storage backend using the `ObjStoreAdapter`.

## Configuration

```yaml
object_storage:
  backends:
    key-store:
      type: s3
      config:
        bucket: my-quicbaas-keys
        region: us-east-1
        access_key: ${AWS_ACCESS_KEY_ID}
        secret_key: ${AWS_SECRET_ACCESS_KEY}

security:
  encryption_enabled: true
  encryption_algorithm: aes-256-gcm
  key_storage_backend: key-store  # References named backend
```

## Benefits

- **Persistence** - Keys survive restarts
- **Replication** - Cross-region key availability with cloud storage
- **Integration** - Works with any go-objstore backend
- **Hardware Security** - Combine with KMS backends for HSM integration

## ObjStoreAdapter

The `ObjStoreAdapter` bridges go-objstore's `common.Storage` interface with go-xkms's `storage.Backend` interface.

```go
// Create an S3 storage backend
s3Storage, _ := factory.NewStorage("s3", map[string]string{
    "bucket":     "my-bucket",
    "region":     "us-east-1",
    "access_key": os.Getenv("AWS_ACCESS_KEY_ID"),
    "secret_key": os.Getenv("AWS_SECRET_ACCESS_KEY"),
})

// Wrap for go-xkms
xkmsBackend := security.NewObjStoreAdapter(s3Storage)

// Use with XKMS
xkmsInstance, _ := xkms.New(xkmsBackend, xkmsConfig)
```

## Security Best Practices

### 1. Credentials Management

Never hardcode credentials. Use environment variables or secrets managers:

```yaml
config:
  access_key: ${AWS_ACCESS_KEY_ID}
  secret_key: ${AWS_SECRET_ACCESS_KEY}
```

### 2. Encryption at Rest

Enable server-side encryption on cloud storage:

**AWS S3:**
```yaml
config:
  sse: AES256  # or aws:kms
```

**GCS:**
- Enable default encryption on bucket

**Azure:**
- Enable storage service encryption

### 3. Access Control

Use IAM policies to restrict access:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"],
      "Resource": "arn:aws:s3:::my-quicbaas-keys/*"
    }
  ]
}
```

### 4. Network Security

- Use TLS for all communication
- Consider VPC endpoints for cloud storage
- Enable bucket policies to restrict IP ranges

### 5. Key Rotation

Regularly rotate storage access keys:

```bash
# Rotate AWS keys
aws iam create-access-key --user-name quicbaas-service
aws iam delete-access-key --user-name quicbaas-service --access-key-id OLD_KEY
```

## Local Development

For development, use local or memory backends:

```yaml
object_storage:
  backends:
    dev-keys:
      type: local
      config:
        path: /tmp/quicbaas-keys

security:
  key_storage_backend: dev-keys
```

Or in-memory for tests:

```yaml
object_storage:
  backends:
    test-keys:
      type: memory

security:
  key_storage_backend: test-keys
```
