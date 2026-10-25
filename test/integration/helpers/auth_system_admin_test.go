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
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// writeSystemAdminFixture points the PIV fixture env vars at a cert and key
// in a temp dir and returns their contents.
func writeSystemAdminFixture(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	dir := t.TempDir()
	certPEM, keyPEM = []byte("cert-pem"), []byte("key-pem")
	certPath, keyPath := filepath.Join(dir, "admin.crt"), filepath.Join(dir, "admin.key")
	require.NoError(t, os.WriteFile(certPath, certPEM, 0o600))
	require.NoError(t, os.WriteFile(keyPath, keyPEM, 0o600))
	t.Setenv("QUICBAAS_TEST_PIV_CERT", certPath)
	t.Setenv("QUICBAAS_TEST_PIV_KEY", keyPath)
	return certPEM, keyPEM
}

func TestSystemAdmin_LogsInWithTheFixtureCert(t *testing.T) {
	certPEM, keyPEM := writeSystemAdminFixture(t)
	var gotCert, gotKey []byte
	login := func(_ context.Context, _ string, cert, key []byte) (string, int, error) {
		gotCert, gotKey = cert, key
		return "system-bearer", 900, nil
	}
	registry := newPrincipalRegistry(systemAdminPrincipalName, enrollSystemAdmin, login)

	bearer, err := registry.bearer(context.Background(), testBaseURL)
	require.NoError(t, err)
	require.Equal(t, "system-bearer", bearer)
	require.Equal(t, certPEM, gotCert)
	require.Equal(t, keyPEM, gotKey)
}

func TestSystemAdmin_MissingFixtureCertIsTyped(t *testing.T) {
	t.Setenv("QUICBAAS_TEST_PIV_CERT", filepath.Join(t.TempDir(), "absent.crt"))

	_, err := enrollSystemAdmin(context.Background(), testBaseURL)
	var authErr *PrincipalAuthError
	require.True(t, errors.As(err, &authErr))
	require.Equal(t, "read client cert", authErr.Step)
	require.ErrorIs(t, err, os.ErrNotExist)
}

func TestSystemAdmin_MissingFixtureKeyIsTyped(t *testing.T) {
	writeSystemAdminFixture(t)
	t.Setenv("QUICBAAS_TEST_PIV_KEY", filepath.Join(t.TempDir(), "absent.key"))

	_, err := enrollSystemAdmin(context.Background(), testBaseURL)
	var authErr *PrincipalAuthError
	require.True(t, errors.As(err, &authErr))
	require.Equal(t, "read client key", authErr.Step)
}

// Every gRPC call made with the system admin credentials carries its bearer,
// so an authorized RPC is no longer reached anonymously.
func TestSystemAdminGRPCCredentials_CarryTheBearer(t *testing.T) {
	writeSystemAdminFixture(t)
	login := func(context.Context, string, []byte, []byte) (string, int, error) {
		return "system-bearer", 900, nil
	}
	registry := newPrincipalRegistry(systemAdminPrincipalName, enrollSystemAdmin, login)

	creds, err := registry.newGRPCCredentials(context.Background())
	require.NoError(t, err)
	md, err := creds.GetRequestMetadata(context.Background())
	require.NoError(t, err)
	require.Equal(t, map[string]string{"authorization": "Bearer system-bearer"}, md)
}

func TestSystemAdminGRPCCredentials_LoginFailureIsReturned(t *testing.T) {
	writeSystemAdminFixture(t)
	refused := errors.New("login refused")
	login := func(context.Context, string, []byte, []byte) (string, int, error) {
		return "", 0, refused
	}
	registry := newPrincipalRegistry(systemAdminPrincipalName, enrollSystemAdmin, login)

	_, err := registry.newGRPCCredentials(context.Background())
	require.ErrorIs(t, err, refused)
}
