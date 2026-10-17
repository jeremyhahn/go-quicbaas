### Security

- **Every KMS verb carries the caller's tenant scope (#1476).**
  `GetKey`, `RotateKey` and `DeleteKey` scoped their key id to the caller's
  tenant, but the other verbs that address a key, policy, rotation policy,
  metadata, version or DEK used the raw id. One tenant could therefore read
  another tenant's public key (`GetPublicKey`), revoke its key, read or
  overwrite its metadata and policies, or use its DEKs. These
  verbs now refuse a context that has no tenant, and resolve every id inside
  the caller's tenant, so another tenant's id is not found. Key policies,
  rotation policies and version status are stored under the tenant-scoped id.
  The default key and the default DEK are kept per tenant.
- **`RotateKey` and `RevokeKey` report an absent key as not-found** (HTTP
  404) instead of an internal error, matching `GetKey` and `DeleteKey`.
