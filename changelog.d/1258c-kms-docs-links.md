### Changed

- **KMS documentation points at go-xkms (go-quicbaas#1258).**
  `docs/kms/` no longer restates go-xkms's key management, backends, AEAD
  safety limits and API schemas; each page links the go-xkms page and keeps
  only QuicBaaS's surface: the `Security().KMS()` / `Security().Crypto()`
  accessors, the transport and permission matrix, the CLI verbs and flags as
  the code defines them, the `kms:` configuration keys and the build tags.
  QuicBaaS's own key store, router and rotation are marked as being removed
  (SPEC row `kms`). Configuration keys and CLI verbs that do not exist
  (`kms.backends`, `kms.dek`, `kms.cluster_dek`, `kms policy`) are gone.
- **IAM authentication-factor documentation points at go-xkms and go-qrdb
  (go-quicbaas#1258).** `docs/iam/` no longer restates the go-xkms WebAuthn
  ceremonies, PIV slot attestation and error mapping, or go-qrdb's factor
  model and admin credential paths; it links them and keeps QuicBaaS's own
  surface: the per-tenant WebAuthn mount and its permissions, the enrollment
  assurance and AAGUID gates, the PIV attestation login rule, the invite and
  PIV activation flow, TOTP MFA, and the `quicbaas auth` / `iam mfa` /
  `iam webauthn` flags as the code defines them. Keys, routes and flags that
  do not exist are gone: `iam.enabled`, `iam.auth.*`, the `/api/v1/iam/mfa/*`
  routes, and the `auth factor rotate --new-*` flags.
- **Authorization and composition documentation points at go-qrdb and go-xkms
  (go-quicbaas#1258).** `docs/iam/` states that authorization decisions are
  go-qrdb's RBAC + ABAC authorizer and names what QuicBaaS still owns: its own
  policy store (being removed, SPEC row `iam`) and PEP policy distribution.
  The fictional policy compiler, bytecode evaluator, bloom filters,
  `pkg/abac`, `/api/v1/iam/pep/*` routes, and the dedicated-IAM-cluster and
  remote-PEP deployment models (`iam.mode`, `iam.remote`, `iam.pep`,
  `iam.oidc`) are gone. `docs/composition/api.md` links go-xkms for PIV,
  FIDO2, the CA and PIN management and keeps QuicBaaS's transports,
  permissions and per-tenant FIDO2 layer.
