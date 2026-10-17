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
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/rbactest"
)

// newGrantorTestServer serves IAM over the real adapter and an AppManager
// whose grantor gate lets the kv-reader role grant kv:read, and registers one
// app in t1.
func newGrantorTestServer(t *testing.T) (*IAMServer, *iam.AppManager, string) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	appManager := iam.NewAppManager(iam.NewMemoryStorage(), nil,
		iam.WithGrantorPermissions(rbactest.New(map[string][]string{"kv-reader": {"kv:read"}})))
	srv := NewIAMServer(NewIAMServiceAdapter(nil, nil, nil, appManager, nil, nil), log)
	app, err := srv.RegisterApp(callerContext("t1"), &iampb.RegisterAppRequest{
		TenantId: "t1", Name: "MyApp", RedirectUris: []string{"https://app.example/callback"},
	})
	require.NoError(t, err)
	return srv, appManager, app.ClientId
}

func TestIAMServer_UpdateApp_GrantableScopesLand(t *testing.T) {
	t.Parallel()
	srv, appManager, clientID := newGrantorTestServer(t)

	ctx := rbactest.CallerContext(callerContext("t1"), "alice", "t1", "kv-reader")
	_, err := srv.UpdateApp(ctx, &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: clientID, Name: "Renamed", Scopes: []string{"kv:read", "openid"},
	})
	require.NoError(t, err)

	app, err := appManager.GetAppInTenant(context.Background(), "t1", clientID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", app.Name)
	assert.Equal(t, []string{"kv:read", "openid"}, app.Scopes)
}

func TestIAMServer_UpdateApp_UngrantableScopeIsPermissionDenied(t *testing.T) {
	t.Parallel()
	srv, appManager, clientID := newGrantorTestServer(t)

	ctx := rbactest.CallerContext(callerContext("t1"), "alice", "t1", "kv-reader")
	_, err := srv.UpdateApp(ctx, &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: clientID, Name: "Renamed", Scopes: []string{"kv:read", "kv:write"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "got %v", err)

	app, err := appManager.GetAppInTenant(context.Background(), "t1", clientID)
	require.NoError(t, err)
	assert.Equal(t, "MyApp", app.Name)
	assert.Empty(t, app.Scopes)
}
