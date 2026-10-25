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

//go:build integration

package helpers

import (
	"strconv"

	"github.com/golang-jwt/jwt/v5"
)

// bearerUserIDClaim is the access-token claim that names the principal's
// numeric account id (pkg/iam/oidc_tokens.go). The server resolves the
// authenticated actor from it, so a test that acts as a principal reads the
// same value.
const bearerUserIDClaim = "uid"

// BearerUserIDError reports a bearer the suite could not read a numeric
// user id from.
type BearerUserIDError struct {
	// Reason names what was wrong with the bearer.
	Reason string

	// Cause is the underlying failure, nil when the claim was absent or
	// malformed.
	Cause error
}

func (e *BearerUserIDError) Error() string {
	if e.Cause != nil {
		return "bearer user id: " + e.Reason + ": " + e.Cause.Error()
	}
	return "bearer user id: " + e.Reason
}

func (e *BearerUserIDError) Unwrap() error { return e.Cause }

// BearerUserID returns the numeric user id in a logged-in principal's bearer
// (its "uid" claim). The token is parsed without verifying its signature: the
// server that issued it verifies it on every request, and the test only needs
// to know which account it names.
func BearerUserID(bearer string) (uint64, error) {
	claims := jwt.MapClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(bearer, claims); err != nil {
		return 0, &BearerUserIDError{Reason: "parse access token", Cause: err}
	}
	raw, ok := claims[bearerUserIDClaim].(string)
	if !ok || raw == "" {
		return 0, &BearerUserIDError{Reason: "access token has no uid claim"}
	}
	userID, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, &BearerUserIDError{Reason: "uid claim is not a numeric id", Cause: err}
	}
	if userID == 0 {
		return 0, &BearerUserIDError{Reason: "uid claim is zero"}
	}
	return userID, nil
}
