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

package server

import (
	"context"
	"crypto/x509"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// ceremonyCAAdapter bridges this server's xkms certificate authority to the
// three qrdb seams that need to issue certificates during bootstrap:
// qrdbsdk.CeremonyCA (init ceremony: TLS serving cert, officer CSR signing, CA
// bundle / SPKI pin), qrdbsdk.CertProvider (admin-mTLS Path A), and
// qrdbsdk.CertSigner (admin-mTLS Path B). One xkms CAAdapter satisfies all
// three, so the whole stack issues from a single hardware-backed CA.
//
// It returns nil when no CA is available (TLS disabled and no external CA
// supplied); callers must treat nil as "cert issuance is unavailable" rather
// than constructing an adapter over a nil CA.
func (s *Server) ceremonyCAAdapter() *xkmssdk.CAAdapter {
	if s == nil || s.xkmsCA == nil {
		return nil
	}
	return xkmssdk.NewCAAdapter(s.xkmsCA, s.config.IAM.SPIFFETrustDomain)
}

// lateBoundCeremonyCA is a qrdbsdk.CeremonyCA whose backing CA is attached
// after construction.
//
// It exists because of a hard ordering constraint: the embedded qrdb node is
// built before initializeCA runs (the CA's own key password may be resolved
// from the node's credential store), yet the node's server options — which
// carry the ceremony CA — must be supplied at node construction. Passing the
// then-nil CA would wire a permanently CA-less ceremony; passing an adapter
// over a nil CA would panic on first use. Late binding resolves the CA at call
// time, which is always after boot completes.
//
// Every method fails closed with CeremonyCAUnavailableError until Set is
// called. The pointer is swapped atomically so a ceremony verb running
// concurrently with boot never observes a torn value.
type lateBoundCeremonyCA struct {
	ca atomic.Pointer[xkmssdk.CAAdapter]
}

// Compile-time proof the late-bound wrapper still satisfies the seam.
var _ qrdbsdk.CeremonyCA = (*lateBoundCeremonyCA)(nil)

// CeremonyCAUnavailableError reports that a ceremony verb needed the
// certificate authority before it was wired (or on a deployment that has none).
// Op names the verb so the operator can tell which capability failed closed.
type CeremonyCAUnavailableError struct {
	// Op is the ceremony operation that required the CA.
	Op string
}

// Error implements the error interface.
func (e *CeremonyCAUnavailableError) Error() string {
	if e == nil || e.Op == "" {
		return "ceremony: certificate authority is not available"
	}
	return "ceremony: certificate authority is not available for " + e.Op
}

// Set attaches the backing CA adapter. A nil adapter leaves the wrapper
// failing closed, which is the correct posture for a deployment with no CA.
func (l *lateBoundCeremonyCA) Set(ca *xkmssdk.CAAdapter) {
	if l == nil {
		return
	}
	l.ca.Store(ca)
}

// resolve returns the attached adapter, or nil when none is attached yet.
func (l *lateBoundCeremonyCA) resolve() *xkmssdk.CAAdapter {
	if l == nil {
		return nil
	}
	return l.ca.Load()
}

// SignCSR signs an officer CSR through the attached CA.
func (l *lateBoundCeremonyCA) SignCSR(ctx context.Context, csrPEM []byte, opts *qrdbsdk.SignCSROptions) (*x509.Certificate, error) {
	ca := l.resolve()
	if ca == nil {
		return nil, &CeremonyCAUnavailableError{Op: "SignCSR"}
	}
	return ca.SignCSR(ctx, csrPEM, opts)
}

// CACertificate returns the attached CA's own certificate.
func (l *lateBoundCeremonyCA) CACertificate() (*x509.Certificate, error) {
	ca := l.resolve()
	if ca == nil {
		return nil, &CeremonyCAUnavailableError{Op: "CACertificate"}
	}
	return ca.CACertificate()
}

// CABundle returns the attached CA's PEM-encoded chain.
func (l *lateBoundCeremonyCA) CABundle() ([]byte, error) {
	ca := l.resolve()
	if ca == nil {
		return nil, &CeremonyCAUnavailableError{Op: "CABundle"}
	}
	return ca.CABundle()
}

// IssueCertificate issues a certificate (generating the keypair) through the
// attached CA.
func (l *lateBoundCeremonyCA) IssueCertificate(ctx context.Context, req *qrdbsdk.IssueCertRequest) (*qrdbsdk.IssuedCert, error) {
	ca := l.resolve()
	if ca == nil {
		return nil, &CeremonyCAUnavailableError{Op: "IssueCertificate"}
	}
	return ca.IssueCertificate(ctx, req)
}

// bindCeremonyCA attaches the now-initialized xkms CA to every ceremony
// consumer: the late-bound CeremonyCA handed to the embedded qrdb node at
// construction time, and the bootstrap ceremony core built for seal-disabled
// deployments. It is called once initializeCA has run.
func (s *Server) bindCeremonyCA() {
	ca := s.ceremonyCAAdapter()
	if ca == nil {
		s.logger.Warn("No certificate authority available: ceremony certificate issuance will fail closed")
		return
	}
	s.ceremonyCA.Set(ca)
	s.logger.Info("Ceremony certificate authority bound to the platform xkms CA")
}

// PINBackendUnavailableError reports that the init ceremony asked to enroll the
// operator's SO / user PINs but no PIN backend was constructed — the embedded
// qrdb node was never brought up, so there is nothing to enroll into.
type PINBackendUnavailableError struct{}

// Error implements the error interface.
func (e *PINBackendUnavailableError) Error() string {
	return "init: PIN backend is unavailable; the embedded qrdb node is not running"
}

// InitPINEnrollError reports a failure enrolling one of the init ceremony PINs.
// Which names the PIN (so / user) so the operator knows which value the backend
// rejected.
type InitPINEnrollError struct {
	// Which is "so" or "user".
	Which string
	// Cause is the backend error.
	Cause error
}

// Error implements the error interface.
func (e *InitPINEnrollError) Error() string {
	return "init: enroll " + e.Which + " PIN: " + e.Cause.Error()
}

// Unwrap returns the underlying cause for errors.Is / errors.As traversal.
func (e *InitPINEnrollError) Unwrap() error { return e.Cause }

// EnrollInitPINs writes the operator-supplied Security Officer and user PINs
// into the PIN backend the embedded qrdb node verifies against. It is the
// consumer of `quicbaasd init --so-pin / --user-pin`: without it those flags are
// mandatory but inert, and every later SO-authorized operation fails because no
// SO PIN was ever set.
//
// Idempotent: a backend that already has both PINs configured is left alone, so
// re-running init against an existing data dir does not rotate credentials the
// operator is already using.
func (s *Server) EnrollInitPINs(soPIN, userPIN string) error {
	if soPIN == "" && userPIN == "" {
		return nil
	}
	backend := s.pinBackend
	if backend == nil {
		return &PINBackendUnavailableError{}
	}
	if soPIN != "" && !backend.SOPINSet() {
		// First-time setup takes an empty current PIN.
		if err := backend.SetSOPIN("", soPIN); err != nil {
			return &InitPINEnrollError{Which: "so", Cause: err}
		}
		s.logger.Info("Security Officer PIN enrolled")
	}
	if userPIN != "" && !backend.UserPINSet() {
		// The user PIN change is SO-authorized.
		if err := backend.SetUserPIN(soPIN, userPIN); err != nil {
			return &InitPINEnrollError{Which: "user", Cause: err}
		}
		s.logger.Info("User PIN enrolled")
	}
	return nil
}
