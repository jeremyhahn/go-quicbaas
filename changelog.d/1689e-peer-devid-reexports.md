### Added

- **DevID validator and TLS-evidence helpers in `sdk/go/xkms` (go-quicbaas#1689).**
  `PeerVerifyIDevID`, `PeerVerifyIAK`, `PeerIsIDevID`, `PeerIsIAK`,
  `PeerEvidenceFromTLS` and `PeerQualifyingDataFromTLS`, with the
  `PeerDevIDPolicy`, `PeerTLSEvidence`, `PeerDevIDError` and
  `PeerDevIDPolicyError` types, are re-exported from the go-xkms SDK beside
  the existing peer-attestation aliases.
