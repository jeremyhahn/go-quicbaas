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

package iam

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"errors"
	"strconv"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Certificate binding -> qrdb mtls auth-factor enrollment
//
// PROBLEM THIS SOLVES
//
// Binding a client certificate to a user (the bootstrap admin PIV cert path and
// the SO-cert path in pkg/server/iam_init.go) wrote only the user-side
// thumbprint index. The UNIFIED login ceremony
// (go-qrdb pkg/auth.LoginResolver -> FactorStore.LookupByCredentialKey) resolves
// against a DIFFERENT store: qrdb's uint64-keyed UserAuthFactor store. A
// certificate bound through either path therefore had no resolvable factor and
// could never complete a CLI/SDK login — `auth login --method mtls` returned
// BEGIN 200 / FINISH 401 "factor not found" with a byte-perfect client
// certificate. This is the certificate-side twin of the WebAuthn registration
// gap closed by webauthn_factor_writethrough.go.
//
// LOOKUP KEY CONTRACT (must match the login path byte for byte)
//
// qrdb indexes a factor under hex(SHA-256(Credential)) — see
// go-qrdb pkg/service/user_auth_factor_service.go lookupKeyFor — and resolves a
// login with the SAME transform over the request's lookup key. Enrollment and
// login therefore have to agree on the raw credential BLOB, not on any encoding
// of it:
//
//	Credential     = SHA-256(cert.Raw)                  (raw 32 bytes)
//	SPKICredential = SHA-256(cert.RawSubjectPublicKeyInfo) (raw 32 bytes)
//
// which is exactly what pkg/service.LoginService.FinishLogin sends for an mtls
// factor: LookupKey = the SPKI digest (renewal-stable, tried first) and
// AltLookupKeys = [the cert-DER digest] (legacy). Note this is NOT the hex
// string the user-side PIV thumbprint index stores; that index is a separate
// user lookup consumed by the REST PIV login handler.
//
// TRANSACTIONAL BEHAVIOUR (deliberate choice: reorder, do not compensate)
//
// The invariant is: "a persisted certificate binding implies a login-resolvable
// qrdb factor". The callers therefore ENROLL FIRST and bind second:
//
//  1. Enroll the mtls factor (this type).
//  2. Persist the user-side cert binding + state transition.
//
// A failure in (1) leaves nothing written at all, so the operator-visible
// failure is a clean no-op. Enrolling second — with compensation — was rejected:
// undoing a factor requires a FORCE removal, and qrdb escalates a forced removal
// of a user's last active factor into disabling the user (see
// userAuthFactorService.Remove -> UserStateUpdater.SetUserActive). Taking a
// destructive, user-disabling action on a bootstrap error path is strictly worse
// than the residue of the chosen order: a factor whose credential is the exact
// certificate the operator configured, on a daemon whose startup then aborts.
//
// IDEMPOTENCY
//
// Re-binding the same certificate must not error. qrdb answers a duplicate
// enroll with FactorAlreadyExistsError; Enroll accepts that ONLY after
// confirming the existing factor belongs to the same user, so a certificate
// already enrolled to a DIFFERENT principal fails closed instead of silently
// reporting a binding that would authenticate somebody else.
// =============================================================================

// MTLSCredentialKey returns the canonical mtls/piv login lookup key for a
// certificate: SHA-256 over the certificate's DER encoding. It is the raw-byte
// counterpart of go-qrdb pkg/auth.CredentialKey(cert.Raw) and the SINGLE
// definition quicbaas uses on both the enrollment side (this package) and the
// login side (pkg/service.LoginService.FinishLogin). A nil certificate yields
// nil so callers fail on the typed validation error rather than on a panic.
func MTLSCredentialKey(cert *x509.Certificate) []byte {
	if cert == nil || len(cert.Raw) == 0 {
		return nil
	}
	sum := sha256.Sum256(cert.Raw)
	return sum[:]
}

