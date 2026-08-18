# Composition — API

> **Status: NORMATIVE** · Last verified: 2026-08-19
> The capability on every transport, with permissions. Feature folder: `docs/composition/`.

## Transport matrix

Every row below is derived from source, not from prose. The columns mean:

- **SDK method** — the method name `sdk/go/client/<pkg>` passes to
  `transport.Request(ctx, serviceName, "<method>", req)`.
- **REST route (transport)** — where `sdk/go/transport/rest` sends it.
- **Served?** — whether that exact method+path pair is registered in the
  server's route table (`pkg/api/rest/*.go`). A **no** is a live SDK-to-server
  mismatch, not a documentation gap.
- **gRPC** — whether `sdk/go/transport/grpc` routes the service through a typed
  handler. **Unix inherits this column exactly**: the Unix transport is gRPC
  over `AF_UNIX` and delegates every `Request` to it
  (`sdk/go/transport/unix/transport.go:114`).
- **QUIC** is likewise not a separate column: `sdk/go/transport/quic` embeds
  `*rest.Transport` and swaps only the `http.Client` for an `http3.Transport`
  (`sdk/go/transport/quic/transport.go:86`), so every QUIC row is its REST row
  carried over HTTP/3. See the caveat in
  [../multiprotocol-api/architecture.md](../multiprotocol-api/architecture.md):
  **no component in this repository serves HTTP/3 today.**
- **MCP** — whether the SDK routes the service to an MCP tool; the registered
  server-side tool names are listed above the table.
- **Embedded** — the `service/method` key registered in
  `sdk/go/transport/embedded/transport.go`.
- **Permission** — `resource:action` from
  `pkg/api/rest/routes_permissions_generated.go`.


These services extend QuicBaaS's XKMS compatibility layer with PIV smart card management, FIDO2/WebAuthn passwordless authentication, Certificate Authority operations, and PIN management. All operations delegate to the underlying go-xkms XKMSService with QuicBaaS-specific tenant context injection and access control enforcement.

Source: `pkg/service/piv.go`, `pkg/service/fido2.go`, `pkg/service/ca.go`, `pkg/service/pin.go`

---

## PIV (Personal Identity Verification)

Manages smart card certificates and key pairs in PIV-compliant slots following NIST SP 800-73. Requires a PIV-capable hardware backend.

### Interface

```go
type PIVService interface {
    ListPIVSlots(ctx context.Context, req *PIVListSlotsRequest) (*PIVListSlotsResponse, error)
    GetPIVCertificate(ctx context.Context, req *PIVGetCertificateRequest) (*PIVGetCertificateResponse, error)
    StorePIVCertificate(ctx context.Context, req *PIVStoreCertificateRequest) error
    DeletePIVCertificate(ctx context.Context, req *PIVDeleteCertificateRequest) error
    GeneratePIVKey(ctx context.Context, req *PIVGenerateKeyRequest) (*PIVGenerateKeyResponse, error)
    GeneratePIVCSR(ctx context.Context, req *PIVGenerateCSRRequest) (*PIVGenerateCSRResponse, error)
}
```

### Operations

| Method | Description |
|--------|-------------|
| `ListPIVSlots` | Returns status of all PIV slots in a backend (occupancy, subject, algorithm, expiration) |
| `GetPIVCertificate` | Retrieves certificate from a slot in PEM or DER format |
| `StorePIVCertificate` | Stores a certificate in a slot (overwrites existing) |
| `DeletePIVCertificate` | Removes certificate from a slot (key pair unaffected) |
| `GeneratePIVKey` | Generates a new key pair with self-signed certificate in a slot |
| `GeneratePIVCSR` | Generates a PKCS#10 CSR for the key in a slot |

**Slot identifiers:** `9a` (Authentication), `9c` (Digital Signature), `9d` (Key Management), `9e` (Card Authentication)

**Key algorithms:** `rsa2048`, `rsa4096`, `ecdsap256`, `ecdsap384`, `ed25519`

### Request/Response Types

- `PIVListSlotsRequest` -- `Backend`, `TenantID`
- `PIVListSlotsResponse` -- `Slots []PIVSlotStatus` (Slot, Name, Description, HasCert, Subject, Issuer, Algorithm, KeySize, NotAfter, Fingerprint, Backend)
- `PIVGetCertificateRequest` -- `Backend`, `Slot`, `Format` ("pem"/"der"), `TenantID`
- `PIVGetCertificateResponse` -- `Slot`, `Certificate []byte`, `Format`
- `PIVStoreCertificateRequest` -- `Backend`, `Slot`, `Certificate []byte`, `Format`, `TenantID`
- `PIVDeleteCertificateRequest` -- `Backend`, `Slot`, `TenantID`
- `PIVGenerateKeyRequest` -- `Backend`, `Slot`, `Algorithm`, `Subject`, `TenantID`
- `PIVGenerateKeyResponse` -- `Slot`, `Certificate []byte`, `PublicKey []byte`
- `PIVGenerateCSRRequest` -- `Backend`, `Slot`, `Subject`, `TenantID`
- `PIVGenerateCSRResponse` -- `Slot`, `CSR []byte`

### Error Types

