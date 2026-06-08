//go:build iam && quic && rest

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

package server

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errAuthzProviderBoom is a sentinel for the transport validator error-path
// tests. fakeOIDCGetter (defined in lifecycle_rest_authz_test.go) is reused as
// the test double; it satisfies the grpc/quic getter interfaces too.
var errAuthzProviderBoom = errors.New("provider boom")

func TestGRPCOIDCTokenValidator_GetterError(t *testing.T) {
	v := &grpcOIDCTokenValidator{getter: &fakeOIDCGetter{err: errAuthzProviderBoom}}
	id, err := v.ValidateGRPCToken(context.Background(), "tok")
	assert.Nil(t, id)
	var gerr *GRPCAuthzError
	require.True(t, errors.As(err, &gerr))
	assert.Equal(t, "get oidc provider", gerr.Op)
	assert.True(t, errors.Is(err, errAuthzProviderBoom))
}

func TestGRPCOIDCTokenValidator_NilProvider(t *testing.T) {
	v := &grpcOIDCTokenValidator{getter: &fakeOIDCGetter{provider: nil, err: nil}}
	id, err := v.ValidateGRPCToken(context.Background(), "tok")
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errGRPCOIDCProviderUnavailable))
}

func TestQUICOIDCTokenValidator_GetterError(t *testing.T) {
	v := &quicOIDCTokenValidator{getter: &fakeOIDCGetter{err: errAuthzProviderBoom}}
	id, err := v.ValidateAuthzToken(context.Background(), "tok")
	assert.Nil(t, id)
	var qerr *QUICAuthzError
	require.True(t, errors.As(err, &qerr))
	assert.Equal(t, "get oidc provider", qerr.Op)
	assert.True(t, errors.Is(err, errAuthzProviderBoom))
}

func TestQUICOIDCTokenValidator_NilProvider(t *testing.T) {
	v := &quicOIDCTokenValidator{getter: &fakeOIDCGetter{provider: nil, err: nil}}
	id, err := v.ValidateAuthzToken(context.Background(), "tok")
	assert.Nil(t, id)
	require.Error(t, err)
	assert.True(t, errors.Is(err, errQUICOIDCProviderUnavailable))
}

func TestGRPCAuthzError_Format(t *testing.T) {
	assert.Equal(t, "grpc authz: op", (&GRPCAuthzError{Op: "op"}).Error())
	assert.Equal(t, "grpc authz: op: boom", (&GRPCAuthzError{Op: "op", Cause: errors.New("boom")}).Error())
}

func TestQUICAuthzError_Format(t *testing.T) {
	assert.Equal(t, "quic authz: op", (&QUICAuthzError{Op: "op"}).Error())
	assert.Equal(t, "quic authz: op: boom", (&QUICAuthzError{Op: "op", Cause: errors.New("boom")}).Error())
}
