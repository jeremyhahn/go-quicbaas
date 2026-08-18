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

package iam

import (
	"crypto/fips140"
	"errors"

	"github.com/jeremyhahn/go-auth/pkg/otp"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// TOTP hash algorithm selection.
//
// RFC 6238 baselines TOTP on HMAC-SHA1 and every authenticator app implements
// it, so SHA1 remains the default outside FIPS mode and existing enrollments
// keep verifying unchanged.
//
// In FIPS 140-3 mode HMAC-SHA1 is not an approved construction: Go's
// crypto/hmac *panics* on it under GODEBUG=fips140=only rather than returning
// an error. The policy below therefore resolves an algorithm to its otp
// equivalent only after checking that the process may use it, so a
// non-approved algorithm surfaces as a typed error on the caller's error path
// instead of taking the process down.
//
// A stored SHA1 secret cannot be used in FIPS mode at all - the shared secret
// is bound to the algorithm the authenticator was provisioned with, and there
// is no way to compute an HMAC-SHA1 code without HMAC-SHA1. Such a user must
// re-enroll; see docs/security/fips.md.

// fipsEnabled reports whether the process runs in Go's FIPS 140-3 mode
// (GOFIPS140 at build time plus GODEBUG=fips140=on|only at run time).
//
// Indirected through a variable so tests can exercise both policies on one
// host. A test may only force it to false around code that computes no HMAC:
// in a FIPS-only runtime, doing so re-admits the call the runtime panics on.
var fipsEnabled = fips140.Enabled

// mfaAlgorithms maps the configured algorithm name onto the otp package's
// algorithm constant.
var mfaAlgorithms = map[MFAAlgorithm]otp.Algorithm{
	MFAAlgorithmSHA1:   otp.AlgorithmSHA1,
	MFAAlgorithmSHA256: otp.AlgorithmSHA256,
	MFAAlgorithmSHA512: otp.AlgorithmSHA512,
}

// fipsApprovedMFAAlgorithms lists the algorithms usable for HMAC in FIPS 140-3
// mode. SHA-1 is deliberately absent.
var fipsApprovedMFAAlgorithms = map[MFAAlgorithm]struct{}{
	MFAAlgorithmSHA256: {},
	MFAAlgorithmSHA512: {},
}

// MFAAlgorithmUnsupportedError is returned when a TOTP secret or configuration
// names a hash algorithm QuicBaaS does not implement. It fails closed: an
// unrecognized identifier is never given a cryptographic meaning it was not
// stored with.
type MFAAlgorithmUnsupportedError struct {
	// Algorithm is the algorithm that was rejected.
	Algorithm MFAAlgorithm
}

// Error implements the error interface.
func (e *MFAAlgorithmUnsupportedError) Error() string {
	return "unsupported TOTP algorithm " + string(e.Algorithm) +
		"; supported algorithms are SHA1, SHA256 and SHA512"
}

// NewMFAAlgorithmUnsupportedError creates a typed error for an unrecognized
// TOTP algorithm.
func NewMFAAlgorithmUnsupportedError(op string, algorithm MFAAlgorithm) *dberrors.QBError {
	return &dberrors.QBError{
		Code: ErrMFAInvalid.Code,
		Op:   op,
		Err:  &MFAAlgorithmUnsupportedError{Algorithm: algorithm},
	}
}

// MFAAlgorithmNotApprovedError is returned when a TOTP secret or configuration
// names a hash algorithm that this process may not use because it is running
// in FIPS 140-3 mode.
type MFAAlgorithmNotApprovedError struct {
	// Algorithm is the algorithm that was rejected.
	Algorithm MFAAlgorithm
}

// Error implements the error interface.
func (e *MFAAlgorithmNotApprovedError) Error() string {
	return "TOTP algorithm " + string(e.Algorithm) +
		" is not FIPS 140-3 approved; the affected user must re-enroll MFA"
}

// NewMFAAlgorithmNotApprovedError creates a typed error for a TOTP algorithm
// that FIPS mode forbids.
func NewMFAAlgorithmNotApprovedError(op string, algorithm MFAAlgorithm) *dberrors.QBError {
	return &dberrors.QBError{
		Code: ErrMFAInvalid.Code,
		Op:   op,
		Err:  &MFAAlgorithmNotApprovedError{Algorithm: algorithm},
	}
}

// defaultMFAAlgorithm returns the algorithm used for new enrollments when the
// configuration does not name one: SHA256 in FIPS mode, otherwise the RFC 6238
// baseline SHA1.
func defaultMFAAlgorithm() MFAAlgorithm {
	if fipsEnabled() {
		return MFAAlgorithmSHA256
	}
	return MFAAlgorithmSHA1
}

// convertAlgorithm converts MFAAlgorithm to otp.Algorithm, rejecting any
// algorithm the current process may not use.
//
// An empty value keeps its historical meaning of SHA1: it predates the field,
// so reading it as anything else would change the meaning of a secret already
// in the store. Any other unrecognized value fails closed - a corrupted or
// future identifier never silently acquires a cryptographic meaning.
func convertAlgorithm(op string, algo MFAAlgorithm) (otp.Algorithm, error) {
	if algo == "" {
		algo = MFAAlgorithmSHA1
	}
	otpAlgo, known := mfaAlgorithms[algo]
	if !known {
		return otpAlgo, NewMFAAlgorithmUnsupportedError(op, algo)
	}
	if fipsEnabled() {
		if _, approved := fipsApprovedMFAAlgorithms[algo]; !approved {
			return otpAlgo, NewMFAAlgorithmNotApprovedError(op, algo)
		}
	}
	return otpAlgo, nil
}

// isMFAAlgorithmPolicyError reports whether err is a refusal to use an
// algorithm, as opposed to a failure to authenticate. The two must not be
// conflated: a policy refusal is not the user presenting a wrong code.
func isMFAAlgorithmPolicyError(err error) bool {
	var notApproved *MFAAlgorithmNotApprovedError
	var unsupported *MFAAlgorithmUnsupportedError
	return errors.As(err, &notApproved) || errors.As(err, &unsupported)
}

// mfaProvisioningParams are the TOTP parameters an authenticator app was
// provisioned with. They are encoded into the enrollment's otpauth:// URI, so
// verification has to use them rather than whatever the manager is configured
// with by the time the user types the code.
type mfaProvisioningParams struct {
	Algorithm MFAAlgorithm
	Digits    int
	Period    int
}

// provisioningParams returns the parameters the enrollment was issued with,
// falling back per field to the current configuration for an enrollment
// persisted before those parameters were recorded.
func (m *DefaultMFAManager) provisioningParams(enrollment *MFAEnrollment) mfaProvisioningParams {
	params := mfaProvisioningParams{
		Algorithm: enrollment.Algorithm,
		Digits:    enrollment.Digits,
		Period:    enrollment.Period,
	}
	if params.Algorithm == "" {
		params.Algorithm = m.config.Algorithm
	}
	if params.Digits == 0 {
		params.Digits = m.config.Digits
	}
	if params.Period == 0 {
		params.Period = m.config.Period
	}
	return params
}
