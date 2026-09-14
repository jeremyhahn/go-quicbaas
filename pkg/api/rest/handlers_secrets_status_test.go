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

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// TestRoutes_SecretStatusRouteExists is the regression for #280 stated at the
// level the defect lives at: the REST client builds
// `PUT /api/v1/secrets/{id}/status` and no route served it, so the call could
// not succeed however the handler behaved.
//
// It asserts against the ROUTE TABLE rather than by calling a handler, because
// a handler test passes whether or not anything routes to it — which is exactly
// how a client and a server disagreed about a path for as long as they did.
func TestRoutes_SecretStatusRouteExists(t *testing.T) {
	var found *RouteSpec
	for i, spec := range Routes() {
		if spec.Method == http.MethodPut && spec.Pattern == "/api/v1/secrets/*" {
			found = &Routes()[i]
			break
		}
	}
	require.NotNil(t, found,
		"no PUT route serves /api/v1/secrets/* — RESTClient.UpdateSecretStatus targets it")
	require.NotNil(t, found.Handler, "the status route has no handler binder")

	// The status route mutates a tenant's secret, so it carries the write
	// verb of go-qrdb's tenant-scoped secrets family, not a verb from the
	// system-only credentials family that guards qrdb's named-credential
	// store (go-quicbaas#534, routes_permissions_generated.go). The gRPC
	// mapping for SecretsManagement/UpdateSecretStatus and the MCP mapping
	// for quicbaas_secrets_update_status now name the same secrets:write
	// (go-quicbaas#682), each pinned in its own package; this assertion is
	// the REST end of that agreement.
	assert.Equal(t, "secrets", found.Resource)
	assert.Equal(t, "write", found.Action)
}

// TestHandleUpdateSecretStatus drives the handler over each status the model
// defines, including `deleted` — which enable/disable cannot express, and which
// is the reason this is a route rather than a third hardcoded suffix.
func TestHandleUpdateSecretStatus(t *testing.T) {
	tests := []struct {
		name           string
		path           string
		body           string
		setupMock      func(*testutil.MockQuicBaaS)
		expectedStatus int
	}{
		{
			name: "enabled",
			path: "app/database/password/status",
			body: `{"status":"enabled"}`,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.MatchedBy(func(req *service.UpdateSecretStatusRequest) bool {
					return req.SecretID == "app/database/password" && req.Status == service.SecretStatusEnabled
				})).Return(&service.UpdateSecretStatusResponse{
					SecretID: "app/database/password", NewStatus: service.SecretStatusEnabled, Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name: "disabled",
			path: "app/database/password/status",
			body: `{"status":"disabled"}`,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.MatchedBy(func(req *service.UpdateSecretStatusRequest) bool {
					return req.Status == service.SecretStatusDisabled
				})).Return(&service.UpdateSecretStatusResponse{
					SecretID: "app/database/password", NewStatus: service.SecretStatusDisabled, Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			// The case that makes this a route: /enable and /disable hardcode
			// one status each, so REST could not reach this one at all.
			name: "deleted, which enable/disable cannot express",
			path: "app/database/password/status",
			body: `{"status":"deleted"}`,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.MatchedBy(func(req *service.UpdateSecretStatusRequest) bool {
					return req.Status == service.SecretStatusDeleted
				})).Return(&service.UpdateSecretStatusResponse{
					SecretID: "app/database/password", NewStatus: service.SecretStatusDeleted, Timestamp: time.Now(),
				}, nil).Once()
			},
			expectedStatus: http.StatusOK,
		},
		{
			name:           "missing secret path",
			path:           "/status",
			body:           `{"status":"enabled"}`,
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "unknown status is refused, not forwarded",
			path:           "app/secret/status",
			body:           `{"status":"archived"}`,
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			// The zero value. Forwarding it would ask the service for a status
			// the model does not define while answering the caller with success.
			name:           "omitted status is refused, not defaulted",
			path:           "app/secret/status",
			body:           `{}`,
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "malformed body",
			path:           "app/secret/status",
			body:           `{`,
			setupMock:      func(m *testutil.MockQuicBaaS) {},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name: "service error surfaces",
			path: "app/secret/status",
			body: `{"status":"enabled"}`,
			setupMock: func(m *testutil.MockQuicBaaS) {
				m.On("UpdateSecretStatus", mock.Anything, mock.Anything).Return(nil, &errors.QBError{
					Code: errors.ErrNotFound, Op: "UpdateSecretStatus", Err: assert.AnError,
				}).Once()
			},
			expectedStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, mockService, cleanup := setupMockServer(t)
			defer cleanup()
			tt.setupMock(mockService)

			req := httptest.NewRequest(http.MethodPut, "/api/v1/secrets/"+tt.path, strings.NewReader(tt.body))
			w := httptest.NewRecorder()

			rctx := chi.NewRouteContext()
			rctx.URLParams.Add("*", tt.path)
			req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

			server.HandleUpdateSecretStatus(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "body: %s", w.Body.String())
			mockService.AssertExpectations(t)
		})
	}
}

// TestHandleSecretsPutAction_RefusesAnUnknownSubResource pins that the
// dispatcher does not fall through. A caller mistyping the sub-resource must be
// told so, not have some other secret operation performed for them.
func TestHandleSecretsPutAction_RefusesAnUnknownSubResource(t *testing.T) {
	server, mockService, cleanup := setupMockServer(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPut, "/api/v1/secrets/app/secret/statuss", strings.NewReader(`{"status":"enabled"}`))
	w := httptest.NewRecorder()
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("*", "app/secret/statuss")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	server.handleSecretsPutAction(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code, "body: %s", w.Body.String())
	mockService.AssertExpectations(t)
}

// TestSecretStatus_IsValid covers the rule the handler defers to.
func TestSecretStatus_IsValid(t *testing.T) {
	for _, valid := range []service.SecretStatus{
		service.SecretStatusEnabled, service.SecretStatusDisabled, service.SecretStatusDeleted,
	} {
		assert.True(t, valid.IsValid(), "%q must be a defined status", valid)
	}
	for _, invalid := range []service.SecretStatus{"", "archived", "ENABLED", "enabled "} {
		assert.False(t, invalid.IsValid(), "%q must not be a defined status", invalid)
	}
}
