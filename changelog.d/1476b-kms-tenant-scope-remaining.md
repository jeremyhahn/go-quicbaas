### Security

- **KMS export, unwrap, re-encryption and gRPC key names are tenant scoped (go-quicbaas#1476).**
  ExportKey and UnwrapKey resolve the key and the wrapping key under the
  caller's tenant and bind the wrapped blob to the tenant-scoped key id, so
  another tenant's key is not found and a blob exported by one tenant does not
  unwrap for another. StartReencryption covers only the caller's tenant's keys,
  and GetReencryptionProgress and CancelReencryption return not-found to any
  other tenant. The gRPC KMS server no longer keeps a global key-name registry:
  key names are unique within a tenant, checked in the service layer. DeleteKey
  releases the name only in the caller's tenant.
