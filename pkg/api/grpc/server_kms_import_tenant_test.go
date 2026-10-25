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

package grpc

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/kmstenant"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

const importedKeyName = "imported-key"

func importKeyRequest(algorithm pb.KeyAlgorithm) *pb.ImportKeyRequest {
	return &pb.ImportKeyRequest{
		Name:        importedKeyName,
		KeyMaterial: make([]byte, 32),
		KeySpec:     &pb.KeySpec{Algorithm: algorithm},
	}
}

func tenantCallerContext(tenantID string) context.Context {
	return contextWithAuthorizedSubject(context.Background(), &platformtoken.AuthorizedContext{
		Subject: "caller", TenantID: tenantID,
	})
}

func importTestLogger(t *testing.T) *logger.Logger {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	return log
}

// TestKMSServer_ImportKey_TenantScoped pins Codex 6800 P1 on #1814: a tenant
// import goes through the tenant-aware KMS service, so the key lands under
// tenant/{tenantID}/ and another tenant can neither see nor overwrite it.
func TestKMSServer_ImportKey_TenantScoped(t *testing.T) {
	mgr := kmstenant.NewManager()
	server := NewKMSServerWithService(mgr, kmstenant.NewServiceOn(t, mgr), importTestLogger(t))
	ctxA := tenantCallerContext(kmstenant.OwnerTenant)
	ctxB := tenantCallerContext(kmstenant.OtherTenant)
	scopedA := "tenant/" + kmstenant.OwnerTenant + "/" + importedKeyName

	resp, err := server.ImportKey(ctxA, importKeyRequest(pb.KeyAlgorithm_KEY_ALG_AES_256_GCM))
	require.NoError(t, err)
	require.Equal(t, importedKeyName, resp.Key.KeyId, "the caller sees its own unscoped name")

	storedA, err := mgr.GetKey(context.Background(), scopedA)
	require.NoError(t, err, "tenant A's key is stored under its namespace")
	_, err = mgr.GetKey(context.Background(), importedKeyName)
	require.Error(t, err, "nothing is stored in the shared namespace")

	_, err = server.GetKey(ctxA, &pb.GetKeyRequest{KeyId: importedKeyName})
	require.NoError(t, err, "tenant A reaches its imported key")
	_, err = server.GetKey(ctxB, &pb.GetKeyRequest{KeyId: importedKeyName})
	require.Equal(t, codes.NotFound, status.Code(err), "tenant B cannot see tenant A's key: %v", err)
	_, err = server.RotateKey(ctxB, &pb.RotateKeyRequest{KeyId: importedKeyName})
	require.Equal(t, codes.NotFound, status.Code(err), "tenant B cannot use tenant A's key: %v", err)

	_, err = server.ImportKey(ctxB, importKeyRequest(pb.KeyAlgorithm_KEY_ALG_AES_256_GCM))
	require.NoError(t, err)
	_, err = mgr.GetKey(context.Background(), "tenant/"+kmstenant.OtherTenant+"/"+importedKeyName)
	require.NoError(t, err, "tenant B's import lands in its own namespace")
	afterA, err := mgr.GetKey(context.Background(), scopedA)
	require.NoError(t, err)
	require.Same(t, storedA, afterA, "tenant B's import did not overwrite tenant A's key")
}

// TestKMSServer_ImportKey_Refusals covers the refused imports: a caller with
// no tenant, an unspecified algorithm, and a tenant caller on a server with
// no tenant-aware service. None of them stores a key.
func TestKMSServer_ImportKey_Refusals(t *testing.T) {
	log := importTestLogger(t)

	t.Run("service refuses a caller with no tenant", func(t *testing.T) {
		mgr := kmstenant.NewManager()
		server := NewKMSServerWithService(mgr, kmstenant.NewServiceOn(t, mgr), log)

		_, err := server.ImportKey(context.Background(), importKeyRequest(pb.KeyAlgorithm_KEY_ALG_AES_256_GCM))

		require.Equal(t, codes.PermissionDenied, status.Code(err), "error: %v", err)
		_, err = mgr.GetKey(context.Background(), importedKeyName)
		require.Error(t, err, "a refused import stores nothing")
	})

	t.Run("service refuses an unspecified algorithm", func(t *testing.T) {
		mgr := kmstenant.NewManager()
		server := NewKMSServerWithService(mgr, kmstenant.NewServiceOn(t, mgr), log)

		_, err := server.ImportKey(tenantCallerContext(kmstenant.OwnerTenant), importKeyRequest(pb.KeyAlgorithm_KEY_ALG_UNSPECIFIED))

		require.Equal(t, codes.InvalidArgument, status.Code(err), "error: %v", err)
	})

	t.Run("manager-only server refuses a tenant caller", func(t *testing.T) {
		mgr := kmstenant.NewManager()
		server := NewKMSServer(mgr, log)

		_, err := server.ImportKey(tenantCallerContext(kmstenant.OwnerTenant), importKeyRequest(pb.KeyAlgorithm_KEY_ALG_AES_256_GCM))

		var unscoped *UnscopedKeyImportError
		require.True(t, stderrors.As(err, &unscoped), "error: %v", err)
		require.Equal(t, kmstenant.OwnerTenant, unscoped.TenantID)
		require.Equal(t, codes.FailedPrecondition, status.Code(err))
		_, err = mgr.GetKey(context.Background(), importedKeyName)
		require.Error(t, err, "a refused import stores nothing")
	})

	t.Run("manager-only server imports for the system tenant", func(t *testing.T) {
		mgr := kmstenant.NewManager()
		server := NewKMSServer(mgr, log)

		_, err := server.ImportKey(tenantCallerContext(tenantctx.SystemTenantID), importKeyRequest(pb.KeyAlgorithm_KEY_ALG_AES_256_GCM))

		require.NoError(t, err)
		_, err = mgr.GetKey(context.Background(), importedKeyName)
		require.NoError(t, err, "the platform-scoped system tenant imports unscoped")
	})
}