| Error | Description |
|-------|-------------|
| `PIVNotAvailableError` | PIV service not configured or backend unsupported |
| `PIVSlotNotFoundError{Slot}` | Slot does not exist or has no certificate |
| `PIVInvalidSlotError{Slot}` | Invalid slot identifier |
| `PIVKeyGenerationError{Slot, Cause}` | Key generation failed (wraps cause) |
| `PIVCertificateOperationError{Slot, Operation, Cause}` | Certificate store/delete/get failed (wraps cause) |

---

## FIDO2/WebAuthn

Implements server-side WebAuthn protocol for passwordless authentication using hardware security keys, biometrics, and platform authenticators.

### Interface

```go
type FIDO2Service interface {
    BeginRegistration(ctx context.Context, req *FIDO2BeginRegistrationRequest) (*FIDO2BeginRegistrationResponse, error)
    FinishRegistration(ctx context.Context, req *FIDO2FinishRegistrationRequest) (*FIDO2FinishRegistrationResponse, error)
    BeginAuthentication(ctx context.Context, req *FIDO2BeginAuthenticationRequest) (*FIDO2BeginAuthenticationResponse, error)
    FinishAuthentication(ctx context.Context, req *FIDO2FinishAuthenticationRequest) (*FIDO2FinishAuthenticationResponse, error)
}
```

### Ceremony Flows

**Registration (credential creation):**
1. `BeginRegistration` -- server generates challenge and credential creation options
2. Client calls `navigator.credentials.create()` with returned options
3. `FinishRegistration` -- server validates attestation and stores credential

**Authentication (assertion):**
1. `BeginAuthentication` -- server generates challenge with allowed credential IDs
2. Client calls `navigator.credentials.get()` with returned options
3. `FinishAuthentication` -- server verifies signature and issues JWT session token

### Operations

| Method | Description |
|--------|-------------|
| `BeginRegistration` | Generates challenge, user ID, RP info, and allowed algorithms for credential creation |
| `FinishRegistration` | Validates authenticator attestation and stores the new credential |
| `BeginAuthentication` | Generates challenge and allowed credential IDs for assertion |
| `FinishAuthentication` | Verifies authenticator signature, returns JWT session token |

### Request/Response Types

- `FIDO2BeginRegistrationRequest` -- `Username`, `DisplayName`, `RPID`, `RPName`
- `FIDO2BeginRegistrationResponse` -- `Challenge`, `UserID`, `RPID`, `RPName`, `CredentialParams []FIDO2CredentialParam{Type, Alg}`
- `FIDO2FinishRegistrationRequest` -- `Username`, `CredentialID`, `PublicKey`, `AttestationData`, `ClientDataJSON` (all base64-encoded)
- `FIDO2FinishRegistrationResponse` -- `Success`, `CredentialID`, `Message`
- `FIDO2BeginAuthenticationRequest` -- `Username`, `RPID`
- `FIDO2BeginAuthenticationResponse` -- `Challenge`, `RPID`, `CredentialIDs []string`
- `FIDO2FinishAuthenticationRequest` -- `Username`, `CredentialID`, `Signature`, `AuthenticatorData`, `ClientDataJSON` (all base64-encoded)
- `FIDO2FinishAuthenticationResponse` -- `Success`, `Token` (JWT), `Message`

### Error Types

| Error | Description |
|-------|-------------|
| `FIDO2NotAvailableError` | FIDO2 service not configured or backend unsupported |
| `FIDO2RegistrationError{Username, Cause}` | Registration ceremony failed (wraps cause) |
| `FIDO2AuthenticationError{Username, Cause}` | Authentication ceremony failed (wraps cause) |
| `FIDO2CredentialNotFoundError{Username}` | No credentials registered for the user |
| `FIDO2InvalidChallengeError` | Challenge verification failed during ceremony |

---

## Certificate Authority (CA)

Provides full PKI lifecycle management: CA certificate retrieval, CSR signing, certificate issuance, revocation with RFC 5280 reason codes, and CRL generation.

### Interface

```go
type CAService interface {
    GetCABundle(ctx context.Context, req *CAGetBundleRequest) (*CAGetBundleResponse, error)
    GetCACertificate(ctx context.Context, req *CAGetCertificateRequest) (*CAGetCertificateResponse, error)
    SignCSR(ctx context.Context, req *CASignCSRRequest) (*CASignCSRResponse, error)
    IssueCertificate(ctx context.Context, req *CAIssueCertificateRequest) (*CAIssueCertificateResponse, error)
    RevokeCertificate(ctx context.Context, req *CARevokeCertificateRequest) (*CARevokeCertificateResponse, error)
    GenerateCRL(ctx context.Context, req *CAGenerateCRLRequest) (*CAGenerateCRLResponse, error)
    IsRevoked(ctx context.Context, req *CAIsRevokedRequest) (*CAIsRevokedResponse, error)
}
```

### Operations

| Method | Description |
|--------|-------------|
| `GetCABundle` | Retrieves full certificate chain (root to issuing CA), filterable by store type and algorithm |
| `GetCACertificate` | Retrieves CA certificate for a specific identity (or default CA) |
| `SignCSR` | Signs a PEM-encoded PKCS#10 CSR using the CA key with a configurable profile |
| `IssueCertificate` | Generates key pair and issues certificate in one operation |
| `RevokeCertificate` | Marks a certificate as revoked with an RFC 5280 reason code |
| `GenerateCRL` | Generates a signed CRL containing all revoked certificates |
| `IsRevoked` | Checks revocation status for a certificate by serial number |

