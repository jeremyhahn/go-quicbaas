//go:build rest

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

package apimodule

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const messagingWatchPattern = "/api/v1/messaging/watch"

// messagingWatchHandler serves POST /api/v1/messaging/watch the way a
// composed node does: the module registers the route with the permission
// the route table declares, and go-qrdb's RBAC middleware enforces it in
// front of the real REST handler backed by an in-memory messaging service.
func messagingWatchHandler(t *testing.T) http.Handler {
	t.Helper()

	svc := messaging.NewPushService(newTestLogger(t), nil)
	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))
	t.Cleanup(func() {
		assert.NoError(t, svc.Stop(ctx))
	})

	mockSvc := testutil.NewMockQuicBaaS()
	mockSvc.On("Messaging").Return(svc)
	restSrv, err := restapi.NewServer(mockSvc, restapi.DefaultConfig(), newTestLogger(t))
	require.NoError(t, err)
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		"watch-token":     {subject: "svc-app", tenant: "tenant-a", scopes: []string{"messaging:watch"}},
		"subscribe-token": {subject: "svc-app", tenant: "tenant-a", scopes: []string{"messaging:subscribe"}},
	}})
	m := newHTTPModule(t, func() *restapi.Server { return restSrv })

	registrar, recs := newCaptureRegistrar()
	m.RegisterHTTP(registrar)
	var route *captured
	for i := range *recs {
		rec := &(*recs)[i]
		if rec.method == http.MethodPost && rec.pattern == messagingWatchPattern {
			route = rec
		}
	}
	require.NotNil(t, route, "the module does not register %s", messagingWatchPattern)
	require.Equal(t, "messaging:watch", string(route.perm))

	am := middleware.New(rbac.New(&fakeRoleServicer{}),
		middleware.WithAuthenticator(neverAuthenticates{}),
		middleware.WithRoleResolver(&fakeRoleServicer{}),
		middleware.WithUserService(&fakeUserServicer{users: map[string]*user.User{}}),
	)
	routes := middleware.NewRESTRouteRegistry()
	routes.Register(http.MethodPost, messagingWatchPattern, permission.Permission(route.perm))
	routes.SetFallback(http.MethodPost, messagingWatchPattern, m)
	return am.RESTMiddleware(routes, route.handler)
}

// TestMessagingWatch_RequiresWatchPermission pins that the watch route is
// refused to a caller holding only messaging:subscribe and served to one
// holding messaging:watch.
func TestMessagingWatch_RequiresWatchPermission(t *testing.T) {
	handler := messagingWatchHandler(t)

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"subscribe grant is refused", "subscribe-token", http.StatusForbidden},
		{"watch grant is served", "watch-token", http.StatusCreated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, messagingWatchPattern,
				strings.NewReader(`{"key_prefix":"orders/"}`))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}
