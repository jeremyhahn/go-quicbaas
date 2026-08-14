// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Package quicbaas — services_security.go provides the M10 SDK extensions
// for security-domain primitives sourced from go-xkms (ACME, TrustedCA,
// WebAuthn, PIV) and go-qrdb (auth factors). These primitives are exposed
// exclusively through the QuicBaaS SDK so that downstream consumers
// (e.g., go-trusted-platform) never need to import go-xkms or go-qrdb
// directly.
//
// All sub-services delegate to the configured Transport. They use
// well-known service names (quicbaas.<Service>) so server implementations
// can register handlers consistently across gRPC, REST, QUIC, MCP, Unix
// transports.
package quicbaas

import (
	"context"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"sync"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	trustedca "github.com/jeremyhahn/go-xkms/sdk/go/acme"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	acmeclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/acme"
	certclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	sshclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	trustclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/trust"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
	trustbootstrap "github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/trust/bootstrap"
)

// =============================================================================
// Service Names
// =============================================================================

const (
	acmeServiceName    = "quicbaas.ACME"
	trustServiceName   = "quicbaas.Trust"
	fido2ServiceName   = "quicbaas.FIDO2"
	pivServiceName     = "quicbaas.PIV"
	authServiceName    = "quicbaas.Auth"
	certExtServiceName = "quicbaas.Certificate"
)

// =============================================================================
// ACME Type Re-exports
// =============================================================================
//
// ACME types are sourced from go-xkms/sdk/go/acme so that consumers do not
// need a direct go-xkms import. These cover challenge verification, server
// configuration, and the ACME client (RFC 8555 + ARI + cross-sign + TPM
// enrollment).

type (
	// ACMEDirectory is the RFC 8555 directory document.
	ACMEDirectory = trustedca.ACMEDirectory

	// ACMEDirectoryMeta is the metadata block of the ACME directory.
	ACMEDirectoryMeta = trustedca.ACMEMeta

	// ACMECertificateAuthority is the minimal CA interface required by
	// challenge verifiers.
	ACMECertificateAuthority = trustedca.ACMECertificateAuthority

	// ACMEChallengeVerifierFunc is the signature used to register a
	// challenge verifier.
	ACMEChallengeVerifierFunc = trustedca.ACMEChallengeVerifierFunc

	// ACMEEnroll01TCGCSRVerifier is the TCG-CSR-IDEVID verification
	// interface required by enroll-01 challenges (TCG TPM enrollment).
	ACMEEnroll01TCGCSRVerifier = trustedca.ACMEEnroll01TCGCSRVerifier

	// ACMEClient is the protocol client for issuing/renewing certificates.
	ACMEClient = trustedca.ACMEClient

	// ACMEClientConfig is the connection configuration for ACMEClient.
	ACMEClientConfig = trustedca.ACMEClientConfig

	// ACMEClientParams are the parameters used to construct an ACMEClient.
	ACMEClientParams = trustedca.ACMEClientParams

	// ACMECertificateRequest describes a certificate request for the
	// ACME client (used by Issue/Renew/CrossSign).
	ACMECertificateRequest = trustedca.ACMECertificateRequest

	// ACMERenewalConfig configures certificate renewal via ACME ARI.
	ACMERenewalConfig = trustedca.ACMERenewalConfig

	// ACMERenewalInfo carries server-side renewal recommendations.
	ACMERenewalInfo = trustedca.ACMERenewalInfo

	// ACMECrossSignConfig configures cross-signing via ACME.
	ACMECrossSignConfig = trustedca.ACMECrossSignConfig

	// ACMEEnrollmentConfig configures TPM enrollment via ACME.
	ACMEEnrollmentConfig = trustedca.ACMEEnrollmentConfig

	// ACMEChainOption controls certificate chain selection.
	ACMEChainOption = trustedca.ACMEChainOption

	// ACMEChainInfo describes a certificate chain returned by the server.
	ACMEChainInfo = trustedca.ACMEChainInfo

	// ACMEOrder represents an ACME order resource.
	ACMEOrder = trustedca.Order

	// ACMEOrderIdentifier represents an order's identifier (DNS, IP, etc.).
	ACMEOrderIdentifier = trustedca.OrderIdentifier

	// ACMEOrderStatus is the status code of an order.
	ACMEOrderStatus = trustedca.OrderStatus
)

// ACME challenge type constants exposed by the SDK.
const (
	ACMEChallengeTypeEnroll01       = trustedca.ACMEChallengeTypeEnroll01
	ACMEChallengeTypeEndorse01      = trustedca.ACMEChallengeTypeEndorse01
	ACMEChallengeTypeHTTP01         = trustedca.ACMEChallengeTypeHTTP01
	ACMEChallengeTypeDNS01          = trustedca.ACMEChallengeTypeDNS01
	ACMEChallengeTypeDeviceAttest01 = trustedca.ACMEChallengeTypeDeviceAttest01
)

// ACME status constants exposed by the SDK.
const (
	ACMEStatusValid       = trustedca.ACMEStatusValid
	ACMEStatusPending     = trustedca.ACMEStatusPending
	ACMEStatusProcessing  = trustedca.ACMEStatusProcessing
	ACMEStatusInvalid     = trustedca.ACMEStatusInvalid
	ACMEStatusDeactivated = trustedca.ACMEStatusDeactivated
	ACMEStatusExpired     = trustedca.ACMEStatusExpired
	ACMEStatusRevoked     = trustedca.ACMEStatusRevoked
	ACMEStatusReady       = trustedca.ACMEStatusReady
)

// ACME challenge verifier function re-exports. These are pure functions
// that consumers register on their ACME server via ACMEServer.RegisterChallenge.
var (
	// ACMEVerifyHTTP01 verifies an HTTP-01 ACME challenge.
	ACMEVerifyHTTP01 = trustedca.ACMEVerifyHTTP01

	// ACMEVerifyDNS01 verifies a DNS-01 ACME challenge.
	ACMEVerifyDNS01 = trustedca.ACMEVerifyDNS01

	// ACMEVerifyEnroll01 verifies an enroll-01 (TCG TPM) ACME challenge.
	ACMEVerifyEnroll01 = trustedca.ACMEVerifyEnroll01

	// ACMEVerifyEndorse01 verifies an endorse-01 ACME challenge.
	ACMEVerifyEndorse01 = trustedca.ACMEVerifyEndorse01

	// NewACMEClient creates a new ACME protocol client.
	NewACMEClient = trustedca.NewACMEClient
)

// ACME sentinel errors re-exported for SDK consumers.
var (
	// ACMEErrInvalidChallengeType is returned when an invalid challenge
	// type is provided.
	ACMEErrInvalidChallengeType = trustedca.ACMEErrInvalidChallengeType

	// ACMEErrAccountAlreadyExists is returned when attempting to create a
	// duplicate account.
	ACMEErrAccountAlreadyExists = trustedca.ACMEErrAccountAlreadyExists

	// ACMEErrInvalidConfiguration is returned when the ACME client
	// configuration is invalid.
	ACMEErrInvalidConfiguration = trustedca.ACMEErrInvalidConfiguration
)

// =============================================================================
// FIDO2 / WebAuthn / PIV Type Re-exports
// =============================================================================
//
// These types travel over the existing go-xkms transport. The SDK re-exports
// them so consumers reference SDK names rather than xkms paths.

