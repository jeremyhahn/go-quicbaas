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

// admin_factor_persister.go durably records the administrator login factors
// the init ceremony enrolls.
//
// This is what makes quicbaas's admin enrollment stronger than xkmsd's: xkmsd
// runs its ceremony against in-memory storage and passes a nil persister
// (cmd/xkmsd/init_ceremony.go carries a TODO(P1) for exactly this), so its
// factors are reported but not stored. quicbaas init runs against the durable
// qrdb-backed store, so an enrolled passkey or PIV credential survives the
// command and is a real login credential on the next boot.
//
// The load-bearing detail is the CREDENTIAL bytes. qrdb indexes a factor by
// (type, credential) and derives the storage key with
// pkg/service.lookupKeyFor: SHA-256 of the credential blob for mtls / piv, and
// the credential blob verbatim for fido2 (it is already a credential ID). Get
// this wrong and enrollment "succeeds" while producing a credential that can
// never authenticate anyone — worse than no factor at all, because the
// operator believes they have one.

package server

import (
	"context"
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// AdminFactorPersistError reports a failure recording an enrolled admin login
// factor.
type AdminFactorPersistError struct {
	Factor string
	Cause  error
}

// Error implements the error interface.
func (e *AdminFactorPersistError) Error() string {
	if e.Cause == nil {
		return "persist admin factor " + e.Factor + " failed"
	}
	return "persist admin factor " + e.Factor + ": " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *AdminFactorPersistError) Unwrap() error { return e.Cause }

// AdminFactorCredentialError reports an enrollment whose credential bytes
// cannot be derived, so it would be stored under a key nothing can look up.
type AdminFactorCredentialError struct {
	Factor string
	Reason string
}

// Error implements the error interface.
func (e *AdminFactorCredentialError) Error() string {
	return "admin factor " + e.Factor + ": " + e.Reason
}

// adminFactorCreatedBy marks factors enrolled by the init ceremony, so an
// auditor can distinguish them from ones a user later self-enrolled.
const adminFactorCreatedBy = "system:init-ceremony"

// adminFactorPersister writes enrolled admin login factors through to qrdb's
// UserAuthFactor store.
type adminFactorPersister struct {
	factors qrdbsdk.UserAuthFactorServicer
	userID  uint64
	logger  *slog.Logger
}

// Compile-time proof this satisfies the seam go-qrdb's EnrollAdminFactors
// calls, so a signature change upstream fails the build here rather than
// silently dropping every enrollment.
var _ qrdbsdk.AdminFactorPersister = (*adminFactorPersister)(nil)

// newAdminFactorPersister builds the persister for one administrator.
func newAdminFactorPersister(
	factors qrdbsdk.UserAuthFactorServicer,
	userID uint64,
	logger *slog.Logger,
) *adminFactorPersister {
	if logger == nil {
		logger = slog.Default()
	}
	return &adminFactorPersister{factors: factors, userID: userID, logger: logger}
}

// PersistAdminFactor records one enrolled factor.
//
// mTLS is deliberately skipped: first-boot already enrolls the administrator's
// issued certificate through enrollFirstBootAdminCert, which additionally
// stores the renewal-stable SPKI credential so the factor survives certificate
// rotation. Writing it a second time here would either collide or register a
// weaker duplicate.
func (p *adminFactorPersister) PersistAdminFactor(
	ctx context.Context,
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) error {
	if p == nil || p.factors == nil {
		return &AdminFactorPersistError{Factor: factor.String()}
	}
	if enrollment == nil {
		return &AdminFactorCredentialError{Factor: factor.String(), Reason: "nil enrollment"}
	}
	if factor == qrdbsdk.FactorMTLS {
		return nil
	}

	credential, err := adminFactorCredential(factor, enrollment)
	if err != nil {
		return err
	}

	// Add is the same servicer seam pkg/iam's cert-login enroller uses, so
	// ceremony-enrolled factors are indistinguishable at the storage layer
	// from ones enrolled through any other path.
	if err := p.factors.Add(ctx, p.userID, &qrdbsdk.AuthFactor{
		UserID:     p.userID,
		Type:       qrdbsdk.AuthFactorType(factor),
		Label:      adminFactorLabel(factor, enrollment),
		Credential: credential,
		Metadata:   enrollment.Metadata,
		CreatedBy:  adminFactorCreatedBy,
	}); err != nil {
		return &AdminFactorPersistError{Factor: factor.String(), Cause: err}
	}

	p.logger.Info("admin login factor persisted",
		slog.String("factor", factor.String()),
		slog.Uint64("user_id", p.userID))
	return nil
}

// adminFactorCredential returns the canonical lookup credential for an
// enrollment, matching what the corresponding login path presents:
//
//   - fido2: the base64url credential ID from the enrollment metadata. qrdb
//     stores fido2 credentials verbatim (they are already IDs), and the same
//     encoding is what a login assertion reduces to.
//   - piv:   the enrolled certificate/public key blob, which qrdb hashes.
//
// A factor with no derivable credential is an error, never a silent skip.
func adminFactorCredential(
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) ([]byte, error) {
	switch factor {
	case qrdbsdk.FactorFIDO2:
		credentialID := enrollment.Metadata["credential_id"]
		if credentialID == "" {
			return nil, &AdminFactorCredentialError{
				Factor: factor.String(),
				Reason: "enrollment carries no credential_id; the passkey could never be looked up at login",
			}
		}
		return []byte(credentialID), nil
	default:
		if len(enrollment.PublicKey) == 0 {
			return nil, &AdminFactorCredentialError{
				Factor: factor.String(),
				Reason: "enrollment carries no public key material",
			}
		}
		return enrollment.PublicKey, nil
	}
}

// adminFactorLabel names the credential for the operator listing their
// factors, preferring a hardware-meaningful detail over a generic string.
func adminFactorLabel(factor qrdbsdk.AdminFactor, enrollment *qrdbsdk.AuthFactorEnrollment) string {
	switch factor {
	case qrdbsdk.FactorFIDO2:
		if rp := enrollment.Metadata["rp_id"]; rp != "" {
			return "init ceremony passkey (" + rp + ")"
		}
		return "init ceremony passkey"
	default:
		if slot := enrollment.Metadata["slot"]; slot != "" {
			return "init ceremony PIV (slot " + slot + ")"
		}
		return "init ceremony " + factor.String()
	}
}
