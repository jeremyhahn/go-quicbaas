//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestXKMSCertByID_BindsCallerToIssuingTenant covers GET, HEAD and DELETE
// /api/v1/xkms/certs/{id}. The go-xkms key store records no tenant, so a
// tenant caller reaches a certificate only when the CA issued it for that
// tenant; a self-signed certificate saved under a key ID is the system's.
func TestXKMSCertByID_BindsCallerToIssuingTenant(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	const keyID = "tenant-bound-cert"
	saveBody, err := json.Marshal(map[string]string{"certificate_pem": selfSignedCertPEM(t)})
	require.NoError(t, err)
	saveReq := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/certs?key_id="+keyID, bytes.NewReader(saveBody)).WithContext(tenantctx.WithSystem(context.Background()))
	saveReq.Header.Set("Content-Type", "application/json")
	saveW := httptest.NewRecorder()
	server.router.ServeHTTP(saveW, saveReq)
	require.Equal(t, http.StatusOK, saveW.Code)

	serve := func(ctx context.Context, method string, handler http.HandlerFunc) *httptest.ResponseRecorder {
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("id", keyID)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, routeCtx)
		req := httptest.NewRequest(method, "/api/v1/xkms/certs/"+keyID, nil).WithContext(ctx)
		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}
	acme := tenantctx.WithTenant(context.Background(), "acme")
	system := tenantctx.WithSystem(context.Background())

	t.Run("tenant cannot read a certificate not issued for it", func(t *testing.T) {
		w := serve(acme, http.MethodGet, server.HandleXKMSGetCert)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.NotContains(t, w.Body.String(), "BEGIN CERTIFICATE")
	})
	t.Run("tenant cannot probe a certificate not issued for it", func(t *testing.T) {
		require.Equal(t, http.StatusNotFound, serve(acme, http.MethodHead, server.HandleXKMSCertExists).Code)
	})
	t.Run("tenant cannot delete a certificate not issued for it", func(t *testing.T) {
		require.Equal(t, http.StatusNotFound, serve(acme, http.MethodDelete, server.HandleXKMSDeleteCert).Code)
		require.Equal(t, http.StatusOK, serve(system, http.MethodHead, server.HandleXKMSCertExists).Code)
	})
	t.Run("caller without a tenant is refused", func(t *testing.T) {
		require.Equal(t, http.StatusNotFound, serve(context.Background(), http.MethodGet, server.HandleXKMSGetCert).Code)
	})
	t.Run("system reads and deletes the certificate", func(t *testing.T) {
		require.Equal(t, http.StatusOK, serve(system, http.MethodGet, server.HandleXKMSGetCert).Code)
		require.Equal(t, http.StatusOK, serve(system, http.MethodDelete, server.HandleXKMSDeleteCert).Code)
		require.Equal(t, http.StatusNotFound, serve(system, http.MethodHead, server.HandleXKMSCertExists).Code)
	})
}
