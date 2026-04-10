# XKMS Extended Services

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
