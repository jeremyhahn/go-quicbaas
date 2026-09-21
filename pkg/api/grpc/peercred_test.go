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

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeerCredentials_IsRoot(t *testing.T) {
	tests := []struct {
		name     string
		uid      uint32
		expected bool
	}{
		{
			name:     "root user",
			uid:      0,
			expected: true,
		},
		{
			name:     "non-root user",
			uid:      1000,
			expected: false,
		},
		{
			name:     "uid 1",
			uid:      1,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creds := &PeerCredentials{UID: tt.uid}
			assert.Equal(t, tt.expected, creds.IsRoot())
		})
	}
}

func TestPeerCredentials_String(t *testing.T) {
	creds := &PeerCredentials{
		PID: 1234,
		UID: 1000,
		GID: 1000,
	}

	str := creds.String()
	assert.Contains(t, str, "PID: 1234")
	assert.Contains(t, str, "UID: 1000")
	assert.Contains(t, str, "GID: 1000")
}

func TestPeerCredentialsAuthInfo_AuthType(t *testing.T) {
	authInfo := &PeerCredentialsAuthInfo{
		Credentials: &PeerCredentials{PID: 1, UID: 0, GID: 0},
	}
	assert.Equal(t, "unix_peercred", authInfo.AuthType())
}

func TestPeerCredentialsUnavailableError(t *testing.T) {
	t.Run("with cause", func(t *testing.T) {
		cause := assert.AnError
		err := &PeerCredentialsUnavailableError{Cause: cause}

		assert.Contains(t, err.Error(), "peer credentials unavailable")
		assert.Contains(t, err.Error(), cause.Error())
		assert.Equal(t, cause, err.Unwrap())
	})

	t.Run("without cause", func(t *testing.T) {
		err := &PeerCredentialsUnavailableError{}

		assert.Equal(t, "peer credentials unavailable", err.Error())
		assert.Nil(t, err.Unwrap())
	})
}

func TestPeerCredentialsVerificationError(t *testing.T) {
	err := &PeerCredentialsVerificationError{Reason: "test reason"}

	assert.Contains(t, err.Error(), "peer credentials verification failed")
	assert.Contains(t, err.Error(), "test reason")
}

func TestAllowAllVerifier(t *testing.T) {
	verifier := &AllowAllVerifier{}

	tests := []struct {
		name   string
		creds  *PeerCredentials
		method string
	}{
		{
			name:   "root user",
			creds:  &PeerCredentials{UID: 0},
			method: "/test.Service/Method",
		},
		{
			name:   "regular user",
			creds:  &PeerCredentials{UID: 1000},
			method: "/test.Service/Method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.Verify(tt.creds, tt.method)
			assert.NoError(t, err)
		})
	}
}

