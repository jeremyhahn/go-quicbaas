// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package quicbaas

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// go-qrdb's first-boot and ceremony services, re-exported so an embedding
// host composes them from this SDK alone. They are aliases: a value of one
// is a value of the go-qrdb type. QuicBaaS's own ceremony facade stays in
// place alongside them.

// First boot: the always-runs path that creates the system tenant, the
// built-in roles and the administrator, and binds the admin certificate.
type (
	// QRDBFirstBootDependencies is go-qrdb's firstboot.Dependencies.
	QRDBFirstBootDependencies = qrdbsdk.FirstBootDependencies
	// QRDBFirstBootResult is go-qrdb's firstboot.Result.
	QRDBFirstBootResult = qrdbsdk.FirstBootResult
	// QRDBFirstBootAdminLookup is go-qrdb's firstboot.AdminLookup.
	QRDBFirstBootAdminLookup = qrdbsdk.FirstBootAdminLookup
	// QRDBFirstBootTenantCreator is go-qrdb's firstboot.TenantCreator.
	QRDBFirstBootTenantCreator = qrdbsdk.FirstBootTenantCreator
	// QRDBFirstBootRoleCreator is go-qrdb's firstboot.RoleCreator.
	QRDBFirstBootRoleCreator = qrdbsdk.FirstBootRoleCreator
	// QRDBFirstBootUserCreator is go-qrdb's firstboot.UserCreator.
	QRDBFirstBootUserCreator = qrdbsdk.FirstBootUserCreator
	// QRDBFirstBootCertProvider is go-qrdb's firstboot.CertProvider.
	QRDBFirstBootCertProvider = qrdbsdk.CertProvider
	// QRDBFirstBootCertSigner is go-qrdb's firstboot.CertSigner.
	QRDBFirstBootCertSigner = qrdbsdk.CertSigner
	// QRDBAdminCertBindingReason is go-qrdb's firstboot.AdminCertBindingReason.
	QRDBAdminCertBindingReason = qrdbsdk.AdminCertBindingReason
	// QRDBAdminCertBindingRequiredError is go-qrdb's
	// firstboot.AdminCertBindingRequiredError.
	QRDBAdminCertBindingRequiredError = qrdbsdk.AdminCertBindingRequiredError
)

var (
	// QRDBFirstBootInitialize runs go-qrdb's first boot (firstboot.Initialize).
	QRDBFirstBootInitialize = qrdbsdk.FirstBootInitialize
	// QRDBVerifyAdminCertBinding is go-qrdb's firstboot.VerifyAdminCertBinding.
	QRDBVerifyAdminCertBinding = qrdbsdk.VerifyAdminCertBinding
	// QRDBVerifyAdminCertBindingWithin is go-qrdb's
	// firstboot.VerifyAdminCertBindingWithin.
	QRDBVerifyAdminCertBindingWithin = qrdbsdk.VerifyAdminCertBindingWithin
)

// Ceremony: go-qrdb's initialization ceremony service.
type (
	// QRDBCeremonyService is go-qrdb's ceremony.CeremonyService.
	QRDBCeremonyService = qrdbsdk.CeremonyService
	// QRDBCeremonyConfig is go-qrdb's ceremony.CeremonyConfig.
	QRDBCeremonyConfig = qrdbsdk.CeremonyConfig
	// QRDBCeremonyState is go-qrdb's ceremony.CeremonyState.
	QRDBCeremonyState = qrdbsdk.CeremonyState
	// QRDBCeremonyInitResult is go-qrdb's service.CeremonyInitResult.
	QRDBCeremonyInitResult = qrdbsdk.CeremonyInitResult
	// QRDBCeremonyStatusInfo is go-qrdb's service.CeremonyStatusInfo.
	QRDBCeremonyStatusInfo = qrdbsdk.CeremonyStatusInfo
)

// QRDBCeremonyStateAwaitingInit is go-qrdb's ceremony.StateAwaitingInit.
const QRDBCeremonyStateAwaitingInit = qrdbsdk.StateAwaitingInit

var (
	// QRDBNewCeremonyService is go-qrdb's ceremony.NewCeremonyService.
	QRDBNewCeremonyService = qrdbsdk.NewCeremonyService
	// QRDBNewServiceCeremonyService is go-qrdb's service.NewCeremonyService,
	// the servicer the transports call.
	QRDBNewServiceCeremonyService = qrdbsdk.NewServiceCeremonyService

	// ErrQRDBCeremonyNilConfig is go-qrdb's ceremony.ErrNilConfig.
	ErrQRDBCeremonyNilConfig = qrdbsdk.ErrCeremonyNilConfig
	// ErrQRDBCeremonyNilBarrier is go-qrdb's ceremony.ErrNilBarrier.
	ErrQRDBCeremonyNilBarrier = qrdbsdk.ErrCeremonyNilBarrier
	// ErrQRDBCeremonyAlreadyInitialized is go-qrdb's
	// ceremony.ErrAlreadyInitialized.
	ErrQRDBCeremonyAlreadyInitialized = qrdbsdk.ErrCeremonyAlreadyInitialized
)