**Certificate profiles:** `server`, `client`, `idevid`, `ldevid`

**Store types:** `software`, `tpm2`, `pkcs11`

**RFC 5280 revocation reason codes:** 0=Unspecified, 1=KeyCompromise, 2=CACompromise, 3=AffiliationChanged, 4=Superseded, 5=CessationOfOperation, 6=CertificateHold

### Request/Response Types

- `CAGetBundleRequest` -- `StoreType`, `Algorithm`, `TenantID`
- `CAGetBundleResponse` -- `BundlePEM []byte`, `Certificates [][]byte` (DER), `ContentType`
- `CAGetCertificateRequest` -- `Identity`, `TenantID`
- `CAGetCertificateResponse` -- `CertificatePEM []byte`, `Subject`, `Issuer`, `SerialNumber`, `NotBefore`, `NotAfter`, `IsCA`
- `CASignCSRRequest` -- `CSRPEM []byte`, `Profile`, `ValidityDays`, `TenantID`
- `CASignCSRResponse` -- `CertificatePEM []byte`, `ChainPEM []byte`, `SerialNumber`
- `CAIssueCertificateRequest` -- `Profile`, `CommonName`, `Organization`, `SANs []string`, `ValidityDays`, `Algorithm`, `TenantID`
- `CAIssueCertificateResponse` -- `CertificatePEM []byte`, `ChainPEM []byte`, `PrivateKeyPEM []byte`, `SerialNumber`
- `CARevokeCertificateRequest` -- `SerialNumber`, `Reason` (int), `TenantID`
- `CARevokeCertificateResponse` -- `Success`, `Message`
- `CAGenerateCRLRequest` -- `TenantID`
- `CAGenerateCRLResponse` -- `CRLPEM []byte`
- `CAIsRevokedRequest` -- `SerialNumber`, `TenantID`
- `CAIsRevokedResponse` -- `Revoked`, `Reason` (int), `Message`

### Error Types

| Error | Description |
|-------|-------------|
| `CANotAvailableError` | CA service not configured or backend unsupported |
| `CASigningError{Cause}` | CSR or certificate signing failed (wraps cause) |
| `CARevocationError{SerialNumber, Cause}` | Certificate revocation failed (wraps cause) |
| `CACertificateNotFoundError{Identity}` | Requested CA certificate or identity not found |
| `CAInvalidCSRError{Cause}` | Provided CSR is malformed or invalid (wraps cause) |
| `CACRLGenerationError{Cause}` | CRL generation failed (wraps cause) |

---

## PIN Management

Manages Security Officer (SO) and User PINs for hardware security tokens following PKCS#11 and PIV conventions. Includes brute-force lockout protection with automatic and SO-initiated recovery.

### Interface

```go
type PINService interface {
    SetSOPIN(ctx context.Context, req *PINSetSOPINRequest) error
    SetUserPIN(ctx context.Context, req *PINSetUserPINRequest) error
    ChangeSOPIN(ctx context.Context, req *PINChangeSOPINRequest) error
    ChangeUserPIN(ctx context.Context, req *PINChangeUserPINRequest) error
    VerifySOPIN(ctx context.Context, req *PINVerifySOPINRequest) error
    VerifyUserPIN(ctx context.Context, req *PINVerifyUserPINRequest) error
    GetLockoutStatus(ctx context.Context, req *PINGetLockoutStatusRequest) (*PINLockoutStatusResponse, error)
    ResetLockout(ctx context.Context, req *PINResetLockoutRequest) error
}
```

### Operations

| Method | Description |
|--------|-------------|
| `SetSOPIN` | Sets the SO PIN (current SO PIN required if already set) |
| `SetUserPIN` | Sets the user PIN (requires SO PIN authorization) |
| `ChangeSOPIN` | Changes SO PIN (requires current SO PIN for authentication) |
| `ChangeUserPIN` | Changes user PIN (requires current user PIN for authentication) |
| `VerifySOPIN` | Verifies SO PIN without side effects |
| `VerifyUserPIN` | Verifies user PIN (failed attempt increments lockout counter) |
| `GetLockoutStatus` | Returns failed attempts, max attempts, lock state, and recovery time |
| `ResetLockout` | Resets lockout counter (requires SO PIN authorization) |

### Request/Response Types

- `PINSetSOPINRequest` -- `CurrentSOPIN` (empty for first-time provisioning), `NewSOPIN`
- `PINSetUserPINRequest` -- `SOPIN` (authorization), `NewUserPIN`
- `PINChangeSOPINRequest` -- `CurrentSOPIN`, `NewSOPIN`
- `PINChangeUserPINRequest` -- `CurrentUserPIN`, `NewUserPIN`
- `PINVerifySOPINRequest` -- `SOPIN`
- `PINVerifyUserPINRequest` -- `UserPIN`
- `PINGetLockoutStatusRequest` -- `TenantID`
- `PINLockoutStatusResponse` -- `FailedAttempts`, `MaxAttempts`, `IsLocked`, `LockoutUntil` (RFC 3339), `RecoverySeconds`
- `PINResetLockoutRequest` -- `SOPIN`

