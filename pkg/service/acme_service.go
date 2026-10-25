// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package service

import (
	"context"
	"crypto/rand"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	stderrors "errors"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"

	xkmsacme "github.com/jeremyhahn/go-xkms/sdk/go/acme"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// ACMEServicer is the service-layer surface for the RFC 8555 ACME subsystem
// that QuicBaaS exposes under Security().ACME(). It is a pure composition
// layer over the go-xkms ACME server (pkg/acme) — QuicBaaS does NOT
// reimplement ACME. The xkms ACME RestServicer (the complete RFC 8555 HTTP
// handler set) is the single source of truth; this servicer maps the
// server-side control verbs onto that surface and exposes the raw handler so
// transports can mount the standard ACME directory / order / finalize / cert
// / challenge endpoints.
//
// ACME is an HTTP-native protocol. There are NO gRPC/QUIC/MCP opcodes for
// ACME by design — the verbs below are reached over REST or the in-process
// embedded transport only.
//
// The protocol-CLIENT verbs (NewOrder / FinalizeOrder / Renew) are wired here
// for the BROKER-AS-CA model: QuicBaaS is itself the certificate authority, so
// these are NOT JWS-signed exchanges against an external ACME directory. Order
// creation returns a lightweight in-process descriptor, finalization signs the
// supplied CSR with the composed CA (identical to Issue minus the challenge
// arguments), and renewal resolves an existing issued certificate. See the SDK
// ACMEClientAPI for the public surface.
type ACMEServicer interface {
	// RegisterChallenge enables a challenge verifier on the server. name is
	// one of "http-01" or "dns-01". The xkms canonical verifier for the named
	// type is installed into the shared challenge map so the directory
	// advertises it and authorizations of that type are validated.
	RegisterChallenge(ctx context.Context, req *ACMERegisterChallengeRequest) (*ACMERegisterChallengeResponse, error)

	// Issue performs a server-side issuance of a certificate from a CSR using
	// the named challenge type. The challenge value identifies the authorized
	// resource. Issuance is performed by the composed CA — this is the
	// control-plane "issue now" path, distinct from the JWS-signed client
	// order flow.
	Issue(ctx context.Context, req *ACMEIssueRequest) (*ACMEIssueResponse, error)

	// NewOrder creates a lightweight in-process ACME order descriptor for the
	// broker-as-CA flow. QuicBaaS is itself the CA, so order creation does NOT
	// require a JWS-signed RFC 8555 account exchange: the returned order is a
	// pending descriptor whose Finalize URL carries a generated order ID. The
	// CSR supplied later to FinalizeOrder is the only thing issuance needs.
	NewOrder(ctx context.Context, req *ACMENewOrderRequest) (*ACMENewOrderResponse, error)

	// FinalizeOrder signs the supplied CSR with the composed CA and returns the
	// issued leaf certificate. It is the broker-as-CA equivalent of the RFC
	// 8555 finalize step (which internally calls the CA's SignCSR) and is
	// identical to Issue minus the challenge arguments.
	FinalizeOrder(ctx context.Context, req *ACMEFinalizeOrderRequest) (*ACMEFinalizeOrderResponse, error)

	// Renew resolves an existing issued certificate by its serial number and,
	// when requested, revokes it. Because the broker-as-CA does not hold the
	// subscriber's private key and the renew request carries no CSR, it cannot
	// re-sign a usable leaf; it confirms the certificate is one the CA issued
	// and reports that renewal requires a fresh CSR via NewOrder/FinalizeOrder.
	Renew(ctx context.Context, req *ACMERenewRequest) (*ACMERenewResponse, error)

	// DirectoryURL returns the externally reachable RFC 8555 directory URL
	// (BaseRESTURI + "/acme/directory") that ACME protocol clients connect to.
	DirectoryURL() string

	// RestServicer returns the underlying xkms ACME RFC 8555 handler set so
	// transports can mount the standard ACME HTTP endpoints. Never nil for a
	// constructed servicer.
	RestServicer() xkmsacme.ACMERestServicer
}

// ACMECA is the narrow capability the ACME servicer needs from the
// xkms-composed CA to perform server-side issuance (the Issue and
// FinalizeOrder verbs). The pkg/server caServiceAdapter satisfies it by
// delegating to the embedded TrustedCA. Keeping this surface minimal keeps the
// service layer transport-agnostic and trivially mockable.
type ACMECA interface {
	// SignCSRPEM signs a PEM-encoded CSR for tenantID, the authorized
	// issuance tenant the CA stamps into the certificate, and returns the
	// issued leaf certificate plus the CA chain, both PEM-encoded.
	SignCSRPEM(ctx context.Context, tenantID string, csrPEM []byte) (certPEM, chainPEM []byte, err error)
}

// ACMECertResolver is the narrow capability the Renew verb needs to confirm
// that a certificate identified by serial number was issued by the composed CA
// and to revoke it. It is satisfied by the xkms TrustedCA the ACME service is
// already constructed with (params.CA exposes CertStore / Revoke), so Renew
// requires no additional dependency.
type ACMECertResolver interface {
	// CertificateBySerial returns the PEM-encoded leaf certificate whose hex
	// serial number matches serialHex. It returns ErrACMECertNotFound when no
	// issued certificate matches.
	CertificateBySerial(serialHex string) (certPEM []byte, err error)

	// RevokeBySerial revokes the certificate identified by serialHex.
	RevokeBySerial(serialHex string, reason int) error
}

// =============================================================================
// Request / Response Types
// =============================================================================

// ACMERegisterChallengeRequest names the challenge type to enable.
type ACMERegisterChallengeRequest struct {
	Name string `json:"name"`
}

// ACMERegisterChallengeResponse reports the registration result.
type ACMERegisterChallengeResponse struct {
	Registered bool `json:"registered"`
}

// ACMEIssueRequest carries a CSR plus the named challenge to perform a
// server-side issuance.
type ACMEIssueRequest struct {
	CSRPEM         []byte `json:"csr_pem"`
	ChallengeType  string `json:"challenge_type"`
	ChallengeValue string `json:"challenge_value"`
}

// ACMEIssueResponse is the result of a server-side ACME Issue call.
type ACMEIssueResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	ChainPEM       []byte `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}

// ACMEOrderIdentifier is a single RFC 8555 order identifier (e.g. a DNS name).
type ACMEOrderIdentifier struct {
	Type  string `json:"type"`
	Value string `json:"value"`
}

// ACMENewOrderRequest carries the identifiers and the challenge family for a
// new broker-as-CA order.
type ACMENewOrderRequest struct {
	Identifiers   []ACMEOrderIdentifier `json:"identifiers"`
	ChallengeType string                `json:"challenge_type"`
}

// ACMENewOrderResponse is the lightweight in-process order descriptor. The
// Finalize and URL fields carry the generated order ID so FinalizeOrder is
// self-describing; the order is pending until a CSR is finalized.
type ACMENewOrderResponse struct {
	OrderID     uint64                `json:"order_id"`
	Status      string                `json:"status"`
	Identifiers []ACMEOrderIdentifier `json:"identifiers"`
	Finalize    string                `json:"finalize"`
	URL         string                `json:"url"`
}

// ACMEFinalizeOrderRequest finalizes an order by signing the supplied CSR. The
// order URL is advisory in the broker-as-CA model — issuance only needs the
// CSR — but it is carried for parity with the RFC 8555 finalize step.
type ACMEFinalizeOrderRequest struct {
	OrderURL string `json:"order_url"`
	CSRPEM   []byte `json:"csr_pem"`
}

// ACMEFinalizeOrderResponse carries the issued certificate, the CA chain, and
// the leaf serial number.
type ACMEFinalizeOrderResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	ChainPEM       []byte `json:"chain_pem"`
	SerialNumber   string `json:"serial_number"`
}

// ACMERenewRequest identifies the certificate to renew by its directory URL.
// In the broker-as-CA model the URL is the hex serial number the CA assigned
// at issuance (the same value Issue/FinalizeOrder return as SerialNumber); a
// "/acme/cert/<serial>" suffix is also accepted.
type ACMERenewRequest struct {
	CertURL string `json:"cert_url"`
	// Revoke, when true, revokes the resolved certificate after confirming it
	// was issued by the composed CA.
	Revoke bool `json:"revoke"`
}

// ACMERenewResponse reports the outcome of a renew attempt. CertificatePEM
// carries the resolved (existing) certificate; Renewable is false because the
// broker holds no subscriber key and the request carries no CSR.
type ACMERenewResponse struct {
	CertificatePEM []byte `json:"certificate_pem"`
	SerialNumber   string `json:"serial_number"`
	Revoked        bool   `json:"revoked"`
	Renewable      bool   `json:"renewable"`
}

// =============================================================================
// Typed errors
// =============================================================================

var (
	// ErrACMENilRequest is returned when a nil request is supplied.
	ErrACMENilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService", Err: errACMENilRequest}

	// ErrACMEEmptyChallengeName is returned when RegisterChallenge is called
	// with an empty challenge name.
	ErrACMEEmptyChallengeName = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.RegisterChallenge", Err: errACMEEmptyChallengeName}

	// ErrACMEUnknownChallenge is returned when RegisterChallenge names a
	// challenge type with no canonical xkms verifier.
	ErrACMEUnknownChallenge = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.RegisterChallenge", Err: errACMEUnknownChallenge}

	// ErrACMEEmptyCSR is returned when an issuance request carries no CSR.
	ErrACMEEmptyCSR = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.Issue", Err: errACMEEmptyCSR}

	// ErrACMEEmptyChallengeType is returned when Issue is called without a
	// challenge type.
	ErrACMEEmptyChallengeType = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.Issue", Err: errACMEEmptyChallengeType}

	// ErrACMECAMissing is returned when a server-side issuance is requested
	// but the servicer has no backing CA.
	ErrACMECAMissing = &errors.QBError{Code: errors.ErrUnavailable, Op: "ACMEService.Issue", Err: errACMECAMissing}

	// ErrACMENoIdentifiers is returned when NewOrder is called with no
	// identifiers.
	ErrACMENoIdentifiers = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.NewOrder", Err: errACMENoIdentifiers}

	// ErrACMEEmptyCertURL is returned when Renew is called without a cert URL.
	ErrACMEEmptyCertURL = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.Renew", Err: errACMEEmptyCertURL}

	// ErrACMECertNotFound is returned when Renew cannot resolve the certificate
	// identified by the supplied serial number.
	ErrACMECertNotFound = &errors.QBError{Code: errors.ErrNotFound, Op: "ACMEService.Renew", Err: errACMECertNotFound}

	// ErrACMERenewNeedsCSR is returned when Renew has resolved (and optionally
	// revoked) the existing certificate but cannot re-issue a usable leaf: the
	// broker-as-CA holds no subscriber private key and the renew request
	// carries no CSR. RFC 8555 renewal is a fresh order + finalize.
	ErrACMERenewNeedsCSR = &errors.QBError{Code: errors.ErrInvalidInput, Op: "ACMEService.Renew", Err: errACMERenewNeedsCSR}

	// ErrACMEResolverMissing is returned when Renew is requested but the
	// servicer has no certificate resolver (no backing CA).
	ErrACMEResolverMissing = &errors.QBError{Code: errors.ErrUnavailable, Op: "ACMEService.Renew", Err: errACMEResolverMissing}
)

var (
	errACMENilRequest         = stderrors.New("request must not be nil")
	errACMEEmptyChallengeName = stderrors.New("challenge name is required")
	errACMEUnknownChallenge   = stderrors.New("no canonical verifier for the named challenge type")
	errACMEEmptyCSR           = stderrors.New("csr is required")
	errACMEEmptyChallengeType = stderrors.New("challenge type is required")
	errACMECAMissing          = stderrors.New("acme service has no backing certificate authority")
	errACMENoIdentifiers      = stderrors.New("at least one identifier is required")
	errACMEEmptyCertURL       = stderrors.New("certificate url is required")
	errACMECertNotFound       = stderrors.New("no issued certificate matches the supplied serial number")
	errACMERenewNeedsCSR      = stderrors.New("renewal requires a fresh CSR via NewOrder/FinalizeOrder: the broker-as-CA holds no subscriber private key")
	errACMEResolverMissing    = stderrors.New("acme service has no certificate resolver")
)

// =============================================================================
// ACMEServiceParams + constructor
// =============================================================================

// ACMEServiceParams carries the dependencies required to compose the xkms
// ACME server. CA, StorageFactory and BaseRESTURI are mandatory; the CA
// issuance capability and a logger are optional.
type ACMEServiceParams struct {
	// BaseRESTURI is the externally reachable base URL the ACME directory
	// advertises (e.g. "https://baas.example.com"). The RFC 8555 routes are
	// mounted under "<BaseRESTURI>/acme".
	BaseRESTURI string

	// CA is the xkms TCG certificate authority the ACME handlers issue
	// through. It is the same TrustedCA that backs Security().Certificate().
	CA xkmsacme.CACertificateAuthority

	// StorageFactory persists ACME accounts / orders / authorizations. It is
	// built over the server's qrdb KVStore so ACME state shares the
	// configured datastore.
	StorageFactory xkmsacme.ACMEStorageFactory

	// IssueCA is the narrow CA capability used by the Issue and FinalizeOrder
	// verbs to perform a server-side CSR signing. Optional; when nil, those
	// verbs return ErrACMECAMissing.
	IssueCA ACMECA

	// CertResolver is the narrow capability used by the Renew verb to resolve
	// (and optionally revoke) an existing certificate by serial. Optional; when
	// nil, Renew returns ErrACMEResolverMissing.
	CertResolver ACMECertResolver

	// Challenges names the challenge types to enable at construction time.
	// When empty, http-01 is enabled by default.
	Challenges []string

	// AllowPrivateChallengeTargets lets the shared challenge-validation
	// client connect to reserved addresses (loopback, private, link-local,
	// metadata). go-xkms refuses them by default to keep a client-named
	// challenge target from reaching internal services (SSRF); a deployment
	// whose devices enroll from a private network sets it.
	AllowPrivateChallengeTargets bool

	// OrderPreSignHooks registers pre-sign extension hooks. For cluster ACME
	// enrollment it carries the SPIFFE node-identity URI-SAN hook so enrolling
	// nodes' transport certificates are stamped with their SPIFFE identity. Nil
	// leaves ordinary issuance untouched.
	OrderPreSignHooks *xkmsacme.ACMEOrderPreSignHookRegistry

	// ClusterCACN, when set, routes TCG-CSR-IDEVID (enroll-01) issuance through
	// the named dedicated cluster intermediate instead of the CA's default
	// issuing identity. Empty preserves default issuance.
	ClusterCACN string

	// Logger is the structured logger. When nil, a discard logger is used.
	Logger *slog.Logger
}

// acmeService is the production ACMEServicer backed by the composed xkms ACME
// RestService.
type acmeService struct {
	rest         xkmsacme.ACMERestServicer
	directoryURL string
	baseURI      string
	issueCA      ACMECA
	certResolver ACMECertResolver
	mu           sync.Mutex
	challenge    map[string]xkmsacme.ACMEChallengeVerifierFunc
	attesting    map[string]xkmsacme.ACMEAttestingVerifier

	// canonical is every plain challenge verifier this server can register,
	// all bound to the one challenge-validation client built at construction.
	canonical map[string]xkmsacme.ACMEChallengeVerifierFunc

	// canonicalAttesting is every attestation challenge verifier this server
	// can register, bound to the same client.
	canonicalAttesting map[string]xkmsacme.ACMEAttestingVerifier
}

// canonicalChallengeVerifiers maps challenge names to their canonical xkms
// verifier functions, every one bound to client: the shared, SSRF-safe
// challenge-validation client go-xkms requires for any verifier that contacts
// a client-named target (http-01). Map-based dispatch keeps challenge
// registration O(1); which challenges are actually offered is selected
// per-deployment via ACMEServiceParams.Challenges.
func canonicalChallengeVerifiers(client *xkmsacme.ACMEChallengeClient) map[string]xkmsacme.ACMEChallengeVerifierFunc {
	return map[string]xkmsacme.ACMEChallengeVerifierFunc{
		xkmsacme.ACMEChallengeTypeHTTP01: func(_ *net.Resolver, _ xkmsacme.ACMECertificateAuthority,
			authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
			return xkmsacme.ACMEVerifyHTTP01(client, authzValue, challengePort, challengeToken, expectedKeyAuth)
		},
		xkmsacme.ACMEChallengeTypeDNS01: func(resolver *net.Resolver, _ xkmsacme.ACMECertificateAuthority,
			authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
			return xkmsacme.ACMEVerifyDNS01(resolver, authzValue, challengePort, challengeToken, expectedKeyAuth)
		},
	}
}

// canonicalAttestingVerifiers maps the TPM attestation challenges
// (enroll-01/endorse-01) to the shared xkms SDK attesting verifiers, bound to
// client, so quicbaas enables attestation identically to xkmsd. go-xkms v0.55
// refuses an attestation challenge served by a plain ChallengeVerifierFunc,
// because only an attesting verifier reports the identity the device attested
// (go-xkms#828). device-attest-01 is not offered: it needs a trust policy over
// the evidence, which this server is not configured with.
func canonicalAttestingVerifiers(client *xkmsacme.ACMEChallengeClient) map[string]xkmsacme.ACMEAttestingVerifier {
	return xkmsacme.ACMEAttestingVerifiers(client, true, true, nil)
}

// NewACMEService composes the xkms ACME RFC 8555 server from params and
// returns an ACMEServicer. Returns nil, nil when CA, StorageFactory or the
// base URI are absent so the service registry can treat ACME as "not
// configured" rather than failing the whole build. Returns a typed error when
// the xkms RestService cannot be constructed from otherwise-complete params.
func NewACMEService(params *ACMEServiceParams) (ACMEServicer, error) {
	if params == nil || params.CA == nil || params.StorageFactory == nil || params.BaseRESTURI == "" {
		return nil, nil
	}

	logger := params.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
	}

	client := xkmsacme.NewACMEChallengeClient(xkmsacme.ACMEChallengeClientConfig{
		AllowPrivateTargets: params.AllowPrivateChallengeTargets,
	})
	canonical := canonicalChallengeVerifiers(client)
	canonicalAttesting := canonicalAttestingVerifiers(client)
	challengeMap := make(map[string]xkmsacme.ACMEChallengeVerifierFunc)
	attestingMap := make(map[string]xkmsacme.ACMEAttestingVerifier)
	names := params.Challenges
	if len(names) == 0 {
		names = []string{xkmsacme.ACMEChallengeTypeHTTP01}
	}
	for _, name := range names {
		if verifier, ok := canonical[name]; ok {
			challengeMap[name] = verifier
		}
		if verifier, ok := canonicalAttesting[name]; ok {
			attestingMap[name] = verifier
		}
	}

	rest, err := xkmsacme.NewACMERestService(&xkmsacme.ACMERestServiceParams{
		AttestingVerifiers: attestingMap,
		BaseRESTURI:        params.BaseRESTURI,
		CA:                 params.CA,
		ChallengeMap:       challengeMap,
		ClusterCACN:        params.ClusterCACN,
		Logger:             logger,
		OrderPreSignHooks:  params.OrderPreSignHooks,
		StorageFactory:     params.StorageFactory,
	})
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "NewACMEService", Err: err}
	}

	return &acmeService{
		rest:               rest,
		directoryURL:       params.BaseRESTURI + "/acme/directory",
		baseURI:            params.BaseRESTURI,
		issueCA:            params.IssueCA,
		certResolver:       params.CertResolver,
		challenge:          challengeMap,
		attesting:          attestingMap,
		canonical:          canonical,
		canonicalAttesting: canonicalAttesting,
	}, nil
}

// RestServicer returns the composed xkms RFC 8555 handler set.
func (s *acmeService) RestServicer() xkmsacme.ACMERestServicer { return s.rest }

// DirectoryURL returns the RFC 8555 directory URL ACME clients connect to.
func (s *acmeService) DirectoryURL() string { return s.directoryURL }

// RegisterChallenge installs the canonical xkms verifier for the named
// challenge type into the shared challenge map.
func (s *acmeService) RegisterChallenge(ctx context.Context, req *ACMERegisterChallengeRequest) (*ACMERegisterChallengeResponse, error) {
	if req == nil {
		return nil, ErrACMENilRequest
	}
	if req.Name == "" {
		return nil, ErrACMEEmptyChallengeName
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.RegisterChallenge", Err: err}
	}

	verifier, plain := s.canonical[req.Name]
	attestingVerifier, attesting := s.canonicalAttesting[req.Name]
	if !plain && !attesting {
		return nil, ErrACMEUnknownChallenge
	}

	s.mu.Lock()
	if plain {
		s.challenge[req.Name] = verifier
	} else {
		s.attesting[req.Name] = attestingVerifier
	}
	s.mu.Unlock()

	return &ACMERegisterChallengeResponse{Registered: true}, nil
}

// Issue performs a server-side issuance from a CSR. The named challenge type
// must be supplied; issuance is delegated to the composed CA.
func (s *acmeService) Issue(ctx context.Context, req *ACMEIssueRequest) (*ACMEIssueResponse, error) {
	if req == nil {
		return nil, ErrACMENilRequest
	}
	if len(req.CSRPEM) == 0 {
		return nil, ErrACMEEmptyCSR
	}
	if req.ChallengeType == "" {
		return nil, ErrACMEEmptyChallengeType
	}
	if s.issueCA == nil {
		return nil, ErrACMECAMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Issue", Err: err}
	}

	// go-xkms stamps the issuance tenant into the certificate, so it comes
	// from the caller's authenticated context, never from the CSR.
	tenantID, err := callerTenant(ctx)
	if err != nil {
		return nil, err
	}
	certPEM, chainPEM, err := s.issueCA.SignCSRPEM(ctx, tenantID, req.CSRPEM)
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Issue", Err: err}
	}

	return &ACMEIssueResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chainPEM,
		SerialNumber:   serialFromCertPEM(certPEM),
	}, nil
}

// NewOrder creates a lightweight in-process order descriptor for the
// broker-as-CA flow. There is no JWS-signed RFC 8555 account exchange: the
// returned order is pending and its Finalize URL carries a freshly generated
// order ID. FinalizeOrder only needs the CSR the caller supplies later, so no
// account/order persistence is forced here.
func (s *acmeService) NewOrder(ctx context.Context, req *ACMENewOrderRequest) (*ACMENewOrderResponse, error) {
	if req == nil {
		return nil, ErrACMENilRequest
	}
	if len(req.Identifiers) == 0 {
		return nil, ErrACMENoIdentifiers
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.NewOrder", Err: err}
	}

	orderID, err := newOrderID()
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.NewOrder", Err: err}
	}

	idStr := strconv.FormatUint(orderID, 10)
	orderURL := s.baseURI + "/acme/order/" + idStr
	return &ACMENewOrderResponse{
		OrderID:     orderID,
		Status:      string(xkmsacme.OrderStatusPending),
		Identifiers: req.Identifiers,
		Finalize:    orderURL + "/finalize",
		URL:         orderURL,
	}, nil
}

// FinalizeOrder signs the supplied CSR with the composed CA. It is identical to
// Issue minus the challenge arguments; the order URL is advisory in the
// broker-as-CA model.
func (s *acmeService) FinalizeOrder(ctx context.Context, req *ACMEFinalizeOrderRequest) (*ACMEFinalizeOrderResponse, error) {
	if req == nil {
		return nil, ErrACMENilRequest
	}
	if len(req.CSRPEM) == 0 {
		return nil, ErrACMEEmptyCSR
	}
	if s.issueCA == nil {
		return nil, ErrACMECAMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.FinalizeOrder", Err: err}
	}

	// go-xkms stamps the issuance tenant into the certificate, so it comes
	// from the caller's authenticated context, never from the CSR.
	tenantID, err := callerTenant(ctx)
	if err != nil {
		return nil, err
	}
	certPEM, chainPEM, err := s.issueCA.SignCSRPEM(ctx, tenantID, req.CSRPEM)
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.FinalizeOrder", Err: err}
	}

	return &ACMEFinalizeOrderResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chainPEM,
		SerialNumber:   serialFromCertPEM(certPEM),
	}, nil
}

// Renew resolves an existing certificate by serial number and, when requested,
// revokes it. The broker-as-CA holds no subscriber private key and the renew
// request carries no CSR, so a usable renewed leaf cannot be re-signed
// in-process; Renew confirms the certificate was issued by the composed CA and
// returns ErrACMERenewNeedsCSR directing the caller to NewOrder/FinalizeOrder.
func (s *acmeService) Renew(ctx context.Context, req *ACMERenewRequest) (*ACMERenewResponse, error) {
	if req == nil {
		return nil, ErrACMENilRequest
	}
	if req.CertURL == "" {
		return nil, ErrACMEEmptyCertURL
	}
	if s.certResolver == nil {
		return nil, ErrACMEResolverMissing
	}
	if err := ctx.Err(); err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Renew", Err: err}
	}

	serialHex := serialFromCertURL(req.CertURL)
	certPEM, err := s.certResolver.CertificateBySerial(serialHex)
	if err != nil {
		// coded-sentinel-ok: the CLASS is the intent. The resolver is an
		// interface; any not-found it reports means this serial names no
		// certificate, which is the answer ACME owes the client. Everything
		// else becomes Internal below.
		if stderrors.Is(err, ErrACMECertNotFound) {
			return nil, ErrACMECertNotFound
		}
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Renew", Err: err}
	}

	revoked := false
	if req.Revoke {
		// reason 0 == unspecified (RFC 5280 CRLReason).
		if err := s.certResolver.RevokeBySerial(serialHex, 0); err != nil {
			return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Renew", Err: err}
		}
		revoked = true
	}

	// The certificate was resolved (and optionally revoked) successfully, but
	// the broker cannot re-issue without a CSR. Surface the resolved cert plus
	// the precise limitation so callers can drive NewOrder/FinalizeOrder.
	return &ACMERenewResponse{
			CertificatePEM: certPEM,
			SerialNumber:   serialHex,
			Revoked:        revoked,
			Renewable:      false,
		},
		ErrACMERenewNeedsCSR
}

// serialFromCertURL extracts the hex serial number from a renew CertURL. The
// broker-as-CA identifies a certificate by the hex serial it returned at
// issuance; the URL may be the bare serial or an "/acme/cert/<serial>" path.
func serialFromCertURL(certURL string) string {
	s := strings.TrimSpace(certURL)
	if idx := strings.LastIndex(s, "/"); idx >= 0 {
		s = s[idx+1:]
	}
	return strings.ToLower(s)
}

// newOrderID returns a random non-zero 64-bit order identifier. Order IDs are
// opaque in the broker-as-CA model — FinalizeOrder issues from the CSR, not
// from persisted order state — so a cryptographically random value is both
// sufficient and collision-resistant without a shared counter.
func newOrderID() (uint64, error) {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return 0, err
	}
	id := binary.BigEndian.Uint64(buf[:])
	if id == 0 {
		id = 1
	}
	return id, nil
}

// serialFromCertPEM extracts the hex serial number from a PEM-encoded leaf
// certificate. Returns an empty string when the PEM cannot be parsed.
func serialFromCertPEM(certPEM []byte) string {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return ""
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil || cert.SerialNumber == nil {
		return ""
	}
	return cert.SerialNumber.Text(16)
}
