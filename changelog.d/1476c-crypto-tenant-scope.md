### Security

- **Crypto service key lookups are tenant scoped (go-quicbaas#1476).**
  Encrypt, Decrypt, Rewrap, EncryptBatch, DecryptBatch, GenerateDataKey,
  GenerateWrappedDataKey, Sign, Verify, GenerateHMAC and VerifyHMAC resolve the
  key id under the caller's tenant, as the KMS verbs do. Another tenant's key
  is not found, and a request whose context carries no tenant is refused.
  Per-key policy checks and AEAD safety tracking use the tenant-scoped id.