### Error Types

| Error | Description |
|-------|-------------|
| `PINNotAvailableError` | PIN service not configured or backend unsupported |
| `PINVerificationError{PINType}` | PIN verification failed (PINType: "SO" or "User") |
| `PINLockedOutError{RecoverySeconds}` | Device locked out; RecoverySeconds=0 means SO reset required |
| `PINChangeError{PINType, Cause}` | PIN change failed (wraps cause) |
| `PINPolicyViolationError{Detail}` | PIN does not meet complexity/length policy |


---

## XKMS Compatibility Layer

### Overview

QuicBaaS exposes xkms-compatible wire-protocol endpoints so that
`xkms.Client` (the go-xkms SDK) can connect directly to `quicbaasd`.
Applications already using go-xkms can target QuicBaaS as their key
management backend without code changes on the client side.

After Phase 8e the compatibility layer has two distinct exposure
modes:

1. **Native QuicBaaS handlers** (preferred). The QuicBaaS daemon
   serves the operation directly through its own multi-protocol
   stack, routing through the composed qrdb + xkms backends per
   [`docs/architecture/composition.md`](architecture.md).
   The QuicBaaS SDK exposes these under `Security()`
   (see [`docs/sdk/security.md`](../ssh/engineering.md)).
2. **Compat bridge**. A handful of operations are still delivered by
   forwarding to the go-xkms backend in-process. These are wire-
   compatible but do **not** participate in the QuicBaaS multi-
   protocol matrix beyond the legacy `/api/v1/xkms/` REST routes and
   the matching `xkms.*` MCP tools.

Multi-tenant isolation is enforced by injecting the tenant ID from
the authentication context into key attributes for both paths.

### Native vs compat bridge

| Capability | Native (Phase 8e canonical) | Compat bridge (xkms backend forward) |
|------------|------------------------------|--------------------------------------|
| Barrier (status / init / unseal / seal / rekey) | Yes — `Security().Barrier()` over REST/gRPC/QUIC/MCP/Unix/embedded | n/a |
| Ceremony (status / claim-cert / claim-share begin+complete / sign-csr / enroll-external-officer) | Yes — `Security().Ceremony()` on all six transports | n/a |
| Credentials (submit/list/rotate/retrieve/versions/delete/gc) | Yes — `Security().Credentials()` on all six transports | n/a |
| CA admin (info / bundle / list / issue / revoke / export-bundle) | Yes — `Security().Certificate()` (Phase 8e added `CAInfo` + `ListCertificates`) | Legacy `/api/v1/xkms/ca/*` routes retained for backward compatibility |
| Auth factors (factor list/add/remove/rotate/relabel) | Yes — `Security().Auth().Factors()` (login is `Security().Auth().BeginLogin/FinishLogin`) | n/a |
| FIDO2 + PIV | Yes — `Security().Auth().{FIDO2,PIV}()` on all six transports (`quicbaas.FIDO2` / `quicbaas.PIV` verbs) | `/api/v1/xkms/fido2/*` + `/api/v1/xkms/piv/*` routes bridge to the same composed services |
| ACME (server + client) | Yes — `Security().ACME().{Server,Client}()` | n/a |
| Trust bootstrap | Yes — `Security().Trust().Bootstrap()` (single canonical lib in xkms/pkg/trust/bootstrap) | n/a |
| Key management (Generate / Get / Delete / List / Rotate) | Yes — `Security().KMS()` | Wire-compatible legacy REST + MCP tools |
| Cryptographic (Sign / Verify / Encrypt / Decrypt) | Yes — `Security().KMS()` + `Security().Crypto()` | Wire-compatible legacy REST + MCP tools |
| Seal / Unseal (xkms legacy, not the barrier) | Yes — `Security().Seal()` | Wire-compatible legacy REST + MCP + gRPC |
| Certificate store (Save/Get/Delete/List/Exists/Chain) | Yes — `Security().Certificate()` + `Security().CertificateExt()` | Legacy `/api/v1/xkms/certs/*` |
| PIN admin (SO/User set/change/verify/lockout) | Yes — qrdb `PINBackend` composed at startup; exposed by qrdb handlers (gRPC/QUIC/MCP) | Legacy `/api/v1/xkms/pin/*` retained |

The rule: any operation that has a native `Security()` accessor
should be reached through the SDK on any transport. The xkms-prefixed
endpoints exist as a compatibility surface for clients that still
import go-xkms directly and have not migrated to the QuicBaaS SDK.

### Protocol support

| Protocol | Supported | Base Path / Namespace |
|----------|-----------|----------------------|
| REST | Yes | `/api/v1/xkms/` |
| MCP | Yes | `xkms.*` tool prefix |
| gRPC | Yes | `xkms.v1.KeystoreService` |
| QUIC | Yes | Same as gRPC (shared protobuf) |
| Unix | Yes | gRPC over Unix domain socket |
| Embedded | Yes (native only — the compat bridge has no embedded route) | In-process function calls |

### Operations matrix

