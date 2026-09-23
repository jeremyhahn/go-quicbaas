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

package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// capturingSecretsFactory installs a secrets client that records the stored
// request, and returns a pointer to it. The recorded Data is COPIED, because
// the command clears its value buffer once the request is sent.
func capturingSecretsFactory(t *testing.T) **service.StoreSecretRequest {
	t.Helper()
	var captured *service.StoreSecretRequest
	orig := secretsClientFactory
	secretsClientFactory = func() (SecretsClient, error) {
		return &MockSecretsClient{
			StoreSecretFunc: func(ctx context.Context, req *service.StoreSecretRequest) (*service.StoreSecretResponse, error) {
				copied := *req
				copied.Data = append([]byte(nil), req.Data...)
				captured = &copied
				return &service.StoreSecretResponse{SecretID: req.SecretID, Version: 1, Timestamp: time.Now()}, nil
			},
		}, nil
	}
	t.Cleanup(func() { secretsClientFactory = orig })
	return &captured
}

// TestSecretsPut_FromFileStoresRawBytes proves --from-file stores the file's
// bytes verbatim — NUL and newline included — with no KEY=VALUE envelope.
func TestSecretsPut_FromFileStoresRawBytes(t *testing.T) {
	captured := capturingSecretsFactory(t)
	raw := []byte("{\"tokens\":{\"a\":\"fake\"}}\n\x00tail\n")
	path := filepath.Join(t.TempDir(), "auth.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"agents/codex/reviewer/auth_json", "--from-file", path})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, *captured)
	assert.Equal(t, "agents/codex/reviewer/auth_json", (*captured).SecretID)
	assert.Equal(t, raw, (*captured).Data)
}

// TestSecretsPut_FromStdinStoresRawStreamAndNeverEchoes proves a piped
// --from-stdin stores the stream verbatim and prints nothing of it.
func TestSecretsPut_FromStdinStoresRawStreamAndNeverEchoes(t *testing.T) {
	captured := capturingSecretsFactory(t)
	const token = "sk-fake-oat-never-echoed"

	var out bytes.Buffer
	cmd := newSecretsPutCommand()
	cmd.SetIn(strings.NewReader(token + "\n"))
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"agents/claude/claude1/oauth_token", "--from-stdin"})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, *captured)
	assert.Equal(t, []byte(token+"\n"), (*captured).Data)
	assert.NotContains(t, out.String(), token)
}

// TestSecretsPut_FromStdinDevNullIsEmptyNotAPrompt proves `--from-stdin
// </dev/null` is refused as an empty value. /dev/null is a character device
// but not a terminal; taking it for one prompted a user who was not there.
func TestSecretsPut_FromStdinDevNullIsEmptyNotAPrompt(t *testing.T) {
	captured := capturingSecretsFactory(t)
	devNull, err := os.Open(os.DevNull)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, devNull.Close()) })

	var out bytes.Buffer
	cmd := newSecretsPutCommand()
	cmd.SetIn(devNull)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"a/b", "--from-stdin"})
	err = cmd.Execute()

	var typed *SecretsPutSourceError
	require.True(t, stderrors.As(err, &typed), "error %v is not a SecretsPutSourceError", err)
	assert.Contains(t, err.Error(), "empty")
	assert.NotContains(t, out.String(), secretValuePrompt)
	assert.Nil(t, *captured, "an empty value was stored")
}

// TestSecretsPut_FromStdinPipeStoresTheStream proves a real pipe — an
// *os.File that is not a terminal — is read as a stream, not prompted for.
func TestSecretsPut_FromStdinPipeStoresTheStream(t *testing.T) {
	captured := capturingSecretsFactory(t)
	const token = "sk-fake-piped-never-echoed"
	r, w, err := os.Pipe()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, r.Close()) })
	_, err = w.WriteString(token + "\n")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	var out bytes.Buffer
	cmd := newSecretsPutCommand()
	cmd.SetIn(r)
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"agents/claude/claude1/oauth_token", "--from-stdin"})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, *captured)
	assert.Equal(t, []byte(token+"\n"), (*captured).Data)
	assert.NotContains(t, out.String(), token)
}

func TestSecretsPut_DataStillStoresJSONObject(t *testing.T) {
	captured := capturingSecretsFactory(t)

	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"db/prod", "--data", "user=admin", "--data", "pass=a=b"})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, *captured)
	assert.JSONEq(t, `{"user":"admin","pass":"a=b"}`, string((*captured).Data))
}

func TestSecretsPut_ValueSourceErrors(t *testing.T) {
	emptyFile := filepath.Join(t.TempDir(), "empty")
	require.NoError(t, os.WriteFile(emptyFile, nil, 0o600))

	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"no source", []string{"a/b"}, "", "a value is required"},
		{"empty stdin", []string{"a/b", "--from-stdin"}, "", "empty"},
		{"empty file", []string{"a/b", "--from-file", emptyFile}, "", "empty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			capturingSecretsFactory(t)
			cmd := newSecretsPutCommand()
			cmd.SetIn(strings.NewReader(tc.stdin))
			cmd.SetArgs(tc.args)
			err := cmd.Execute()
			require.Error(t, err)
			var typed *SecretsPutSourceError
			require.True(t, stderrors.As(err, &typed), "error %v is not a SecretsPutSourceError", err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

func TestSecretsPut_SourcesAreMutuallyExclusive(t *testing.T) {
	capturingSecretsFactory(t)
	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"a/b", "--data", "k=v", "--from-stdin"})
	require.Error(t, cmd.Execute())
}

func TestSecretsPut_MissingFileIsReadError(t *testing.T) {
	capturingSecretsFactory(t)
	cmd := newSecretsPutCommand()
	cmd.SetArgs([]string{"a/b", "--from-file", filepath.Join(t.TempDir(), "absent")})
	err := cmd.Execute()
	var typed *SecretsPutReadError
	require.True(t, stderrors.As(err, &typed), "error %v is not a SecretsPutReadError", err)
}
