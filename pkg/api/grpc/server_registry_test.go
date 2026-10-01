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

package grpc

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// recordingRegistryService records the request each handler passed in and
// replays canned answers.
type recordingRegistryService struct {
	gotRegister   *service.RegisterServiceInstanceRequest
	gotRenew      *service.RenewServiceLeaseRequest
	gotDeregister *service.DeregisterServiceInstanceRequest
	gotResolve    *service.ResolveServiceRequest
	gotWatch      *service.WatchServiceRequest

	events chan service.RegistryEvent
	err    error
}

func (r *recordingRegistryService) Register(_ context.Context, req *service.RegisterServiceInstanceRequest) (*service.RegisterServiceInstanceResponse, error) {
	r.gotRegister = req
	if r.err != nil {
		return nil, r.err
	}
	return &service.RegisterServiceInstanceResponse{LeaseID: 7, TTLSeconds: req.TTLSeconds}, nil
}

func (r *recordingRegistryService) Renew(_ context.Context, req *service.RenewServiceLeaseRequest) (*service.RenewServiceLeaseResponse, error) {
	r.gotRenew = req
	if r.err != nil {
		return nil, r.err
	}
	return &service.RenewServiceLeaseResponse{LeaseID: req.LeaseID, TTLSeconds: 30}, nil
}

func (r *recordingRegistryService) Deregister(_ context.Context, req *service.DeregisterServiceInstanceRequest) error {
	r.gotDeregister = req
	return r.err
}

func (r *recordingRegistryService) Resolve(_ context.Context, req *service.ResolveServiceRequest) (*service.ResolveServiceResponse, error) {
	r.gotResolve = req
	if r.err != nil {
		return nil, r.err
	}
	return &service.ResolveServiceResponse{
		Instances: []service.RegistryServiceInstance{{ID: "a", Service: req.Service, Address: "10.0.0.1:80", Metadata: map[string]string{"k": "v"}}},
		Revision:  41,
	}, nil
}

func (r *recordingRegistryService) Watch(_ context.Context, req *service.WatchServiceRequest) (<-chan service.RegistryEvent, error) {
	r.gotWatch = req
	if r.err != nil {
		return nil, r.err
	}
	return r.events, nil
}

// registryWatchStream collects what the server sends.
type registryWatchStream struct {
	ctx     context.Context
	sent    []*pb.RegistryEvent
	sendErr error
}

func (s *registryWatchStream) Send(ev *pb.RegistryEvent) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.sent = append(s.sent, ev)
	return nil
}
func (s *registryWatchStream) SetHeader(metadata.MD) error  { return nil }
func (s *registryWatchStream) SendHeader(metadata.MD) error { return nil }
func (s *registryWatchStream) SetTrailer(metadata.MD)       {}
func (s *registryWatchStream) Context() context.Context     { return s.ctx }
func (s *registryWatchStream) SendMsg(interface{}) error    { return nil }
func (s *registryWatchStream) RecvMsg(interface{}) error    { return nil }

func TestRegistryServer_Register(t *testing.T) {
	svc := &recordingRegistryService{}
	resp, err := NewRegistryServer(svc).Register(context.Background(), &pb.RegisterServiceInstanceRequest{
		Instance:   &pb.ServiceInstance{Id: "i", Service: "api", Address: "10.0.0.9:443", Metadata: map[string]string{"zone": "z"}},
		TtlSeconds: 12,
	})
	require.NoError(t, err)
	assert.Equal(t, int64(7), resp.LeaseId)
	assert.Equal(t, int64(12), resp.TtlSeconds)
	require.NotNil(t, svc.gotRegister)
	assert.Equal(t, service.RegistryServiceInstance{ID: "i", Service: "api", Address: "10.0.0.9:443", Metadata: map[string]string{"zone": "z"}}, svc.gotRegister.Instance)
	assert.Equal(t, int64(12), svc.gotRegister.TTLSeconds)
}

func TestRegistryServer_RegisterRejectsMissingInstance(t *testing.T) {
	_, err := NewRegistryServer(&recordingRegistryService{}).Register(context.Background(), &pb.RegisterServiceInstanceRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = NewRegistryServer(&recordingRegistryService{}).Register(context.Background(), nil)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestRegistryServer_ErrorMapping(t *testing.T) {
	cases := map[string]struct {
		err  error
		want codes.Code
	}{
		"not found": {&qberrors.QBError{Code: qberrors.ErrNotFound, Op: "x", Err: &registry.LeaseNotFoundError{LeaseID: 1}}, codes.NotFound},
		"invalid":   {&qberrors.QBError{Code: qberrors.ErrInvalidInput, Op: "x", Err: errors.New("bad")}, codes.InvalidArgument},
		"store":     {&qberrors.QBError{Code: qberrors.ErrUnavailable, Op: "x", Err: errors.New("down")}, codes.Unavailable},
		"compacted": {&qberrors.QBError{Code: qberrors.ErrOutOfRange, Op: "x", Err: &registry.WatchCompactedError{Service: "api", Err: errors.New("c")}}, codes.OutOfRange},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := NewRegistryServer(&recordingRegistryService{err: tc.err})
			ctx := context.Background()
			_, err := srv.Renew(ctx, &pb.RenewServiceLeaseRequest{LeaseId: 1})
			assert.Equal(t, tc.want, status.Code(err), "Renew")
			_, err = srv.Deregister(ctx, &pb.DeregisterServiceInstanceRequest{LeaseId: 1})
			assert.Equal(t, tc.want, status.Code(err), "Deregister")
			_, err = srv.Resolve(ctx, &pb.ResolveServiceRequest{Service: "api"})
			assert.Equal(t, tc.want, status.Code(err), "Resolve")
			_, err = srv.Register(ctx, &pb.RegisterServiceInstanceRequest{Instance: &pb.ServiceInstance{}})
			assert.Equal(t, tc.want, status.Code(err), "Register")
			err = srv.Watch(&pb.WatchServiceRequest{Service: "api"}, &registryWatchStream{ctx: ctx})
			assert.Equal(t, tc.want, status.Code(err), "Watch")
		})
	}
}

