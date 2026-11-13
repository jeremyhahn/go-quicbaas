package iam

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// grantTypesTenant is the tenant newGrantTypesFixture registers its client in.
const grantTypesTenant = "tenant-grant-types"

// An end user in one tenant is never given a code for a client registered in
// another (#1922). The client and redirect URI are valid, so the refusal is a
// redirectable access_denied (RFC 6749 §4.1.2.1), not a direct error.
func TestOIDCProvider_Authorize_OtherTenantUserIsAccessDenied(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)
	req := f.authorizeRequest()
	req.TenantID = "tenant-other"

	resp, err := f.provider.Authorize(context.Background(), req)

	var mismatchErr *AuthorizeTenantMismatchError
	require.ErrorAs(t, err, &mismatchErr)
	var oauthErr *OAuthError
	require.ErrorAs(t, err, &oauthErr)
	assert.Equal(t, ErrOAuthAccessDenied, oauthErr.ErrorCode)
	assert.NotContains(t, oauthErr.ErrorDescription, grantTypesTenant, "the refusal names no tenant")
	var directErr *AuthorizeDirectError
	assert.NotErrorAs(t, err, &directErr, "a validated redirect URI gets a redirected error")
	assert.Nil(t, resp)
	assert.Empty(t, f.tokenStorage.authCodes, "no authorization code may be minted")
}

// An end user whose tenant is unknown is not in the client's tenant either:
// the check fails closed.
func TestOIDCProvider_Authorize_NoTenantUserIsAccessDenied(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)
	req := f.authorizeRequest()
	req.TenantID = ""

	resp, err := f.provider.Authorize(context.Background(), req)

	var mismatchErr *AuthorizeTenantMismatchError
	require.ErrorAs(t, err, &mismatchErr)
	assert.Nil(t, resp)
	assert.Empty(t, f.tokenStorage.authCodes, "no authorization code may be minted")
}

// An end user in the client's tenant is given a code.
func TestOIDCProvider_Authorize_SameTenantUserIssuesCode(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)
	req := f.authorizeRequest()
	req.TenantID = grantTypesTenant

	resp, err := f.provider.Authorize(context.Background(), req)

	require.NoError(t, err)
	assert.NotEmpty(t, resp.Code)
}

// An unregistered redirect URI is refused directly even for an end user in
// another tenant: the tenant check never runs before the redirect URI is
// validated, so no error is sent to an unvalidated URI.
func TestOIDCProvider_Authorize_OtherTenantBadRedirectIsDirect(t *testing.T) {
	t.Parallel()
	f := newGrantTypesFixture(t, GrantTypeAuthorizationCode)
	req := f.authorizeRequest()
	req.TenantID = "tenant-other"
	req.RedirectURI = "https://evil.example/cb"

	_, err := f.provider.Authorize(context.Background(), req)

	var directErr *AuthorizeDirectError
	require.ErrorAs(t, err, &directErr)
	assert.Equal(t, ErrOAuthInvalidRequest, directErr.ErrorCode)
	var mismatchErr *AuthorizeTenantMismatchError
	assert.NotErrorAs(t, err, &mismatchErr)
}
