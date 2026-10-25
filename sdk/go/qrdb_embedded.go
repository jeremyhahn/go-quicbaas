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

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// QRDB Embedded Servicer Type Re-exports
// =============================================================================
//
// These aliases let QuicBaaS SDK consumers populate QRDBEmbeddedServicers
// without importing go-qrdb/pkg/service, go-qrdb/pkg/ceremony, or
// go-qrdb/pkg/tenant directly.

// QRDBKVServicer is a re-export of the qrdb service.KVServicer interface.
// Required field on QRDBEmbeddedServicers.
type QRDBKVServicer = qrdbsdk.KVServicer

// QRDBAdminServicer is a re-export of the qrdb service.AdminServicer interface.
// Required field on QRDBEmbeddedServicers.
type QRDBAdminServicer = qrdbsdk.AdminServicer

// QRDBBarrierServicer is a re-export of the qrdb service.BarrierServicer
// interface. Required to enable Security().Barrier() embedded dispatch.
type QRDBBarrierServicer = qrdbsdk.BarrierServicer

// QRDBCeremonyServicer is a re-export of the qrdb service.CeremonyServicer
// interface. Required to enable Security().Ceremony() embedded dispatch.
type QRDBCeremonyServicer = qrdbsdk.ServiceCeremonyServicer

// QRDBCredentialServicer is a re-export of the qrdb service.CredentialServicer
// interface. Required to enable Security().Credentials() embedded dispatch.
type QRDBCredentialServicer = qrdbsdk.ServiceCredentialServicer

// QRDBTenantServicer is a re-export of the qrdb tenant.TenantServicer
// interface. Required to enable per-tenant barrier operations through the
// embedded transport.
type QRDBTenantServicer = qrdbsdk.ServiceTenantServicer

// QRDBUserAuthFactorServicer is a re-export of the qrdb
// service.UserAuthFactorServicer interface. Required to enable per-user
// multi-factor auth operations through the embedded transport.
type QRDBUserAuthFactorServicer = qrdbsdk.UserAuthFactorServicer

// QRDBUserServicer is a re-export of the qrdb user.UserServicer interface.
// Required so the embedded transport can resolve a subject (tenant +
// username) to a uint64 user id during user-login factor enrollment.
type QRDBUserServicer = qrdbsdk.UserServicer

// QRDBAuthFactorServicer is a re-export of the qrdb service.AuthFactorServicer
// interface. Required to enable authentication factor enrollment and
// verification through the embedded transport.
type QRDBAuthFactorServicer = qrdbsdk.AuthFactorServicer

// QRDBIndexServicer is a re-export of the qrdb service.IndexServicer
// interface. Required so a qrdb DAO with secondary indexes (for example the
// WebAuthn credential store) can create and query its indexes through the
// embedded transport. Build one with qrdbsdk.NewIndexService.
type QRDBIndexServicer = qrdbsdk.IndexServicer

// QRDBCeremonyCA is a re-export of the qrdb ceremony.CeremonyCA interface.
// Optional companion to QRDBCeremonyServicer that supplies the CA used by
// the embedded ceremony to mint officer / share certificates during
// initialization.
type QRDBCeremonyCA = qrdbsdk.CeremonyCA

// QRDBCeremonySignCSROptions is a re-export of the qrdb ceremony.SignCSROptions
// type. Consumers implementing a QRDBCeremonyCA bridge (e.g. delegating to an
// external CA) use it as the SignCSR parameter type.
type QRDBCeremonySignCSROptions = qrdbsdk.SignCSROptions

// QRDBCeremonyIssueCertRequest is a re-export of the qrdb ceremony.IssueCertRequest
// type. Consumers implementing a QRDBCeremonyCA bridge use it as the
// IssueCertificate parameter type.
type QRDBCeremonyIssueCertRequest = qrdbsdk.IssueCertRequest

// QRDBCeremonyIssuedCert is a re-export of the qrdb ceremony.IssuedCert type.
// Consumers implementing a QRDBCeremonyCA bridge return it from IssueCertificate.
type QRDBCeremonyIssuedCert = qrdbsdk.IssuedCert

// =============================================================================
// QRDBEmbeddedServicers
// =============================================================================

// QRDBEmbeddedServicers packages the qrdb-specific service implementations
// the embedded transport requires. Only the servicers a caller actually uses
// need be non-nil — the qrdb embedded transport returns a typed
// NilServiceError for any operation against an absent servicer, so consumers
// can opt in to exactly the surface they care about.
//
// KV and Admin are mandatory because the qrdb embedded transport constructor
// takes them as positional arguments; every other servicer is wired via a
// functional option and is therefore optional.
//
// Pair with WithEmbeddedQRDB to install the servicers into a QuicBaaS SDK
// client built with WithEmbedded. Without WithEmbeddedQRDB, calls to
// Security().Barrier(), Security().Ceremony(), Security().Credentials(), and
// other qrdb-backed accessors return an explicit configuration error rather
// than falling through to the network transport.
type QRDBEmbeddedServicers struct {
	// KV is the qrdb KVServicer used for key-value operations.
	// REQUIRED: positional argument to the qrdb embedded transport constructor.
	KV QRDBKVServicer

	// Admin is the qrdb AdminServicer used for cluster admin operations.
	// REQUIRED: positional argument to the qrdb embedded transport constructor.
	Admin QRDBAdminServicer

	// Barrier exposes the qrdb StorageBarrier lifecycle (initialize, unseal,
	// seal, status, rekey, Shamir share management). Must be non-nil for
	// Security().Barrier() calls to succeed against the embedded transport.
	Barrier QRDBBarrierServicer

	// Ceremony exposes the qrdb initialization ceremony state machine
	// (officer enrollment, share distribution, CSR signing). Must be non-nil
	// for Security().Ceremony() calls to succeed against the embedded transport.
	Ceremony QRDBCeremonyServicer

	// CeremonyCA is the CA used during ceremony initialization to mint
	// officer and share certificates. Optional; required only when the
	// ceremony Initialize call needs to issue certificates.
	CeremonyCA QRDBCeremonyCA

	// Credential exposes the qrdb named-credential service (submit,
	// retrieve, list, rotate, delete). Must be non-nil for
	// Security().Credentials() calls to succeed against the embedded transport.
	Credential QRDBCredentialServicer

	// Tenant exposes per-tenant lifecycle operations including the
	// per-tenant barrier (init / unseal / seal / status). Optional.
	Tenant QRDBTenantServicer

	// AuthFactor exposes authentication factor enrollment and verification.
	// Optional.
	AuthFactor QRDBAuthFactorServicer

	// UserAuthFactor exposes per-user multi-factor authentication
	// management. Optional.
	UserAuthFactor QRDBUserAuthFactorServicer

	// User exposes the qrdb user store (create / get / GetUserByUsername /
	// cert-binding). Required for user-login factor enrollment so the SDK
	// can resolve a subject (tenant + username) to a uint64 user id and
	// enroll the factor for that exact user. Optional otherwise.
	User QRDBUserServicer

	// Index exposes qrdb secondary-index operations (create / query).
	// Required by any qrdb DAO that declares indexes, such as the WebAuthn
	// credential store behind passkey enrollment and login. Optional
	// otherwise.
	Index QRDBIndexServicer
}
