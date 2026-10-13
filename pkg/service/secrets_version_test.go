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

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/conv"
)

// requireSecretVersionError asserts err is a QBError with code, caused by a
// failed version conversion of type T.
func requireSecretVersionError[T error](t *testing.T, err error, code errors.ErrorCode) {
	t.Helper()
	var qbErr *errors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, code, qbErr.Code)
	var convErr T
	assert.ErrorAs(t, err, &convErr)
}

func TestSecretVersionArg(t *testing.T) {
	got, err := secretVersionArg("op", 3)
	require.NoError(t, err)
	assert.Equal(t, 3, got)

	_, err = secretVersionArg("op", math.MaxUint64)
	requireSecretVersionError[*conv.ConversionOverflowError](t, err, errors.ErrInvalidInput)
}

func TestBackendSecretVersion(t *testing.T) {
	got, err := backendSecretVersion("op", 3)
	require.NoError(t, err)
	assert.Equal(t, uint64(3), got)

	_, err = backendSecretVersion("op", -1)
	requireSecretVersionError[*conv.ConversionNegativeError](t, err, errors.ErrInternal)
}

func TestSecretsService_VersionBeyondInt(t *testing.T) {
	ctx := tenantctx.WithSystem(context.Background())
	svc := newSecretsService(newMockSecretsManager(), newMockLogger())

	t.Run("GetSecret", func(t *testing.T) {
		resp, err := svc.GetSecret(ctx, &GetSecretRequest{SecretID: "s", Version: math.MaxUint64})
		assert.Nil(t, resp)
		requireSecretVersionError[*conv.ConversionOverflowError](t, err, errors.ErrInvalidInput)
	})

	t.Run("GetSecretVersion", func(t *testing.T) {
		resp, err := svc.GetSecretVersion(ctx, &GetSecretVersionRequest{SecretID: "s", Version: math.MaxUint64})
		assert.Nil(t, resp)
		requireSecretVersionError[*conv.ConversionOverflowError](t, err, errors.ErrInvalidInput)
	})

	t.Run("RollbackSecret", func(t *testing.T) {
		resp, err := svc.RollbackSecret(ctx, &RollbackSecretRequest{SecretID: "s", TargetVersion: math.MaxUint64})
		assert.Nil(t, resp)
		requireSecretVersionError[*conv.ConversionOverflowError](t, err, errors.ErrInvalidInput)
	})
}

func TestSecretsService_ListSecretVersions_NegativeBackendVersion(t *testing.T) {
	mockManager := newMockSecretsManager()
	mockManager.secrets["test-secret"] = &mockSecretData{
		path: "test-secret",
		versions: []mockSecretVersion{
			{version: 1, data: map[string][]byte{"data": []byte("v1")}},
			{version: -1, data: map[string][]byte{"data": []byte("corrupt")}},
		},
	}
	svc := newSecretsService(mockManager, newMockLogger())

	resp, err := svc.ListSecretVersions(tenantctx.WithSystem(context.Background()), &ListSecretVersionsRequest{SecretID: "test-secret"})
	assert.Nil(t, resp)
	requireSecretVersionError[*conv.ConversionNegativeError](t, err, errors.ErrInternal)
}
