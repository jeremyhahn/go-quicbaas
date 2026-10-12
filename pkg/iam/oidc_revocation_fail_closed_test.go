package iam

import (
	"context"
	"errors"
	"testing"
	"time"
)

var errRevocationDown = errors.New("revocation store unavailable")

// revocationDownStorage delegates everything to the wrapped store except the
// revocation lookup, which fails: the store knows the token but cannot say
// whether it was revoked.
type revocationDownStorage struct {
	TokenStorage
}

func (revocationDownStorage) IsTokenRevoked(context.Context, string) (bool, error) {
	return false, errRevocationDown
}

func TestValidateAccessToken_RevocationStoreErrorDeniesToken(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	ctx := context.Background()
	token, _ := mintIntrospectToken(t, provider, "tenant-a", "fc-user", "", time.Hour)

	if _, err := provider.ValidateAccessToken(ctx, token); err != nil {
		t.Fatalf("ValidateAccessToken with a healthy store: %v", err)
	}

	provider.tokenService.storage = revocationDownStorage{TokenStorage: provider.tokenService.storage}

	claims, err := provider.ValidateAccessToken(ctx, token)
	var checkErr *TokenRevocationCheckError
	if !errors.As(err, &checkErr) || !errors.Is(err, errRevocationDown) {
		t.Fatalf("error = %v, want TokenRevocationCheckError wrapping the store error", err)
	}
	if claims != nil {
		t.Fatalf("claims = %v, want nil when the revocation store errors", claims)
	}
}

func TestRefreshSession_RevocationStoreErrorMintsNothing(t *testing.T) {
	provider, _ := newSessionRefreshProvider(t, 15*time.Minute, time.Hour)
	ctx := context.Background()

	value, _, err := provider.IssueSessionRefresh(ctx, loginSession())
	if err != nil {
		t.Fatalf("IssueSessionRefresh: %v", err)
	}

	provider.tokenService.storage = revocationDownStorage{TokenStorage: provider.tokenService.storage}

	result, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: value})
	if !errors.Is(err, errRevocationDown) {
		t.Fatalf("error = %v, want the revocation store error", err)
	}
	if result != nil {
		t.Fatalf("result = %v, want no credentials minted when the revocation store errors", result)
	}
}
