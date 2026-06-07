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
	"crypto/x509"
	"encoding/pem"
	stderrors "errors"
	"io"
	"log/slog"
	"net"
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
// The protocol-CLIENT verbs (NewOrder / FinalizeOrder / Renew) are NOT
// service-layer operations: RFC 8555 order creation and finalization are
// JWS-signed exchanges that require a per-account signing key, a registered
// account, replay nonces, and persistent account/order storage. They are
// performed by an ACME protocol client (go-xkms/pkg/acme/client) speaking to
// the directory this servicer mounts — not by a server-side RPC. See the SDK
// ACMEClientAPI for those.
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

	// DirectoryURL returns the externally reachable RFC 8555 directory URL
	// (BaseRESTURI + "/acme/directory") that ACME protocol clients connect to.
	DirectoryURL() string

	// RestServicer returns the underlying xkms ACME RFC 8555 handler set so
	// transports can mount the standard ACME HTTP endpoints. Never nil for a
	// constructed servicer.
	RestServicer() xkmsacme.ACMERestServicer
}

// ACMECA is the narrow capability the ACME servicer needs from the
// xkms-composed CA to perform server-side issuance (the Issue verb). The
// pkg/server caServiceAdapter satisfies it by delegating to the embedded
// TrustedCA. Keeping this surface minimal keeps the service layer
// transport-agnostic and trivially mockable.
type ACMECA interface {
	// SignCSRPEM signs a PEM-encoded CSR and returns the issued leaf
	// certificate plus the CA chain, both PEM-encoded.
	SignCSRPEM(ctx context.Context, csrPEM []byte) (certPEM, chainPEM []byte, err error)
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
)

var (
	errACMENilRequest         = stderrors.New("request must not be nil")
	errACMEEmptyChallengeName = stderrors.New("challenge name is required")
	errACMEUnknownChallenge   = stderrors.New("no canonical verifier for the named challenge type")
	errACMEEmptyCSR           = stderrors.New("csr is required")
	errACMEEmptyChallengeType = stderrors.New("challenge type is required")
	errACMECAMissing          = stderrors.New("acme service has no backing certificate authority")
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

	// IssueCA is the narrow CA capability used by the Issue verb to perform a
	// server-side CSR signing. Optional; when nil, Issue returns
	// ErrACMECAMissing.
	IssueCA ACMECA

	// Challenges names the challenge types to enable at construction time.
	// When empty, http-01 is enabled by default.
	Challenges []string

	// Logger is the structured logger. When nil, a discard logger is used.
	Logger *slog.Logger
}

// acmeService is the production ACMEServicer backed by the composed xkms ACME
// RestService.
type acmeService struct {
	rest         xkmsacme.ACMERestServicer
	directoryURL string
	issueCA      ACMECA
	mu           sync.Mutex
	challenge    map[string]xkmsacme.ACMEChallengeVerifierFunc
}

// canonicalChallengeVerifiers maps challenge names to their canonical xkms
// verifier functions. Map-based dispatch keeps challenge registration O(1)
// and avoids a growing switch as new challenge families are added.
var canonicalChallengeVerifiers = map[string]xkmsacme.ACMEChallengeVerifierFunc{
	xkmsacme.ACMEChallengeTypeHTTP01: func(resolver *net.Resolver, _ xkmsacme.ACMECertificateAuthority,
		authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
		return xkmsacme.ACMEVerifyHTTP01(resolver, nil, authzValue, challengePort, challengeToken, expectedKeyAuth)
	},
	xkmsacme.ACMEChallengeTypeDNS01: func(resolver *net.Resolver, _ xkmsacme.ACMECertificateAuthority,
		authzValue, challengePort, challengeToken, expectedKeyAuth string) error {
		return xkmsacme.ACMEVerifyDNS01(resolver, nil, authzValue, challengePort, challengeToken, expectedKeyAuth)
	},
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

	challengeMap := make(map[string]xkmsacme.ACMEChallengeVerifierFunc)
	names := params.Challenges
	if len(names) == 0 {
		names = []string{xkmsacme.ACMEChallengeTypeHTTP01}
	}
	for _, name := range names {
		if verifier, ok := canonicalChallengeVerifiers[name]; ok {
			challengeMap[name] = verifier
		}
	}

	rest, err := xkmsacme.NewACMERestService(&xkmsacme.ACMERestServiceParams{
		BaseRESTURI:    params.BaseRESTURI,
		CA:             params.CA,
		ChallengeMap:   challengeMap,
		Logger:         logger,
		StorageFactory: params.StorageFactory,
	})
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "NewACMEService", Err: err}
	}

	return &acmeService{
		rest:         rest,
		directoryURL: params.BaseRESTURI + "/acme/directory",
		issueCA:      params.IssueCA,
		challenge:    challengeMap,
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

	verifier, ok := canonicalChallengeVerifiers[req.Name]
	if !ok {
		return nil, ErrACMEUnknownChallenge
	}

	s.mu.Lock()
	s.challenge[req.Name] = verifier
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

	certPEM, chainPEM, err := s.issueCA.SignCSRPEM(ctx, req.CSRPEM)
	if err != nil {
		return nil, &errors.QBError{Code: errors.ErrInternal, Op: "ACMEService.Issue", Err: err}
	}

	return &ACMEIssueResponse{
		CertificatePEM: certPEM,
		ChainPEM:       chainPEM,
		SerialNumber:   serialFromCertPEM(certPEM),
	}, nil
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
