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

package cli

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// writeSession persists a session envelope in a temp dir and returns its path.
func writeSession(t *testing.T, token *TokenData) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := SaveToken(path, token); err != nil {
		t.Fatalf("SaveToken: %v", err)
	}
	return path
}

// refreshFailedError is a sentinel the fake refresher returns.
type refreshFailedError struct{}

func (e *refreshFailedError) Error() string { return "refresh rejected by server" }

// =============================================================================
// Happy paths
// =============================================================================

func TestTokenManager_UsesLiveTokenWithoutRefreshing(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "live-access",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(time.Hour),
		TokenType:    "Bearer",
	})

	var calls atomic.Int32
	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			calls.Add(1)
			return nil, &refreshFailedError{}
		}))

	got, err := mgr.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "live-access" {
		t.Fatalf("AccessToken = %q, want live-access", got)
	}
	if calls.Load() != 0 {
		t.Fatalf("refresher called %d times for a live token", calls.Load())
	}
}

func TestTokenManager_RenewsExpiredTokenAndPersistsRotation(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale-access",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenType:    "Bearer",
	})

	var presented string
	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(_ context.Context, refreshToken string) (*TokenData, error) {
			presented = refreshToken
			return &TokenData{
				AccessToken:  "fresh-access",
				RefreshToken: "refresh-2",
				ExpiresAt:    time.Now().Add(15 * time.Minute),
				TokenType:    "Bearer",
			}, nil
		}))

	got, err := mgr.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "fresh-access" {
		t.Fatalf("AccessToken = %q, want fresh-access", got)
	}
	if presented != "refresh-1" {
		t.Fatalf("refresher was presented %q, want refresh-1", presented)
	}

	// The rotated credential must be on disk: the server revoked refresh-1, so
	// a client that kept it would be locked out at the next renewal. This is
	// the assertion that keeps `quicbaas mcp serve --stdio` alive over hours.
	stored, err := LoadToken(path)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if stored.AccessToken != "fresh-access" || stored.RefreshToken != "refresh-2" {
		t.Fatalf("persisted session = %+v, want the rotated pair", stored)
	}
}

func TestTokenManager_RenewsWithinSkewWindow(t *testing.T) {
	// A token with less life left than the skew window is renewed BEFORE use so
	// it cannot expire while the request is in flight.
	path := writeSession(t, &TokenData{
		AccessToken:  "about-to-expire",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(RefreshSkew / 2),
		TokenType:    "Bearer",
	})

	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			return &TokenData{
				AccessToken:  "renewed",
				RefreshToken: "refresh-2",
				ExpiresAt:    time.Now().Add(time.Hour),
			}, nil
		}))

	got, err := mgr.AccessToken(context.Background())
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if got != "renewed" {
		t.Fatalf("AccessToken = %q, want renewed", got)
	}
}

func TestTokenManager_KeepsCredentialWhenServerDoesNotRotate(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
		TokenType:    "Bearer",
	})

	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			// Server renewed without rotating.
			return &TokenData{AccessToken: "fresh", ExpiresAt: time.Now().Add(time.Hour)}, nil
		}))

	if _, err := mgr.AccessToken(context.Background()); err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	stored, err := LoadToken(path)
	if err != nil {
		t.Fatalf("LoadToken: %v", err)
	}
	if stored.RefreshToken != "refresh-1" {
		t.Fatalf("refresh credential = %q, want the retained refresh-1", stored.RefreshToken)
	}
	if stored.TokenType != "Bearer" {
		t.Fatalf("token type = %q, want the retained Bearer", stored.TokenType)
	}
}

// =============================================================================
// Concurrency
// =============================================================================

