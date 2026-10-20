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

package apimodule

import (
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// TestModule_ProjectsTheQRDBIdentityForEveryService proves the projection is
// the module's, not the prompt service's: AccountService, which refuses a
// caller with no Subject, is handed the qrdb-authenticated caller as its
// agent and tenant.
func TestModule_ProjectsTheQRDBIdentityForEveryService(t *testing.T) {
	var got *service.MaterializeAccountCredentialRequest
	composed := &mocks.MockQuicBaaS{MockAccountService: &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(_ context.Context, req *service.MaterializeAccountCredentialRequest) (*service.MaterializeAccountCredentialResponse, error) {
			got = req
			return &service.MaterializeAccountCredentialResponse{}, nil
		},
	}}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)

	client := pb.NewAccountServiceClient(startAuthenticatedModule(t, m, "tenant-a"))
	_, err = client.MaterializeAccountCredential(context.Background(), &pb.MaterializeAccountCredentialRequest{AccountId: "acct-1"})
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "alice", got.AgentID)
	assert.Equal(t, "tenant-a", got.TenantID)
}

// subjectProbeStream is a server stream carrying a fixed context.
type subjectProbeStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *subjectProbeStream) Context() context.Context { return s.ctx }

// TestLazyServiceDesc_StreamSeesTheProjectedSubject proves a stream handler
// reads the Subject projected from the qrdb identity, and that a stream with
// no identity reaches its handler with no Subject rather than an empty one.
func TestLazyServiceDesc_StreamSeesTheProjectedSubject(t *testing.T) {
	var subject, anonymous bool
	var seen string
	src := &grpc.ServiceDesc{
		ServiceName: "probe.Probe",
		Streams: []grpc.StreamDesc{{StreamName: "Watch", ServerStreams: true, Handler: func(_ any, ss grpc.ServerStream) error {
			s, ok := auth.SubjectFromContext(ss.Context())
			if ok {
				subject, seen = true, s.ID
			} else {
				anonymous = true
			}
			return nil
		}}},
	}
	desc := lazyServiceDesc(src, func() (any, error) { return struct{}{}, nil }, "")
	handler := desc.Streams[0].Handler

	identified := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{Subject: "alice", TenantID: "tenant-a"})
	require.NoError(t, handler(nil, &subjectProbeStream{ctx: identified}))
	assert.True(t, subject)
	assert.Equal(t, "alice", seen)

	require.NoError(t, handler(nil, &subjectProbeStream{ctx: context.Background()}))
	assert.True(t, anonymous)
}