func TestRootOnlyVerifier(t *testing.T) {
	verifier := &RootOnlyVerifier{}

	tests := []struct {
		name        string
		creds       *PeerCredentials
		expectError bool
	}{
		{
			name:        "root user allowed",
			creds:       &PeerCredentials{UID: 0},
			expectError: false,
		},
		{
			name:        "non-root user denied",
			creds:       &PeerCredentials{UID: 1000},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.Verify(tt.creds, "/test.Service/Method")
			if tt.expectError {
				assert.Error(t, err)
				var verifyErr *PeerCredentialsVerificationError
				require.ErrorAs(t, err, &verifyErr)
				assert.Contains(t, verifyErr.Reason, "root privileges required")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestUIDVerifier(t *testing.T) {
	verifier := NewUIDVerifier(1000, 1001)

	tests := []struct {
		name        string
		creds       *PeerCredentials
		expectError bool
	}{
		{
			name:        "root always allowed",
			creds:       &PeerCredentials{UID: 0},
			expectError: false,
		},
		{
			name:        "allowed uid 1000",
			creds:       &PeerCredentials{UID: 1000},
			expectError: false,
		},
		{
			name:        "allowed uid 1001",
			creds:       &PeerCredentials{UID: 1001},
			expectError: false,
		},
		{
			name:        "denied uid 1002",
			creds:       &PeerCredentials{UID: 1002},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.Verify(tt.creds, "/test.Service/Method")
			if tt.expectError {
				assert.Error(t, err)
				var verifyErr *PeerCredentialsVerificationError
				require.ErrorAs(t, err, &verifyErr)
				assert.Contains(t, verifyErr.Reason, "user not authorized")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGIDVerifier(t *testing.T) {
	verifier := NewGIDVerifier(100, 101)

	tests := []struct {
		name        string
		creds       *PeerCredentials
		expectError bool
	}{
		{
			name:        "root always allowed",
			creds:       &PeerCredentials{UID: 0, GID: 999},
			expectError: false,
		},
		{
			name:        "allowed gid 100",
			creds:       &PeerCredentials{UID: 1000, GID: 100},
			expectError: false,
		},
		{
			name:        "allowed gid 101",
			creds:       &PeerCredentials{UID: 1000, GID: 101},
			expectError: false,
		},
		{
			name:        "denied gid 102",
			creds:       &PeerCredentials{UID: 1000, GID: 102},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := verifier.Verify(tt.creds, "/test.Service/Method")
			if tt.expectError {
				assert.Error(t, err)
				var verifyErr *PeerCredentialsVerificationError
				require.ErrorAs(t, err, &verifyErr)
				assert.Contains(t, verifyErr.Reason, "group not authorized")
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestChainVerifier(t *testing.T) {
	t.Run("all pass", func(t *testing.T) {
		uidVerifier := NewUIDVerifier(1000)
		gidVerifier := NewGIDVerifier(100)
		chain := NewChainVerifier(uidVerifier, gidVerifier)

		creds := &PeerCredentials{UID: 1000, GID: 100}
		err := chain.Verify(creds, "/test.Service/Method")
		assert.NoError(t, err)
	})

	t.Run("first fails", func(t *testing.T) {
		uidVerifier := NewUIDVerifier(1001) // 1000 not allowed
		gidVerifier := NewGIDVerifier(100)
		chain := NewChainVerifier(uidVerifier, gidVerifier)

		creds := &PeerCredentials{UID: 1000, GID: 100}
		err := chain.Verify(creds, "/test.Service/Method")
		assert.Error(t, err)
	})

	t.Run("second fails", func(t *testing.T) {
		uidVerifier := NewUIDVerifier(1000)
		gidVerifier := NewGIDVerifier(101) // 100 not allowed
		chain := NewChainVerifier(uidVerifier, gidVerifier)

		creds := &PeerCredentials{UID: 1000, GID: 100}
		err := chain.Verify(creds, "/test.Service/Method")
		assert.Error(t, err)
	})

	t.Run("empty chain passes", func(t *testing.T) {
		chain := NewChainVerifier()

		creds := &PeerCredentials{UID: 1000, GID: 100}
		err := chain.Verify(creds, "/test.Service/Method")
		assert.NoError(t, err)
	})
}

func TestContextWithPeerCredentials(t *testing.T) {
	creds := &PeerCredentials{
		PID: 1234,
		UID: 1000,
		GID: 1000,
	}

	ctx := context.Background()
	ctx = ContextWithPeerCredentials(ctx, creds)

	retrieved := PeerCredentialsFromContext(ctx)
	require.NotNil(t, retrieved)
	assert.Equal(t, creds.PID, retrieved.PID)
	assert.Equal(t, creds.UID, retrieved.UID)
	assert.Equal(t, creds.GID, retrieved.GID)
}

func TestPeerCredentialsFromContext_NotPresent(t *testing.T) {
	ctx := context.Background()
	creds := PeerCredentialsFromContext(ctx)
	assert.Nil(t, creds)
}

func TestUnixTransportCredentials_Info(t *testing.T) {
	creds := NewUnixTransportCredentials()
	info := creds.Info()

	assert.Equal(t, "unix_peercred", info.SecurityProtocol)
}

func TestUnixTransportCredentials_Clone(t *testing.T) {
	creds := NewUnixTransportCredentials()
	clone := creds.Clone()

	assert.NotNil(t, clone)
	assert.IsType(t, &UnixTransportCredentials{}, clone)
}

func TestUnixTransportCredentials_OverrideServerName(t *testing.T) {
	creds := NewUnixTransportCredentials()
	err := creds.OverrideServerName("test-server")
	assert.NoError(t, err)
}

func TestUnixTransportCredentials_ClientHandshake(t *testing.T) {
	creds := NewUnixTransportCredentials()

	// Client handshake should be a no-op
	ctx := context.Background()
	conn, authInfo, err := creds.ClientHandshake(ctx, "authority", nil)

	assert.Nil(t, conn)
	assert.Nil(t, authInfo)
	assert.NoError(t, err)
}

func TestNewUIDVerifier_Empty(t *testing.T) {
	verifier := NewUIDVerifier()
	assert.NotNil(t, verifier)
	assert.Empty(t, verifier.AllowedUIDs)

	// Non-root should be denied
	err := verifier.Verify(&PeerCredentials{UID: 1000}, "/test")
	assert.Error(t, err)

	// Root should still be allowed
	err = verifier.Verify(&PeerCredentials{UID: 0}, "/test")
	assert.NoError(t, err)
}

func TestNewGIDVerifier_Empty(t *testing.T) {
	verifier := NewGIDVerifier()
	assert.NotNil(t, verifier)
	assert.Empty(t, verifier.AllowedGIDs)

	// Non-root should be denied
	err := verifier.Verify(&PeerCredentials{UID: 1000, GID: 100}, "/test")
	assert.Error(t, err)

	// Root should still be allowed
	err = verifier.Verify(&PeerCredentials{UID: 0, GID: 100}, "/test")
	assert.NoError(t, err)
}
