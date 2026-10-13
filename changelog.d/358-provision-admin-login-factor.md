### Fixed

- **A provisioned tenant admin can log in with the certificate provisioning
  returns (go-quicbaas#358).** Cert-mode tenant provisioning bound the signed
  admin certificate to the new admin but never enrolled it as an `mtls` login
  factor, so `auth login --method mtls` could not resolve it. Provisioning now
  enrolls it through the same enroller first-boot uses, under the provisioned
  tenant and with the tenant in the factor's provenance. If the enrollment
  fails, provisioning returns no certificate and deletes the admin it created,
  then fails with `*TenantProvisionLoginFactorError`; a retry starts over. If
  that cleanup fails too, it fails with `*TenantProvisionRollbackError`. The
  cleanup runs detached from the request, bounded by its own timeout, so a
  canceled or timed-out request no longer strands an admin that has no login
  factor. It never deletes the tenant, because qrdb retires a deleted tenant
  ID and a retry could not reuse it. It deletes only the admin its own call
  created, so it cannot remove an admin that a concurrent provision of the
  same tenant created. Provisioning an
  existing tenant again now returns the existing admin's ID instead of an
  empty one, and fails with `*TenantAdminLoginCredentialMissingError` rather
  than reporting success when that admin holds no active login factor. PIV
  invite activation also enrolls the activated certificate as an `mtls` login
  factor now, under the invited tenant, before it activates the user. If that
  enrollment fails, activation returns `*iam.PIVActivationLoginFactorError`
  and the user stays pending. A certificate already enrolled for another user
  is rejected before the invite is consumed, so that activation can be
  retried with the right certificate, on any transport.
- **A failed or losing PIV invite activation no longer leaves its certificate
  able to log in (go-quicbaas#1275).** Activation now consumes the invite
  before it activates the user, so exactly one of two racing activations wins;
  the other fails with `*iam.PIVActivationRedeemError` (wrapping
  `ErrInviteUsed`) instead of succeeding. Any activation that fails after
  enrolling revokes the login factor it created, never one that existed
  before the call. If the user update fails after the invite is consumed,
  activation fails with `*iam.PIVActivationUpdateError` and the user must be
  re-invited. If revoking the factor fails, activation fails with
  `*iam.PIVActivationFactorCleanupError`, which names the residual factor,
  and the audit log records its ID. Only the activation that consumed the
  invite enrolls the factor, so a losing activation that presented the same
  certificate can no longer revoke the factor the winner logs in with. A
  factor store failure after the invite is consumed means the user must be
  re-invited. The revocation also runs detached from the request, bounded by a
  timeout, so canceling the request does not leave the factor in place.
  Activation is also arbitrated per user, not only per token: a pending user
  can hold several live invites (a retried provisioning mints another), and
  before this change two activations presenting different tokens could each
  enroll a factor. Activation now takes a per-user claim in the tenant store,
  a create-if-absent decided by the store, before it writes anything. The
  loser fails with `*iam.UserActivationInProgressError`, which satisfies
  `iam.IsUserNotPending` and so maps to the same conflict status, having
  written nothing. A failed activation releases the claim; a successful one
  keeps it, so a later invite of the same user cannot activate it again unless
  an administrator returns the user to pending. A tenant store that cannot
  perform guarded writes is refused with
  `*iam.UserActivationClaimUnsupportedError`. A factor revoked by a failed
  activation still reserves its certificate, so the re-invited user must
  activate with a different certificate until qrdb can release it.
- **Tenant provisioning no longer leaves an admin nobody can use
  (go-quicbaas#358).** Provisioning now accepts only `prefix` isolation and
  refuses `barrier` (the default), `volume` and `raft` before writing
  anything, with `*ProvisionBarrierIsolationError`: those keep the tenant's
  IAM behind its own barrier, sealed until the tenant admin initializes it,
  and qrdb cannot yet hold that admin outside it. The admin CSR is parsed,
  signature-checked and held to the key policy (RSA 2048+, ECDSA
  P-256/P-384/P-521, Ed25519) before any write, failing with
  `*ProvisionAdminCSRInvalidError` or `*ProvisionAdminCSRKeyError`. When qrdb
  fails after creating the admin, for example because the CA will not sign,
  provisioning deletes the admin that call created, so a retry starts over. A re-run reports the existing admin only if that account is
  active and holds the tenant-admin role; otherwise it fails with
  `*TenantAdminAccountMismatchError`. In invite mode an omitted
  `invite_expiry_hours` now gets the default invite lifetime, and a value
  above the maximum is capped, as user invites are; before, an omitted expiry
  minted an invite that had already expired.
