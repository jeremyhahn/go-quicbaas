//go:build rest

package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestXKMSPIV_TenantCallerCannotTouchPlatformSlots pins Codex 7955 / #1996 on
// REST: the six PIV handlers run through the service layer's PIVService, so a
// tenant caller is refused by the platform-scope check and the platform slot
// keeps its certificate, while the platform caller still reaches the slot.
func TestXKMSPIV_TenantCallerCannotTouchPlatformSlots(t *testing.T) {
	const backend = "file"
	const slot = "9a"

	cleanup := initXKMSForTest(t)
	defer cleanup()

	memory, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	store, err := xkmssdk.NewPIVFileBackend(&xkmssdk.PIVFileStorageConfig{Backend: memory, DEREnabled: true})
	require.NoError(t, err)
	require.NoError(t, xkmssdk.InitializePIV(&xkmssdk.PIVManagerConfig{
		Stores: map[string]xkmssdk.PIVCertificateStorage{backend: store},
	}))
	t.Cleanup(xkmssdk.XKMSResetPIV)

	server, mockService, srvCleanup := setupMockServer(t)
	defer srvCleanup()
	mockService.PIVSvc = service.NewPIVService(func() *xkmssdk.XKMSService { return &xkmssdk.XKMSService{} })

	systemCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "platform-admin", TenantID: qrdbsdk.SystemTenantID})
	tenantCtx := qrdbsdk.WithIdentity(context.Background(),
		&qrdbsdk.Identity{Subject: "acme-admin", TenantID: "acme"})

	serve := func(ctx context.Context, method, target string, body any, handler http.HandlerFunc) *httptest.ResponseRecorder {
		t.Helper()
		var payload []byte
		if body != nil {
			payload, err = json.Marshal(body)
			require.NoError(t, err)
		}
		routeCtx := chi.NewRouteContext()
		routeCtx.URLParams.Add("slot", slot)
		ctx = context.WithValue(ctx, chi.RouteCtxKey, routeCtx)
		req := httptest.NewRequest(method, target, bytes.NewReader(payload)).WithContext(ctx)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler(w, req)
		return w
	}
	certURL := "/api/v1/xkms/piv/slots/" + slot + "/cert?backend=" + backend
	readBack := func() []byte {
		t.Helper()
		w := serve(systemCtx, http.MethodGet, certURL+"&format=der", nil, server.HandleXKMSGetPIVCertificate)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp transportTypes.GetPIVCertificateResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.Certificate
	}

	stored := pivTestCertDER(t)
	w := serve(systemCtx, http.MethodPut, certURL, &transportTypes.StorePIVCertificateRequest{
		Backend: backend, Certificate: stored, Format: "der",
	}, server.HandleXKMSStorePIVCertificate)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, stored, readBack())

	verbs := []struct {
		name    string
		method  string
		target  string
		body    any
		handler http.HandlerFunc
	}{
		{"ListPIVSlots", http.MethodGet, "/api/v1/xkms/piv/slots?backend=" + backend, nil, server.HandleXKMSListPIVSlots},
		{"GetPIVCertificate", http.MethodGet, certURL + "&format=der", nil, server.HandleXKMSGetPIVCertificate},
		{"StorePIVCertificate", http.MethodPut, certURL, &transportTypes.StorePIVCertificateRequest{
			Backend: backend, Certificate: pivTestCertDER(t), Format: "der",
		}, server.HandleXKMSStorePIVCertificate},
		{"DeletePIVCertificate", http.MethodDelete, certURL, nil, server.HandleXKMSDeletePIVCertificate},
		{"GeneratePIVKey", http.MethodPost, "/api/v1/xkms/piv/slots/" + slot + "/key", &transportTypes.GeneratePIVKeyRequest{
			Backend: backend, Algorithm: "ecdsap256", Subject: "acme",
		}, server.HandleXKMSGeneratePIVKey},
		{"GeneratePIVCSR", http.MethodPost, "/api/v1/xkms/piv/slots/" + slot + "/csr", &transportTypes.GeneratePIVCSRRequest{
			Backend: backend, Subject: "acme",
		}, server.HandleXKMSGeneratePIVCSR},
	}
	for _, verb := range verbs {
		t.Run("tenant refused on "+verb.name, func(t *testing.T) {
			w := serve(tenantCtx, verb.method, verb.target, verb.body, verb.handler)
			require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
			require.NotContains(t, w.Body.String(), "certificate\":")
			require.Equal(t, stored, readBack(), "tenant caller %s changed the platform PIV slot", verb.name)
		})
	}

	t.Run("platform caller lists and deletes the slot", func(t *testing.T) {
		w := serve(systemCtx, http.MethodGet, "/api/v1/xkms/piv/slots?backend="+backend, nil, server.HandleXKMSListPIVSlots)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		w = serve(systemCtx, http.MethodDelete, certURL, nil, server.HandleXKMSDeletePIVCertificate)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}

// TestXKMSPIV_ServiceNotWired verifies the PIV handlers fail closed with 503
// when the service facade carries no PIV service.
func TestXKMSPIV_ServiceNotWired(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()

	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/v1/xkms/piv/slots", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// pivTestCertDER returns a fresh self-signed certificate in DER form.
func pivTestCertDER(t *testing.T) []byte {
	t.Helper()
	block, _ := pem.Decode([]byte(selfSignedCertPEM(t)))
	require.NotNil(t, block)
	return block.Bytes
}
