### Fixed

- **A PIV activation whose user write landed keeps its login factor
  (go-quicbaas#1275).** When the user update reported failure but had
  committed (a lost response or a timeout), the activation revoked the
  certificate's new mTLS login factor. That left an active, certificate-bound
  user with a spent invite and no usable login, and no further activation
  could repair it because the user was no longer pending. The activation now
  re-reads the persisted user, detached from the request's cancellation and
  bounded, and succeeds if the user is active and bound to the certificate.
  Only an update that did not land, or a re-read that fails, revokes the
  factor. The FIDO2 activation's reconcile re-read uses the same bounded,
  detached read.
