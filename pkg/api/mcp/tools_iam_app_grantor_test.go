//go:build mcp

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

package mcp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/rbactest"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// newGrantorToolRegistry returns a registry whose IAM service updates apps
// through an AppManager with the grantor gate, where the kv-reader role
// grants kv:read, and the client ID of one app registered in t1.
func newGrantorToolRegistry(t *testing.T) (*ToolRegistry, *iam.AppManager, string) {
	t.Helper()
	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil,
		iam.WithGrantorPermissions(rbactest.New(map[string][]string{"kv-reader": {"kv:read"}})))
	app, err := apps.RegisterApp(context.Background(), "t1", iam.RegisterAppOptions{
		Name: "MyApp", RedirectURIs: []string{"https://app.example/callback"},
	})
	require.NoError(t, err)
	reg := newIAMTestRegistry(&mockIAMService{
		updateAppFunc: func(ctx context.Context, req *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
			updated, err := apps.UpdateAppInTenant(ctx, "t1", req.ClientID, req.Options())
			if err != nil {
				return nil, err
			}
			return &service.UpdateAppResponse{App: updated}, nil
		},
	})
	return reg, apps, app.ClientID
}

func TestAppUpdate_GrantableScopesLand(t *testing.T) {
	reg, apps, clientID := newGrantorToolRegistry(t)

	ctx := rbactest.CallerContext(context.Background(), "alice", "t1", "kv-reader")
	_, err := reg.executeIAMTool(ctx, "quicbaas_iam_app_update", map[string]interface{}{
		"client_id": clientID, "name": "Renamed", "scopes": []interface{}{"kv:read", "openid"},
	})
	require.NoError(t, err)

	app, err := apps.GetAppInTenant(context.Background(), "t1", clientID)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", app.Name)
	assert.Equal(t, []string{"kv:read", "openid"}, app.Scopes)
}

func TestAppUpdate_UngrantableScopeIsPermissionDenied(t *testing.T) {
	reg, apps, clientID := newGrantorToolRegistry(t)

	ctx := rbactest.CallerContext(context.Background(), "alice", "t1", "kv-reader")
	_, err := reg.executeIAMTool(ctx, "quicbaas_iam_app_update", map[string]interface{}{
		"client_id": clientID, "name": "Renamed", "scopes": []interface{}{"kv:read", "kv:write"},
	})
	var notGrantable *iam.ScopeNotGrantableError
	require.ErrorAs(t, err, &notGrantable)
	assert.Equal(t, mcptransport.MCPPermissionDenied, toolErrorCode(err))

	app, err := apps.GetAppInTenant(context.Background(), "t1", clientID)
	require.NoError(t, err)
	assert.Equal(t, "MyApp", app.Name)
	assert.Empty(t, app.Scopes)
}
