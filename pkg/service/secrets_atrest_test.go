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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// atRestServices returns a service over a seal-off Manager and one over a
// seal-on Manager, both on the same backend, so a test can seed a secret
// through the second and read it through the first.
func atRestServices(t *testing.T) (off, on *secretsService) {
	t.Helper()
	backend := secrets.NewMemoryBackend()
	offMgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: backend, AtRestBarrier: func() bool { return false }})
	require.NoError(t, err)
	onMgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: backend, AtRestBarrier: func() bool { return true }})
	require.NoError(t, err)
	return newSecretsService(offMgr, newMockLogger()), newSecretsService(onMgr, newMockLogger())
}

func atRestCtx() context.Context {
	return auth.SubjectIntoContext(tenantctx.WithSystem(context.Background()), interfaces.Subject{ID: "alice", Authenticated: true})
}

func requireAtRestPrecondition(t *testing.T, name string, err error) {
	t.Helper()
	require.Error(t, err, name)
	var qe *qberrors.QBError
	require.True(t, stderrors.As(err, &qe), "%s: a QBError: %v", name, err)
	require.Equal(t, qberrors.ErrPreconditionFailed, qe.Code, "%s: outermost code: %v", name, err)
	require.True(t, stderrors.Is(err, secrets.ErrAtRestEncryptionRequired), "%s: errors.Is the sentinel: %v", name, err)
}

func TestSecretsService_StoreRefusedWithoutAtRestBarrier(t *testing.T) {
	t.Parallel()
	off, on := atRestServices(t)
	ctx := atRestCtx()

	_, err := off.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	requireAtRestPrecondition(t, "StoreSecret", err)

	_, err = on.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	require.NoError(t, err)
	_, err = on.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte("two")})
	require.NoError(t, err)

	_, err = off.RotateSecret(ctx, &RotateSecretRequest{SecretID: "db-password", NewData: []byte("three")})
	requireAtRestPrecondition(t, "RotateSecret", err)
	_, err = off.RollbackSecret(ctx, &RollbackSecretRequest{SecretID: "db-password", TargetVersion: 1})
	requireAtRestPrecondition(t, "RollbackSecret", err)
}

func TestSecretsService_GetRefusedWithoutAtRestBarrier(t *testing.T) {
	t.Parallel()
	off, on := atRestServices(t)
	ctx := atRestCtx()

	_, err := on.StoreSecret(ctx, &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	require.NoError(t, err)

	_, err = off.GetSecret(ctx, &GetSecretRequest{SecretID: "db-password"})
	requireAtRestPrecondition(t, "GetSecret", err)
}

func TestSecretsService_EnsureRefusedWithoutAtRestBarrier(t *testing.T) {
	t.Parallel()
	off, _ := atRestServices(t)

	_, err := off.EnsureSecret(atRestCtx(), &EnsureSecretRequest{SecretID: "app/token", Length: 8, Encoding: "numeric"})
	requireAtRestPrecondition(t, "EnsureSecret", err)
}