func TestTokenManager_RenewalIsSingleFlight(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	var calls atomic.Int32
	// release gates the first refresher call so every goroutine is parked on
	// the renewal at the same time — without a shared lock they would all
	// redeem the (single-use) credential.
	release := make(chan struct{})
	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			n := calls.Add(1)
			if n == 1 {
				<-release
			}
			return &TokenData{
				AccessToken:  "fresh",
				RefreshToken: "refresh-2",
				ExpiresAt:    time.Now().Add(time.Hour),
			}, nil
		}))

	const workers = 8
	var wg sync.WaitGroup
	results := make([]string, workers)
	errs := make([]error, workers)
	started := make(chan struct{}, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			started <- struct{}{}
			results[idx], errs[idx] = mgr.AccessToken(context.Background())
		}(i)
	}
	for i := 0; i < workers; i++ {
		<-started
	}
	close(release)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("refresher called %d times, want exactly 1 (single-flight)", got)
	}
	for i := 0; i < workers; i++ {
		if errs[i] != nil {
			t.Fatalf("worker %d: %v", i, errs[i])
		}
		if results[i] != "fresh" {
			t.Fatalf("worker %d got %q, want fresh", i, results[i])
		}
	}
}

// =============================================================================
// Failure paths
// =============================================================================

func TestTokenManager_RefreshFailureSurfacesTypedError(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	sentinel := &refreshFailedError{}
	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) { return nil, sentinel }))

	_, err := mgr.AccessToken(context.Background())
	var failed *StoredSessionRefreshFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("error = %v, want StoredSessionRefreshFailedError", err)
	}
	if !errors.Is(err, error(sentinel)) {
		t.Fatalf("error %v does not wrap the underlying cause", err)
	}
}

func TestTokenManager_ExpiredWithNoRefreshCredential(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken: "stale",
		ExpiresAt:   time.Now().Add(-time.Minute),
	})

	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			t.Fatal("refresher must not be called without a credential to present")
			return nil, nil
		}))

	_, err := mgr.AccessToken(context.Background())
	var expired *StoredSessionExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("error = %v, want StoredSessionExpiredError", err)
	}
}

func TestTokenManager_ExpiredWithNoRefresherConfigured(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	_, err := NewTokenManager(path, nil).AccessToken(context.Background())
	var expired *StoredSessionExpiredError
	if !errors.As(err, &expired) {
		t.Fatalf("error = %v, want StoredSessionExpiredError", err)
	}
}

func TestTokenManager_MissingSessionFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent")

	_, err := NewTokenManager(path, nil).AccessToken(context.Background())
	var unavailable *StoredSessionUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("error = %v, want StoredSessionUnavailableError", err)
	}
}

func TestTokenManager_RefresherReturnsEmptySession(t *testing.T) {
	path := writeSession(t, &TokenData{
		AccessToken:  "stale",
		RefreshToken: "refresh-1",
		ExpiresAt:    time.Now().Add(-time.Minute),
	})

	mgr := NewTokenManager(path, SessionRefresherFunc(
		func(context.Context, string) (*TokenData, error) {
			return &TokenData{}, nil
		}))

	_, err := mgr.AccessToken(context.Background())
	var failed *StoredSessionRefreshFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("error = %v, want StoredSessionRefreshFailedError", err)
	}
}

// =============================================================================
// tokenUsable
// =============================================================================

func TestTokenUsable(t *testing.T) {
	cases := []struct {
		name  string
		token *TokenData
		want  bool
	}{
		{"nil", nil, false},
		{"no access token", &TokenData{ExpiresAt: time.Now().Add(time.Hour)}, false},
		{"expired", &TokenData{AccessToken: "a", ExpiresAt: time.Now().Add(-time.Second)}, false},
		{"inside skew", &TokenData{AccessToken: "a", ExpiresAt: time.Now().Add(RefreshSkew / 2)}, false},
		{"live", &TokenData{AccessToken: "a", ExpiresAt: time.Now().Add(time.Hour)}, true},
		{"no expiry recorded", &TokenData{AccessToken: "a"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenUsable(tc.token); got != tc.want {
				t.Fatalf("tokenUsable = %v, want %v", got, tc.want)
			}
		})
	}
}
