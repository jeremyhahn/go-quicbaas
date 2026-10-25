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
	"fmt"

	"github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	"github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
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
// initialization ceremony. It is qrdb's officer request itself, so every
// field (role, share-envelope encryption key, factors, designated factor)
// reaches qrdb without narrowing.
type CeremonyOfficer = transport.CeremonyOfficerRequest

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

	// Policy is the Shamir custody policy for Threshold >= 2
	// ("designated" when empty).
	Policy string

	// Officers are the officers participating in the ceremony.
	Officers []CeremonyOfficer

	// Hostname is the optional hostname for the issued TLS certificate.
	Hostname string

	// SANs is the optional list of Subject Alternative Names.
	SANs []string

	// AdminCSRPEM, AdminCertPEM and AdminGenerate select the bootstrap
	// admin's credential path (go-qrdb#548): CSR-sign, bring-your-own
	// certificate, or generate (dev/test only). Exactly one must be set.
	AdminCSRPEM  string
	AdminCertPEM string

	// AdminCertPoolPEM is the CA pool that verifies AdminCertPEM.
	AdminCertPoolPEM string

	AdminGenerate bool

	// Secret is the system barrier passphrase. Required when the barrier's
	// strategy needs one (e.g. "passphrase"); nil for "none". The caller
	// owns the Secret and destroys it once CeremonyInitialize returns.
	Secret *secure.Secret
}

// adminSelectorCount reports how many bootstrap admin credential paths the
// request selects.
func (r *CeremonyInitRequest) adminSelectorCount() int {
	n := 0
	if r.AdminCSRPEM != "" {
		n++
	}
	if r.AdminCertPEM != "" {
		n++
	}
	if r.AdminGenerate {
		n++
	}
	return n
}

// CeremonyAdminSelectorMissingError signals a ceremony init request that
// selects no bootstrap admin credential path. go-qrdb has no
// credential-less default, so the request is refused before it is sent.
type CeremonyAdminSelectorMissingError struct{}

// Error returns the error message.
func (e *CeremonyAdminSelectorMissingError) Error() string {
	return "ceremony init requires exactly one of admin CSR, admin certificate or admin generate; none was set"
}

// CeremonyAdminSelectorConflictError signals a ceremony init request that
// selects more than one bootstrap admin credential path.
type CeremonyAdminSelectorConflictError struct {
	// Count is the number of paths the request selected.
	Count int
}

// Error returns the error message.
func (e *CeremonyAdminSelectorConflictError) Error() string {
	return fmt.Sprintf("ceremony init requires exactly one of admin CSR, admin certificate or admin generate; %d were set", e.Count)
}

// CeremonySecretError signals that the barrier passphrase could not be moved
// into sealed memory before the ceremony call.
type CeremonySecretError struct {
	// Err is the go-qrdb secure error that refused the passphrase.
	Err error
}

// Error returns the error message.
func (e *CeremonySecretError) Error() string {
	return "ceremony barrier secret could not be sealed: " + e.Err.Error()
}

// Unwrap returns the underlying secure error.
func (e *CeremonySecretError) Unwrap() error {
	return e.Err
}

// NewCeremonySecret seals b into a *secure.Secret for CeremonyInitRequest
// and wipes b, whatever the outcome. An empty b yields a nil Secret (no
// passphrase, strategy "none"). The caller destroys a non-nil Secret once
// CeremonyInitialize returns.
func NewCeremonySecret(ctx context.Context, b []byte) (*secure.Secret, error) {
	if len(b) == 0 {
		return nil, nil
	}
	defer secure.Wipe(b)
	secret, err := secure.NewSecret(ctx, b)
	if err != nil {
		return nil, errors.Wrap(&CeremonySecretError{Err: err}, errors.ErrInternal, "service.NewCeremonySecret")
	}
	return secret, nil
}

// CeremonyInitInfo contains the output of a successful ceremony
// initialisation. AdminKeyPEM is set only when AdminCredentialSource is
// "generated": qrdb returns it exactly once and keeps no copy, so every
// transport must deliver it to the caller.
type CeremonyInitInfo struct {
	// SPKIPin is the base64-encoded SHA-256 hash of the CA certificate's SPKI.
	SPKIPin string

	// CACertPEM is the PEM-encoded CA certificate.
	CACertPEM []byte

	// TLSCertPEM is the PEM-encoded TLS certificate issued for the server.
	TLSCertPEM []byte

	// State is the ceremony state after initialisation.
	State string

	// ShareCount is the number of Shamir shares generated.
	ShareCount int

	// Holders carries the CA-issued certificate of each initial holder.
	Holders []transport.CeremonyHolderCert

	// AdminCertPEM is the bootstrap administrator's signed certificate.
	AdminCertPEM []byte

	// AdminKeyPEM is the generated administrator's private key; present
	// only on the generate path.
	AdminKeyPEM []byte

	// AdminCredentialSource names the admin credential path qrdb took;
	// "generated" when AdminKeyPEM is set.
	AdminCredentialSource string
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

	// CeremonySignCSR signs a CSR during the ceremony.
	CeremonySignCSR(ctx context.Context, username, soPin, csrPEM, role string) (*CeremonySignCSRResult, error)
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
	switch n := req.adminSelectorCount(); {
	case n == 0:
		return nil, errors.Wrap(&CeremonyAdminSelectorMissingError{}, errors.ErrInvalidInput, "service.CeremonyInitialize")
	case n > 1:
		return nil, errors.Wrap(&CeremonyAdminSelectorConflictError{Count: n}, errors.ErrInvalidInput, "service.CeremonyInitialize")
	}
	wire := &transport.CeremonyInitializeRequest{
		SOPin:                  req.SOPin,
		UserPin:                req.UserPin,
		CredentialSealStrategy: req.CredentialSealStrategy,
		Threshold:              req.Threshold,
		Policy:                 req.Policy,
		Officers:               req.Officers,
		Hostname:               req.Hostname,
		SANs:                   req.SANs,
		AdminCSRPEM:            req.AdminCSRPEM,
		AdminCertPEM:           req.AdminCertPEM,
		AdminCertPoolPEM:       req.AdminCertPoolPEM,
		AdminGenerate:          req.AdminGenerate,
	}
	resp, err := s.initialize(ctx, wire, req.Secret)
	if err != nil {
		return nil, err
	}
	return &CeremonyInitInfo{
		SPKIPin:    resp.SPKIPin,
		CACertPEM:  []byte(resp.CACertPEM),
		TLSCertPEM: []byte(resp.TLSCertPEM),
		State:      resp.State,
		ShareCount: resp.ShareCount,

		Holders:               resp.Holders,
		AdminCertPEM:          []byte(resp.AdminCertPEM),
		AdminKeyPEM:           resp.AdminKeyPEM,
		AdminCredentialSource: resp.AdminCredentialSource,
	}, nil
}

// initialize sends wire to qrdb. A non-nil secret is opened only for the
// duration of the call, so the passphrase plaintext never outlives it; a nil
// secret sends no passphrase (strategy "none").
func (s *CeremonyService) initialize(ctx context.Context, wire *transport.CeremonyInitializeRequest, secret *secure.Secret) (*transport.CeremonyInitializeResponse, error) {
	if secret == nil {
		return s.client.CeremonyInitialize(ctx, wire)
	}
	var resp *transport.CeremonyInitializeResponse
	err := secret.WithBytes(ctx, func(b []byte) error {
		wire.Secret = b
		defer func() { wire.Secret = nil }()
		var callErr error
		resp, callErr = s.client.CeremonyInitialize(ctx, wire)
		return callErr
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
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
