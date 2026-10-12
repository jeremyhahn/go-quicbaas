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

package webauthnrp

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateRPID_AcceptsWhatTheRelyingPartyAccepts(t *testing.T) {
	for _, rpID := range []string{"example.com", "auth.example.com", "localhost", "192.0.2.10"} {
		assert.NoError(t, ValidateRPID(rpID), rpID)
	}
}

func TestValidateRPID_RefusesWhatTheRelyingPartyRefuses(t *testing.T) {
	for _, rpID := range []string{"", "https://example.com", "example.com:8443", "example.com/path"} {
		err := ValidateRPID(rpID)
		var rpErr *RPIDError
		require.True(t, errors.As(err, &rpErr), "%q: want *RPIDError, got %v", rpID, err)
		assert.NotNil(t, errors.Unwrap(err), "the library's reason must stay reachable")
		assert.Contains(t, err.Error(), "bare effective domain")
	}
}

func TestValidateOrigin_AcceptsEveryFormTheRelyingPartyMatches(t *testing.T) {
	for _, origin := range []string{
		"https://app.example.com",
		"http://localhost:8080",
		"https://app.example.com/path?q=1",
		"android:apk-key-hash:abc123",
		"http://app.example.%%%&123?1",
	} {
		assert.NoError(t, ValidateOrigin(origin, 0), origin)
	}
}

func TestValidateOrigin_RefusesEmptyAndHostlessEntries(t *testing.T) {
	tests := []struct {
		origin string
		reason string
	}{
		{origin: "", reason: reasonOriginEmpty},
		{origin: "https://", reason: reasonOriginNoHost},
		{origin: "http://:8080", reason: reasonOriginNoHost},
	}
	for _, tt := range tests {
		err := ValidateOrigin(tt.origin, 3)
		var originErr *OriginError
		require.True(t, errors.As(err, &originErr), "%q: want *OriginError, got %v", tt.origin, err)
		assert.Equal(t, 3, originErr.Index)
		assert.Equal(t, tt.reason, originErr.Reason)
		assert.Equal(t, "entry 3 "+tt.reason, err.Error())
	}
}

func TestValidateOrigins_AcceptsAWellFormedList(t *testing.T) {
	assert.NoError(t, ValidateOrigins(nil))
	assert.NoError(t, ValidateOrigins([]string{"https://a.example.com", "https://b.example.com"}))
}

func TestValidateOrigins_ReportsTheFirstBadEntryByPosition(t *testing.T) {
	err := ValidateOrigins([]string{"https://a.example.com", "https://", ""})
	var originErr *OriginError
	require.True(t, errors.As(err, &originErr))
	assert.Equal(t, 1, originErr.Index)
	assert.Equal(t, reasonOriginNoHost, originErr.Reason)
}
