//go:build rest

// Copyright (c) 2025 Jeremy Hahn

package rest

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// POST /api/v1/xkms/keys/{id}/rotate on the platform OIDC signing backend is
// refused with 403 and the typed error.
func TestXKMSRotateKey_RefusesOIDCSigningBackend(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	server, _, srvCleanup := setupMockServer(t)
	defer srvCleanup()

	kid := dbxkms.OIDCSigningBackendName + ":::oidc-signing"
	req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/keys/"+kid+"/rotate", nil)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), (&dbxkms.OIDCSigningKeyRotationRefusedError{KID: kid}).Error())
}