type (
	// FIDO2BeginRegistrationRequest is the request payload for
	// FIDO2.BeginRegistration / WebAuthn.BeginRegistration.
	FIDO2BeginRegistrationRequest = xkmstransport.BeginRegistrationRequest

	// FIDO2BeginRegistrationResponse is the response payload for
	// BeginRegistration.
	FIDO2BeginRegistrationResponse = xkmstransport.BeginRegistrationResponse

	// FIDO2FinishRegistrationRequest is the request payload for
	// FIDO2.FinishRegistration / WebAuthn.FinishRegistration.
	FIDO2FinishRegistrationRequest = xkmstransport.FinishRegistrationRequest

	// FIDO2FinishRegistrationResponse is the response payload for
	// FinishRegistration.
	FIDO2FinishRegistrationResponse = xkmstransport.FinishRegistrationResponse

	// FIDO2BeginAuthenticationRequest is the request payload for
	// BeginAuthentication / BeginLogin.
	FIDO2BeginAuthenticationRequest = xkmstransport.BeginAuthenticationRequest

	// FIDO2BeginAuthenticationResponse is the response payload for
	// BeginAuthentication / BeginLogin.
	FIDO2BeginAuthenticationResponse = xkmstransport.BeginAuthenticationResponse

	// FIDO2FinishAuthenticationRequest is the request payload for
	// FinishAuthentication / FinishLogin.
	FIDO2FinishAuthenticationRequest = xkmstransport.FinishAuthenticationRequest

	// FIDO2FinishAuthenticationResponse is the response payload for
	// FinishAuthentication / FinishLogin.
	FIDO2FinishAuthenticationResponse = xkmstransport.FinishAuthenticationResponse

	// PIVListSlotsRequest is the request payload for PIV.ListPIVSlots.
	PIVListSlotsRequest = xkmstransport.ListPIVSlotsRequest

	// PIVListSlotsResponse is the response payload for PIV.ListPIVSlots.
	PIVListSlotsResponse = xkmstransport.ListPIVSlotsResponse

	// PIVSlotStatus is a single PIV slot's status entry.
	PIVSlotStatus = xkmstransport.PIVSlotStatus

	// PIVGetCertificateRequest retrieves a stored PIV certificate.
	PIVGetCertificateRequest = xkmstransport.GetPIVCertificateRequest

	// PIVGetCertificateResponse contains a stored PIV certificate.
	PIVGetCertificateResponse = xkmstransport.GetPIVCertificateResponse

	// PIVStoreCertificateRequest stores or imports a PIV certificate.
	PIVStoreCertificateRequest = xkmstransport.StorePIVCertificateRequest

	// PIVDeleteCertificateRequest removes a stored PIV certificate.
	PIVDeleteCertificateRequest = xkmstransport.DeletePIVCertificateRequest

	// PIVGenerateKeyRequest generates a new PIV key + cert.
	PIVGenerateKeyRequest = xkmstransport.GeneratePIVKeyRequest

	// PIVGenerateKeyResponse holds the generated material.
	PIVGenerateKeyResponse = xkmstransport.GeneratePIVKeyResponse

	// PIVGenerateCSRRequest generates a CSR from a PIV slot key.
	PIVGenerateCSRRequest = xkmstransport.GeneratePIVCSRRequest

	// PIVGenerateCSRResponse holds the generated CSR.
	PIVGenerateCSRResponse = xkmstransport.GeneratePIVCSRResponse
)

// =============================================================================
// Auth Factor Type Re-exports (from go-qrdb)
// =============================================================================
//
// AuthFactor models live in go-qrdb. The SDK exposes them so consumers stay
// inside the quicbaas SDK boundary.

type (
	// AuthFactorEnrollRequest enrolls a custodian/user in an auth factor.
	AuthFactorEnrollRequest = qrdbtransport.EnrollFactorRequest

	// AuthFactorEnrollResponse describes a successful enrollment.
	AuthFactorEnrollResponse = qrdbtransport.EnrollFactorResponse

	// AuthFactorChallengeRequest issues a challenge for an enrolled factor.
	AuthFactorChallengeRequest = qrdbtransport.ChallengeFactorRequest

	// AuthFactorChallengeResponse contains the issued challenge nonce and
	// any factor-specific method data (e.g., WebAuthn options).
	AuthFactorChallengeResponse = qrdbtransport.ChallengeFactorResponse

	// AuthFactorVerifyRequest is submitted with the custodian's response.
	AuthFactorVerifyRequest = qrdbtransport.VerifyFactorRequest

	// AuthFactorListRequest filters enrolled factors.
	AuthFactorListRequest = qrdbtransport.ListFactorsRequest

	// AuthFactorListResponse is the list of enrolled factors.
	AuthFactorListResponse = qrdbtransport.ListFactorsResponse

	// AuthFactorEntry is a single enrolled factor record.
	AuthFactorEntry = qrdbtransport.EnrollmentEntry

	// AuthFactorRevokeRequest permanently revokes an enrollment.
	AuthFactorRevokeRequest = qrdbtransport.RevokeFactorRequest

	// Unified login ceremony types (mtls | webauthn/fido2 | piv).
	// Re-exported from go-qrdb so consumers (e.g. go-trusted-platform)
	// drive the login ceremony entirely through the quicbaas SDK without
	// importing go-qrdb directly.

	// BeginLoginRequest opens a login attempt for a factor type.
	BeginLoginRequest = qrdbtransport.BeginLoginRequest

	// BeginLoginResponse carries the server-issued login challenge/session.
	BeginLoginResponse = qrdbtransport.BeginLoginResponse

	// FinishLoginRequest submits the factor proof to complete login.
	FinishLoginRequest = qrdbtransport.FinishLoginRequest

	// Identity is the resolved authenticated entity carried on a
	// FinishLoginResponse (subject + tenant + roles + metadata).
	Identity = qrdbtransport.Identity

	// FactorType enumerates the unified-login factor types.
	FactorType = qrdbtransport.FactorType
)

// FinishLoginResponse carries the issued OIDC bearer, the resolved identity,
// and the credential that renews the session. It is defined in the shared
// transport package so every concrete transport can construct it; see
// transport.FinishLoginResponse for the rationale.
type FinishLoginResponse = transport.FinishLoginResponse

// Unified-login factor type constants, re-exported from go-qrdb so consumers
// select a login method without importing go-qrdb.
const (
	// FactorTypeMTLS selects the client-certificate / mTLS factor.
	FactorTypeMTLS = qrdbtransport.FactorTypeMTLS

	// FactorTypeFIDO2 selects the WebAuthn / FIDO2 factor.
	FactorTypeFIDO2 = qrdbtransport.FactorTypeFIDO2

	// FactorTypePIV selects the PIV smart-card factor.
	FactorTypePIV = qrdbtransport.FactorTypePIV
)

// =============================================================================
// ACME Service Sub-Interface
// =============================================================================

// ACMEService provides ACME server-side challenge verifier registration
// plus an ACME protocol client for issuing/renewing/cross-signing certificates.
// Server() and Client() return distinct sub-clients with focused surfaces.
type ACMEService interface {
	// Server returns the ACME server-side configuration client. Use this
	// to register challenge verifiers (enroll-01, endorse-01, http-01,
	// dns-01) and trigger server-side issuance.
	Server() ACMEServerClient

	// Client returns the ACME protocol client used to obtain certificates
	// from a remote ACME directory (RFC 8555 + ARI + cross-sign).
	Client() ACMEClientAPI
}

// ACMEServerClient is the server-side ACME control surface.
type ACMEServerClient interface {
	// RegisterChallenge installs a challenge verifier on the server.
	// name is one of "http-01", "dns-01", "enroll-01", "endorse-01" or a
	// vendor extension. verifier must satisfy the verifier function
	// signature (ACMEChallengeVerifierFunc).
	RegisterChallenge(ctx context.Context, name string, verifier ACMEChallengeVerifierFunc) error

	// Issue performs a server-side issuance of a certificate from a CSR
	// using the named challenge type. Returns the issued certificate
	// chain in PEM format.
	Issue(ctx context.Context, csrPEM []byte, challengeType, challengeValue string) (*ACMEIssueResponse, error)
}

// ACMEIssueResponse is the result of a server-side ACME Issue call.
type ACMEIssueResponse struct {
	CertificatePEM []byte    `json:"certificate_pem"`
	ChainPEM       []byte    `json:"chain_pem"`
	SerialNumber   string    `json:"serial_number"`
	NotBefore      time.Time `json:"not_before"`
	NotAfter       time.Time `json:"not_after"`
}

// ACMEClientAPI is the client-side ACME protocol surface. It is named
// ACMEClientAPI to disambiguate from the existing ACMEClient struct alias
// re-exported above.
type ACMEClientAPI interface {
	// NewOrder creates a new ACME order for the given identifiers.
	// challengeType selects the challenge family (http-01, dns-01, etc.).
	NewOrder(ctx context.Context, identifiers []ACMEOrderIdentifier, challengeType string) (*ACMEOrder, error)

	// FinalizeOrder finalizes an order with the supplied CSR and returns
	// the issued certificate PEM.
	FinalizeOrder(ctx context.Context, orderURL string, csrPEM []byte) ([]byte, error)

	// Renew renews a certificate using ACME ARI when supported, falling
	// back to a fresh order otherwise. certURL identifies the cert in the
	// ACME directory.
	Renew(ctx context.Context, certURL string) ([]byte, error)
}

// =============================================================================
// Trust Service Sub-Interface
// =============================================================================

