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

package orchestration

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOAuth2TokenRefresher_Refresh_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm() error = %v", err)
		}
		if got := r.FormValue("grant_type"); got != "refresh_token" {
			t.Fatalf("grant_type = %s, want refresh_token", got)
		}
		if got := r.FormValue("refresh_token"); got != "rt-1" {
			t.Fatalf("refresh_token = %s, want rt-1", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-at","refresh_token":"new-rt","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	refresher := NewOAuth2TokenRefresher(OAuth2RefresherConfig{})
	renewed, err := refresher.Refresh(context.Background(), &AccountCredential{
		RefreshToken:  "rt-1",
		TokenEndpoint: srv.URL,
		ClientID:      "client-1",
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if renewed.AccessToken != "new-at" || renewed.RefreshToken != "new-rt" {
		t.Fatalf("Refresh() = %+v, want access_token=new-at refresh_token=new-rt", renewed)
	}
	if !renewed.ExpiresAt.After(time.Now().Add(59 * time.Minute)) {
		t.Fatalf("Refresh().ExpiresAt = %v, want ~1 hour out", renewed.ExpiresAt)
	}
}

func TestOAuth2TokenRefresher_Refresh_ServerRejects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"refresh token expired"}`))
	}))
	defer srv.Close()

	refresher := NewOAuth2TokenRefresher(OAuth2RefresherConfig{})
	_, err := refresher.Refresh(context.Background(), &AccountCredential{
		RefreshToken:  "rt-1",
		TokenEndpoint: srv.URL,
	})
	if err == nil {
		t.Fatalf("Refresh() error = nil, want an OAuth2TokenEndpointError")
	}
}

func TestOAuth2TokenRefresher_Refresh_MissingFields(t *testing.T) {
	refresher := NewOAuth2TokenRefresher(OAuth2RefresherConfig{})
	if _, err := refresher.Refresh(context.Background(), &AccountCredential{}); err == nil {
		t.Fatalf("Refresh() error = nil, want AccountInvalidError for missing token_endpoint/refresh_token")
	}
}

func TestOAuth2TokenRefresher_Refresh_NoRotationKeepsOldRefreshToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"new-at","expires_in":60}`))
	}))
	defer srv.Close()

	refresher := NewOAuth2TokenRefresher(OAuth2RefresherConfig{})
	renewed, err := refresher.Refresh(context.Background(), &AccountCredential{
		RefreshToken:  "rt-1",
		TokenEndpoint: srv.URL,
	})
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if renewed.RefreshToken != "rt-1" {
		t.Fatalf("Refresh().RefreshToken = %s, want rt-1 (server did not rotate)", renewed.RefreshToken)
	}
}
