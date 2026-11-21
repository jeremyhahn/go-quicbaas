//go:build rest

// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Two nodes share one replicated store. A REST hosted rotation of the
// platform OIDC key on node B answers 403 with the typed refusal and leaves
// the shared version record and key material as they were;
// SigningKeyManager.Rotate still rotates it.
func TestOIDCSigningStore_RESTHostedRotationUnavailableWithoutDelegate(t *testing.T) {
	a, b, kv, before, kid := hostedRotationNodes(t)

	w := restHostedRotate(t, tenantctx.WithSystem(context.Background()), kid)
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), (&dbxkms.RotationDelegateUnavailableError{
		KID: kid, Backend: oidcSigningBackendName,
	}).Error())
	require.Equal(t, before, snapshotKV(t, kv))

	requireManagerRotates(t, a, b)
}

// restHostedRotate calls the hosted xkms rotate handler for kid as a caller
// whose request context is ctx.
func restHostedRotate(t *testing.T, ctx context.Context, kid string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/"+kid+"/rotate", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", kid)
	req = req.WithContext(context.WithValue(ctx, chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	(&rest.Server{}).HandleXKMSRotateKey(w, req)
	return w
}

// restRotateError is a REST hosted rotation's non-200 answer.
type restRotateError struct {
	Code    int
	Message string
}

func (e *restRotateError) Error() string {
	return http.StatusText(e.Code) + ": " + e.Message
}

// viaREST rotates through the hosted REST xkms rotate route.
var viaREST = hostedRotate{
	rotate: func(t *testing.T, ctx context.Context, kid string) error {
		w := restHostedRotate(t, ctx, kid)
		if w.Code == http.StatusOK {
			return nil
		}
		var body struct {
			Error string `json:"error"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), w.Body.String())
		return &restRotateError{Code: w.Code, Message: body.Error}
	},
}

func TestOIDCSigningStore_RESTHostedRotationRotatesThroughLease(t *testing.T) {
	requireHostedRotationRotatesThroughLease(t, viaREST)
}

func TestOIDCSigningStore_RESTConcurrentHostedRotationLosesLease(t *testing.T) {
	requireConcurrentHostedRotationLosesLease(t, viaREST)
}

func TestOIDCSigningStore_RESTUnauthorizedHostedRotationRefused(t *testing.T) {
	requireUnauthorizedHostedRotationRefused(t, viaREST)
	a, _, _, _, kid := hostedRotationNodes(t)
	useNodeRotation(t, a)
	w := restHostedRotate(t, tenantctx.WithTenant(context.Background(), "tenant-plain"), kid)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
}

func TestOIDCSigningStore_RESTDedicatedTenantHostedRotation(t *testing.T) {
	requireDedicatedTenantKeyRotates(t, viaREST)
}