// TrustService publishes trust artifacts (DANE TLSA records, cross-signed
// roots, AIA-driven imports) and exposes the canonical trust-bootstrap
// dispatch via Bootstrap(). All four are required by the §5d single-dep
// surface.
type TrustService interface {
	// PublishDANE publishes DANE TLSA records for the given
	// (name, port, cert) tuple via the DANE publisher registered on the
	// server; xkms ships no DNS implementation, so this returns a typed
	// not-configured error when no publisher is registered.
	PublishDANE(ctx context.Context, name string, port int, cert *x509.Certificate) error

	// CrossSignedRootPool returns a *x509.CertPool of cross-signed roots
	// usable for verifying chains that bridge between issuers.
	CrossSignedRootPool(ctx context.Context) (*x509.CertPool, error)

	// AutoImportAIA walks an AIA chain and imports referenced authority
	// certificates into the local trust store. The provided certChain
	// must be DER-encoded.
	AutoImportAIA(ctx context.Context, certChain []*x509.Certificate) error

	// Bootstrap returns the trust-bootstrap dispatcher that wraps
	// go-truststrap's DANE / Noise / SPKI / Direct / Auto modes.
	// Backed by xkms/pkg/trust/bootstrap (the single canonical
	// implementation in the stack).
	Bootstrap() TrustBootstrapClient
}

// TrustBootstrapClient is the SDK surface for trust bootstrapping —
// fetching the initial CA bundle from a remote server using the
// caller-selected verification strategy. Implementations forward to
// the canonical xkms/pkg/trust/bootstrap.Fetch via the
// sdk/go/xkms/trust/bootstrap re-export.
//
// This is a local-only client: each call performs a direct outbound
// network fetch from the caller (DNS for DANE, TCP for Noise, HTTPS
// for SPKI/Direct). There is no transport hop through the configured
// quicbaas backend — the SDK is acting as a library facade, not a
// remote proxy.
type TrustBootstrapClient interface {
	// Fetch executes a single bootstrap attempt and returns the
	// resulting CA bundle.
	Fetch(ctx context.Context, cfg trustbootstrap.Config) (*trustbootstrap.CABundle, error)

	// DeriveNoiseAddr returns the canonical host:port the Noise
	// bootstrap channel listens on, derived from a server URL.
	// Exposed so CLI / GUI callers can offer a default before the
	// caller supplies an explicit NoiseConfig.ServerAddr.
	DeriveNoiseAddr(serverURL string) string
}

// =============================================================================
// SSH Service Sub-Interface
// =============================================================================

// SSHService is the Security() sub-service for SSH key, known-hosts, and
// authorized-keys management. It is a type alias for the canonical go-xkms
// SSH client contract: QuicBaaS composes that contract over its own
// multi-protocol transports rather than declaring a parallel one.
//
//	Keys()           — generate / import / get / list / delete / export /
//	                   forward-signing ssh.Signer
//	KnownHosts()     — pin / list / remove / verify / HostKeyCallback
//	AuthorizedKeys() — add / get / list / remove / emit / verify
//
// Session().Dial and Console report CodeUnimplemented on every QuicBaaS
// transport: an outbound SSH session must run in the caller's own process,
// and terminating it on the server would turn a key vault into an SSH jump
// host. Remote keys still drive a local session — resolve Keys().Signer,
// which delegates each signature over the wire and never moves the private
// half. This matches go-xkms, whose remote transports refuse both verbs for
// the same reason.
//
// Close() is a no-op: the transport belongs to the parent SDK client.
type SSHService = sshclient.Client

// newSSHService builds the transport-backed SSH service.
func newSSHService(t transport.Transport) SSHService {
	return sshclient.NewClient(t)
}

// =============================================================================
// Certificate Extensions (CrossSign / Chain / IssueDeviceIdentity)
// =============================================================================

// CertificateExtensions adds higher-level certificate operations not present
// in the existing CertificateClient. Implementations dispatch to the
// quicbaas.Certificate service over the configured transport.
type CertificateExtensions interface {
	// CrossSign cross-signs an external root certificate with the local
	// CA. Returns the cross-signed certificate PEM.
	CrossSign(ctx context.Context, foreignRootPEM []byte) ([]byte, error)

	// Chain walks the AIA chain for the supplied certificate PEM and
	// returns the chain in issuer order, root-last.
	Chain(ctx context.Context, certPEM []byte) ([][]byte, error)

	// IssueDeviceIdentity issues a TCG device identity certificate
	// (EK, AK, or IDevID per the request type).
	IssueDeviceIdentity(ctx context.Context, req *DeviceIdentityRequest) (*DeviceIdentityResponse, error)
}

// DeviceIdentityType selects the TCG identity certificate flavor. It aliases
// the leaf client/certificate type so transport handlers — which cannot import
// this package — can construct the values the SDK asserts on.
type DeviceIdentityType = certclient.DeviceIdentityType

const (
	// DeviceIdentityEK issues an Endorsement Key certificate.
	DeviceIdentityEK = certclient.DeviceIdentityEK

	// DeviceIdentityAK issues an Attestation Key certificate.
	DeviceIdentityAK = certclient.DeviceIdentityAK

	// DeviceIdentityIDevID issues an IEEE 802.1AR IDevID certificate.
	DeviceIdentityIDevID = certclient.DeviceIdentityIDevID
)

// DeviceIdentityRequest describes a TCG device identity issuance request.
type DeviceIdentityRequest = certclient.DeviceIdentityRequest

// DeviceIdentityResponse is the issued device identity certificate.
type DeviceIdentityResponse = certclient.DeviceIdentityResponse

// =============================================================================
// FIDO2 Sub-Interface (WebAuthn relying-party ceremony)
// =============================================================================

// FIDO2Client is the single canonical WebAuthn/FIDO2 ceremony surface
// (registration + authentication) for the stack. The verb shape and the
// payload types are go-xkms's FIDO2 transport contract verbatim
// (BeginRegistration / FinishRegistration / BeginAuthentication /
// FinishAuthentication over the xkms wire types); the SDK adds no parallel
// ceremony model.
//
// Tenant scoping is out-of-band: the ceremony targets the tenant carried on
// the request context via transport.ContextWithTenant, falling back to the
// transport's configured default tenant. Local CTAP2 authenticator discovery
// is a separate, deliberately non-transport surface — see Devices().
type FIDO2Client interface {
	// BeginRegistration starts a WebAuthn registration ceremony and
	// returns the JSON-marshaled CredentialCreationOptions plus the opaque
	// ceremony session ID.
	BeginRegistration(ctx context.Context, req *FIDO2BeginRegistrationRequest) (*FIDO2BeginRegistrationResponse, error)

	// FinishRegistration completes a WebAuthn registration ceremony with
	// the authenticator attestation produced by the client.
	FinishRegistration(ctx context.Context, req *FIDO2FinishRegistrationRequest) (*FIDO2FinishRegistrationResponse, error)

	// BeginAuthentication starts a WebAuthn authentication ceremony and
	// returns the JSON-marshaled CredentialRequestOptions plus the opaque
	// ceremony session ID.
	BeginAuthentication(ctx context.Context, req *FIDO2BeginAuthenticationRequest) (*FIDO2BeginAuthenticationResponse, error)

	// FinishAuthentication completes a WebAuthn authentication ceremony
	// with the authenticator assertion and returns the issued token.
	FinishAuthentication(ctx context.Context, req *FIDO2FinishAuthenticationRequest) (*FIDO2FinishAuthenticationResponse, error)
}

// =============================================================================
// Auth Factor Sub-Interface (M14)
// =============================================================================

// AuthFactorsClient exposes go-qrdb's User N-AuthFactor model. Implementations
// delegate to qrdb's AuthFactor service via the configured transport. Login
// itself is NOT part of this surface: the unified login ceremony is
// AuthService.BeginLogin / AuthService.FinishLogin, which rides every
// transport.
type AuthFactorsClient interface {
	// Add enrolls a new auth factor for a user.
	Add(ctx context.Context, req *AuthFactorEnrollRequest) (*AuthFactorEnrollResponse, error)

	// List returns all enrolled factors for a user. Pass an empty userID
	// to list factors for the calling identity (server-determined).
	List(ctx context.Context, userID string) (*AuthFactorListResponse, error)

	// Remove revokes an enrollment. force=true allows removing the last
	// remaining factor (which moves the user to inactive state).
	Remove(ctx context.Context, factorID string, force bool) error

	// Rotate replaces an existing factor with a fresh credential in a
	// single transaction.
	Rotate(ctx context.Context, oldID string, newFactor *AuthFactorEnrollRequest) (*AuthFactorEnrollResponse, error)

	// Relabel updates the human-readable label of an enrolled factor.
	Relabel(ctx context.Context, factorID, label string) error
}

// =============================================================================
// AuthService — extended Auth surface
// =============================================================================

