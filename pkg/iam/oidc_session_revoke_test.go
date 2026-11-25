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

package iam

import (
	"context"
	"errors"
	"testing"
)

// TestRevokeSubjectBearers_KeepsCurrentLoginRevokesOther logs in twice as the
// same principal and revokes every bearer except the first login's: the second
// login's bearer is rejected as revoked and the first one keeps working.
func TestRevokeSubjectBearers_KeepsCurrentLoginRevokesOther(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	bearer1, bearer2 := loginTwice(t, provider)
	sid1 := sessionIDOf(t, provider, bearer1)

	claims1, err := provider.ValidateAccessToken(ctx, bearer1)
	if err != nil {
		t.Fatalf("validate first bearer: %v", err)
	}
	stored, err := provider.tokenService.storage.GetToken(withTokenTenant(ctx, sessionIDTenant), claims1.JTI)
	if err != nil {
		t.Fatalf("read the first bearer's token record: %v", err)
	}
	if stored.SessionID != sid1 {
		t.Fatalf("stored access token sid = %q, want %q", stored.SessionID, sid1)
	}

	count, err := provider.RevokeSubjectBearers(ctx, sessionIDTenant, claims1.Subject, sid1)
	if err != nil {
		t.Fatalf("RevokeSubjectBearers: %v", err)
	}
	if count < 1 {
		t.Fatalf("revoked %d bearers, want at least 1", count)
	}

	var revoked *TokenRevokedError
	if _, err := provider.ValidateAccessToken(ctx, bearer2); !errors.As(err, &revoked) {
		t.Fatalf("second login's bearer: err = %v, want TokenRevokedError", err)
	}
	if _, err := provider.ValidateAccessToken(ctx, bearer1); err != nil {
		t.Fatalf("the kept login's bearer was rejected: %v", err)
	}
}

// TestRevokeSubjectBearers_EmptyKeepRevokesAll revokes every login bearer of
// the subject when no session is kept.
func TestRevokeSubjectBearers_EmptyKeepRevokesAll(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	bearer1, bearer2 := loginTwice(t, provider)
	claims1, err := provider.ValidateAccessToken(ctx, bearer1)
	if err != nil {
		t.Fatalf("validate first bearer: %v", err)
	}

	count, err := provider.RevokeSubjectBearers(ctx, sessionIDTenant, claims1.Subject, "")
	if err != nil {
		t.Fatalf("RevokeSubjectBearers: %v", err)
	}
	if count < 2 {
		t.Fatalf("revoked %d bearers, want at least 2", count)
	}
	for i, bearer := range []string{bearer1, bearer2} {
		var revoked *TokenRevokedError
		if _, err := provider.ValidateAccessToken(ctx, bearer); !errors.As(err, &revoked) {
			t.Fatalf("bearer %d: err = %v, want TokenRevokedError", i+1, err)
		}
	}
}

// TestRevokeSubjectBearers_RequiresSubjectAndTenant refuses a call that names
// no subject or no tenant.
func TestRevokeSubjectBearers_RequiresSubjectAndTenant(t *testing.T) {
	provider := newSessionIDProvider(t)
	ctx := context.Background()

	if _, err := provider.RevokeSubjectBearers(ctx, "", sessionIDUser, ""); err == nil {
		t.Fatal("an empty tenant was accepted")
	}
	if _, err := provider.RevokeSubjectBearers(ctx, sessionIDTenant, "", ""); err == nil {
		t.Fatal("an empty subject was accepted")
	}
}
