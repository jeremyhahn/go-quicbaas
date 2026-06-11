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

package iam

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// -----------------------------------------------------------------------------
// agent_registrar.go
// -----------------------------------------------------------------------------

func TestDefaultAgentRuntimeScopes(t *testing.T) {
	scopes := DefaultAgentRuntimeScopes()
	assert.ElementsMatch(t, []string{
		"agent:runtime:spawn",
		"agent:runtime:prompt",
		"agent:runtime:status",
	}, scopes)
}

func TestDefaultAgentRuntimeScopes_FreshSlice(t *testing.T) {
	// Each call must return an independent slice so callers can't mutate
	// shared package state.
	a := DefaultAgentRuntimeScopes()
	a[0] = "mutated"
	b := DefaultAgentRuntimeScopes()
	assert.Equal(t, "agent:runtime:spawn", b[0])
}

// -----------------------------------------------------------------------------
// orchestrator_auth.go — in-memory store leftovers
// -----------------------------------------------------------------------------

func TestInMemoryServiceAccountStore_DeleteServiceAccount(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryServiceAccountStore()

	acct := &ServiceAccount{ID: "a1", TenantID: "t1", ClientID: "client-1"}
	require.NoError(t, store.PutServiceAccount(ctx, acct))

	require.NoError(t, store.DeleteServiceAccount(ctx, "a1"))

	_, err := store.GetServiceAccount(ctx, "a1")
	require.Error(t, err)
}

func TestInMemoryServiceAccountStore_DeleteServiceAccount_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryServiceAccountStore()

	err := store.DeleteServiceAccount(ctx, "missing")
	require.Error(t, err)
	var nf *ServiceAccountNotFoundError
	require.True(t, errors.As(err, &nf))
}

func TestInMemoryServiceAccountStore_GetByClientID(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryServiceAccountStore()

	acct := &ServiceAccount{ID: "a1", TenantID: "t1", ClientID: "client-1"}
	require.NoError(t, store.PutServiceAccount(ctx, acct))

	got, err := store.GetServiceAccountByClientID(ctx, "client-1")
	require.NoError(t, err)
	assert.Equal(t, "a1", got.ID)
}

func TestInMemoryServiceAccountStore_GetByClientID_NotFound(t *testing.T) {
	ctx := context.Background()
	store := NewInMemoryServiceAccountStore()

	_, err := store.GetServiceAccountByClientID(ctx, "no-client")
	require.Error(t, err)
	var nf *ServiceAccountNotFoundError
	require.True(t, errors.As(err, &nf))
}

// -----------------------------------------------------------------------------
// identity.go — GetUserByPIVThumbprint
// -----------------------------------------------------------------------------

func TestIdentityStore_GetUserByPIVThumbprint(t *testing.T) {
	ctx := context.Background()
	store := newTestIdentityStore()

	created, err := store.CreateUser(ctx, "tenant1", CreateUserOptions{
		Email:    "piv@example.com",
		Username: "pivuser",
	})
	require.NoError(t, err)

	// Attach a PIV thumbprint via update so the storage index reflects it.
	thumb := "ABCDEF0123456789"
	_, err = store.UpdateUser(ctx, "tenant1", created.ID, UpdateUserOptions{
		PIVCertThumbprint: &thumb,
	})
	require.NoError(t, err)

	got, err := store.GetUserByPIVThumbprint(ctx, thumb)
	require.NoError(t, err)
	assert.Equal(t, created.ID, got.ID)
}

func TestIdentityStore_GetUserByPIVThumbprint_EmptyThumbprint(t *testing.T) {
	ctx := context.Background()
	store := newTestIdentityStore()

	_, err := store.GetUserByPIVThumbprint(ctx, "")
	require.Error(t, err)
}

// -----------------------------------------------------------------------------
// errors.go — typed error Error()/Unwrap() leftovers
// -----------------------------------------------------------------------------

func TestWebAuthnErrorConstructors(t *testing.T) {
	cause := errors.New("boom")

	credErr := NewCredentialNotFoundError("t1", "cred-1")
	assert.Contains(t, credErr.Error(), "cred-1")

	challNF := NewChallengeNotFoundError("chal-1")
	assert.Contains(t, challNF.Error(), "chal-1")

	challExp := NewChallengeExpiredError("chal-2")
	assert.Contains(t, challExp.Error(), "chal-2")

	regErr := NewWebAuthnRegistrationError("t1", "user-1", cause)
	assert.Contains(t, regErr.Error(), "user-1")
	require.True(t, errors.Is(regErr, cause))

	authErr := NewWebAuthnAuthenticationError("t1", "a@b.com", cause)
	assert.Contains(t, authErr.Error(), "a@b.com")
	require.True(t, errors.Is(authErr, cause))
}

func TestBatchErrors_Error(t *testing.T) {
	assert.Equal(t, "unsupported operation type", (&BatchUnsupportedOperationError{}).Error())
	assert.Equal(t, "CSV must have header and at least one data row", (&BatchCSVHeaderError{}).Error())
	assert.Contains(t, (&BatchCSVNotSupportedError{OperationType: "users"}).Error(), "users")
	assert.Equal(t, "operation cannot be cancelled", (&BatchCannotCancelError{}).Error())
}

func TestMTLSErrors_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("chain")
	ver := &MTLSCertVerificationError{Cause: cause}
	assert.Contains(t, ver.Error(), "chain")
	assert.Equal(t, cause, ver.Unwrap())

	parse := &MTLSCertParseError{Cause: cause}
	assert.Contains(t, parse.Error(), "chain")
	assert.Equal(t, cause, parse.Unwrap())
}

func TestPIVCertIssuerRejectedError_Error(t *testing.T) {
	e := &PIVCertIssuerRejectedError{Issuer: "CN=Bad", AllowedIssuers: []string{"CN=Good"}}
	assert.Contains(t, e.Error(), "CN=Bad")
}

func TestAAGUIDNotAllowedError_Error(t *testing.T) {
	withName := &ErrAAGUIDNotAllowedError{AAGUID: "aaa", AuthenticatorName: "YubiKey"}
	assert.Contains(t, withName.Error(), "YubiKey")

	noName := &ErrAAGUIDNotAllowedError{AAGUID: "bbb"}
	assert.Contains(t, noName.Error(), "bbb")
	assert.False(t, strings.Contains(noName.Error(), "("))
}

// -----------------------------------------------------------------------------
// piv.go — typed error Error()/Unwrap()
// -----------------------------------------------------------------------------

func TestPIVErrors_Error(t *testing.T) {
	assert.Contains(t, (&PIVActivatorConfigError{Reason: "no logger"}).Error(), "no logger")
	assert.Contains(t, (&PIVCertDecodeError{Reason: "bad pem"}).Error(), "bad pem")

	cause := errors.New("update failed")
	upd := &PIVActivationUpdateError{Cause: cause}
	assert.Contains(t, upd.Error(), "update failed")
	assert.Equal(t, cause, upd.Unwrap())
}

// -----------------------------------------------------------------------------
// invite.go — InviteTokenGenerationError + TokenHashExported
// -----------------------------------------------------------------------------

func TestInviteTokenGenerationError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("rng down")
	e := &InviteTokenGenerationError{Cause: cause}
	assert.Contains(t, e.Error(), "rng down")
	assert.Equal(t, cause, e.Unwrap())
}

func TestTokenHashExported(t *testing.T) {
	h1 := TokenHashExported("token")
	h2 := TokenHashExported("token")
	assert.Equal(t, h1, h2)
	assert.Len(t, h1, 64) // SHA-256 hex
	assert.NotEqual(t, h1, TokenHashExported("other"))
}
