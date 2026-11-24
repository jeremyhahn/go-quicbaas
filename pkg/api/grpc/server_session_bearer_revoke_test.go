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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// fakeBearerRevoker records the call it receives and answers with count.
type fakeBearerRevoker struct {
	calls   int
	tenant  string
	subject string
	keep    string
	count   int
}

func (f *fakeBearerRevoker) RevokeSubjectBearers(_ context.Context, tenantID, subject, keepSessionID string) (int, error) {
	f.calls++
	f.tenant, f.subject, f.keep = tenantID, subject, keepSessionID
	return f.count, nil
}

func TestRevokeOtherSessions_NotFoundRevokesOtherBearers(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)
	revoker := &fakeBearerRevoker{count: 3}
	srv = srv.WithBearerRevoker(revoker)

	resp, err := srv.RevokeOtherSessions(sessionOwnerContext("tenant-1", 10001), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: "sid-of-the-current-bearer",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, revoker.calls)
	assert.Equal(t, "tenant-1", revoker.tenant)
	assert.Equal(t, "10001", revoker.subject)
	assert.Equal(t, "sid-of-the-current-bearer", revoker.keep)
	assert.Equal(t, int32(1+3), resp.RevokedCount)
}

func TestRevokeOtherSessions_FoundRevokesOtherBearers(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	keep := svc.CreateTestSession("tenant-1", 10001)
	revoker := &fakeBearerRevoker{count: 2}
	srv = srv.WithBearerRevoker(revoker)

	resp, err := srv.RevokeOtherSessions(sessionOwnerContext("tenant-1", 10001), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: keep.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 1, revoker.calls)
	assert.Equal(t, keep.ID, revoker.keep)
	assert.Equal(t, int32(2), resp.RevokedCount)
}

func TestRevokeAllSessions_RevokesBearers(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)
	revoker := &fakeBearerRevoker{count: 2}
	srv = srv.WithBearerRevoker(revoker)

	resp, err := srv.RevokeAllSessions(sessionOwnerContext("tenant-1", 10001), &iampb.RevokeAllSessionsRequest{
		TenantId: "tenant-1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, revoker.calls)
	assert.Equal(t, "tenant-1", revoker.tenant)
	assert.Equal(t, "10001", revoker.subject)
	assert.Equal(t, "", revoker.keep)
	assert.Equal(t, int32(1+2), resp.RevokedCount)
}

func TestRevokeOtherSessions_NoBearerRevokerRefuses(t *testing.T) {
	srv, svc := newTestSessionServer(t)
	svc.CreateTestSession("tenant-1", 10001)

	resp, err := srv.RevokeOtherSessions(sessionOwnerContext("tenant-1", 10001), &iampb.RevokeOtherSessionsRequest{
		TenantId:      "tenant-1",
		KeepSessionId: "sid-of-the-current-bearer",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err))
}
