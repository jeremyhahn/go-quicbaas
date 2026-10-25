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

package service

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/sshfake"
)

var errAuditUnavailable = stderrors.New("audit log unavailable")

// auditSource hands out logger, or fails with err.
type auditSource struct {
	logger iam.AuditLogger
	err    error
}

func (s auditSource) GetAuditLogger(context.Context) (iam.AuditLogger, error) {
	return s.logger, s.err
}

// auditedQuicBaaS is a QuicBaaS that carries an SSH client and an audit log;
// every other method is the nil embedded interface and must not be reached.
type auditedQuicBaaS struct {
	QuicBaaS
	auditSource
	client xkmsssh.Client
}

func (q auditedQuicBaaS) SSHClient() xkmsssh.Client { return q.client }

// agentSessionContext is the context of a call an agent session's
// certificate authenticated.
func agentSessionContext() context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            "agent-session-42",
		Username:      "agent-session-42",
		UserID:        1042,
		TenantID:      "acme",
		Roles:         []string{"agent-session"},
		Authenticated: true,
	})
}

func signRequest() SSHSignRequest {
	return SSHSignRequest{Scope: "tenant", Owner: "acme", Name: "project-signing", Digest: []byte("digest")}
}

func TestSSHService_SignDigestAuditsThePrincipal(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	logger := &mockAuditLogger{}
	svc := NewSSHService(fake, WithSSHAudit(auditSource{logger: logger}))

	_, err := svc.SignDigest(agentSessionContext(), signRequest())
	require.NoError(t, err)

	require.Len(t, logger.events, 1)
	event := logger.events[0]
	assert.Equal(t, sshSignAuditAction, event.Action)
	assert.Equal(t, iam.AuditCategorySecurity, event.Category)
	assert.Equal(t, iam.AuditOutcomeSuccess, event.Outcome)
	assert.Equal(t, "acme", event.TenantID)
	require.NotNil(t, event.Actor)
	assert.Equal(t, "1042", event.Actor.UserID)
	assert.Equal(t, "agent-session-42", event.Actor.Username)
	assert.Equal(t, &iam.AuditResource{Type: "ssh_key", ID: "tenant/acme/project-signing", Name: "project-signing"}, event.Resource)
}

func TestSSHService_SignDigestWithoutAUserIDLeavesTheActorUserIDEmpty(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	logger := &mockAuditLogger{}
	svc := NewSSHService(fake, WithSSHAudit(auditSource{logger: logger}))
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "svc-signer", TenantID: "acme", Authenticated: true,
	})

	_, err := svc.SignDigest(ctx, signRequest())
	require.NoError(t, err)

	require.Len(t, logger.events, 1)
	require.NotNil(t, logger.events[0].Actor)
	assert.Empty(t, logger.events[0].Actor.UserID)
	assert.Equal(t, "svc-signer", logger.events[0].Actor.Username)
}

// TestSSHService_SignDigestAuditsABoundBearerByUsername signs as alice's
// bearer identity after qrdb's BindUser set its Subject to her account id:
// the audit names her username and account id, never the numeric Subject.
func TestSSHService_SignDigestAuditsABoundBearerByUsername(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	logger := &mockAuditLogger{}
	svc := NewSSHService(fake, WithSSHAudit(auditSource{logger: logger}))
	identity := auth.MarkTokenSubject(&qrdbsdk.Identity{Subject: "alice", TenantID: "acme"}, 42)
	identity.BindUser(42, "alice")
	ctx := auth.ContextWithIdentitySubject(qrdbsdk.WithIdentity(context.Background(), identity))

	_, err := svc.SignDigest(ctx, signRequest())
	require.NoError(t, err)

	require.Len(t, logger.events, 1)
	require.NotNil(t, logger.events[0].Actor)
	assert.Equal(t, "alice", logger.events[0].Actor.Username)
	assert.Equal(t, "42", logger.events[0].Actor.UserID)
}

func TestSSHService_SignDigestAuditsARefusedSignature(t *testing.T) {
	// SignerPublicKey unset => the fake reports NotFound from Signer().
	logger := &mockAuditLogger{}
	svc := NewSSHService(sshfake.NewClient(), WithSSHAudit(auditSource{logger: logger}))

	_, err := svc.SignDigest(agentSessionContext(), signRequest())
	var notFound *xkmsssh.NotFoundError
	require.ErrorAs(t, err, &notFound)

	require.Len(t, logger.events, 1)
	event := logger.events[0]
	assert.Equal(t, iam.AuditOutcomeFailure, event.Outcome)
	assert.Equal(t, "1042", event.Actor.UserID)
	assert.Equal(t, err.Error(), event.Details["error"])
}

func TestSSHService_SignDigestSignsWhenTheAuditLogIsUnavailable(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	svc := NewSSHService(fake, WithSSHAudit(auditSource{err: errAuditUnavailable}))

	sig, err := svc.SignDigest(agentSessionContext(), signRequest())
	require.NoError(t, err)
	assert.Equal(t, "ssh-ed25519", sig.Format)
}

func TestSSHService_SignDigestWithoutAPrincipalNamesNoActor(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	logger := &mockAuditLogger{}
	svc := NewSSHService(fake, WithSSHAudit(auditSource{logger: logger}))

	_, err := svc.SignDigest(context.Background(), signRequest())
	require.NoError(t, err)
	require.Len(t, logger.events, 1)
	assert.Nil(t, logger.events[0].Actor)
}

func TestSSHServiceFor_AuditsThroughTheService(t *testing.T) {
	fake := sshfake.NewClient()
	fake.SignerPublicKey = testSSHPublicKey(t)
	logger := &mockAuditLogger{}
	svc := SSHServiceFor(auditedQuicBaaS{auditSource: auditSource{logger: logger}, client: fake})

	_, err := svc.SignDigest(agentSessionContext(), signRequest())
	require.NoError(t, err)
	require.Len(t, logger.events, 1)
	assert.Equal(t, "1042", logger.events[0].Actor.UserID)
}

func TestSSHServiceFor_NilServiceIsNotConfigured(t *testing.T) {
	_, err := SSHServiceFor(nil).SignDigest(context.Background(), signRequest())
	var notConfigured *SSHNotConfiguredError
	require.ErrorAs(t, err, &notConfigured)
}
