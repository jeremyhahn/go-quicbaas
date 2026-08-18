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
// An empty or unrecognized value keeps its historical meaning of SHA1 so that
// no already-stored secret changes meaning; in FIPS mode it is then rejected
// along with an explicit SHA1.
func convertAlgorithm(op string, algo MFAAlgorithm) (otp.Algorithm, error) {
	otpAlgo, known := mfaAlgorithms[algo]
	if !known {
		algo, otpAlgo = MFAAlgorithmSHA1, otp.AlgorithmSHA1
	}
	if fipsEnabled() {
		if _, approved := fipsApprovedMFAAlgorithms[algo]; !approved {
			return otpAlgo, NewMFAAlgorithmNotApprovedError(op, algo)
		}
	}
	return otpAlgo, nil
}
