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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// A secret version's created_by is the principal that wrote it, through the
// normal store / rotate / rollback path, on both the list and the single-version
// read (#862).
func TestSecretsService_VersionCreatedByIsTheWritingPrincipal(t *testing.T) {
	t.Parallel()

	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: secrets.NewMemoryBackend()})
	require.NoError(t, err)
	svc := newSecretsService(mgr, newMockLogger())

	as := func(id string) context.Context {
		return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: id, Authenticated: true})
	}

	_, err = svc.StoreSecret(as("alice"), &StoreSecretRequest{SecretID: "db-password", Name: "db-password", Data: []byte("one")})
	require.NoError(t, err)
	_, err = svc.RotateSecret(as("bob"), &RotateSecretRequest{SecretID: "db-password", NewData: []byte("two")})
	require.NoError(t, err)
	_, err = svc.RollbackSecret(as("carol"), &RollbackSecretRequest{SecretID: "db-password", TargetVersion: 1})
	require.NoError(t, err)

	list, err := svc.ListSecretVersions(context.Background(), &ListSecretVersionsRequest{SecretID: "db-password"})
	require.NoError(t, err)
	got := make(map[uint64]string, len(list.Versions))
	for _, v := range list.Versions {
		got[v.Version] = v.CreatedBy
	}
	require.Equal(t, map[uint64]string{1: "alice", 2: "bob", 3: "carol"}, got)

	one, err := svc.GetSecretVersion(context.Background(), &GetSecretVersionRequest{SecretID: "db-password", Version: 2})
	require.NoError(t, err)
	require.Equal(t, "bob", one.Version.CreatedBy)
}
