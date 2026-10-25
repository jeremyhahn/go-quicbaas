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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/middleware"
	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"

	restapi "github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	quicbaaspermission "github.com/jeremyhahn/go-quicbaas/pkg/permission"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

const spendReportPattern = "/api/v1/orchestrator/cost"

// spendReportHandler serves the spend report route through the module's own
// registration and qrdb's REST middleware, with one bearer token holding
// orchestrator:status alone and another holding spend_report:read.
func spendReportHandler(t *testing.T) http.Handler {
	t.Helper()

	restSrv, err := restapi.NewServer(testutil.NewMockQuicBaaS(), restapi.DefaultConfig(), newTestLogger(t))
	require.NoError(t, err)
	restSrv.SetModuleTokenAuthorizer(&fakeTokenAuthorizer{tokens: map[string]fakeToken{
		"status-token": {subject: "svc-app", tenant: "tenant-a", scopes: []string{"orchestrator:status"}},
		"spend-token":  {subject: "svc-app", tenant: "tenant-a", scopes: []string{string(quicbaaspermission.SpendReportRead)}},
	}})
	m := newHTTPModule(t, func() *restapi.Server { return restSrv })

	registrar, recs := newCaptureRegistrar()
	m.RegisterHTTP(registrar)
	var route *captured
	for i := range *recs {
		rec := &(*recs)[i]
		if rec.method == http.MethodGet && rec.pattern == spendReportPattern {
			route = rec
		}
	}
	require.NotNil(t, route, "the module does not register %s", spendReportPattern)
	require.Equal(t, quicbaaspermission.SpendReportRead, route.perm)

	am := middleware.New(rbac.New(&fakeRoleServicer{}),
		middleware.WithAuthenticator(neverAuthenticates{}),
		middleware.WithRoleResolver(&fakeRoleServicer{}),
		middleware.WithUserService(&fakeUserServicer{users: map[string]*user.User{}}),
	)
	routes := middleware.NewRESTRouteRegistry()
	routes.Register(http.MethodGet, spendReportPattern, permission.Permission(route.perm))
	routes.SetFallback(http.MethodGet, spendReportPattern, m)
	return am.RESTMiddleware(routes, route.handler)
}

// TestSpendReport_RequiresSpendReportRead pins go-quicbaas#885 on REST (and
// QUIC, which serves the same route table): the spend report enumerates the
// tenant's attribution tree and account roster, so orchestrator:status alone
// is refused. The served request carries a malformed round so the handler
// answers 400 on its own, proving authorization let it through.
func TestSpendReport_RequiresSpendReportRead(t *testing.T) {
	handler := spendReportHandler(t)

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"session status alone is refused", "status-token", http.StatusForbidden},
		{"spend report read is served", "spend-token", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, spendReportPattern+"?round=later", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			assert.Equal(t, tc.want, w.Code, "body: %s", w.Body.String())
		})
	}
}
