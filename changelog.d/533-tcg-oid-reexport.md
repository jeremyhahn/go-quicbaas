### Added

- **`sdk/go/xkms/ca` re-exports the TCG certificate-policy OIDs** as aliases of
  the go-xkms values: `OIDTCGKpAIKCertificate`,
  `OIDTCGVerifiedTPM{Residency,Fixed,Restricted}` and the `x509.OID` forms
  `TCGVerifiedTPM{Residency,Fixed,Restricted}Policy`. A relying party must
  check `tcg-cap-verifiedTPMRestricted` on an IAK/LAK certificate before
  trusting an attestation, and the platform may reach the arc only through
  this SDK. (#533)

- **`sdk/go/xkms/ca` re-exports the IAK certificate surface** from go-xkms#371:
  `CAIAKCertificateReader` (`IAKCertificate(ctx, model, serial)`) and
  `CAIAKCommonName` (`"iak-<model>-<serial>"`), the name the CA now issues and
  stores the Initial Attestation Key under so the device's IDevID record no
  longer overwrites it. (#533)

- **`sdk/go/xkms` re-exports the attested-peer verification primitive** from
  go-xkms#273: `PeerAttestationVerifier` / `NewPeerAttestationVerifier` /
  `WithPeerVerifierClock`, `PeerEvidence`, `PeerPolicy`, `PeerVerdict`,
  `PeerChecks`, `PeerClockInfo`, `PeerFailureClass` with
  `PeerFailure{None,Absence,Cryptographic}`, the typed
  `Peer{Policy,Context,ChannelBinding}Error`s, and `PeerChannelBinding` /
  `PeerQualifyingData` with `PeerChannelBindingExporterLabel` /
  `PeerChannelBindingLength`. Pure aliases; the platform's remote-attestation
  path is the consumer. Not aliased: `peer.FailureClassError`, which go-xkms's
  own SDK does not yet export (go-xkms#391); it follows in a later alpha. (#533)

### Changed

- go-xkms, its SDK and xkey moved to `v0.9.4-alpha` in both modules — the tag
  that carries the `Restricted` OID (go-xkms#359), the IAK certstore fix
  (go-xkms#383) and the attested-peer primitive (go-xkms#385). Minimal version
  selection carries go-truststrap to `v0.4.0-alpha` with it. (#533)
