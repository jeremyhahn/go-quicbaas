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

//go:build grpc && iam

package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// mockPEPSubscribeStream implements iampb.PEPService_SubscribeServer for testing.
type mockPEPSubscribeStream struct {
	ctx       context.Context
	sent      []*iampb.PolicyUpdate
	sendError error
}

func (m *mockPEPSubscribeStream) Send(update *iampb.PolicyUpdate) error {
	if m.sendError != nil {
		return m.sendError
	}
	m.sent = append(m.sent, update)
	return nil
}
func (m *mockPEPSubscribeStream) SetHeader(md metadata.MD) error  { return nil }
func (m *mockPEPSubscribeStream) SendHeader(md metadata.MD) error { return nil }
func (m *mockPEPSubscribeStream) SetTrailer(md metadata.MD)       {}
func (m *mockPEPSubscribeStream) Context() context.Context        { return m.ctx }
func (m *mockPEPSubscribeStream) SendMsg(msg interface{}) error   { return nil }
func (m *mockPEPSubscribeStream) RecvMsg(msg interface{}) error   { return nil }

func TestPEPSubscribe_EmptyServiceID(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	stream := &mockPEPSubscribeStream{ctx: context.Background()}

	err := srv.Subscribe(&iampb.SubscribeRequest{ServiceId: ""}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPSubscribe_ServiceError(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(ctx context.Context, _ string, _ []string, _ chan<- *PolicyUpdate) error {
			return &dberrors.QBError{Code: dberrors.ErrInternal, Op: "subscribe", Err: errors.New("subscribe failed")}
		},
	}
	srv := newTestPEPServer(t, svc)
	stream := &mockPEPSubscribeStream{ctx: context.Background()}

	err := srv.Subscribe(&iampb.SubscribeRequest{
		ServiceId:        "test-svc",
		ResourcePatterns: []string{"doc:*"},
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPEPSubscribe_ContextCanceled(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(ctx context.Context, _ string, _ []string, _ chan<- *PolicyUpdate) error {
			<-ctx.Done()
			return ctx.Err()
		},
	}
	srv := newTestPEPServer(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockPEPSubscribeStream{ctx: ctx}

	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	err := srv.Subscribe(&iampb.SubscribeRequest{
		ServiceId:        "test-svc",
		ResourcePatterns: []string{"resource:*"},
	}, stream)
	// Context cancel causes the stream.Context().Done() path
	require.NoError(t, err)
}

func TestPEPSubscribe_NilErrorReturn(t *testing.T) {
	// When the subscribe function returns nil immediately (no error),
	// the errChan receives nil and the select loop terminates gracefully.
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(_ context.Context, _ string, _ []string, _ chan<- *PolicyUpdate) error {
			return nil
		},
	}
	srv := newTestPEPServer(t, svc)
	stream := &mockPEPSubscribeStream{ctx: context.Background()}

	err := srv.Subscribe(&iampb.SubscribeRequest{
		ServiceId:        "test-svc",
		ResourcePatterns: []string{"doc:*"},
	}, stream)
	require.NoError(t, err)
}

func TestPEPSubscribe_Success(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(ctx context.Context, _ string, _ []string, updates chan<- *PolicyUpdate) error {
			// Send 3 updates into the buffered channel, then block until
			// the context is cancelled (server owns the channel close).
			updates <- &PolicyUpdate{
				Type:       PolicyUpdateCreated,
				PolicyData: []byte(`{"effect":"allow"}`),
				PolicyID:   "policy-1",
				Version:    1,
			}
			updates <- &PolicyUpdate{
				Type:       PolicyUpdateUpdated,
				PolicyData: []byte(`{"effect":"deny"}`),
				PolicyID:   "policy-2",
				Version:    2,
			}
			updates <- &PolicyUpdate{
				Type:     PolicyUpdateDeleted,
				PolicyID: "policy-3",
				Version:  3,
			}
			// Block until context cancellation signals end of subscription.
			<-ctx.Done()
			return nil
		},
	}
	srv := newTestPEPServer(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	stream := &mockPEPSubscribeStream{ctx: ctx}

	// Cancel after enough time for updates to be processed.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := srv.Subscribe(&iampb.SubscribeRequest{
		ServiceId:        "test-svc",
		ResourcePatterns: []string{"doc:*", "user:*"},
	}, stream)
	require.NoError(t, err)
	assert.Len(t, stream.sent, 3)
	assert.Equal(t, iampb.PolicyUpdate_CREATED, stream.sent[0].Type)
	assert.Equal(t, "policy-1", stream.sent[0].PolicyId)
	assert.Equal(t, iampb.PolicyUpdate_UPDATED, stream.sent[1].Type)
	assert.Equal(t, iampb.PolicyUpdate_DELETED, stream.sent[2].Type)
}

func TestPEPSubscribe_SendError(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		subscribeFunc: func(ctx context.Context, _ string, _ []string, updates chan<- *PolicyUpdate) error {
			updates <- &PolicyUpdate{
				Type:       PolicyUpdateSnapshot,
				PolicyData: []byte(`{"snapshot":true}`),
				PolicyID:   "policy-snapshot",
				Version:    1,
			}
			// Block until context cancelled; server owns channel close.
			<-ctx.Done()
			return nil
		},
	}
	srv := newTestPEPServer(t, svc)
	stream := &mockPEPSubscribeStream{
		ctx:       context.Background(),
		sendError: errors.New("connection lost"),
	}

	err := srv.Subscribe(&iampb.SubscribeRequest{
		ServiceId:        "test-svc",
		ResourcePatterns: []string{"doc:*"},
	}, stream)
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

// --- PEPServiceAdapter.Check and CheckBatch tests ---

func TestPEPServiceAdapter_Check(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkFunc: func(_ context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{Allowed: true, MatchedPolicy: "p1"}, nil
		},
	}
	srv := newTestPEPServer(t, svc)

	resp, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1", TenantId: "t1"},
		Action:   "read",
		Resource: "doc:1",
	})
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
}

func TestPEPServiceAdapter_CheckBatch_AllDenied(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkBatchFunc: func(_ context.Context, _ *iam.Subject, _ string, resources []string, _ map[string]interface{}) ([]bool, int64, error) {
			results := make([]bool, len(resources))
			// All denied
			return results, 500, nil
		},
	}
	srv := newTestPEPServer(t, svc)

	resp, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user1"},
		Action:    "delete",
		Resources: []string{"doc:1", "doc:2"},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 2)
	assert.False(t, resp.Results[0])
	assert.False(t, resp.Results[1])
}
