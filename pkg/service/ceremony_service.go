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

package service

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// =============================================================================
// Ceremony Service — facade over go-qrdb's CeremonyClient.
// =============================================================================
//
// CeremonyServicer mirrors go-qrdb's pkg/service.CeremonyServicer surface
// and re-exports the same operations through the QuicBaaS service layer.
// The implementation is a thin facade over the SDK transport.CeremonyClient.
//
// Phase 9 wires the facade and its REST shim only; Phase 11 lifecycle
// wiring will install a non-nil CeremonyClient. Until then the service
// returns CeremonyServiceMissingError so handlers report 503 cleanly.
// =============================================================================

// CeremonyOfficer describes a single officer participating in the
// initialization ceremony.
type CeremonyOfficer struct {
	// Username is the unique identifier for the officer.
	Username string

	// CSRPEM is the PEM-encoded CSR submitted by the officer.
	CSRPEM string
}

// CeremonyInitRequest holds the parameters for initialising the ceremony
// at the service-layer boundary. Mirrors transport.CeremonyInitializeRequest.
type CeremonyInitRequest struct {
	// SOPin is the Security Officer PIN.
	SOPin string

	// UserPin is the User PIN.
	UserPin string

	// CredentialSealStrategy is the optional credential-seal strategy.
	CredentialSealStrategy string

	// Threshold is the M-of-N threshold (zero for single-admin mode).
	Threshold int

	// Officers are the officers participating in the ceremony.
	Officers []CeremonyOfficer

	// Hostname is the optional hostname for the issued TLS certificate.
	Hostname string

	// SANs is the optional list of Subject Alternative Names.
	SANs []string
}

// CeremonyInitInfo contains the output of a successful ceremony
// initialisation.
type CeremonyInitInfo struct {
	// SPKIPin is the base64-encoded SHA-256 hash of the CA certificate's SPKI.
	SPKIPin string

	// CACertPEM is the PEM-encoded CA certificate.
	CACertPEM []byte

	// TLSCertPEM is the PEM-encoded TLS certificate issued for the server.
	TLSCertPEM []byte

	// SOCertPEM is the PEM-encoded Security Officer certificate.
	SOCertPEM []byte

	// SOKeyPEM is the PEM-encoded Security Officer private key.
	SOKeyPEM []byte

	// State is the ceremony state after initialisation.
	State string

	// ShareCount is the number of Shamir shares generated.
	ShareCount int
}

// CeremonyChallenge contains the challenge nonce issued during the
// certificate claim handshake.
type CeremonyChallenge struct {
	// NonceHex is the hex-encoded random challenge.
	NonceHex string

	// ExpiresAt is the deadline by which the officer must respond.
	ExpiresAt time.Time
}

// CeremonyClaimCertResult contains the output of a successful certificate
// claim operation.
type CeremonyClaimCertResult struct {
	// CertPEM is the PEM-encoded officer certificate.
	CertPEM []byte

	// CACertPEM is the PEM-encoded CA certificate for trust chain
	// verification.
	CACertPEM []byte
}

// CeremonySignCSRResult contains the output of CSR signing.
type CeremonySignCSRResult struct {
	// CertPEM is the PEM-encoded certificate issued from the CSR.
	CertPEM []byte
}

// CeremonyStatusInfo describes the current state of the initialization
// ceremony.
type CeremonyStatusInfo struct {
	// State is the current ceremony phase.
	State string

	// SPKIPin is the base64-encoded SPKI hash. Empty when CA is not yet
	// initialised.
	SPKIPin string
}

// CeremonyEnrollExternalOfficerRequest is the service-layer request for
// enrolling an officer whose key material lives outside the daemon.
type CeremonyEnrollExternalOfficerRequest struct {
	// Username is the unique identifier for the officer.
	Username string

	// CertPEM is the PEM-encoded X.509 certificate. Mutually exclusive with
	// PubKeyPEM.
	CertPEM string

	// PubKeyPEM is the PEM-encoded SPKI public key. Mutually exclusive with
	// CertPEM.
	PubKeyPEM string

	// ShareIndex is the 1-based Shamir share index assigned to this officer.
	ShareIndex int

	// AuthFactor is the auth factor identifier ("mtls", "webauthn", "piv").
	AuthFactor string

	// ShareBytes is the optional plaintext share to seal to the officer's
	// public key. Empty when only the trust record is being persisted.
	ShareBytes []byte
}

