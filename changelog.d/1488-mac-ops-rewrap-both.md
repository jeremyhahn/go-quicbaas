### Security

- **HMAC verbs check the MAC operations; Rewrap checks decrypt and encrypt
  (go-quicbaas#1488).** A MAC key is not a signing key: GenerateHMAC now asks
  the key policy for `generate_mac` and VerifyHMAC for `verify_mac`, instead of
  `sign` and `verify`, so a key granted only signing operations can no longer
  produce or check MACs. Rewrap decrypts under the source key version and
  encrypts under the destination, so it now needs both `decrypt` and `encrypt`
  on the key; it checked `encrypt` only. A denial on either check returns
  forbidden before any key usage is recorded or the crypto manager is called.
