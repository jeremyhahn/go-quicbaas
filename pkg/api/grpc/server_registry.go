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
	stderrors "errors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// RegistryServer serves the service registry over gRPC (and, through the
// shared server, over the Unix-domain socket). Every handler delegates to the
// service-layer RegistryService; the tenant is the one the authorization
// interceptor placed in the request context.
type RegistryServer struct {
	svc service.RegistryService
}

// Compile-time assertion that RegistryServer implements the generated
// interface.
var _ pb.RegistryServiceServer = (*RegistryServer)(nil)

// NewRegistryServer creates the gRPC service registry server.
func NewRegistryServer(svc service.RegistryService) *RegistryServer {
	return &RegistryServer{svc: svc}
}

// RegisterRegistryServiceServer registers the RegistryService on a gRPC
// server.
func RegisterRegistryServiceServer(server *grpc.Server, srv *RegistryServer) {
	pb.RegisterRegistryServiceServer(server, srv)
}

// Register lists an instance under a new lease.
func (s *RegistryServer) Register(ctx context.Context, req *pb.RegisterServiceInstanceRequest) (*pb.RegisterServiceInstanceResponse, error) {
	if req == nil || req.Instance == nil {
		return nil, status.Error(codes.InvalidArgument, "instance is required")
	}
	resp, err := s.svc.Register(ctx, &service.RegisterServiceInstanceRequest{
		Instance:   instanceFromProto(req.Instance),
		TTLSeconds: req.TtlSeconds,
	})
	if err != nil {
		return nil, registryStatus(err)
	}
	return &pb.RegisterServiceInstanceResponse{LeaseId: resp.LeaseID, TtlSeconds: resp.TTLSeconds}, nil
}

// Renew refreshes a registration's lease.
func (s *RegistryServer) Renew(ctx context.Context, req *pb.RenewServiceLeaseRequest) (*pb.RenewServiceLeaseResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	resp, err := s.svc.Renew(ctx, &service.RenewServiceLeaseRequest{LeaseID: req.LeaseId})
	if err != nil {
		return nil, registryStatus(err)
	}
	return &pb.RenewServiceLeaseResponse{LeaseId: resp.LeaseID, TtlSeconds: resp.TTLSeconds}, nil
}

// Deregister revokes a registration's lease.
func (s *RegistryServer) Deregister(ctx context.Context, req *pb.DeregisterServiceInstanceRequest) (*pb.DeregisterServiceInstanceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if err := s.svc.Deregister(ctx, &service.DeregisterServiceInstanceRequest{LeaseID: req.LeaseId}); err != nil {
		return nil, registryStatus(err)
	}
	return &pb.DeregisterServiceInstanceResponse{}, nil
}

// Resolve lists a service's instances.
func (s *RegistryServer) Resolve(ctx context.Context, req *pb.ResolveServiceRequest) (*pb.ResolveServiceResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	resp, err := s.svc.Resolve(ctx, &service.ResolveServiceRequest{Service: req.Service})
	if err != nil {
		return nil, registryStatus(err)
	}
	out := &pb.ResolveServiceResponse{
		Instances: make([]*pb.ServiceInstance, 0, len(resp.Instances)),
		Revision:  resp.Revision,
	}
	for _, inst := range resp.Instances {
		out.Instances = append(out.Instances, instanceToProto(inst))
	}
	return out, nil
}

// Watch streams a service's registration changes until the client cancels or
// the store ends the watch.
func (s *RegistryServer) Watch(req *pb.WatchServiceRequest, stream pb.RegistryService_WatchServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}
	ctx := stream.Context()
	events, err := s.svc.Watch(ctx, &service.WatchServiceRequest{Service: req.Service, FromRevision: req.FromRevision})
	if err != nil {
		return registryStatus(err)
	}
	for {
		select {
		case <-ctx.Done():
			return status.FromContextError(ctx.Err()).Err()
		case ev, ok := <-events:
			if !ok {
				return nil
			}
			if ev.Err != nil {
				return registryStatus(ev.Err)
			}
			if err := stream.Send(eventToProto(ev)); err != nil {
				return err
			}
		}
	}
}

// registryStatus maps a registry failure to its gRPC status. A compacted
// watch start revision is OUT_OF_RANGE — distinct from a malformed request —
// so a client can tell "resolve again" from "fix the call".
func registryStatus(err error) error {
	var compacted *registry.WatchCompactedError
	if stderrors.As(err, &compacted) {
		return status.Error(codes.OutOfRange, compacted.Error())
	}
	return toProtoError(err)
}

// registryEventTypes maps registry event types onto their proto enum.
var registryEventTypes = map[service.RegistryEventType]pb.RegistryEvent_EventType{
	service.RegistryEventPut:    pb.RegistryEvent_EVENT_TYPE_PUT,
	service.RegistryEventDelete: pb.RegistryEvent_EVENT_TYPE_DELETE,
}

// eventToProto converts a registry event to its proto form.
func eventToProto(ev service.RegistryEvent) *pb.RegistryEvent {
	return &pb.RegistryEvent{
		Type:     registryEventTypes[ev.Type],
		Instance: instanceToProto(ev.Instance),
		Revision: ev.Revision,
	}
}

// instanceToProto converts a registered instance to its proto form.
func instanceToProto(inst service.RegistryServiceInstance) *pb.ServiceInstance {
	return &pb.ServiceInstance{
		Id:       inst.ID,
		Service:  inst.Service,
		Address:  inst.Address,
		Metadata: inst.Metadata,
	}
}

// instanceFromProto converts a proto instance to the service form.
func instanceFromProto(inst *pb.ServiceInstance) service.RegistryServiceInstance {
	return service.RegistryServiceInstance{
		ID:       inst.Id,
		Service:  inst.Service,
		Address:  inst.Address,
		Metadata: inst.Metadata,
	}
}
