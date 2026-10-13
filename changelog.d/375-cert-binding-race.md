### Fixed

- **A replaced or revoked PIV certificate can no longer go on resolving the
  account after concurrent binding updates (go-quicbaas#375).** QuicBaaS no
  longer keeps its own legacy-PIV thumbprint index. Both the SPKI thumbprint
  and the legacy certificate digest now resolve through qrdb's own
  certificate-binding index, which is updated in the same write as the user
  record.