// AuthService is the extended Auth sub-interface returned by
// SecurityService.Auth(). It embeds the existing AuthClient surface
// (Login/RefreshToken/sessions/...) AND adds factor-typed sub-services
// (FIDO2, PIV, Devices, Factors) per §5e of the architecture spec.
//
// Embedding AuthClient preserves backward compatibility: any consumer
// that called Security().Auth().Login(...) continues to work unchanged.
type AuthService interface {
	AuthClient

	// FIDO2 returns the WebAuthn/FIDO2 relying-party ceremony client. It
	// is the single canonical ceremony accessor (registration +
	// authentication) for the stack, in go-xkms's FIDO2 verb shape; CTAP2
	// local-device discovery is exposed separately via Devices(). The
	// ceremony targets the tenant carried on the request context
	// (transport.ContextWithTenant) or the transport's default tenant.
	FIDO2() FIDO2Client

	// PIV returns the backend-scoped PIV client in go-xkms's PIVService
	// shape (backends: file / pkcs11 / tpm2). Card/hardware interaction is
	// client-local per xkms convention; these verbs manage PIV slots on
	// the composed server-side backends.
	PIV() XKMSPIVService

	// Devices returns the client-LOCAL CTAP2 authenticator helper. Unlike
	// the WebAuthn ceremony (which is a server round-trip), Devices()
	// enumerates USB-HID authenticators physically attached to THIS client;
	// it performs no transport request. See LocalDevices.
	Devices() LocalDevices

	// Factors returns the multi-factor enrollment + login client.
	Factors() AuthFactorsClient

	// BeginLogin starts the unified login ceremony for any factor type
	// (mtls / fido2 / piv) and returns the server-issued challenge. It
	// routes to qrdb's verification-capable LoginClient — the single
	// canonical login backbone shared across the stack. WebAuthn/PIV
	// receive a method-specific challenge (the FactorChallenger); mTLS
	// uses the handshake.
	BeginLogin(ctx context.Context, req qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error)

	// FinishLogin submits the factor proof produced from the BeginLogin
	// challenge and, on success, returns the issued bearer and resolved
	// identity. The proof is verified server-side by the registered factor
	// authenticator before any token is issued.
	FinishLogin(ctx context.Context, req qrdbtransport.FinishLoginRequest) (*FinishLoginResponse, error)
}

// =============================================================================
// Service Implementations (transport-backed)
// =============================================================================

// acmeServerImpl is a transport-backed ACMEServerClient. RegisterChallenge is
// invoked locally — the verifier function is registered with the in-process
// ACME server. Issue dispatches over the transport.
type acmeServerImpl struct {
	t          transport.Transport
	mu         sync.Mutex
	verifiers  map[string]ACMEChallengeVerifierFunc
	registered map[string]bool
}

func newACMEServer(t transport.Transport) *acmeServerImpl {
	return &acmeServerImpl{
		t:          t,
		verifiers:  make(map[string]ACMEChallengeVerifierFunc),
		registered: make(map[string]bool),
	}
}

// RegisterChallenge installs a challenge verifier in the SDK process and
// notifies the server that the challenge type is active. The verifier
// itself stays in-process: the server invokes it via the embedded
// ACMERestService API surface, which is wired by the server-side
// integration code.
func (s *acmeServerImpl) RegisterChallenge(ctx context.Context, name string, verifier ACMEChallengeVerifierFunc) error {
	if name == "" {
		return sdkerrors.NewInvalidInput("acme.RegisterChallenge", "name is required")
	}
	if verifier == nil {
		return sdkerrors.NewInvalidInput("acme.RegisterChallenge", "verifier is required")
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("acme.RegisterChallenge", "context is required")
	}

	s.mu.Lock()
	s.verifiers[name] = verifier
	already := s.registered[name]
	s.mu.Unlock()

	if already {
		return nil
	}

	if _, err := s.t.Request(ctx, acmeServiceName, "RegisterChallenge", &acmeclient.RegisterChallengeRequest{Name: name}); err != nil {
		return err
	}

	s.mu.Lock()
	s.registered[name] = true
	s.mu.Unlock()
	return nil
}

// Verifier returns the locally-registered verifier (for use by an
// embedded ACME server). Returns nil when no verifier is registered.
func (s *acmeServerImpl) Verifier(name string) ACMEChallengeVerifierFunc {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.verifiers[name]
}

// Issue triggers a server-side ACME issuance using the named challenge. The
// challenge identified by challengeType/challengeValue must already have been
// validated; the server signs the supplied CSR with the composed CA and
// returns the issued chain.
func (s *acmeServerImpl) Issue(ctx context.Context, csrPEM []byte, challengeType, challengeValue string) (*ACMEIssueResponse, error) {
	if len(csrPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("acme.Issue", "csr is required")
	}
	if challengeType == "" {
		return nil, sdkerrors.NewInvalidInput("acme.Issue", "challenge type is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("acme.Issue", "context is required")
	}

	resp, err := s.t.Request(ctx, acmeServiceName, "Issue", &acmeclient.IssueRequest{
		CSRPEM:         csrPEM,
		ChallengeType:  challengeType,
		ChallengeValue: challengeValue,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("acme.Issue", "nil response from server")
	}

	out, ok := resp.(*acmeclient.IssueResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("acme.Issue", "invalid response type")
	}
	return &ACMEIssueResponse{
		CertificatePEM: out.CertificatePEM,
		ChainPEM:       out.ChainPEM,
		SerialNumber:   out.SerialNumber,
	}, nil
}

// acmeClientImpl is the ACMEClientAPI surface for the ACME PROTOCOL CLIENT
// verbs (NewOrder / FinalizeOrder / Renew).
//
// QuicBaaS is the BROKER-AS-CA: it IS the certificate authority. These three
// verbs are therefore NOT JWS-signed RFC 8555 exchanges against an external
// ACME directory — they are in-process service calls routed over the quicbaas
// transport (REST + embedded) to the server-side ACMEServicer, exactly like
// the Issue control verb. NewOrder returns a lightweight pending order
// descriptor, FinalizeOrder signs the supplied CSR with the composed CA, and
// Renew resolves an existing certificate by serial.
type acmeClientImpl struct {
	t transport.Transport
}

// NewOrder creates a lightweight in-process broker-as-CA order. The returned
// ACMEOrder is a pending descriptor whose Finalize URL carries a generated
// order ID; FinalizeOrder only needs the CSR supplied later.
func (c *acmeClientImpl) NewOrder(ctx context.Context, identifiers []ACMEOrderIdentifier, challengeType string) (*ACMEOrder, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("acme.NewOrder", "context is required")
	}
	if len(identifiers) == 0 {
		return nil, sdkerrors.NewInvalidInput("acme.NewOrder", "at least one identifier is required")
	}

	wireIDs := make([]acmeclient.OrderIdentifier, len(identifiers))
	for i, id := range identifiers {
		wireIDs[i] = acmeclient.OrderIdentifier{Type: id.Type, Value: id.Value}
	}

	resp, err := c.t.Request(ctx, acmeServiceName, "NewOrder", &acmeclient.NewOrderRequest{
		Identifiers:   wireIDs,
		ChallengeType: challengeType,
	})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*acmeclient.NewOrderResponse)
	if !ok || out == nil {
		return nil, sdkerrors.NewInternal("acme.NewOrder", "invalid response type")
	}

	order := &ACMEOrder{
		ID:       out.OrderID,
		Status:   ACMEOrderStatus(out.Status),
		Finalize: out.Finalize,
		URL:      out.URL,
	}
	order.Identifiers = make([]ACMEOrderIdentifier, len(out.Identifiers))
	for i, id := range out.Identifiers {
		order.Identifiers[i] = ACMEOrderIdentifier{Type: id.Type, Value: id.Value}
	}
	return order, nil
}

// FinalizeOrder signs the supplied CSR with the composed CA and returns the
// issued leaf certificate PEM. orderURL is advisory in the broker-as-CA model.
func (c *acmeClientImpl) FinalizeOrder(ctx context.Context, orderURL string, csrPEM []byte) ([]byte, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("acme.FinalizeOrder", "context is required")
	}
	if len(csrPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("acme.FinalizeOrder", "csr is required")
	}

	resp, err := c.t.Request(ctx, acmeServiceName, "FinalizeOrder", &acmeclient.FinalizeOrderRequest{
		OrderURL: orderURL,
		CSRPEM:   csrPEM,
	})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*acmeclient.FinalizeOrderResponse)
	if !ok || out == nil {
		return nil, sdkerrors.NewInternal("acme.FinalizeOrder", "invalid response type")
	}
	return out.CertificatePEM, nil
}

// Renew resolves an existing certificate by serial (certURL is the hex serial
// the CA assigned at issuance, optionally as an "/acme/cert/<serial>" path).
// Because the broker-as-CA holds no subscriber private key and this signature
// carries no CSR, a usable leaf cannot be re-signed in-process; Renew confirms
// the certificate exists and returns a typed error directing the caller to
// NewOrder/FinalizeOrder with a fresh CSR.
func (c *acmeClientImpl) Renew(ctx context.Context, certURL string) ([]byte, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("acme.Renew", "context is required")
	}
	if certURL == "" {
		return nil, sdkerrors.NewInvalidInput("acme.Renew", "certificate url is required")
	}

	resp, err := c.t.Request(ctx, acmeServiceName, "Renew", &acmeclient.RenewRequest{
		CertURL: certURL,
	})
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*acmeclient.RenewResponse)
	if !ok || out == nil {
		return nil, sdkerrors.NewInternal("acme.Renew", "invalid response type")
	}
	return out.CertificatePEM, nil
}

