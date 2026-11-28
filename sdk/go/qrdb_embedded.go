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
	qrdbservice "github.com/jeremyhahn/go-qrdb/pkg/service"
	qrdbtimeseries "github.com/jeremyhahn/go-qrdb/pkg/timeseries"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// QRDBBackupServicer is a re-export of the qrdb service.BackupServicer
// interface. Required to enable Storage().QRDBBackup() (go-qrdb's
// BackupClient) through the embedded transport.
type QRDBBackupServicer = qrdbservice.BackupServicer

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

// QRDBCustodianGroupServicer is a re-export of the qrdb
// ceremony.CustodianGroupServicer interface. Required to enable custodian
// group operations through the embedded transport.
type QRDBCustodianGroupServicer = qrdbsdk.CustodianGroupServicer

// QRDBShareServicer is a re-export of the qrdb ceremony.ShareServicer
// interface. Required to enable Shamir share submission and tracking
// through the embedded transport.
type QRDBShareServicer = qrdbsdk.ShareServicer

// QRDBTimeSeriesServicer is a re-export of the qrdb
// timeseries.TimeSeriesServicer interface. Required to serve
// Search().QRDBTimeSeries() through the embedded transport.
type QRDBTimeSeriesServicer = qrdbtimeseries.TimeSeriesServicer

// QRDBUserAuthFactorServicer is a re-export of the qrdb
// service.UserAuthFactorServicer interface. Required to enable per-user
// multi-factor auth operations through the embedded transport.
type QRDBUserAuthFactorServicer = qrdbsdk.UserAuthFactorServicer

// QRDBUserServicer is a re-export of the qrdb user.UserServicer interface.
// Required so the embedded transport can resolve a subject (tenant +
// username) to a uint64 user id during user-login factor enrollment.
type QRDBUserServicer = qrdbsdk.UserServicer

// QRDBEmbeddingServicer is a re-export of the qrdb service.EmbeddingServicer
// interface. It serves the embedded transport's text embedding calls.
type QRDBEmbeddingServicer = qrdbservice.EmbeddingServicer

// QRDBNodeAuditLogger is a re-export of qrdb's NodeAuditLogger, go-qrdb's
// compliance-grade audit sink. It backs Security().QRDBAudit() on the
// embedded transport.
type QRDBNodeAuditLogger = qrdbsdk.NodeAuditLogger

// QRDBAuthFactorServicer is a re-export of the qrdb service.AuthFactorServicer
// interface. Required to enable authentication factor enrollment and
// verification through the embedded transport.
type QRDBAuthFactorServicer = qrdbsdk.AuthFactorServicer

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

	// CustodianGroup exposes the Shamir custodian-group management surface
	// (create groups, add/remove members, distribute shares). Optional.
	CustodianGroup QRDBCustodianGroupServicer

	// Share exposes Shamir share submission and tracking. Optional.
	Share QRDBShareServicer

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

	// Embedding serves AI().QRDBEmbedding() on the embedded transport. When
	// nil, the embedded client's embedding calls fail as unavailable in
	// go-qrdb.
	Embedding QRDBEmbeddingServicer

	// Backup exposes go-qrdb's backup service (create, list, get, delete,
	// restore, verify). Must be non-nil for Storage().QRDBBackup() calls to
	// succeed against the embedded transport. Optional otherwise.
	Backup QRDBBackupServicer
	// TimeSeries exposes the qrdb time-series service that backs
	// Search().QRDBTimeSeries(). Optional: without it the time-series
	// calls return an error.
	TimeSeries QRDBTimeSeriesServicer
	// Audit is the node's audit sink. When it is set, Security().QRDBAudit()
	// emits to and queries this sink on the embedded transport. When it is
	// nil, Security().QRDBAudit() answers Unavailable.
	Audit QRDBNodeAuditLogger
}
