# XKMS Compatibility Layer

## Overview

QuicBaaS exposes xkms-compatible wire-protocol endpoints so that `xkms.Client` (the go-xkms SDK) can connect directly to `quicbaasd`. This allows applications already using go-xkms to target QuicBaaS as their key management backend without code changes on the client side.

The compatibility layer delegates to go-xkms's backend layer (`github.com/jeremyhahn/go-xkms/sdk/go`) for all cryptographic operations. Multi-tenant isolation is enforced by injecting the tenant ID from the authentication context into key attributes.

## Protocol Support

| Protocol | Supported | Base Path / Namespace |
|----------|-----------|----------------------|
| REST     | Yes       | `/api/v1/xkms/`     |
| MCP      | Yes       | `xkms.*` tool prefix |
| gRPC     | Yes       | `xkms.v1.KeystoreService` |
| QUIC     | Yes       | Same as gRPC (shared protobuf) |

## Operations Matrix

| Group | Operations | REST | MCP | gRPC |
|-------|-----------|------|-----|------|
| **Health** | Health | `GET /health` | -- | `Health` |
| **Backends** | ListBackends, GetBackend | `GET /backends`, `GET /backends/{id}` | `xkms.listBackends` | `ListBackends`, `GetBackendInfo` |
| **Key Management** | GenerateKey, GetKey, DeleteKey, ListKeys, RotateKey | `POST /keys`, `GET /keys/{id}`, `DELETE /keys/{id}`, `GET /keys`, `POST /keys/{id}/rotate` | `xkms.generateKey`, `xkms.getKey`, `xkms.deleteKey`, `xkms.listKeys`, `xkms.rotateKey` | `GenerateKey`, `GetKey`, `DeleteKey`, `ListKeys`, `RotateKey` |
| **Key Versions** | ListVersions, Enable, Disable, EnableAll, DisableAll | `GET /keys/{id}/versions`, `POST .../enable`, `POST .../disable`, `POST .../enable-all`, `POST .../disable-all` | -- | -- |
| **Cryptographic** | Sign, Verify, Encrypt, Decrypt | `POST /keys/{id}/sign`, `POST /keys/{id}/verify`, `POST /keys/{id}/encrypt`, `POST /keys/{id}/decrypt` | `xkms.sign`, `xkms.verify`, `xkms.encrypt`, `xkms.decrypt` | `Sign`, `Verify`, `Encrypt`, `Decrypt` |
| **Certificate** | SaveCert, GetCert, DeleteCert, ListCerts, CertExists, SaveCertChain, GetCertChain | `POST /certs`, `GET /certs/{id}`, `DELETE /certs/{id}`, `GET /certs`, `HEAD /certs/{id}` | `xkms.saveCert`, `xkms.getCert`, `xkms.deleteCert`, `xkms.listCerts`, `xkms.certExists`, `xkms.saveCertChain`, `xkms.getCertChain` | `SaveCert`, `GetCert`, `DeleteCert`, `ListCerts` |
| **Seal** | Seal, Unseal, CanSeal | `POST /seal`, `POST /unseal`, `GET /seal/capability` | `xkms.seal`, `xkms.unseal`, `xkms.canSeal` | `Seal`, `Unseal`, `CanSeal` |
| **PIV** | ListSlots, GetCert, StoreCert, DeleteCert, GenerateKey, GenerateCSR | `GET /piv/slots`, `GET /piv/slots/{slot}/cert`, `PUT /piv/slots/{slot}/cert`, `DELETE /piv/slots/{slot}/cert`, `POST /piv/slots/{slot}/key`, `POST /piv/slots/{slot}/csr` | `xkms.piv.listSlots`, `xkms.piv.getCert`, `xkms.piv.storeCert`, `xkms.piv.deleteCert`, `xkms.piv.generateKey`, `xkms.piv.generateCSR` | -- |
| **FIDO2** | BeginRegistration, FinishRegistration, BeginAuthentication, FinishAuthentication | `POST /fido2/register/begin`, `POST /fido2/register/finish`, `POST /fido2/authenticate/begin`, `POST /fido2/authenticate/finish` | `xkms.fido2.beginRegistration`, `xkms.fido2.finishRegistration`, `xkms.fido2.beginAuthentication`, `xkms.fido2.finishAuthentication` | -- |
| **CA** | GetBundle, GetCert, SignCSR, IssueCert, RevokeCert, GenerateCRL, IsRevoked | `GET /ca/bundle`, `GET /ca/cert`, `POST /ca/sign`, `POST /ca/issue`, `POST /ca/revoke`, `POST /ca/crl`, `GET /ca/revoked/{serial}` | `xkms.ca.getBundle`, `xkms.ca.getCert`, `xkms.ca.signCSR`, `xkms.ca.issueCert`, `xkms.ca.revokeCert`, `xkms.ca.generateCRL`, `xkms.ca.isRevoked` | -- |
| **PIN** | SetSO, SetUser, ChangeSO, ChangeUser, VerifySO, VerifyUser, LockoutStatus, ResetLockout | `POST /pin/so/set`, `POST /pin/user/set`, `POST /pin/so/change`, `POST /pin/user/change`, `POST /pin/so/verify`, `POST /pin/user/verify`, `GET /pin/lockout`, `POST /pin/lockout/reset` | `xkms.pin.setSO`, `xkms.pin.setUser`, `xkms.pin.changeSO`, `xkms.pin.changeUser`, `xkms.pin.verifySO`, `xkms.pin.verifyUser`, `xkms.pin.lockoutStatus`, `xkms.pin.resetLockout` | -- |