// acmeServiceImpl is the ACMEService composite.
type acmeServiceImpl struct {
	server ACMEServerClient
	client ACMEClientAPI
}

func newACMEService(t transport.Transport) ACMEService {
	return &acmeServiceImpl{
		server: newACMEServer(t),
		client: &acmeClientImpl{t: t},
	}
}

func (s *acmeServiceImpl) Server() ACMEServerClient { return s.server }
func (s *acmeServiceImpl) Client() ACMEClientAPI    { return s.client }

// trustServiceImpl is the TrustService.
type trustServiceImpl struct {
	t transport.Transport
}

func (s *trustServiceImpl) PublishDANE(ctx context.Context, name string, port int, cert *x509.Certificate) error {
	if name == "" {
		return sdkerrors.NewInvalidInput("trust.PublishDANE", "name is required")
	}
	if port <= 0 {
		return sdkerrors.NewInvalidInput("trust.PublishDANE", "port must be positive")
	}
	if cert == nil {
		return sdkerrors.NewInvalidInput("trust.PublishDANE", "cert is required")
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("trust.PublishDANE", "context is required")
	}
	_, err := s.t.Request(ctx, trustServiceName, "PublishDANE", &trustclient.PublishDANERequest{
		Name:    name,
		Port:    port,
		CertDER: cert.Raw,
	})
	return err
}

// TrustCrossSignedRootPoolResponse is the wire-level response payload
// for TrustService.CrossSignedRootPool. Each element is either a
// PEM-encoded or DER-encoded certificate. It aliases the leaf
// client/trust type so transport handlers (which cannot import this
// package without a cycle) and SDK callers share one definition.
type TrustCrossSignedRootPoolResponse = trustclient.CrossSignedRootPoolResponse

func (s *trustServiceImpl) CrossSignedRootPool(ctx context.Context) (*x509.CertPool, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("trust.CrossSignedRootPool", "context is required")
	}
	resp, err := s.t.Request(ctx, trustServiceName, "CrossSignedRootPool", &trustclient.CrossSignedRootPoolRequest{})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return x509.NewCertPool(), nil
	}
	payload, ok := resp.(*TrustCrossSignedRootPoolResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("trust.CrossSignedRootPool", "invalid response type")
	}

	pool := x509.NewCertPool()
	for _, derOrPEM := range payload.RootPEMs {
		block, _ := pem.Decode(derOrPEM)
		if block != nil {
			cert, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, sdkerrors.Wrap(err, sdkerrors.CodeInternal,
					"trust.CrossSignedRootPool", "failed to parse PEM root")
			}
			pool.AddCert(cert)
			continue
		}
		// Fallback: try DER directly
		cert, err := x509.ParseCertificate(derOrPEM)
		if err != nil {
			return nil, sdkerrors.Wrap(err, sdkerrors.CodeInternal,
				"trust.CrossSignedRootPool", "failed to parse DER root")
		}
		pool.AddCert(cert)
	}
	return pool, nil
}

// trustAutoImportAIARequest aliases the leaf client/trust request type
// so the SDK and transport handlers share one wire definition.
type trustAutoImportAIARequest = trustclient.AutoImportAIARequest

func (s *trustServiceImpl) AutoImportAIA(ctx context.Context, certChain []*x509.Certificate) error {
	if len(certChain) == 0 {
		return sdkerrors.NewInvalidInput("trust.AutoImportAIA", "chain is required")
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("trust.AutoImportAIA", "context is required")
	}
	chainDER := make([][]byte, len(certChain))
	for i, c := range certChain {
		if c == nil {
			return sdkerrors.NewInvalidInput("trust.AutoImportAIA", "chain entry is nil")
		}
		chainDER[i] = c.Raw
	}
	_, err := s.t.Request(ctx, trustServiceName, "AutoImportAIA", &trustAutoImportAIARequest{
		ChainDER: chainDER,
	})
	return err
}

func (s *trustServiceImpl) Bootstrap() TrustBootstrapClient {
	return trustBootstrapImpl{}
}

func newTrustService(t transport.Transport) TrustService {
	return &trustServiceImpl{t: t}
}

// trustBootstrapImpl is the TrustBootstrapClient. It forwards every
// call to the canonical xkms/pkg/trust/bootstrap library via the
// SDK's type-alias re-export. There is no per-instance state — the
// underlying Fetch is stateless — so we use a zero-sized struct.
type trustBootstrapImpl struct{}

func (trustBootstrapImpl) Fetch(ctx context.Context, cfg trustbootstrap.Config) (*trustbootstrap.CABundle, error) {
	return trustbootstrap.Fetch(ctx, cfg)
}

func (trustBootstrapImpl) DeriveNoiseAddr(serverURL string) string {
	return trustbootstrap.DeriveNoiseAddr(serverURL)
}

// certExtImpl implements CertificateExtensions.
type certExtImpl struct {
	t transport.Transport
}

type certCrossSignRequest = certclient.CrossSignRequest

// CertCrossSignResponse is the wire-level response payload for
// CertificateExtensions.CrossSign.
type CertCrossSignResponse = certclient.CrossSignResponse

func (c *certExtImpl) CrossSign(ctx context.Context, foreignRootPEM []byte) ([]byte, error) {
	if len(foreignRootPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("certificate.CrossSign", "foreign root PEM is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("certificate.CrossSign", "context is required")
	}
	resp, err := c.t.Request(ctx, certExtServiceName, "CrossSign", &certCrossSignRequest{ForeignRootPEM: foreignRootPEM})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("certificate.CrossSign", "nil response from server")
	}
	out, ok := resp.(*CertCrossSignResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("certificate.CrossSign", "invalid response type")
	}
	return out.CrossSignedPEM, nil
}

type certChainRequest = certclient.ChainRequest

// CertChainResponse is the wire-level response payload for
// CertificateExtensions.Chain. ChainPEM is ordered issuer-first.
type CertChainResponse = certclient.ChainResponse

func (c *certExtImpl) Chain(ctx context.Context, certPEM []byte) ([][]byte, error) {
	if len(certPEM) == 0 {
		return nil, sdkerrors.NewInvalidInput("certificate.Chain", "certificate PEM is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("certificate.Chain", "context is required")
	}
	resp, err := c.t.Request(ctx, certExtServiceName, "Chain", &certChainRequest{CertificatePEM: certPEM})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("certificate.Chain", "nil response from server")
	}
	out, ok := resp.(*CertChainResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("certificate.Chain", "invalid response type")
	}
	return out.ChainPEM, nil
}

func (c *certExtImpl) IssueDeviceIdentity(ctx context.Context, req *DeviceIdentityRequest) (*DeviceIdentityResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("certificate.IssueDeviceIdentity", "request is required")
	}
	if req.Type == "" {
		return nil, sdkerrors.NewInvalidInput("certificate.IssueDeviceIdentity", "identity type is required")
	}
	if req.DeviceID == "" {
		return nil, sdkerrors.NewInvalidInput("certificate.IssueDeviceIdentity", "device ID is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("certificate.IssueDeviceIdentity", "context is required")
	}
	resp, err := c.t.Request(ctx, certExtServiceName, "IssueDeviceIdentity", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("certificate.IssueDeviceIdentity", "nil response from server")
	}
	out, ok := resp.(*DeviceIdentityResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("certificate.IssueDeviceIdentity", "invalid response type")
	}
	return out, nil
}

func newCertificateExtensions(t transport.Transport) CertificateExtensions {
	return &certExtImpl{t: t}
}

// fido2TransportClient is the transport-backed FIDO2Client. Each verb is a
// single request on the quicbaas.FIDO2 service carrying the xkms wire
// payload verbatim; the concrete transport bridges it to the per-tenant
// xkms relying-party service on the server.
type fido2TransportClient struct {
	t transport.Transport
}

// BeginRegistration starts a WebAuthn registration ceremony.
func (c *fido2TransportClient) BeginRegistration(ctx context.Context, req *FIDO2BeginRegistrationRequest) (*FIDO2BeginRegistrationResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginRegistration", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginRegistration", "request is required")
	}
	if req.Username == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginRegistration", "username is required")
	}
	resp, err := c.t.Request(ctx, fido2ServiceName, "BeginRegistration", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("fido2.BeginRegistration", "nil response from server")
	}
	out, ok := resp.(*FIDO2BeginRegistrationResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("fido2.BeginRegistration", "invalid response type")
	}
	return out, nil
}

