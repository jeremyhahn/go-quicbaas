### Security

- **Six crypto verbs gate on per-key authorization (go-quicbaas#1480).**
  Rewrap, EncryptBatch, DecryptBatch, GenerateWrappedDataKey, GenerateHMAC and
  VerifyHMAC scoped the key to the caller's tenant but skipped the per-key
  policy check that Encrypt and Decrypt make. A principal the key policy denies
  could therefore still use the key through these verbs. Each now checks the
  key policy before it records key usage or calls the crypto manager, and a
  denial returns forbidden and is counted and audited as Encrypt's is. Rewrap
  and EncryptBatch check the `encrypt` operation, DecryptBatch `decrypt`,
  GenerateWrappedDataKey `generate_data_key`, GenerateHMAC `sign` and
  VerifyHMAC `verify`.