func TestRegistryServer_RenewDeregisterResolve(t *testing.T) {
	svc := &recordingRegistryService{}
	srv := NewRegistryServer(svc)
	ctx := context.Background()

	renewed, err := srv.Renew(ctx, &pb.RenewServiceLeaseRequest{LeaseId: 99})
	require.NoError(t, err)
	assert.Equal(t, int64(99), svc.gotRenew.LeaseID)
	assert.Equal(t, int64(30), renewed.TtlSeconds)

	_, err = srv.Deregister(ctx, &pb.DeregisterServiceInstanceRequest{LeaseId: 98})
	require.NoError(t, err)
	assert.Equal(t, int64(98), svc.gotDeregister.LeaseID)

	resolved, err := srv.Resolve(ctx, &pb.ResolveServiceRequest{Service: "api"})
	require.NoError(t, err)
	assert.Equal(t, "api", svc.gotResolve.Service)
	assert.Equal(t, int64(41), resolved.Revision)
	require.Len(t, resolved.Instances, 1)
	assert.Equal(t, "10.0.0.1:80", resolved.Instances[0].Address)
	assert.Equal(t, map[string]string{"k": "v"}, resolved.Instances[0].Metadata)

	for name, call := range map[string]func() error{
		"renew":      func() error { _, err := srv.Renew(ctx, nil); return err },
		"deregister": func() error { _, err := srv.Deregister(ctx, nil); return err },
		"resolve":    func() error { _, err := srv.Resolve(ctx, nil); return err },
		"watch":      func() error { return srv.Watch(nil, &registryWatchStream{ctx: ctx}) },
	} {
		assert.Equal(t, codes.InvalidArgument, status.Code(call()), name)
	}
}

func TestRegistryServer_WatchStreamsUntilStoreEnds(t *testing.T) {
	events := make(chan service.RegistryEvent, 3)
	events <- service.RegistryEvent{Type: service.RegistryEventPut, Instance: service.RegistryServiceInstance{ID: "a", Service: "api"}, Revision: 5}
	events <- service.RegistryEvent{Type: service.RegistryEventDelete, Instance: service.RegistryServiceInstance{ID: "a", Service: "api"}, Revision: 6}
	events <- service.RegistryEvent{Err: &registry.WatchCompactedError{Service: "api", FromRevision: 4, Err: errors.New("c")}}
	close(events)

	svc := &recordingRegistryService{events: events}
	stream := &registryWatchStream{ctx: context.Background()}
	err := NewRegistryServer(svc).Watch(&pb.WatchServiceRequest{Service: "api", FromRevision: 4}, stream)

	assert.Equal(t, codes.OutOfRange, status.Code(err), "a compacted watch ends OUT_OF_RANGE")
	assert.Equal(t, int64(4), svc.gotWatch.FromRevision)
	require.Len(t, stream.sent, 2)
	assert.Equal(t, pb.RegistryEvent_EVENT_TYPE_PUT, stream.sent[0].Type)
	assert.Equal(t, int64(5), stream.sent[0].Revision)
	assert.Equal(t, pb.RegistryEvent_EVENT_TYPE_DELETE, stream.sent[1].Type)
	assert.Equal(t, int64(6), stream.sent[1].Revision)
}

func TestRegistryServer_WatchEndsCleanlyAndOnSendFailure(t *testing.T) {
	closed := make(chan service.RegistryEvent)
	close(closed)
	err := NewRegistryServer(&recordingRegistryService{events: closed}).
		Watch(&pb.WatchServiceRequest{Service: "api"}, &registryWatchStream{ctx: context.Background()})
	assert.NoError(t, err, "a watch whose channel closes ends cleanly")

	one := make(chan service.RegistryEvent, 1)
	one <- service.RegistryEvent{Type: service.RegistryEventPut, Revision: 1}
	sendErr := errors.New("client gone")
	err = NewRegistryServer(&recordingRegistryService{events: one}).
		Watch(&pb.WatchServiceRequest{Service: "api"}, &registryWatchStream{ctx: context.Background(), sendErr: sendErr})
	assert.ErrorIs(t, err, sendErr)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = NewRegistryServer(&recordingRegistryService{events: make(chan service.RegistryEvent)}).
		Watch(&pb.WatchServiceRequest{Service: "api"}, &registryWatchStream{ctx: ctx})
	assert.Equal(t, codes.Canceled, status.Code(err))
}