// CeremonyEnrollExternalOfficerResponse is the service-layer response.
type CeremonyEnrollExternalOfficerResponse struct {
	// SealedShare is the base64-encoded sealed share envelope. Empty when
	// no ShareBytes were supplied.
	SealedShare string
}

// CeremonyServiceMissingError signals that the ceremony facade was
// constructed without an underlying go-qrdb CeremonyClient.
type CeremonyServiceMissingError struct{}

// Error returns the error message.
func (e *CeremonyServiceMissingError) Error() string {
	return "ceremony service not configured"
}

// CeremonyServicer exposes the initialization ceremony lifecycle through
// the QuicBaaS service facade.
type CeremonyServicer interface {
	// CeremonyInitialize starts the initialization ceremony.
	CeremonyInitialize(ctx context.Context, req *CeremonyInitRequest) (*CeremonyInitInfo, error)

	// CeremonyStatus returns the current ceremony state.
	CeremonyStatus(ctx context.Context) (*CeremonyStatusInfo, error)

	// CeremonyBeginClaimCert starts a certificate claim challenge.
	CeremonyBeginClaimCert(ctx context.Context, username string) (*CeremonyChallenge, error)

	// CeremonyCompleteClaimCert completes a certificate claim with a
	// signed nonce.
	CeremonyCompleteClaimCert(ctx context.Context, username, nonceHex, signatureBase64 string) (*CeremonyClaimCertResult, error)

	// CeremonyClaimShare retrieves the Shamir share for an officer.
	CeremonyClaimShare(ctx context.Context, username string) (string, error)

	// CeremonySignCSR signs a CSR during the ceremony.
	CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*CeremonySignCSRResult, error)

	// CeremonyEnrollExternalOfficer enrols an officer whose key material
	// lives outside the daemon.
	CeremonyEnrollExternalOfficer(ctx context.Context, req *CeremonyEnrollExternalOfficerRequest) (*CeremonyEnrollExternalOfficerResponse, error)
}

// CeremonyService implements CeremonyServicer by delegating to a
// transport.CeremonyClient. The client field is allowed to be nil during
// early boot; every method short-circuits with CeremonyServiceMissingError
// when the underlying transport is missing.
type CeremonyService struct {
	// client is the qrdb SDK CeremonyClient. May be nil before lifecycle
	// wiring completes.
	client transport.CeremonyClient
}

// Compile-time interface check.
var _ CeremonyServicer = (*CeremonyService)(nil)

// NewCeremonyService constructs a CeremonyService backed by the supplied
// qrdb CeremonyClient. Passing nil yields a service whose methods all
// return CeremonyServiceMissingError.
func NewCeremonyService(client transport.CeremonyClient) *CeremonyService {
	return &CeremonyService{client: client}
}

// CeremonyInitialize starts the initialization ceremony.
func (s *CeremonyService) CeremonyInitialize(ctx context.Context, req *CeremonyInitRequest) (*CeremonyInitInfo, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	if req == nil {
		req = &CeremonyInitRequest{}
	}
	officers := make([]transport.CeremonyOfficerRequest, 0, len(req.Officers))
	for _, o := range req.Officers {
		officers = append(officers, transport.CeremonyOfficerRequest{
			Username: o.Username,
			CSRPEM:   o.CSRPEM,
		})
	}
	resp, err := s.client.CeremonyInitialize(ctx, &transport.CeremonyInitializeRequest{
		SOPin:                  req.SOPin,
		UserPin:                req.UserPin,
		CredentialSealStrategy: req.CredentialSealStrategy,
		Threshold:              req.Threshold,
		Officers:               officers,
		Hostname:               req.Hostname,
		SANs:                   req.SANs,
	})
	if err != nil {
		return nil, err
	}
	return &CeremonyInitInfo{
		SPKIPin:    resp.SPKIPin,
		CACertPEM:  []byte(resp.CACertPEM),
		TLSCertPEM: []byte(resp.TLSCertPEM),
		SOCertPEM:  []byte(resp.SOCertPEM),
		SOKeyPEM:   []byte(resp.SOKeyPEM),
		State:      resp.State,
		ShareCount: resp.ShareCount,
	}, nil
}