// MTLSSPKICredentialKey returns the renewal-stable secondary lookup key for a
// certificate: SHA-256 over its SubjectPublicKeyInfo. It matches go-qrdb
// pkg/auth.SPKICredentialKey(cert), so a certificate renewed with the same
// keypair keeps resolving to its factor after the cert-DER digest changes.
func MTLSSPKICredentialKey(cert *x509.Certificate) []byte {
	if cert == nil || len(cert.RawSubjectPublicKeyInfo) == 0 {
		return nil
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return sum[:]
}

// CertLoginFactorConfigError reports a missing dependency for certificate
// factor enrollment. It is returned at construction time so a misconfiguration
// surfaces at wiring time rather than mid-bootstrap.
type CertLoginFactorConfigError struct {
	// Field names the missing or invalid dependency.
	Field string
}

// Error implements error.
func (e *CertLoginFactorConfigError) Error() string {
	if e == nil || e.Field == "" {
		return "iam.certfactor: invalid configuration"
	}
	return "iam.certfactor: missing or invalid field: " + e.Field
}

// CertLoginFactorEnrollError reports that the mtls auth factor backing a
// certificate binding could not be enrolled — meaning the certificate would not
// be resolvable by the unified login ceremony. The binding fails closed with
// this error rather than persisting a certificate that cannot log in.
type CertLoginFactorEnrollError struct {
	// UserID is the qrdb numeric user the factor was being enrolled for.
	UserID uint64

	// Cause is the underlying enrollment failure.
	Cause error
}

// Error implements error.
func (e *CertLoginFactorEnrollError) Error() string {
	if e == nil {
		return "iam.certfactor: enrollment failed"
	}
	msg := "iam.certfactor: failed to enroll the bound certificate as an mtls login factor for user " +
		strconv.FormatUint(e.UserID, 10)
	if e.Cause != nil {
		return msg + ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the underlying cause for errors.Is / errors.As.
func (e *CertLoginFactorEnrollError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// CertLoginFactorOwnerMismatchError reports that the certificate being bound is
// already enrolled as a login factor for a DIFFERENT user. Accepting the binding
// would advertise a credential that authenticates somebody else, so enrollment
// fails closed.
type CertLoginFactorOwnerMismatchError struct {
	// RequestedUserID is the user the binding was requested for.
	RequestedUserID uint64

	// EnrolledUserID is the user the existing factor belongs to.
	EnrolledUserID uint64

	// FactorID is the existing factor's identifier.
	FactorID string
}

// Error implements error.
func (e *CertLoginFactorOwnerMismatchError) Error() string {
	if e == nil {
		return "iam.certfactor: certificate already enrolled for another user"
	}
	return "iam.certfactor: certificate is already enrolled as login factor " + e.FactorID +
		" for user " + strconv.FormatUint(e.EnrolledUserID, 10) +
		", refusing to bind it to user " + strconv.FormatUint(e.RequestedUserID, 10)
}

// CertLoginFactorEnrollment describes one certificate-to-user binding that must
// become resolvable by the unified login ceremony.
type CertLoginFactorEnrollment struct {
	// UserID is the qrdb numeric id of the principal the certificate
	// authenticates. Required.
	UserID uint64

	// Certificate is the bound client certificate. Required.
	Certificate *x509.Certificate

	// Label is the operator-visible display name recorded on the factor
	// (shown by `quicbaas auth factor list`). Optional.
	Label string

	// Metadata carries non-secret provenance for the factor (binding source,
	// tenant, certificate subject/serial). It never carries key material.
	// Optional.
	Metadata map[string]string
}

// CertLoginFactorEnroller enrolls the qrdb mtls auth factor that makes a
// certificate binding resolvable by the unified login ceremony. It is the single
// place quicbaas derives certificate factor credentials, so the bootstrap PIV
// path and the SO-cert path cannot drift apart from each other or from the login
// resolver.
//
// The factor type is always mtls: mtls is the login method a bound client
// certificate authenticates with (`quicbaas auth login --method mtls`), and the
// server derives the lookup key from the verified TLS peer certificate. A piv
// factor is a distinct credential proven by an on-card signature and is enrolled
// by the PIV enrollment ceremony, not by a certificate binding.
type CertLoginFactorEnroller struct {
	factors qrdbsdk.UserAuthFactorServicer
}

// NewCertLoginFactorEnroller constructs an enroller over the qrdb user-side
// auth-factor service — the SAME store go-qrdb's LoginResolver reads. A nil
// service is rejected: it would silently reproduce the split-store bug this
// type exists to close.
func NewCertLoginFactorEnroller(factors qrdbsdk.UserAuthFactorServicer) (*CertLoginFactorEnroller, error) {
	if factors == nil {
		return nil, &CertLoginFactorConfigError{Field: "UserAuthFactorServicer"}
	}
	return &CertLoginFactorEnroller{factors: factors}, nil
}

// Enroll registers the certificate as an mtls login factor for the user. It is
// idempotent: re-enrolling the same certificate for the same user succeeds
// without writing a duplicate. See the package commentary above for the ordering
// contract callers must honour (enroll BEFORE persisting the binding).
func (e *CertLoginFactorEnroller) Enroll(ctx context.Context, enrollment CertLoginFactorEnrollment) error {
	if e == nil || e.factors == nil {
		return &CertLoginFactorConfigError{Field: "UserAuthFactorServicer"}
	}
	if enrollment.UserID == 0 {
		return &CertLoginFactorConfigError{Field: "UserID"}
	}
	credential := MTLSCredentialKey(enrollment.Certificate)
	if len(credential) == 0 {
		return &CertLoginFactorConfigError{Field: "Certificate"}
	}

	factor := &qrdbsdk.AuthFactor{
		UserID:         enrollment.UserID,
		Type:           qrdbsdk.AuthFactorMTLS,
		Label:          enrollment.Label,
		Credential:     credential,
		SPKICredential: MTLSSPKICredentialKey(enrollment.Certificate),
		Metadata:       enrollment.Metadata,
		CreatedBy:      qrdbsdk.FactorCreatedByAdmin,
	}

	err := e.factors.Add(ctx, enrollment.UserID, factor)
	if err == nil {
		return nil
	}
	if !errors.Is(err, qrdbsdk.ErrFactorAlreadyExists) {
		return &CertLoginFactorEnrollError{UserID: enrollment.UserID, Cause: err}
	}
	return e.confirmOwnership(ctx, enrollment.UserID, factor)
}

// confirmOwnership resolves the factor that already holds this certificate's
// credential and accepts the duplicate ONLY when it belongs to the requesting
// user. Both indexed keys are probed because qrdb reports a collision on either
// the cert-DER credential or the renewal-stable SPKI credential.
func (e *CertLoginFactorEnroller) confirmOwnership(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	keys := [][]byte{factor.Credential}
	if len(factor.SPKICredential) > 0 {
		keys = append(keys, factor.SPKICredential)
	}
	var lastErr error
	for _, key := range keys {
		existing, err := e.factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, key)
		if err != nil {
			lastErr = err
			continue
		}
		if existing == nil {
			continue
		}
		if existing.UserID != userID {
			return &CertLoginFactorOwnerMismatchError{
				RequestedUserID: userID,
				EnrolledUserID:  existing.UserID,
				FactorID:        existing.ID,
			}
		}
		// Same certificate, same user: the login-resolvable factor already
		// exists, so the binding is complete.
		return nil
	}
	// qrdb reported a duplicate but the record cannot be read back (e.g. the
	// existing factor is revoked, which leaves its index entry in place). Fail
	// closed — a binding whose factor cannot be resolved is the exact defect
	// this enrollment exists to prevent.
	return &CertLoginFactorEnrollError{UserID: userID, Cause: lastErr}
}
