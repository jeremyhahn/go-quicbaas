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
	"crypto/x509"
	"encoding/pem"
	"log/slog"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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

	credential, spkiCredential, metadata, err := adminFactorRecord(factor, enrollment)
	if err != nil {
		return err
	}

	// Add is the same servicer seam pkg/iam's cert-login enroller uses, so
	// ceremony-enrolled factors are indistinguishable at the storage layer
	// from ones enrolled through any other path.
	if err := p.factors.Add(ctx, p.userID, &qrdbsdk.AuthFactor{
		UserID:         p.userID,
		Type:           qrdbsdk.AuthFactorType(factor),
		Label:          adminFactorLabel(factor, enrollment),
		Credential:     credential,
		SPKICredential: spkiCredential,
		Metadata:       metadata,
		CreatedBy:      adminFactorCreatedBy,
	}); err != nil {
		return &AdminFactorPersistError{Factor: factor.String(), Cause: err}
	}

	p.logger.Info("admin login factor persisted",
		slog.String("factor", factor.String()),
		slog.Uint64("user_id", p.userID))
	return nil
}

// adminFactorRecord returns everything a persisted factor needs to be usable:
// the lookup credential, the renewal-stable secondary credential, and the
// metadata the login verifier reads. Each must match what the corresponding
// login path PRESENTS, which is why the derivation lives next to a citation of
// that path rather than being chosen here.
//
//   - fido2: the base64url credential ID from the enrollment metadata, stored
//     verbatim because qrdb does not digest fido2 keys — they are already IDs —
//     and the same encoding is what a login assertion reduces to. No
//     certificate is involved, so there is no secondary credential.
//   - piv:   digests over the enrolled CERTIFICATE, plus the certificate itself
//     in metadata. See pivAdminFactorRecord for which digest is which and why
//     none of the three is optional.
//
// A factor with no derivable credential is an error, never a silent skip: the
// failure this whole file exists to prevent is an enrollment that reports
// success and produces a credential nobody can authenticate with.
func adminFactorRecord(
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) (credential, spkiCredential []byte, metadata map[string]string, err error) {
	switch factor {
	case qrdbsdk.FactorFIDO2:
		credentialID := enrollment.Metadata["credential_id"]
		if credentialID == "" {
			return nil, nil, nil, &AdminFactorCredentialError{
				Factor: factor.String(),
				Reason: "enrollment carries no credential_id; the passkey could never be looked up at login",
			}
		}
		// A FIDO2 lookup key is the credential ID verbatim; qrdb does not
		// digest it, and there is no certificate to derive anything else from.
		return []byte(credentialID), nil, enrollment.Metadata, nil
	default:
		return pivAdminFactorRecord(factor, enrollment)
	}
}

// pivAdminFactorRecord derives what a PIV factor must carry to be usable.
//
// The enrollment's PublicKey is go-xkms's PEM encoding of the slot certificate
// (go-xkms@v0.9.1-alpha pkg/factor/piv/factor.go:147). Storing those bytes as
// the credential — which is what this did — indexed the factor under
// SHA-256(PEM), while the login prover presents SHA-256(SPKI) and
// SHA-256(cert.Raw) (cmd/quicbaas/canonical_auth_login.go:617-626). Those can
// never be equal, so the factor stored cleanly and no login could resolve it.
//
// Three things are required, and all three were missing:
//
//   - Credential = SHA-256(cert.Raw). It is both the DER lookup key the prover
//     offers as its alternate AND the value go-xkms's verifier re-derives and
//     compares (pkg/auth_factors/piv.go:107-109), so it is not a free choice.
//   - SPKICredential = SHA-256(RawSubjectPublicKeyInfo), the prover's PRIMARY
//     key and the renewal-stable one: a certificate reissued over the same
//     keypair keeps working.
//   - Metadata["cert_pem"], which the verifier decodes to get the certificate
//     at all; without it verification fails with MissingChallengeFieldError
//     even once the lookup resolves.
//
// The two digests come from pkg/iam's certificate helpers rather than being
// recomputed here. They are named for mTLS because that is where they were
// first needed, but the derivation is certificate-generic and deliberately
// shared: qrdb calls the SPKI digest "THE single SPKI binding algorithm for the
// whole stack", and a second implementation is how the two paths drift into
// storing different bytes for the same card.
func pivAdminFactorRecord(
	factor qrdbsdk.AdminFactor,
	enrollment *qrdbsdk.AuthFactorEnrollment,
) (credential, spkiCredential []byte, metadata map[string]string, err error) {
	if len(enrollment.PublicKey) == 0 {
		return nil, nil, nil, &AdminFactorCredentialError{
			Factor: factor.String(),
			Reason: "enrollment carries no public key material",
		}
	}

	cert, parseErr := parseAdminFactorCertificate(enrollment.PublicKey)
	if parseErr != nil {
		return nil, nil, nil, &AdminFactorCredentialError{
			Factor: factor.String(),
			Reason: "enrollment key material is not a certificate, so no login key can be derived: " + parseErr.Error(),
		}
	}

	credential = iam.MTLSCredentialKey(cert)
	spkiCredential = iam.MTLSSPKICredentialKey(cert)
	if len(credential) == 0 || len(spkiCredential) == 0 {
		return nil, nil, nil, &AdminFactorCredentialError{
			Factor: factor.String(),
			Reason: "certificate yielded no login key",
		}
	}

	// The caller's metadata is copied rather than mutated: enrollment is the
	// ceremony's record and a persister that edited it in place would change
	// what every later reader of that enrollment sees.
	metadata = make(map[string]string, len(enrollment.Metadata)+1)
	for k, v := range enrollment.Metadata {
		metadata[k] = v
	}
	metadata["cert_pem"] = string(pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: cert.Raw,
	}))
	return credential, spkiCredential, metadata, nil
}

// parseAdminFactorCertificate reads the enrollment's key material as a
// certificate, accepting PEM or bare DER.
//
// PEM is what go-xkms's PIV factor produces today. DER is accepted because the
// field is typed as opaque key material, and a caller that hands over the DER
// directly is not wrong — refusing it would make the persister depend on an
// encoding choice made in another module.
func parseAdminFactorCertificate(material []byte) (*x509.Certificate, error) {
	if block, _ := pem.Decode(material); block != nil {
		return x509.ParseCertificate(block.Bytes)
	}
	return x509.ParseCertificate(material)
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
