//go:build rest

// Copyright (c) 2025 Jeremy Hahn

package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api/rest"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// Two nodes share one replicated store. A REST hosted rotation of the
// platform OIDC key on node B answers 403 with the typed refusal and leaves
// the shared version record and key material as they were;
// SigningKeyManager.Rotate still rotates it.
func TestOIDCSigningStore_RESTHostedRotationRefusedOnPeer(t *testing.T) {
	a, b, kv, before, kid := hostedRotationNodes(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/"+kid+"/rotate", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", kid)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	w := httptest.NewRecorder()
	(&rest.Server{}).HandleXKMSRotateKey(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), (&dbxkms.OIDCSigningKeyRotationRefusedError{KID: kid}).Error())
	require.Equal(t, before, snapshotKV(t, kv))

	requireManagerRotates(t, a, b)
}
