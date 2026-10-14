//go:build rest

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

package rest

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// sshAuditedService is a QuicBaaS mock that also exposes an audit logger, the
// capability the SSH service needs to record ssh.key.sign events.
type sshAuditedService struct {
	*testutil.MockQuicBaaS
	logger iam.AuditLogger
}

func (s *sshAuditedService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return s.logger, nil
}

// newAuditedSSHTestServer returns a REST server backed by an embedded xkms
// SSH client whose service records audit events into capture.
func newAuditedSSHTestServer(t *testing.T, capture *captureAuditLogger) *Server {
	t.Helper()

	_, sshClient, cleanup := newSSHTestServer(t)
	t.Cleanup(cleanup)

	mock := testutil.NewMockQuicBaaS()
	mock.SSHClientOverride = sshClient

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	config := DefaultConfig()
	config.Address = ":0"
	server, err := NewServer(&sshAuditedService{MockQuicBaaS: mock, logger: capture}, config, log)
	require.NoError(t, err)
	t.Cleanup(func() { server.Close() })
	return server
}

func signSSHDigestRequest(server *Server, name string) *httptest.ResponseRecorder {
	digest := base64.StdEncoding.EncodeToString(make([]byte, 32))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/ssh/keys/user/alice/"+name+"/sign",
		strings.NewReader(`{"digest":"`+digest+`"}`))
	req.Header.Set("Content-Type", "application/json")
	req = withChiURLParams(req, map[string]string{"scope": "user", "owner": "alice", "name": name})
	w := httptest.NewRecorder()
	server.HandleSignSSHDigest(w, req)
	return w
}

// A REST signature goes through the audited SSH service: a handler that
// called the xkms signer directly would sign but record nothing.
func TestSSHSignDigest_AuditsASignature(t *testing.T) {
	capture := &captureAuditLogger{}
	server := newAuditedSSHTestServer(t, capture)
	sshGenerateKey(t, server, "user", "alice", "laptop")

	w := signSSHDigestRequest(server, "laptop")

	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	require.Len(t, capture.events, 1)
	event := capture.events[0]
	assert.Equal(t, "ssh.key.sign", event.Action)
	assert.Equal(t, iam.AuditOutcomeSuccess, event.Outcome)
	assert.Equal(t, "user/alice/laptop", event.Resource.ID)
}

func TestSSHSignDigest_AuditsARefusedSignature(t *testing.T) {
	capture := &captureAuditLogger{}
	server := newAuditedSSHTestServer(t, capture)

	w := signSSHDigestRequest(server, "missing")

	require.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
	require.Len(t, capture.events, 1)
	event := capture.events[0]
	assert.Equal(t, "ssh.key.sign", event.Action)
	assert.Equal(t, iam.AuditOutcomeFailure, event.Outcome)
	assert.NotEmpty(t, event.Details["error"])
}