// FinishRegistration completes a WebAuthn registration ceremony.
func (c *fido2TransportClient) FinishRegistration(ctx context.Context, req *FIDO2FinishRegistrationRequest) (*FIDO2FinishRegistrationResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishRegistration", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishRegistration", "request is required")
	}
	if req.SessionID == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishRegistration", "session_id is required")
	}
	if req.AttestationJSON == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishRegistration", "attestation_json is required")
	}
	resp, err := c.t.Request(ctx, fido2ServiceName, "FinishRegistration", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("fido2.FinishRegistration", "nil response from server")
	}
	out, ok := resp.(*FIDO2FinishRegistrationResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("fido2.FinishRegistration", "invalid response type")
	}
	return out, nil
}

// BeginAuthentication starts a WebAuthn authentication ceremony.
func (c *fido2TransportClient) BeginAuthentication(ctx context.Context, req *FIDO2BeginAuthenticationRequest) (*FIDO2BeginAuthenticationResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginAuthentication", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginAuthentication", "request is required")
	}
	if req.Username == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.BeginAuthentication", "username is required")
	}
	resp, err := c.t.Request(ctx, fido2ServiceName, "BeginAuthentication", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("fido2.BeginAuthentication", "nil response from server")
	}
	out, ok := resp.(*FIDO2BeginAuthenticationResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("fido2.BeginAuthentication", "invalid response type")
	}
	return out, nil
}

// FinishAuthentication completes a WebAuthn authentication ceremony.
func (c *fido2TransportClient) FinishAuthentication(ctx context.Context, req *FIDO2FinishAuthenticationRequest) (*FIDO2FinishAuthenticationResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishAuthentication", "context is required")
	}
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishAuthentication", "request is required")
	}
	if req.SessionID == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishAuthentication", "session_id is required")
	}
	if req.AssertionJSON == "" {
		return nil, sdkerrors.NewInvalidInput("fido2.FinishAuthentication", "assertion_json is required")
	}
	resp, err := c.t.Request(ctx, fido2ServiceName, "FinishAuthentication", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("fido2.FinishAuthentication", "nil response from server")
	}
	out, ok := resp.(*FIDO2FinishAuthenticationResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("fido2.FinishAuthentication", "invalid response type")
	}
	return out, nil
}

// pivTransportClient is the transport-backed XKMSPIVService. Each verb is a
// single request on the quicbaas.PIV service carrying the xkms wire payload
// verbatim; the concrete transport bridges it to the composed xkms PIV
// service (backend-scoped slots: file / pkcs11 / tpm2) on the server.
type pivTransportClient struct {
	t transport.Transport
}

// pivRequest performs one quicbaas.PIV transport round-trip after common
// nil-ctx / nil-req validation.
func (c *pivTransportClient) pivRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("piv."+method, "context is required")
	}
	return c.t.Request(ctx, pivServiceName, method, req)
}

// ListPIVSlots returns the status of all PIV slots in the requested backend.
func (c *pivTransportClient) ListPIVSlots(ctx context.Context, req *PIVListSlotsRequest) (*PIVListSlotsResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("piv.ListPIVSlots", "request is required")
	}
	resp, err := c.pivRequest(ctx, "ListPIVSlots", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return &PIVListSlotsResponse{}, nil
	}
	out, ok := resp.(*PIVListSlotsResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("piv.ListPIVSlots", "invalid response type")
	}
	return out, nil
}

// GetPIVCertificate retrieves the certificate stored in a PIV slot.
func (c *pivTransportClient) GetPIVCertificate(ctx context.Context, req *PIVGetCertificateRequest) (*PIVGetCertificateResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("piv.GetPIVCertificate", "request is required")
	}
	if req.Slot == "" {
		return nil, sdkerrors.NewInvalidInput("piv.GetPIVCertificate", "slot is required")
	}
	resp, err := c.pivRequest(ctx, "GetPIVCertificate", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewNotFound("piv.GetPIVCertificate", "certificate not found")
	}
	out, ok := resp.(*PIVGetCertificateResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("piv.GetPIVCertificate", "invalid response type")
	}
	return out, nil
}

// StorePIVCertificate stores a certificate in a PIV slot.
func (c *pivTransportClient) StorePIVCertificate(ctx context.Context, req *PIVStoreCertificateRequest) error {
	if req == nil {
		return sdkerrors.NewInvalidInput("piv.StorePIVCertificate", "request is required")
	}
	if req.Slot == "" {
		return sdkerrors.NewInvalidInput("piv.StorePIVCertificate", "slot is required")
	}
	_, err := c.pivRequest(ctx, "StorePIVCertificate", req)
	return err
}

// DeletePIVCertificate removes the certificate from a PIV slot.
func (c *pivTransportClient) DeletePIVCertificate(ctx context.Context, req *PIVDeleteCertificateRequest) error {
	if req == nil {
		return sdkerrors.NewInvalidInput("piv.DeletePIVCertificate", "request is required")
	}
	if req.Slot == "" {
		return sdkerrors.NewInvalidInput("piv.DeletePIVCertificate", "slot is required")
	}
	_, err := c.pivRequest(ctx, "DeletePIVCertificate", req)
	return err
}

// GeneratePIVKey generates a new key pair in a PIV slot with a self-signed
// certificate.
func (c *pivTransportClient) GeneratePIVKey(ctx context.Context, req *PIVGenerateKeyRequest) (*PIVGenerateKeyResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("piv.GeneratePIVKey", "request is required")
	}
	if req.Slot == "" {
		return nil, sdkerrors.NewInvalidInput("piv.GeneratePIVKey", "slot is required")
	}
	resp, err := c.pivRequest(ctx, "GeneratePIVKey", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("piv.GeneratePIVKey", "nil response from server")
	}
	out, ok := resp.(*PIVGenerateKeyResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("piv.GeneratePIVKey", "invalid response type")
	}
	return out, nil
}

// ImportPIVCertificate imports a certificate into a PIV slot.
func (c *pivTransportClient) ImportPIVCertificate(ctx context.Context, req *PIVStoreCertificateRequest) error {
	if req == nil {
		return sdkerrors.NewInvalidInput("piv.ImportPIVCertificate", "request is required")
	}
	if req.Slot == "" {
		return sdkerrors.NewInvalidInput("piv.ImportPIVCertificate", "slot is required")
	}
	_, err := c.pivRequest(ctx, "ImportPIVCertificate", req)
	return err
}

// ExportPIVCertificate exports the certificate from a PIV slot.
func (c *pivTransportClient) ExportPIVCertificate(ctx context.Context, req *PIVGetCertificateRequest) (*PIVGetCertificateResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("piv.ExportPIVCertificate", "request is required")
	}
	if req.Slot == "" {
		return nil, sdkerrors.NewInvalidInput("piv.ExportPIVCertificate", "slot is required")
	}
	resp, err := c.pivRequest(ctx, "ExportPIVCertificate", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewNotFound("piv.ExportPIVCertificate", "certificate not found")
	}
	out, ok := resp.(*PIVGetCertificateResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("piv.ExportPIVCertificate", "invalid response type")
	}
	return out, nil
}

// GeneratePIVCSR generates a certificate signing request for a PIV slot key.
func (c *pivTransportClient) GeneratePIVCSR(ctx context.Context, req *PIVGenerateCSRRequest) (*PIVGenerateCSRResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("piv.GeneratePIVCSR", "request is required")
	}
	if req.Slot == "" {
		return nil, sdkerrors.NewInvalidInput("piv.GeneratePIVCSR", "slot is required")
	}
	resp, err := c.pivRequest(ctx, "GeneratePIVCSR", req)
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, sdkerrors.NewInternal("piv.GeneratePIVCSR", "nil response from server")
	}
	out, ok := resp.(*PIVGenerateCSRResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("piv.GeneratePIVCSR", "invalid response type")
	}
	return out, nil
}

