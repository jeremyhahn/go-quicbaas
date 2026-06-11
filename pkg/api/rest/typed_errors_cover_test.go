//go:build rest

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

package rest

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	secure "github.com/jeremyhahn/go-xkms/sdk/go/secure"
)

// TestPolicyParseError_MessageAndUnwrap pins the PolicyParseError contract: the
// message reads with and without an underlying cause, and Unwrap exposes the
// ErrPolicyParse sentinel so handlers can branch with errors.Is.
func TestPolicyParseError_MessageAndUnwrap(t *testing.T) {
	t.Run("without cause", func(t *testing.T) {
		err := &PolicyParseError{Reason: "missing statements"}
		assert.Equal(t, "iam.policy.parse: missing statements", err.Error())
		assert.ErrorIs(t, err, ErrPolicyParse)
	})

	t.Run("with cause", func(t *testing.T) {
		cause := stderrors.New("unexpected token")
		err := &PolicyParseError{Reason: "bad json", Cause: cause}
		assert.Equal(t, "iam.policy.parse: bad json: unexpected token", err.Error())
		assert.ErrorIs(t, err, ErrPolicyParse)
	})
}

// TestSSHBadBodyError_Message covers both the nil-Err and wrapped-Err branches
// of SSHBadBodyError.Error and confirms Unwrap exposes the cause.
func TestSSHBadBodyError_Message(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		err := &SSHBadBodyError{}
		assert.Equal(t, "rest/ssh: bad request body", err.Error())
		assert.Nil(t, err.Unwrap())
	})

	t.Run("wrapped error", func(t *testing.T) {
		cause := stderrors.New("eof")
		err := &SSHBadBodyError{Err: cause}
		assert.Equal(t, "rest/ssh: bad request body: eof", err.Error())
		assert.ErrorIs(t, err, cause)
	})
}

// TestSSHHostNotPinnedError_Message pins the (scope/owner/host) message format
// surfaced to known-hosts verify callers.
func TestSSHHostNotPinnedError_Message(t *testing.T) {
	err := &SSHHostNotPinnedError{Scope: "tenant", Owner: "alice", Host: "git.example.com"}
	assert.Equal(t, "rest/ssh: no host pin for tenant/alice/git.example.com", err.Error())
}

// TestSSHBufferProvider_Passphrase exercises both the nil-guard and the
// clone-on-read happy path of the SDK PassphraseProvider adapter.
func TestSSHBufferProvider_Passphrase(t *testing.T) {
	ctx := context.Background()

	t.Run("nil provider", func(t *testing.T) {
		var p *sshBufferProvider
		buf, err := p.Passphrase(ctx, "key")
		require.Error(t, err)
		assert.Nil(t, buf)
		var bad *SSHBadBodyError
		assert.ErrorAs(t, err, &bad)
	})

	t.Run("nil buffer", func(t *testing.T) {
		p := &sshBufferProvider{}
		buf, err := p.Passphrase(ctx, "key")
		require.Error(t, err)
		assert.Nil(t, buf)
	})

	t.Run("clones backing buffer", func(t *testing.T) {
		src, err := secure.NewBuffer(8)
		require.NoError(t, err)
		defer src.Destroy()
		copy(src.Bytes(), []byte("secret12"))

		p := &sshBufferProvider{buf: src}
		clone, err := p.Passphrase(ctx, "key")
		require.NoError(t, err)
		require.NotNil(t, clone)
		defer clone.Destroy()

		// The clone is independent: it carries the same bytes but is not the
		// same buffer, so destroying the source must not corrupt it.
		assert.Equal(t, src.Bytes(), clone.Bytes())
		assert.NotSame(t, src, clone)
	})
}
