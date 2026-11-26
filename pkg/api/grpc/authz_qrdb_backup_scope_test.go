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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// qrdbBackupMethods are the six qrdb.v1 backup RPCs, which act on every
// tenant's data and so admit only the system tenant.
var qrdbBackupMethods = []string{
	"/qrdb.v1.QRDBService/BackupCreate",
	"/qrdb.v1.QRDBService/BackupList",
	"/qrdb.v1.QRDBService/BackupGet",
	"/qrdb.v1.QRDBService/BackupDelete",
	"/qrdb.v1.QRDBService/BackupRestore",
	"/qrdb.v1.QRDBService/BackupVerify",
}

func TestQRDBBackupRPCs_RefuseNonSystemTenantToken(t *testing.T) {
	interceptor := UnaryAuthorizationInterceptor(&tenantTokenAuthorizer{token: "good", tenantID: "acme"})
	for _, method := range qrdbBackupMethods {
		t.Run(method, func(t *testing.T) {
			called := false
			_, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: method},
				func(context.Context, interface{}) (interface{}, error) { called = true; return "ok", nil })
			require.Error(t, err)
			assert.Equal(t, codes.PermissionDenied, status.Code(err))
			assert.Contains(t, status.Convert(err).Message(), "system tenant")
			assert.False(t, called, "handler must not run for a non-system tenant")
		})
	}
}

func TestQRDBBackupRPCs_AdmitSystemTenantToken(t *testing.T) {
	interceptor := UnaryAuthorizationInterceptor(&tenantTokenAuthorizer{token: "good", tenantID: qrdbsdk.SystemTenantID})
	for _, method := range qrdbBackupMethods {
		t.Run(method, func(t *testing.T) {
			called := false
			resp, err := interceptor(ctxWithBearer("good"), nil,
				&grpc.UnaryServerInfo{FullMethod: method},
				func(context.Context, interface{}) (interface{}, error) { called = true; return "ok", nil })
			require.NoError(t, err)
			assert.Equal(t, "ok", resp)
			assert.True(t, called, "handler must run for the system tenant")
		})
	}
}

func TestQRDBBackupMethods_MapCrossScopePermissions(t *testing.T) {
	crossScope := map[string]bool{
		string(qrdbsdk.PermissionBackupCreate):  true,
		string(qrdbsdk.PermissionBackupDelete):  true,
		string(qrdbsdk.PermissionBackupList):    true,
		string(qrdbsdk.PermissionBackupRead):    true,
		string(qrdbsdk.PermissionBackupRestore): true,
		string(qrdbsdk.PermissionBackupVerify):  true,
	}
	require.Len(t, systemTenantOnlyGRPCMethods, len(qrdbBackupMethods))
	for _, method := range qrdbBackupMethods {
		resource, action, ok := grpcResourceAndAction(method)
		require.True(t, ok, "%s must be mapped", method)
		assert.True(t, crossScope[resource+":"+action], "%s maps %s:%s, not a backup permission", method, resource, action)
		assert.True(t, isSystemTenantOnlyGRPCMethod(method), "%s must be system-tenant only", method)
	}
}