| Group | Operations | REST | MCP | gRPC | Native SDK accessor |
|-------|-----------|------|-----|------|---------------------|
| **Health** | Health | `GET /health` | -- | `Health` | `ClusterMgmt().Health()` |
| **Backends** | ListBackends, GetBackend | `GET /backends`, `GET /backends/{id}` | `xkms.listBackends` | `ListBackends`, `GetBackendInfo` | `Security().KMS()` |
| **Key Management** | GenerateKey, GetKey, DeleteKey, ListKeys, RotateKey | `POST /keys`, `GET /keys/{id}`, `DELETE /keys/{id}`, `GET /keys`, `POST /keys/{id}/rotate` | `xkms.generateKey`, `xkms.getKey`, `xkms.deleteKey`, `xkms.listKeys`, `xkms.rotateKey` | `GenerateKey`, `GetKey`, `DeleteKey`, `ListKeys`, `RotateKey` | `Security().KMS()` |
| **Key Versions** | ListVersions, Enable, Disable, EnableAll, DisableAll | `GET /keys/{id}/versions`, `POST .../enable`, `POST .../disable`, `POST .../enable-all`, `POST .../disable-all` | -- | -- | `Security().KMS()` |
| **Cryptographic** | Sign, Verify, Encrypt, Decrypt | `POST /keys/{id}/sign`, `POST /keys/{id}/verify`, `POST /keys/{id}/encrypt`, `POST /keys/{id}/decrypt` | `xkms.sign`, `xkms.verify`, `xkms.encrypt`, `xkms.decrypt` | `Sign`, `Verify`, `Encrypt`, `Decrypt` | `Security().KMS()` |
| **Certificate Store** | SaveCert, GetCert, DeleteCert, ListCerts, CertExists, SaveCertChain, GetCertChain | `POST /certs`, `GET /certs/{id}`, `DELETE /certs/{id}`, `GET /certs`, `HEAD /certs/{id}` | `xkms.saveCert`, `xkms.getCert`, `xkms.deleteCert`, `xkms.listCerts`, `xkms.certExists`, `xkms.saveCertChain`, `xkms.getCertChain` | `SaveCert`, `GetCert`, `DeleteCert`, `ListCerts` | `Security().Certificate()` |
| **Seal (xkms legacy)** | Seal, Unseal, CanSeal | `POST /seal`, `POST /unseal`, `GET /seal/capability` | `xkms.seal`, `xkms.unseal`, `xkms.canSeal` | `Seal`, `Unseal`, `CanSeal` | `Security().Seal()` |
| **Barrier (canonical)** | Status, Initialize, InitializeShamir, Unseal, UnsealWithShare, UnsealWithShares, Seal, Rekey | `POST /api/v1/barrier/*` | `quicbaas.barrier.*` | `quicbaas.v1.BarrierService` | `Security().Barrier()` |
| **Ceremony (canonical)** | Status, ClaimCert, BeginClaimShare/CompleteClaimShare, SignCSR, EnrollExternalOfficer, Initialize | `POST /api/v1/ceremony/*` | `quicbaas.ceremony.*` | `quicbaas.v1.CeremonyService` | `Security().Ceremony()` |
| **Credentials (canonical)** | Submit, List, Rotate, RetrieveVersion, ListVersions, Delete, GCExpired | `POST /api/v1/credentials/*` | `quicbaas.credentials.*` | `quicbaas.v1.CredentialService` | `Security().Credentials()` |
| **CA admin (Phase 8e canonical)** | CAInfo, GetCABundle, ListCertificates, IssueCertificate, RevokeCertificate, SignCSR | `GET /api/v1/ca/info`, `GET /api/v1/ca/bundle`, `GET /api/v1/ca/certificates`, `POST /api/v1/ca/issue`, `POST /api/v1/ca/revoke`, `POST /api/v1/ca/sign` | `quicbaas.ca.*` | `quicbaas.v1.CertificateService` | `Security().Certificate()` |
| **PIV** | ListSlots, GetCert, StoreCert, DeleteCert, GenerateKey, ImportCert, ExportCert, GenerateCSR | `GET /piv/slots`, `GET /piv/slots/{slot}/cert`, `PUT /piv/slots/{slot}/cert`, `DELETE /piv/slots/{slot}/cert`, `POST /piv/slots/{slot}/key`, `POST /piv/slots/{slot}/csr` (Import maps onto `PUT .../cert`, Export onto `GET .../cert` — quicbaasd does not mount xkms's dedicated `/import` and `/export` routes) | `xkms.piv.listSlots`, `xkms.piv.getCert`, `xkms.piv.storeCert`, `xkms.piv.deleteCert`, `xkms.piv.generateKey`, `xkms.piv.importCert`, `xkms.piv.exportCert`, `xkms.piv.generateCSR` | `quicbaas.piv.v1.PIVService` | `Security().Auth().PIV()` |
| **FIDO2** | BeginRegistration, FinishRegistration, BeginAuthentication, FinishAuthentication | `POST /fido2/register/begin`, `POST /fido2/register/finish`, `POST /fido2/authenticate/begin`, `POST /fido2/authenticate/finish` | `xkms.fido2.beginRegistration`, `xkms.fido2.finishRegistration`, `xkms.fido2.beginAuthentication`, `xkms.fido2.finishAuthentication` | `quicbaas.fido2.v1.FIDO2Service` | `Security().Auth().FIDO2()` |
| **CA (legacy compat)** | GetBundle, GetCert, SignCSR, IssueCert, RevokeCert, GenerateCRL, IsRevoked | `GET /ca/bundle`, `GET /ca/cert`, `POST /ca/sign`, `POST /ca/issue`, `POST /ca/revoke`, `POST /ca/crl`, `GET /ca/revoked/{serial}` | `xkms.ca.getBundle`, `xkms.ca.getCert`, `xkms.ca.signCSR`, `xkms.ca.issueCert`, `xkms.ca.revokeCert`, `xkms.ca.generateCRL`, `xkms.ca.isRevoked` | -- | `Security().Certificate()` (use the native surface for new code) |
| **PIN admin** | SetSO, SetUser, ChangeSO, ChangeUser, VerifySO, VerifyUser, LockoutStatus, ResetLockout | `POST /pin/so/set`, `POST /pin/user/set`, `POST /pin/so/change`, `POST /pin/user/change`, `POST /pin/so/verify`, `POST /pin/user/verify`, `GET /pin/lockout`, `POST /pin/lockout/reset` | `xkms.pin.setSO`, `xkms.pin.setUser`, `xkms.pin.changeSO`, `xkms.pin.changeUser`, `xkms.pin.verifySO`, `xkms.pin.verifyUser`, `xkms.pin.lockoutStatus`, `xkms.pin.resetLockout` | `BarrierService.PINStatus` (selected verbs) | qrdb PINBackend (composed; no direct SDK accessor — barrier and ceremony flows manage PINs) |

Notes:

- Native (Phase 8e canonical) rows surface the post-unification
  endpoints. The QuicBaaS SDK's `Security()` accessor is the
  recommended client-side entry point.
- Compat rows preserve the wire surface the go-xkms SDK has always
  spoken. They forward in-process to the same backends the native
  surface uses, so behaviour is identical; only the routes/tool names
  differ.
- The FIDO2 compat surface is tenant-scoped: pass `?tenant_id=` on
  the REST routes (or the `tenant_id` MCP tool argument); otherwise
  the authenticated principal's tenant is used.
- All four `/api/v1/xkms/fido2/*` routes — and the matching
  `xkms.fido2.*` MCP tools — are **authorization-gated**, including
  the authenticate legs (registration → `user_authfactors:add`,
  authentication → `user_authfactors:get`). Pre-auth passkey login
  deliberately lives only on the canonical public surfaces: the
  unified ceremony at `/api/v1/auth/login/{begin,finish}` and the
  tenant WebAuthn mount at
  `/api/v1/iam/{tenant_id}/webauthn/login/{begin,finish}`.

### REST endpoints

#### Compat-bridge xkms routes

All compat routes are mounted under `/api/v1/xkms/`. Defined in
`pkg/api/rest/routes_xkms.go`:

```
GET    /health
GET    /backends
GET    /backends/{id}
POST   /keys
GET    /keys
GET    /keys/{id}
DELETE /keys/{id}
POST   /keys/{id}/rotate
POST   /keys/{id}/sign
POST   /keys/{id}/verify
POST   /keys/{id}/encrypt
POST   /keys/{id}/decrypt
GET    /keys/{id}/versions
POST   /keys/{id}/versions/{version}/enable
POST   /keys/{id}/versions/{version}/disable
POST   /keys/{id}/versions/enable-all
POST   /keys/{id}/versions/disable-all
POST   /certs
GET    /certs
GET    /certs/{id}
DELETE /certs/{id}
HEAD   /certs/{id}
POST   /seal
POST   /unseal
GET    /seal/capability
GET    /piv/slots
GET    /piv/slots/{slot}/cert
PUT    /piv/slots/{slot}/cert
DELETE /piv/slots/{slot}/cert
POST   /piv/slots/{slot}/key
POST   /piv/slots/{slot}/csr
POST   /fido2/register/begin
POST   /fido2/register/finish
POST   /fido2/authenticate/begin
POST   /fido2/authenticate/finish
GET    /ca/bundle
GET    /ca/cert
POST   /ca/sign
POST   /ca/issue
POST   /ca/revoke
POST   /ca/crl
GET    /ca/revoked/{serial}
POST   /pin/so/set
POST   /pin/user/set
POST   /pin/so/change
POST   /pin/user/change
POST   /pin/so/verify
POST   /pin/user/verify
GET    /pin/lockout
POST   /pin/lockout/reset
```

#### Native canonical routes

Native canonical operations live under `/api/v1/` (no `xkms/` prefix)
and are the preferred path for new integrations:

```
# Barrier
POST   /api/v1/barrier/status
POST   /api/v1/barrier/init
POST   /api/v1/barrier/init-shamir
POST   /api/v1/barrier/unseal
POST   /api/v1/barrier/unseal-share
POST   /api/v1/barrier/unseal-shares
POST   /api/v1/barrier/seal
POST   /api/v1/barrier/rekey

# Ceremony
POST   /api/v1/ceremony/status
POST   /api/v1/ceremony/initialize
POST   /api/v1/ceremony/claim-cert/begin
POST   /api/v1/ceremony/claim-cert/complete
POST   /api/v1/ceremony/officer-share/begin
POST   /api/v1/ceremony/officer-share/complete
POST   /api/v1/ceremony/sign-csr
POST   /api/v1/ceremony/enroll-external-officer

# Credentials
POST   /api/v1/credentials/submit
GET    /api/v1/credentials
POST   /api/v1/credentials/rotate
GET    /api/v1/credentials/{name}
POST   /api/v1/credentials/{name}/retrieve-version
GET    /api/v1/credentials/{name}/versions
DELETE /api/v1/credentials/{name}
POST   /api/v1/credentials/{name}/gc

# CA admin
GET    /api/v1/ca/info
GET    /api/v1/ca/bundle
GET    /api/v1/ca/certificates
POST   /api/v1/ca/issue
POST   /api/v1/ca/revoke
POST   /api/v1/ca/sign
```

### MCP tool names

#### Compat-bridge (xkms.* prefix)

Defined in [`pkg/api/mcp/tools_xkms.go`](../../pkg/api/mcp/tools_xkms.go).

**Core:**
`xkms.listBackends`, `xkms.generateKey`, `xkms.getKey`, `xkms.deleteKey`, `xkms.listKeys`, `xkms.rotateKey`, `xkms.sign`, `xkms.verify`, `xkms.encrypt`, `xkms.decrypt`

**Certificate:**
`xkms.saveCert`, `xkms.getCert`, `xkms.deleteCert`, `xkms.listCerts`, `xkms.certExists`, `xkms.saveCertChain`, `xkms.getCertChain`

**Seal (xkms legacy):**
`xkms.seal`, `xkms.unseal`, `xkms.canSeal`

**PIV:**
`xkms.piv.listSlots`, `xkms.piv.getCert`, `xkms.piv.storeCert`, `xkms.piv.deleteCert`, `xkms.piv.generateKey`, `xkms.piv.importCert`, `xkms.piv.exportCert`, `xkms.piv.generateCSR`

**FIDO2** (tenant via `tenant_id` argument, else the authenticated
tenant; all four tools authorization-gated — see the operations-matrix
notes):
`xkms.fido2.beginRegistration`, `xkms.fido2.finishRegistration`, `xkms.fido2.beginAuthentication`, `xkms.fido2.finishAuthentication`

**CA (legacy):**
`xkms.ca.getBundle`, `xkms.ca.getCert`, `xkms.ca.signCSR`, `xkms.ca.issueCert`, `xkms.ca.revokeCert`, `xkms.ca.generateCRL`, `xkms.ca.isRevoked`

**PIN:**
`xkms.pin.setSO`, `xkms.pin.setUser`, `xkms.pin.changeSO`, `xkms.pin.changeUser`, `xkms.pin.verifySO`, `xkms.pin.verifyUser`, `xkms.pin.lockoutStatus`, `xkms.pin.resetLockout`

#### Native (quicbaas.* prefix)

Phase 8e Barrier and Ceremony tools and Phase 8e CA admin tools:

**Barrier:**
`quicbaas.barrier.status`, `quicbaas.barrier.init`, `quicbaas.barrier.initShamir`, `quicbaas.barrier.unseal`, `quicbaas.barrier.unsealShare`, `quicbaas.barrier.unsealShares`, `quicbaas.barrier.seal`, `quicbaas.barrier.rekey`

**Ceremony:**
`quicbaas.ceremony.status`, `quicbaas.ceremony.initialize`, `quicbaas.ceremony.claimCertBegin`, `quicbaas.ceremony.claimCertComplete`, `quicbaas.ceremony.claimShare`, `quicbaas.ceremony.signCSR`, `quicbaas.ceremony.enrollExternalOfficer`

**Credentials:**
`quicbaas.credentials.submit`, `quicbaas.credentials.list`, `quicbaas.credentials.rotate`, `quicbaas.credentials.retrieveVersion`, `quicbaas.credentials.listVersions`, `quicbaas.credentials.delete`, `quicbaas.credentials.gcExpired`

**CA admin:**
`quicbaas.ca.info`, `quicbaas.ca.bundle`, `quicbaas.ca.list`, `quicbaas.ca.issue`, `quicbaas.ca.revoke`, `quicbaas.ca.sign`

### gRPC services

The compatibility layer registers two distinct gRPC service trees:

| Service | Module | Native or compat? | RPCs |
|---------|--------|-------------------|------|
| `xkms.v1.KeystoreService` | `pkg/api/grpc/server_xkms.go` | Compat | `Health`, `ListBackends`, `GetBackendInfo`, `GenerateKey`, `GetKey`, `DeleteKey`, `ListKeys`, `RotateKey`, `Sign`, `Verify`, `Encrypt`, `Decrypt`, `SaveCert`, `GetCert`, `DeleteCert`, `ListCerts`, `Seal`, `Unseal`, `CanSeal` |
| `quicbaas.v1.BarrierService` | [`pkg/server/grpc_barrier_ceremony.go`](../../pkg/server/grpc_barrier_ceremony.go) | Native (Phase 8b/c/d) | `Status`, `Initialize`, `InitializeShamir`, `Unseal`, `UnsealWithShare`, `UnsealWithShares`, `Seal`, `Rekey` |
| `quicbaas.v1.CeremonyService` | `pkg/server/grpc_barrier_ceremony.go` | Native | `Status`, `Initialize`, `ClaimCertBegin`, `ClaimCertComplete`, `BeginClaimShare`, `CompleteClaimShare`, `SignCSR`, `EnrollExternalOfficer` |
| `quicbaas.v1.CredentialService` | qrdb proto, registered by the credential handler | Native | `Submit`, `List`, `Rotate`, `RetrieveVersion`, `ListVersions`, `Delete`, `GCExpired` |
| `quicbaas.v1.CertificateService` | Native (Phase 8e added `CAInfo` + `ListCertificates`) | Native | `CAInfo`, `GetCABundle`, `ListCertificates`, `IssueCertificate`, `RevokeCertificate`, `SignCSR`, plus cert-store ops |

The compat `xkms.v1.KeystoreService` declares barrier methods
(`BarrierStatus`, `BarrierInitialize`, `BarrierUnseal`, `BarrierSeal`)
that return `codes.Unimplemented` — the QuicBaaS native
`quicbaas.v1.BarrierService` is the canonical barrier surface and
the compat declaration exists only to keep the proto contract intact
for older xkms SDK builds.

### Authentication

All xkms operations — native or compat — require authentication via
JWT bearer token, mTLS, or one of the canonical N-factors per
`Security().Auth().Factors()`. The gRPC layer uses QuicBaaS's
standard interceptors. The REST layer uses QuicBaaS's middleware
chain. The MCP layer enforces authentication before dispatching to
any `xkms.*` or `quicbaas.*` tool handler. The embedded transport is
not a security boundary — see
[`docs/architecture/embedded-mode.md`](user.md).

### Source files

| File | Purpose |
|------|---------|
| `pkg/api/rest/routes_xkms.go` | Compat REST route registration |
| [`pkg/api/mcp/tools_xkms.go`](../../pkg/api/mcp/tools_xkms.go) | Compat MCP tool definitions |
| `pkg/api/grpc/server_xkms.go` | Compat gRPC service |
| [`pkg/api/rest/handlers_kms.go`](../../pkg/api/rest/handlers_kms.go) | Native KMS handler (Security().KMS) |
| [`pkg/server/grpc_barrier_ceremony.go`](../../pkg/server/grpc_barrier_ceremony.go) | Native gRPC Barrier + Ceremony services |
| `pkg/server/baas_transport_ceremony.go` | Native QUIC Barrier + Ceremony handlers |
| [`cmd/quicbaas/ca.go`](../../cmd/quicbaas/ca.go) | Native CA verb tree (CAInfo + ListCertificates verbs landed Phase 8e) |
| [`cmd/quicbaas/credential.go`](../../cmd/quicbaas/credential.go) | Native credential verb tree (versioned rotation landed Phase 8e) |

### Trust resolver SDK re-exports and quicbaasd trust: config (Phase C)

#### SDK re-export packages

| Package | Re-exports from |
|---------|----------------|
| `sdk/go/xkms/trust/resolver` | `go-xkms/sdk/go/trust/resolver` — full resolver surface |
| `sdk/go/xkms/trust/bootstrap` | `go-xkms/sdk/go/bootstrap` — now includes Phase B1 TOFU additions: `ModeTOFU`, `TOFUConfig`, `Pin`, `PinStore`, `StoragePinStore`, `NewStoragePinStore` |

Platform code imports through the quicbaas re-export, not go-xkms directly:

```go
import xkmsresolver "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/resolver"

r, err := xkmsresolver.New(&xkmsresolver.Config{
    Scopes: map[string]*xkmsresolver.ScopeConfig{
        "default": {
            Strategies: []xkmsresolver.Strategy{xkmsresolver.StrategyEmbedded},
        },
    },
})
tlsCfg, err := r.TLSConfig(ctx, "default", "kms.example.com:8443")
```

#### quicbaasd trust: config section

The optional `trust:` section configures quicbaasd's outbound TLS trust
resolver. When absent, outbound connections use the OS trust store.

```yaml
trust:
  scopes:
    default:
      # Ordered strategy chain — resolver returns on the first success.
      # Remove tofu once DANE is published for this deployment.
      # tofu MUST be explicitly listed; it is never an implicit fallback.
      strategies: [dane, noise, spki, tofu]
      server-url: "https://kms.example.com:8443"
      per-method-timeout: 5s
```

Strategy reference: `dane` (DNSSEC TLSA), `noise` (Noise_NK channel),
`spki` (pinned SHA-256 SPKI), `embedded` (OS trust store), `ca-file`
(local PEM), `provider-ca` (platform CA bundle), `tofu` (trust-on-first-use
— terminal fallback before DANE is published; remove once DANE is live).

The Go-level `DefaultConfig()` intentionally omits `tofu`; the shipped
YAML templates include it with an explicit comment.

### Migration guidance

New integrations should always use the QuicBaaS SDK and target the
native surface:

```go
sdk, _ := quicbaas.NewClient("quicbaas.local:443", quicbaas.WithTLS(...))
defer sdk.Close()

bar, _ := sdk.Security().Barrier()
cer, _ := sdk.Security().Ceremony()
creds, _ := sdk.Security().Credentials()
ca := sdk.Security().Certificate()
```

Existing callers that import go-xkms's `xkms.Client` against a
QuicBaaS endpoint continue to work unchanged through the compat
bridge. There is no scheduled deprecation date for the bridge.
