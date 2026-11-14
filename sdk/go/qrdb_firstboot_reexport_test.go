// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package quicbaas

import (
	"context"
	"errors"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// The re-exported first-boot and ceremony types are go-qrdb's own: a value
// of one is a value of the other, with no conversion.
var (
	_ qrdbsdk.FirstBootDependencies          = QRDBFirstBootDependencies{}
	_ *qrdbsdk.FirstBootResult               = (*QRDBFirstBootResult)(nil)
	_ qrdbsdk.FirstBootAdminLookup           = QRDBFirstBootAdminLookup(nil)
	_ qrdbsdk.FirstBootTenantCreator         = QRDBFirstBootTenantCreator(nil)
	_ qrdbsdk.FirstBootRoleCreator           = QRDBFirstBootRoleCreator(nil)
	_ qrdbsdk.FirstBootUserCreator           = QRDBFirstBootUserCreator(nil)
	_ qrdbsdk.CertProvider                   = QRDBFirstBootCertProvider(nil)
	_ qrdbsdk.CertSigner                     = QRDBFirstBootCertSigner(nil)
	_ *qrdbsdk.CeremonyService               = (*QRDBCeremonyService)(nil)
	_ *qrdbsdk.CeremonyConfig                = (*QRDBCeremonyConfig)(nil)
	_ qrdbsdk.CeremonyState                  = QRDBCeremonyStateAwaitingInit
	_ *qrdbsdk.CeremonyInitResult            = (*QRDBCeremonyInitResult)(nil)
	_ *qrdbsdk.CeremonyStatusInfo            = (*QRDBCeremonyStatusInfo)(nil)
	_ qrdbsdk.AdminCertBindingReason         = QRDBAdminCertBindingReason("")
	_ *qrdbsdk.AdminCertBindingRequiredError = (*QRDBAdminCertBindingRequiredError)(nil)
)

// TestQRDBFirstBootInitialize_ReExportCallsGoQRDB: the SDK's first-boot entry
// point is go-qrdb's; called with no dependencies it refuses rather than
// bootstrapping anything.
func TestQRDBFirstBootInitialize_ReExportCallsGoQRDB(t *testing.T) {
	res, err := QRDBFirstBootInitialize(context.Background(), QRDBFirstBootDependencies{})
	if err == nil {
		t.Fatalf("QRDBFirstBootInitialize with no dependencies = %+v, want an error", res)
	}
}

// TestQRDBNewCeremonyService_ReExportCallsGoQRDB: the SDK's ceremony
// constructor is go-qrdb's, and its sentinel matches.
func TestQRDBNewCeremonyService_ReExportCallsGoQRDB(t *testing.T) {
	_, err := QRDBNewCeremonyService(nil, nil, nil, nil)
	if !errors.Is(err, ErrQRDBCeremonyNilConfig) {
		t.Fatalf("QRDBNewCeremonyService(nil...) = %v, want ErrQRDBCeremonyNilConfig", err)
	}
	if !errors.Is(err, qrdbsdk.ErrCeremonyNilConfig) {
		t.Fatalf("ErrQRDBCeremonyNilConfig is not go-qrdb's ErrCeremonyNilConfig: %v", err)
	}
}