**Note:** FIDO2, CA, and PIN operations are registered but currently return `ErrOperationNotSupported` in the MCP layer. Key Versions are REST-only.

## REST Endpoints

All routes are mounted under `/api/v1/xkms/`. Defined in `pkg/api/rest/routes_xkms.go`.

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

## MCP Tool Names

All MCP tools use the `xkms.` prefix. Defined in `pkg/api/mcp/tools_xkms.go`.

**Core:**
`xkms.listBackends`, `xkms.generateKey`, `xkms.getKey`, `xkms.deleteKey`, `xkms.listKeys`, `xkms.rotateKey`, `xkms.sign`, `xkms.verify`, `xkms.encrypt`, `xkms.decrypt`

**Certificate:**
`xkms.saveCert`, `xkms.getCert`, `xkms.deleteCert`, `xkms.listCerts`, `xkms.certExists`, `xkms.saveCertChain`, `xkms.getCertChain`

**Seal:**
`xkms.seal`, `xkms.unseal`, `xkms.canSeal`

**PIV:**
`xkms.piv.listSlots`, `xkms.piv.getCert`, `xkms.piv.storeCert`, `xkms.piv.deleteCert`, `xkms.piv.generateKey`, `xkms.piv.generateCSR`

**FIDO2:**
`xkms.fido2.beginRegistration`, `xkms.fido2.finishRegistration`, `xkms.fido2.beginAuthentication`, `xkms.fido2.finishAuthentication`

**CA:**
`xkms.ca.getBundle`, `xkms.ca.getCert`, `xkms.ca.signCSR`, `xkms.ca.issueCert`, `xkms.ca.revokeCert`, `xkms.ca.generateCRL`, `xkms.ca.isRevoked`

**PIN:**
`xkms.pin.setSO`, `xkms.pin.setUser`, `xkms.pin.changeSO`, `xkms.pin.changeUser`, `xkms.pin.verifySO`, `xkms.pin.verifyUser`, `xkms.pin.lockoutStatus`, `xkms.pin.resetLockout`

## gRPC Service

The `XKMSServer` struct in `pkg/api/grpc/server_xkms.go` implements the `xkms.v1.KeystoreServiceServer` protobuf interface. It is registered via:

```go
xkmspb.RegisterKeystoreServiceServer(server, NewXKMSServer(log))
```

Implemented RPCs: `Health`, `ListBackends`, `GetBackendInfo`, `GenerateKey`, `GetKey`, `DeleteKey`, `ListKeys`, `RotateKey`, `Sign`, `Verify`, `Encrypt`, `Decrypt`, `SaveCert`, `GetCert`, `DeleteCert`, `ListCerts`, `Seal`, `Unseal`, `CanSeal`.

Barrier operations (`BarrierStatus`, `BarrierInitialize`, `BarrierUnseal`, `BarrierSeal`) return `codes.Unimplemented` -- these are not applicable to the QuicBaaS xkms bridge.

## Authentication

All xkms operations require authentication via JWT bearer token, consistent with other QuicBaaS APIs. The gRPC layer uses QuicBaaS's standard interceptors. The REST layer uses QuicBaaS's middleware chain. The MCP layer enforces authentication before dispatching to any `xkms.*` tool handler.

## Source Files

| File | Purpose |
|------|---------|
| `pkg/api/rest/routes_xkms.go` | REST route registration |
| `pkg/api/mcp/tools_xkms.go` | MCP tool definitions and handlers |
| `pkg/api/grpc/server_xkms.go` | gRPC service implementation |
