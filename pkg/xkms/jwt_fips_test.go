// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package xkms

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestJWTHMAC_FIPSRefusesAShortSecret: in FIPS mode a JWT HMAC secret shorter
// than 14 bytes is refused with the usual signing or verification error, never
// passed to HMAC (which would panic the process).
func TestJWTHMAC_FIPSRefusesAShortSecret(t *testing.T) {
	original := fipsEnabled
	fipsEnabled = func() bool { return true }
	t.Cleanup(func() { fipsEnabled = original })

	claims := JWTClaims{"sub": "test"}
	short := []byte("thirteen-byte")
	long := []byte("a-22-byte-hmac-secret!")
	require.Len(t, long, 22)

	_, err := JWTSignHMACWithAlgorithm(claims, short, "HS256")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrJWTSigningFailed), "err = %v", err)
	assert.Contains(t, err.Error(), "14 bytes")

	_, err = JWTSignHMACWithAlgorithm(claims, []byte{}, "HS256")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrJWTSigningFailed), "err = %v", err)
	assert.Contains(t, err.Error(), "14 bytes")

	token, err := JWTSignHMACWithAlgorithm(claims, long, "HS256")
	require.NoError(t, err)

	_, err = JWTVerifyHMACWithAlgorithm(token, short, "HS256")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrJWTVerificationFailed), "err = %v", err)
	assert.Contains(t, err.Error(), "14 bytes")

	got, err := JWTVerifyHMACWithAlgorithm(token, long, "HS256")
	require.NoError(t, err)
	assert.Equal(t, "test", got["sub"])
}