// CeremonyStatus returns the current ceremony state.
func (s *CeremonyService) CeremonyStatus(ctx context.Context) (*CeremonyStatusInfo, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	resp, err := s.client.CeremonyStatus(ctx)
	if err != nil {
		return nil, err
	}
	return &CeremonyStatusInfo{
		State:   resp.State,
		SPKIPin: resp.SPKIPin,
	}, nil
}

// CeremonyBeginClaimCert starts a certificate claim challenge.
func (s *CeremonyService) CeremonyBeginClaimCert(ctx context.Context, username string) (*CeremonyChallenge, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	resp, err := s.client.CeremonyClaimCertBegin(ctx, &transport.ClaimCertBeginRequest{Username: username})
	if err != nil {
		return nil, err
	}
	ch := &CeremonyChallenge{NonceHex: resp.NonceHex}
	if resp.ExpiresAt != "" {
		if t, parseErr := time.Parse(time.RFC3339, resp.ExpiresAt); parseErr == nil {
			ch.ExpiresAt = t
		}
	}
	return ch, nil
}

// CeremonyCompleteClaimCert completes a certificate claim handshake.
func (s *CeremonyService) CeremonyCompleteClaimCert(ctx context.Context, username, nonceHex, signatureBase64 string) (*CeremonyClaimCertResult, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	resp, err := s.client.CeremonyClaimCertComplete(ctx, &transport.ClaimCertCompleteRequest{
		Username:  username,
		NonceHex:  nonceHex,
		Signature: signatureBase64,
	})
	if err != nil {
		return nil, err
	}
	return &CeremonyClaimCertResult{
		CertPEM:   []byte(resp.CertPEM),
		CACertPEM: []byte(resp.CACertPEM),
	}, nil
}

// CeremonyClaimShare retrieves the Shamir share for an officer. The qrdb
// transport returns the share JSON directly; the service layer surfaces it
// as a single base64-encoded string for symmetry with the REST contract.
func (s *CeremonyService) CeremonyClaimShare(ctx context.Context, username string) (string, error) {
	if s.client == nil {
		return "", &CeremonyServiceMissingError{}
	}
	resp, err := s.client.CeremonyClaimShare(ctx, &transport.ClaimShareRequest{Username: username})
	if err != nil {
		return "", err
	}
	return resp.ShareJSON, nil
}

// CeremonySignCSR signs a CSR during the ceremony.
func (s *CeremonyService) CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*CeremonySignCSRResult, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	resp, err := s.client.CeremonySignCSR(ctx, &transport.SignCSRInitRequest{
		Username: username,
		SOPin:    soPin,
		CSRPEM:   csrPEM,
		Role:     role,
	})
	if err != nil {
		return nil, err
	}
	return &CeremonySignCSRResult{CertPEM: []byte(resp.CertPEM)}, nil
}

// CeremonyEnrollExternalOfficer enrols an externally-keyed officer.
func (s *CeremonyService) CeremonyEnrollExternalOfficer(ctx context.Context, req *CeremonyEnrollExternalOfficerRequest) (*CeremonyEnrollExternalOfficerResponse, error) {
	if s.client == nil {
		return nil, &CeremonyServiceMissingError{}
	}
	if req == nil {
		req = &CeremonyEnrollExternalOfficerRequest{}
	}
	resp, err := s.client.CeremonyEnrollExternalOfficer(ctx, &transport.EnrollExternalOfficerRequest{
		Username:   req.Username,
		CertPEM:    req.CertPEM,
		PubKeyPEM:  req.PubKeyPEM,
		ShareIndex: req.ShareIndex,
		AuthFactor: req.AuthFactor,
		ShareBytes: req.ShareBytes,
	})
	if err != nil {
		return nil, err
	}
	return &CeremonyEnrollExternalOfficerResponse{SealedShare: resp.SealedShare}, nil
}
