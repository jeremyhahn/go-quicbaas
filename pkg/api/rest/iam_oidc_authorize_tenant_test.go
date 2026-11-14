//go:build rest

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// The provider's refusal of an end user in another tenant (#1922) comes after
// the client and redirect URI are validated, so the REST handler redirects it
// to the client with access_denied and the state, and no code (RFC 6749
// §4.1.2.1).
func TestAuthorize_TenantMismatchRedirectsAccessDenied(t *testing.T) {
	handler := setupAuthorizeHandler(t)
	svc, ok := handler.server.service.(*authorizeService)
	require.True(t, ok)
	req := &iam.AuthorizeRequest{
		ClientID:            "app_tenant-auth_web",
		RedirectURI:         "https://app.example/callback",
		ResponseType:        iam.ResponseTypeCode,
		State:               "xyz",
		CodeChallenge:       "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM",
		CodeChallengeMethod: iam.CodeChallengeMethodS256,
		UserID:              7,
		TenantID:            "tenant-other",
	}

	_, err := svc.provider.Authorize(context.Background(), req)
	var mismatchErr *iam.AuthorizeTenantMismatchError
	require.ErrorAs(t, err, &mismatchErr)

	w := httptest.NewRecorder()
	respondAuthorizeError(w, httptest.NewRequest(http.MethodGet, "/oauth2/authorize", nil),
		svc.provider.Issuer(), req, err)

	assertAuthorizeErrorRedirect(t, w, iam.ErrOAuthAccessDenied, "xyz")
}