// authFactorsImpl implements AuthFactorsClient.
//
// User-login factor enrollment (Add / List / Remove / Rotate / Relabel) is
// routed to the qrdb user-side auth-factor store via the qrdb client's
// UserAuthFactors() surface — the SAME uint64-keyed UserAuthFactor store the
// unified login ceremony resolves against (go-qrdb LoginResolver +
// UserAuthFactorServicer.LookupByCredentialKey). This closes the split-store
// gap where enrollment wrote to the string-subject ceremony registry while
// login read the uint64-keyed user store, so an enrolled factor could never
// be resolved at login time.
//
// The Credential bytes written for each type are byte-identical to the
// lookup key the login proof reduces to, so the store's (type, credential)
// index matches what login submits:
//
//	mtls : SHA-256(certificate DER)  — equals proveCanonicalMTLS' fingerprint
//	piv  : SHA-256(slot certificate) — equals proveCanonicalPIV's cert hash
//	fido2: WebAuthn credentialID     — equals proveCanonicalFIDO2's lookup key
//	                                   and WebAuthnFactorVerifier's compare key
//
// This path is auth-critical and fails closed: when the credential lookup
// key cannot be derived for the requested type, enrollment is rejected
// rather than writing a factor login can never resolve.
//
// The legacy string-subject ceremony registry (qrdb AuthFactorClient) is left
// untouched for non-login custodian/ceremony enrolment; this surface no
// longer routes there.
//
// Login() (challenge / response token mint) is unchanged and still routes
// over the quicbaas transport to the server login handler.
type authFactorsImpl struct {
	c *client
	t transport.Transport
}

// authFactorMethodParam reads a string value from the request's MethodParams.
func authFactorMethodParam(req *AuthFactorEnrollRequest, key string) string {
	if req == nil || req.MethodParams == nil {
		return ""
	}
	if v, ok := req.MethodParams[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// authFactorCredentialFor derives the canonical credential lookup-key bytes
// the qrdb user-side store must index for the given factor type. The bytes
// MUST equal what the login proof reduces to so login resolution matches.
//
//	mtls / piv: MethodParams["credential_sha256"] is the hex SHA-256 of the
//	            certificate DER; decode it to the raw 32-byte key.
//	fido2     : MethodParams["credential_id"] is the WebAuthn credentialID
//	            (base64url) — stored as its raw string bytes, matching both
//	            proveCanonicalFIDO2 (lookup key = []byte(credentialID)) and
//	            WebAuthnFactorVerifier.Verify (compare credential_id ==
//	            string(factor.Credential)).
func authFactorCredentialFor(factorType string, req *AuthFactorEnrollRequest) ([]byte, error) {
	switch factorType {
	case "mtls", "piv":
		hexKey := authFactorMethodParam(req, "credential_sha256")
		if hexKey == "" {
			return nil, sdkerrors.NewInvalidInput("authfactors.Add",
				factorType+" enrollment requires MethodParams[\"credential_sha256\"] (hex SHA-256 of the certificate)")
		}
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput, "authfactors.Add",
				"credential_sha256 is not valid hex")
		}
		if len(raw) == 0 {
			return nil, sdkerrors.NewInvalidInput("authfactors.Add", "credential_sha256 decoded to empty bytes")
		}
		return raw, nil
	case "fido2":
		credID := authFactorMethodParam(req, "credential_id")
		if credID == "" {
			return nil, sdkerrors.NewInvalidInput("authfactors.Add",
				"fido2 enrollment requires MethodParams[\"credential_id\"] (the WebAuthn credentialID) so login can resolve the factor")
		}
		return []byte(credID), nil
	default:
		return nil, sdkerrors.NewInvalidInput("authfactors.Add", "unsupported factor type: "+factorType)
	}
}

// authFactorSPKICredentialFor derives the optional renewal-stable SPKI lookup
// credential for the given factor type.
//
//	mtls / piv: MethodParams["spki_sha256"] is the hex SHA-256 of the
//	            certificate's SubjectPublicKeyInfo — the raw-byte form of
//	            go-qrdb pkg/auth.SPKICredentialKey(cert). When present the
//	            qrdb store indexes it alongside the SHA-256(cert) credential
//	            so login survives certificate renewal with the same keypair.
//	fido2     : no SPKI concept; always nil.
//
// The parameter is optional: factors enrolled without it simply resolve
// through the legacy credential key only.
func authFactorSPKICredentialFor(factorType string, req *AuthFactorEnrollRequest) ([]byte, error) {
	switch factorType {
	case "mtls", "piv":
		hexKey := authFactorMethodParam(req, "spki_sha256")
		if hexKey == "" {
			return nil, nil
		}
		raw, err := hex.DecodeString(hexKey)
		if err != nil {
			return nil, sdkerrors.Wrap(err, sdkerrors.CodeInvalidInput, "authfactors.Add",
				"spki_sha256 is not valid hex")
		}
		if len(raw) == 0 {
			return nil, sdkerrors.NewInvalidInput("authfactors.Add", "spki_sha256 decoded to empty bytes")
		}
		return raw, nil
	default:
		return nil, nil
	}
}

// authFactorMetadata projects the non-credential MethodParams entries into a
// string metadata map for the stored factor. The credential lookup key is
// kept narrow (in Credential); descriptive context (cert CN, serial, reader,
// slot, AAGUID, device id) is retained in Metadata.
func authFactorMetadata(req *AuthFactorEnrollRequest) map[string]string {
	if req == nil || req.MethodParams == nil {
		return nil
	}
	out := make(map[string]string, len(req.MethodParams))
	for k, v := range req.MethodParams {
		if k == "credential_sha256" || k == "spki_sha256" || k == "credential_id" || k == "label" {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// factorMetadataToAny widens a string metadata map to map[string]any for the
// response carrier.
func factorMetadataToAny(in map[string]string) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// userFactors returns the qrdb user-side N-factor client, backed by the SAME
// UserAuthFactor store the login ceremony resolves against. Returns a typed
// configuration error when the transport does not expose it.
func (c *authFactorsImpl) userFactors() (*QRDBClient, qrdbtransport.UserAuthFactorClient, error) {
	qrdb, err := c.c.qrdbClient()
	if err != nil {
		return nil, nil, err
	}
	uaf := qrdb.UserAuthFactors()
	if uaf == nil {
		return nil, nil, sdkerrors.NewUnavailable("authfactors",
			"qrdb user-side auth-factor surface is unavailable — the transport was built without a UserAuthFactor service")
	}
	return qrdb, uaf, nil
}

// resolveSubjectUserID resolves the request's Subject (a tenant-scoped
// username) to the qrdb uint64 user id via the qrdb user store — the same
// store the login resolver's UserLookup reads. Enrolling against this id
// guarantees the factor binds to the principal login will resolve.
func resolveSubjectUserID(ctx context.Context, qrdb *QRDBClient, tenantID, subject string) (uint64, error) {
	users := qrdb.Users()
	if users == nil {
		return 0, sdkerrors.NewUnavailable("authfactors", "qrdb user surface is unavailable")
	}
	u, err := users.GetUserByUsername(ctx, tenantID, subject)
	if err != nil {
		return 0, err
	}
	if u == nil || u.ID == 0 {
		return 0, sdkerrors.NewNotFound("authfactors", "user not found for subject: "+subject)
	}
	return u.ID, nil
}

func (c *authFactorsImpl) Add(ctx context.Context, req *AuthFactorEnrollRequest) (*AuthFactorEnrollResponse, error) {
	if req == nil {
		return nil, sdkerrors.NewInvalidInput("authfactors.Add", "request is required")
	}
	if req.FactorName == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.Add", "factor name is required")
	}
	if req.Subject == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.Add", "subject is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("authfactors.Add", "context is required")
	}

	credential, err := authFactorCredentialFor(req.FactorName, req)
	if err != nil {
		return nil, err
	}
	spkiCredential, err := authFactorSPKICredentialFor(req.FactorName, req)
	if err != nil {
		return nil, err
	}

	qrdb, uaf, err := c.userFactors()
	if err != nil {
		return nil, err
	}
	userID, err := resolveSubjectUserID(ctx, qrdb, req.TenantID, req.Subject)
	if err != nil {
		return nil, err
	}

	addResp, err := uaf.AddAuthFactor(ctx, &qrdbtransport.AuthFactorAddRequest{
		UserID:         userID,
		Type:           req.FactorName,
		Label:          authFactorMethodParam(req, "label"),
		Credential:     credential,
		SPKICredential: spkiCredential,
		Metadata:       authFactorMetadata(req),
		CreatedBy:      "self",
	})
	if err != nil {
		return nil, err
	}

	resp := &AuthFactorEnrollResponse{
		FactorName: req.FactorName,
		UserID:     req.Subject,
		TenantID:   req.TenantID,
	}
	resp.EnrollmentID = addResp.Factor.ID
	resp.EnrolledAt = addResp.Factor.RegisteredAt
	resp.Metadata = factorMetadataToAny(addResp.Factor.Metadata)
	return resp, nil
}

func (c *authFactorsImpl) List(ctx context.Context, userID string) (*AuthFactorListResponse, error) {
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("authfactors.List", "context is required")
	}
	if userID == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.List", "subject (username) is required")
	}

	qrdb, uaf, err := c.userFactors()
	if err != nil {
		return nil, err
	}
	uid, err := resolveSubjectUserID(ctx, qrdb, "", userID)
	if err != nil {
		return nil, err
	}
	listResp, err := uaf.ListAuthFactors(ctx, &qrdbtransport.AuthFactorListRequest{UserID: uid})
	if err != nil {
		return nil, err
	}
	out := &AuthFactorListResponse{
		Enrollments: make([]AuthFactorEntry, 0, len(listResp.Factors)),
		Total:       len(listResp.Factors),
	}
	for i := range listResp.Factors {
		f := &listResp.Factors[i]
		out.Enrollments = append(out.Enrollments, AuthFactorEntry{
			EnrollmentID: f.ID,
			FactorName:   f.Type,
			UserID:       userID,
			Metadata:     factorMetadataToAny(f.Metadata),
			EnrolledAt:   f.RegisteredAt,
			RevokedAt:    f.RevokedAt,
		})
	}
	return out, nil
}

