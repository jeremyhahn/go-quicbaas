//go:build rest

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

package rest

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

func TestParseUserIDParam_AcceptsTheFullUint64Range(t *testing.T) {
	for _, want := range []uint64{1, 1 << 53, (1 << 53) + 1, math.MaxUint64} {
		got, err := parseUserIDParam(strconv.FormatUint(want, 10))
		require.NoError(t, err)
		assert.Equal(t, want, got)
	}
}

func TestParseUserIDParam_RefusesANonID(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr error
	}{
		{name: "non-numeric", value: "user-1", wantErr: strconv.ErrSyntax},
		{name: "negative", value: "-1", wantErr: strconv.ErrSyntax},
		{name: "overflowing", value: "18446744073709551616", wantErr: strconv.ErrRange},
		{name: "zero", value: "0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserIDParam(tt.value)
			assert.Zero(t, got)

			var idErr *UserIDParamError
			require.ErrorAs(t, err, &idErr)
			assert.Equal(t, tt.value, idErr.Value)
			if tt.wantErr != nil {
				assert.ErrorIs(t, err, tt.wantErr)
			} else {
				assert.NoError(t, errors.Unwrap(err))
			}
		})
	}
}

func TestRequireUserIDParam_ParsesThePathValue(t *testing.T) {
	req := withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
		map[string]string{"user_id": "9007199254740993"})
	w := httptest.NewRecorder()

	id, ok := requireUserIDParam(w, req)
	require.True(t, ok)
	assert.Equal(t, uint64(9007199254740993), id)
}

func TestRequireUserIDParam_RefusesAMissingOrInvalidValue(t *testing.T) {
	for _, value := range []string{"", "user-1", "18446744073709551616", "0"} {
		req := withChiParams(httptest.NewRequest(http.MethodGet, "/x", nil),
			map[string]string{"user_id": value})
		w := httptest.NewRecorder()

		id, ok := requireUserIDParam(w, req)
		assert.False(t, ok, "value %q", value)
		assert.Zero(t, id)
		assert.Equal(t, http.StatusBadRequest, w.Code, "value %q", value)
	}
}

func TestRequireTenantUserSubject_ResolvesTheVerifiedAccount(t *testing.T) {
	handler, cleanup := setupHandlerWithService(t, testutil.NewMockQuicBaaS())
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "alice", IDKind: interfaces.SubjectIDUsername, UserID: "30001",
		TenantID: "t1", Authenticated: true,
	}))
	w := httptest.NewRecorder()

	subject, id, ok := handler.server.requireTenantUserSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, uint64(30001), id)
	assert.Equal(t, "t1", subject.TenantID)
}

// A numeric login name is a name, not an account: "00042" must resolve to
// the account its credential verified, never to user 42.
func TestRequireTenantUserSubject_NeverReadsTheLoginNameAsAnAccount(t *testing.T) {
	handler, cleanup := setupHandlerWithService(t, testutil.NewMockQuicBaaS())
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "00042", IDKind: interfaces.SubjectIDUsername, UserID: "7",
		TenantID: "t1", Authenticated: true,
	}))
	w := httptest.NewRecorder()

	_, id, ok := handler.server.requireTenantUserSubject(w, req)
	require.True(t, ok)
	assert.Equal(t, uint64(7), id)
}

func TestRequireTenantUserSubject_RefusesACallerWithNoVerifiedAccount(t *testing.T) {
	handler, cleanup := setupHandlerWithService(t, testutil.NewMockQuicBaaS())
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "00042", TenantID: "t1", Authenticated: true,
	}))
	w := httptest.NewRecorder()

	_, id, ok := handler.server.requireTenantUserSubject(w, req)
	assert.False(t, ok)
	assert.Zero(t, id)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestRequireTenantUserSubject_ReportsAServiceThatCannotResolveCallers(t *testing.T) {
	svc := struct{ service.QuicBaaS }{testutil.NewMockQuicBaaS()}
	handler, cleanup := setupHandlerWithService(t, svc)
	defer cleanup()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req = req.WithContext(ctxWithTestSubject(context.Background(), "30001", "t1"))
	w := httptest.NewRecorder()

	_, _, ok := handler.server.requireTenantUserSubject(w, req)
	assert.False(t, ok)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestRequireTenantUserSubject_RefusesACallerThatIsNotAUser(t *testing.T) {
	handler, cleanup := setupHandlerWithService(t, testutil.NewMockQuicBaaS())
	defer cleanup()
	for _, subjectID := range []string{"svc-account", "18446744073709551616", "0"} {
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req = req.WithContext(ctxWithTestSubject(context.Background(), subjectID, "t1"))
		w := httptest.NewRecorder()

		_, id, ok := handler.server.requireTenantUserSubject(w, req)
		assert.False(t, ok, "subject %q", subjectID)
		assert.Zero(t, id)
		assert.Equal(t, http.StatusForbidden, w.Code, "subject %q", subjectID)
	}
}
