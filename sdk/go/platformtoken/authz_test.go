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

package platformtoken

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

// TestAuthorizeProjectsUsername verifies the name claim is surfaced as
// AuthorizedContext.Username so resource servers can match principals by
// username in addition to subject id.
func TestAuthorizeProjectsUsername(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "user-123"},
		TenantID:         "tenant-a",
		SCP:              []string{"keys:get"},
		Roles:            []string{"reader"},
		Name:             "alice",
	}

	ac := Authorize(claims)

	if ac.Subject != "user-123" {
		t.Fatalf("Subject = %q, want %q", ac.Subject, "user-123")
	}
	if ac.Username != "alice" {
		t.Fatalf("Username = %q, want %q", ac.Username, "alice")
	}
	if ac.TenantID != "tenant-a" {
		t.Fatalf("TenantID = %q, want %q", ac.TenantID, "tenant-a")
	}
}

// TestAuthorizeUsernameEmptyWhenNoName verifies a token without a name claim
// yields an empty Username rather than a spurious value.
func TestAuthorizeUsernameEmptyWhenNoName(t *testing.T) {
	claims := &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Subject: "svc-1"},
	}

	ac := Authorize(claims)

	if ac.Username != "" {
		t.Fatalf("Username = %q, want empty", ac.Username)
	}
}