func (c *authFactorsImpl) Remove(ctx context.Context, factorID string, force bool) error {
	if factorID == "" {
		return sdkerrors.NewInvalidInput("authfactors.Remove", "factor ID is required")
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("authfactors.Remove", "context is required")
	}
	_, uaf, err := c.userFactors()
	if err != nil {
		return err
	}
	_, err = uaf.RemoveAuthFactor(ctx, &qrdbtransport.AuthFactorRemoveRequest{
		FactorID: factorID,
		Force:    force,
	})
	return err
}

func (c *authFactorsImpl) Rotate(ctx context.Context, oldID string, newFactor *AuthFactorEnrollRequest) (*AuthFactorEnrollResponse, error) {
	if oldID == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.Rotate", "old factor ID is required")
	}
	if newFactor == nil {
		return nil, sdkerrors.NewInvalidInput("authfactors.Rotate", "new factor is required")
	}
	if newFactor.FactorName == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.Rotate", "new factor name is required")
	}
	if newFactor.Subject == "" {
		return nil, sdkerrors.NewInvalidInput("authfactors.Rotate", "new factor subject is required")
	}
	if ctx == nil {
		return nil, sdkerrors.NewInvalidInput("authfactors.Rotate", "context is required")
	}

	credential, err := authFactorCredentialFor(newFactor.FactorName, newFactor)
	if err != nil {
		return nil, err
	}
	spkiCredential, err := authFactorSPKICredentialFor(newFactor.FactorName, newFactor)
	if err != nil {
		return nil, err
	}
	qrdb, uaf, err := c.userFactors()
	if err != nil {
		return nil, err
	}
	userID, err := resolveSubjectUserID(ctx, qrdb, newFactor.TenantID, newFactor.Subject)
	if err != nil {
		return nil, err
	}

	rotateResp, err := uaf.RotateAuthFactor(ctx, &qrdbtransport.AuthFactorRotateRequest{
		OldFactorID: oldID,
		NewFactor: qrdbtransport.AuthFactorAddRequest{
			UserID:         userID,
			Type:           newFactor.FactorName,
			Label:          authFactorMethodParam(newFactor, "label"),
			Credential:     credential,
			SPKICredential: spkiCredential,
			Metadata:       authFactorMetadata(newFactor),
			CreatedBy:      "self",
		},
	})
	if err != nil {
		return nil, err
	}

	resp := &AuthFactorEnrollResponse{
		FactorName: newFactor.FactorName,
		UserID:     newFactor.Subject,
		TenantID:   newFactor.TenantID,
	}
	resp.EnrollmentID = rotateResp.NewFactor.ID
	resp.EnrolledAt = rotateResp.NewFactor.RegisteredAt
	resp.Metadata = factorMetadataToAny(rotateResp.NewFactor.Metadata)
	return resp, nil
}

func (c *authFactorsImpl) Relabel(ctx context.Context, factorID, label string) error {
	if factorID == "" {
		return sdkerrors.NewInvalidInput("authfactors.Relabel", "factor ID is required")
	}
	if label == "" {
		return sdkerrors.NewInvalidInput("authfactors.Relabel", "label is required")
	}
	if ctx == nil {
		return sdkerrors.NewInvalidInput("authfactors.Relabel", "context is required")
	}
	_, uaf, err := c.userFactors()
	if err != nil {
		return err
	}
	_, err = uaf.RelabelAuthFactor(ctx, &qrdbtransport.AuthFactorRelabelRequest{
		FactorID: factorID,
		Label:    label,
	})
	return err
}

// =============================================================================
// AuthService Implementation (extended Auth surface)
// =============================================================================

// authServiceImpl is the concrete AuthService. It composes the existing
// AuthClient with the new factor-typed sub-services. AuthClient methods are
// delegated to the underlying client.auth() so behavior is unchanged.
type authServiceImpl struct {
	AuthClient
	c       *client
	t       transport.Transport
	fido2   FIDO2Client
	piv     XKMSPIVService
	devices LocalDevices
	factors AuthFactorsClient
}

func newAuthService(c *client) AuthService {
	t := c.transport
	return &authServiceImpl{
		AuthClient: c.auth(),
		c:          c,
		t:          t,
		fido2:      &fido2TransportClient{t: t},
		piv:        &pivTransportClient{t: t},
		devices:    newLocalDevices(),
		factors:    &authFactorsImpl{c: c, t: t},
	}
}

func (s *authServiceImpl) FIDO2() FIDO2Client         { return s.fido2 }
func (s *authServiceImpl) PIV() XKMSPIVService        { return s.piv }
func (s *authServiceImpl) Devices() LocalDevices      { return s.devices }
func (s *authServiceImpl) Factors() AuthFactorsClient { return s.factors }

// BeginLogin routes the unified login ceremony to qrdb's LoginClient — the
// single canonical login backbone. The registered factor authenticator issues
// the method-specific challenge (WebAuthn RP options / PIV nonce); mTLS uses
// the handshake.
func (s *authServiceImpl) BeginLogin(ctx context.Context, req qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	// Route through the transport so BOTH embedded (in-process dispatch ->
	// LoginCeremony() facade -> node) and network (-> quicbaas server login
	// handler -> facade -> node) flow through one path. The qrdb login
	// ceremony is embedded-only, so a direct qrdbClient().Login() would work
	// only in-process; routing via the quicbaas transport reaches the server's
	// in-process node from any client.
	resp, err := s.t.Request(ctx, authServiceName, "BeginLogin", &req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*qrdbtransport.BeginLoginResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("auth.BeginLogin", "invalid response type from transport")
	}
	return out, nil
}

// FinishLogin routes proof submission through the quicbaas transport to the
// server login handler, which forwards to qrdb's LoginClient. The server
// derives the mTLS lookup key from the verified peer certificate; the
// embedded transport trusts the caller-supplied key in-process.
func (s *authServiceImpl) FinishLogin(ctx context.Context, req qrdbtransport.FinishLoginRequest) (*FinishLoginResponse, error) {
	resp, err := s.t.Request(ctx, authServiceName, "FinishLogin", &req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*FinishLoginResponse)
	if !ok {
		return nil, sdkerrors.NewInternal("auth.FinishLogin", "invalid response type from transport")
	}
	return out, nil
}

// =============================================================================
// SecurityService Extensions (ACME, Trust, AuthService, CertificateExtensions)
// =============================================================================

// extendedSecurityService composes the existing SecurityService with the
// M10 sub-services. The original SecurityService methods (Auth(), Seal(),
// KMS(), ...) are delegated to the underlying client; the new methods
// (ACME(), Trust(), CertificateExt()) are added on top.
type extendedSecurityService interface {
	SecurityService

	// ACME returns the ACMEService sub-interface.
	ACME() ACMEService

	// Trust returns the TrustService sub-interface (DANE / cross-signed
	// roots / AIA auto-import).
	Trust() TrustService

	// CertificateExt returns the certificate extension client
	// (CrossSign, Chain, IssueDeviceIdentity).
	CertificateExt() CertificateExtensions
}

// (No change to SecurityService interface itself; new methods are added
// via extendedSecurityService below. Consumers continue to call
// client.Security() and may upcast to ExtendedSecurityService when they
// need the M10 surface.)

// ExtendedSecurityService is the public name of the M10-enriched security
// service surface.
type ExtendedSecurityService = extendedSecurityService

// =============================================================================
// Test Constructors
// =============================================================================

// NewClientWithTransport constructs a Client backed by an arbitrary
// transport.Transport implementation. This constructor is intended for
// tests and embedded scenarios where a real network transport is not
// desired. The returned client owns the transport's lifecycle: Close()
// will close the supplied transport.
func NewClientWithTransport(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{transport: t, opts: defaultOptions()}
}
