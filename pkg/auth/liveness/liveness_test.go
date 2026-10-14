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

package liveness

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/golang-jwt/jwt/v5"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// recordingSource is a fake Source that records the arguments of the last
// TokenLiveness call and returns a programmed (live, err) result.
type recordingSource struct {
	gotTenant  string
	gotSubject string
	gotUserID  uint64
	gotJTI     string
	called     bool

	retLive bool
	retErr  error
}

func (r *recordingSource) TokenLiveness(_ context.Context, tenantID, subject string, userID uint64, principalType, jti string) (bool, error) {
	r.called = true
	r.gotTenant = tenantID
	r.gotSubject = subject
	r.gotUserID = userID
	r.gotJTI = jti
	return r.retLive, r.retErr
}

// errSourceFailure is a sentinel store error used to assert error pass-through.
var errSourceFailure = errors.New("liveness: source store failure")

// TestCheckLiveness_MapsClaimsAndPassesActive verifies CheckLiveness maps
// claims.TenantID/Subject/ID onto TokenLiveness(tenant, subject, jti) and
// returns the source's live verdict unchanged.
func TestCheckLiveness_MapsClaimsAndPassesActive(t *testing.T) {
	src := &recordingSource{retLive: true, retErr: nil}
	checker := NewChecker(src)

	claims := &platformtoken.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:      "jti-123",
			Subject: "user-42",
		},
		TenantID: "tenant-a",
		UserID:   "18446744073709551615",
	}

	active, err := checker.CheckLiveness(context.Background(), "raw.token.value", claims)
	if err != nil {
		t.Fatalf("CheckLiveness() error = %v, want nil", err)
	}
	if !active {
		t.Fatalf("CheckLiveness() active = false, want true")
	}

	if src.gotTenant != "tenant-a" {
		t.Errorf("tenant = %q, want tenant-a", src.gotTenant)
	}
	if src.gotSubject != "user-42" {
		t.Errorf("subject = %q, want user-42", src.gotSubject)
	}
	if src.gotUserID != math.MaxUint64 {
		t.Errorf("user id = %d, want %d", src.gotUserID, uint64(math.MaxUint64))
	}
	if src.gotJTI != "jti-123" {
		t.Errorf("jti = %q, want jti-123", src.gotJTI)
	}
}

// A uid claim that is not a decimal uint64 is refused with a typed error
// before the source is asked, rather than read as "no user".
func TestCheckLiveness_RefusesAMalformedUserIDClaim(t *testing.T) {
	for name, uid := range map[string]string{
		"non-numeric": "user-42",
		"overflowing": "18446744073709551616",
		"negative":    "-1",
	} {
		t.Run(name, func(t *testing.T) {
			src := &recordingSource{retLive: true}
			claims := &platformtoken.Claims{TenantID: "tenant-a", UserID: uid}

			active, err := NewChecker(src).CheckLiveness(context.Background(), "raw", claims)

			var claimErr *UserIDClaimError
			if !errors.As(err, &claimErr) {
				t.Fatalf("CheckLiveness() error = %v, want *UserIDClaimError", err)
			}
			if claimErr.Value != uid {
				t.Errorf("UserIDClaimError.Value = %q, want %q", claimErr.Value, uid)
			}
			if active {
				t.Error("CheckLiveness() active = true, want false on a malformed claim")
			}
			if src.called {
				t.Error("the source was asked about a token whose uid claim names no user")
			}
		})
	}
}

// TestCheckLiveness_PassesErrorAndNonLive verifies CheckLiveness passes a
// fail-closed (false, err) result from the source straight through.
func TestCheckLiveness_PassesErrorAndNonLive(t *testing.T) {
	src := &recordingSource{retLive: false, retErr: errSourceFailure}
	checker := NewChecker(src)

	claims := &platformtoken.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:      "jti-err",
			Subject: "user-err",
		},
		TenantID: "tenant-err",
	}

	active, err := checker.CheckLiveness(context.Background(), "raw", claims)
	if !errors.Is(err, errSourceFailure) {
		t.Fatalf("CheckLiveness() error = %v, want errSourceFailure", err)
	}
	if active {
		t.Fatalf("CheckLiveness() active = true, want false on error")
	}

	// Even on the error path the claims must be mapped through to the source.
	if src.gotTenant != "tenant-err" || src.gotSubject != "user-err" || src.gotJTI != "jti-err" {
		t.Errorf("claim mapping on error path = (%q,%q,%q)", src.gotTenant, src.gotSubject, src.gotJTI)
	}
}
