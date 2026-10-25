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
	"errors"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// signedBearer mints a signed access token carrying claims, the shape the
// server issues.
func signedBearer(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("bearer-user-id-test-key"))
	require.NoError(t, err)
	return token
}

func TestBearerUserID_ReadsTheUIDClaim(t *testing.T) {
	bearer := signedBearer(t, jwt.MapClaims{"sub": "tenant-admin", "uid": "18446744073709551615"})

	userID, err := BearerUserID(bearer)

	require.NoError(t, err)
	assert.Equal(t, uint64(18446744073709551615), userID)
}

func TestBearerUserID_RefusesABearerWithoutANumericUID(t *testing.T) {
	cases := map[string]string{
		"not a token":   "opaque-bearer",
		"no uid claim":  signedBearer(t, jwt.MapClaims{"sub": "tenant-admin"}),
		"numeric claim": signedBearer(t, jwt.MapClaims{"uid": 42}),
		"name not id":   signedBearer(t, jwt.MapClaims{"uid": "user-1234abcd"}),
		"zero":          signedBearer(t, jwt.MapClaims{"uid": "0"}),
	}
	for name, bearer := range cases {
		t.Run(name, func(t *testing.T) {
			userID, err := BearerUserID(bearer)

			var bearerErr *BearerUserIDError
			require.True(t, errors.As(err, &bearerErr), "want *BearerUserIDError, got %v", err)
			assert.Zero(t, userID)
		})
	}
}
