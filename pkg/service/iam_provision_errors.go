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

import "strconv"

// ProvisionTenantIDRequiredError is returned when a provision request names
// no tenant. An empty id would resolve to the system tenant.
type ProvisionTenantIDRequiredError struct{}

func (e *ProvisionTenantIDRequiredError) Error() string {
	return "provision tenant: tenant_id is required"
}

// ProvisionAdminCSRRequiredError is returned when cert-mode provisioning has
// no admin CSR. QuicBaaS never generates the admin's key, so without a CSR
// there is nothing to sign.
type ProvisionAdminCSRRequiredError struct{}

func (e *ProvisionAdminCSRRequiredError) Error() string {
	return "provision tenant: cert enrollment requires admin_csr_pem"
}

// ProvisionConflictingEnrollmentError is returned when invite-mode
// provisioning also carries an admin CSR.
type ProvisionConflictingEnrollmentError struct{}

func (e *ProvisionConflictingEnrollmentError) Error() string {
	return "provision tenant: admin_csr_pem is not accepted with invite enrollment"
}

// ProvisionSystemTenantInviteError is returned when invite-mode provisioning
// targets the system tenant. The system admin is enrolled by first-boot only.
type ProvisionSystemTenantInviteError struct{}

func (e *ProvisionSystemTenantInviteError) Error() string {
	return "provision tenant: invite enrollment is refused for the system tenant"
}

// ProvisionEnrollmentModeError is returned for an unknown admin_enrollment.
type ProvisionEnrollmentModeError struct {
	// Mode is the rejected value.
	Mode string
}

func (e *ProvisionEnrollmentModeError) Error() string {
	return "provision tenant: unknown admin_enrollment " + strconv.Quote(e.Mode) + " (want cert or invite)"
}

// ProvisionInviteExpiryError is returned for a negative invite lifetime.
type ProvisionInviteExpiryError struct {
	// Hours is the rejected value.
	Hours int
}

func (e *ProvisionInviteExpiryError) Error() string {
	return "provision tenant: invite_expiry_hours must not be negative, got " + strconv.Itoa(e.Hours)
}

// ProvisionBarrierIsolationError is returned when provisioning asks for an
// isolation type whose IAM qrdb routes through the tenant's own barrier.
// Provisioning creates that barrier sealed and uninitialised, and qrdb has no
// control-plane store to hold the tenant admin until the admin initialises
// it, so the role seed and the admin write would be refused halfway.
type ProvisionBarrierIsolationError struct {
	// Isolation is the requested isolation type, with the empty default
	// reported as the barrier it stands for.
	Isolation string
}

func (e *ProvisionBarrierIsolationError) Error() string {
	return "provision tenant: isolation_type " + strconv.Quote(e.Isolation) +
		" keeps the tenant's IAM behind its own barrier, which is sealed until the tenant admin initialises it," +
		" and qrdb cannot yet hold the tenant admin outside that barrier; provision with isolation_type \"prefix\""
}

// ProvisionAdminCSRInvalidError is returned when the admin CSR is not a
// well-formed, self-signed PEM certificate request.
type ProvisionAdminCSRInvalidError struct {
	// Reason says which check failed.
	Reason string
	// Cause is the parser's or verifier's error, nil when there is none.
	Cause error
}

func (e *ProvisionAdminCSRInvalidError) Error() string {
	if e.Cause == nil {
		return "provision tenant: invalid admin_csr_pem: " + e.Reason
	}
	return "provision tenant: invalid admin_csr_pem: " + e.Reason + ": " + e.Cause.Error()
}

// Unwrap exposes the cause for errors.Is / errors.As.
func (e *ProvisionAdminCSRInvalidError) Unwrap() error { return e.Cause }

// ProvisionAdminCSRKeyError is returned when the admin CSR carries a public
// key outside the admin certificate key policy.
type ProvisionAdminCSRKeyError struct {
	// Key describes the refused key.
	Key string
}

func (e *ProvisionAdminCSRKeyError) Error() string {
	return "provision tenant: admin_csr_pem key " + e.Key +
		" is refused (want RSA of at least 2048 bits, ECDSA P-256/P-384/P-521, or Ed25519)"
}
