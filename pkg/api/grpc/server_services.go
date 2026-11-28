// Copyright (c) 2026 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-only

package grpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/serviceplane"
)

// ServicesServer serves quicbaas.services.v1.ServicesService: the service
// plane's seven lifecycle verbs and its two reads, over ServicesService.
type ServicesServer struct {
	svc service.ServicesService
}

// NewServicesServer serves svc over gRPC.
func NewServicesServer(svc service.ServicesService) *ServicesServer {
	return &ServicesServer{svc: svc}
}

// RegisterServicesServiceServer registers srv on server.
func RegisterServicesServiceServer(server *grpc.Server, srv *ServicesServer) {
	pb.RegisterServicesServiceServer(server, srv)
}

func (s *ServicesServer) service() (service.ServicesService, error) {
	if s == nil || s.svc == nil {
		return nil, status.Error(codes.Unavailable, "the service plane is not available on this node")
	}
	return s.svc, nil
}

// Deploy verifies and launches a signed manifest.
func (s *ServicesServer) Deploy(ctx context.Context, req *pb.DeployServiceRequest) (*pb.ServiceRecord, error) {
	return s.deploy(ctx, req, false)
}

// Redeploy replaces a settled service's manifest.
func (s *ServicesServer) Redeploy(ctx context.Context, req *pb.DeployServiceRequest) (*pb.ServiceRecord, error) {
	return s.deploy(ctx, req, true)
}

func (s *ServicesServer) deploy(ctx context.Context, req *pb.DeployServiceRequest, redeploy bool) (*pb.ServiceRecord, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if req == nil || req.Manifest == nil {
		return nil, status.Error(codes.InvalidArgument, "manifest is required")
	}
	in := &service.DeployServiceRequest{Manifest: &serviceplane.SignedManifest{
		Payload:   req.Manifest.Payload,
		KeyID:     req.Manifest.KeyId,
		Signature: req.Manifest.Signature,
	}}
	act := svc.Deploy
	if redeploy {
		act = svc.Redeploy
	}
	rec, err := act(ctx, in)
	if err != nil {
		return nil, toProtoError(err)
	}
	return serviceRecordToProto(rec)
}

// Undeploy halts a settled service, revokes its grants and removes it.
func (s *ServicesServer) Undeploy(ctx context.Context, req *pb.ServiceNameRequest) (*pb.UndeployServiceResponse, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	if err := svc.Undeploy(ctx, &service.ServiceNameRequest{Name: req.Name}); err != nil {
		return nil, toProtoError(err)
	}
	return &pb.UndeployServiceResponse{}, nil
}

// Start launches a stopped service.
func (s *ServicesServer) Start(ctx context.Context, req *pb.ServiceNameRequest) (*pb.ServiceRecord, error) {
	return s.byName(ctx, req, func(svc service.ServicesService) servicesNameVerb { return svc.Start })
}

// Stop halts a running service.
func (s *ServicesServer) Stop(ctx context.Context, req *pb.ServiceNameRequest) (*pb.ServiceRecord, error) {
	return s.byName(ctx, req, func(svc service.ServicesService) servicesNameVerb { return svc.Stop })
}

// Resume launches a halted service.
func (s *ServicesServer) Resume(ctx context.Context, req *pb.ServiceNameRequest) (*pb.ServiceRecord, error) {
	return s.byName(ctx, req, func(svc service.ServicesService) servicesNameVerb { return svc.Resume })
}

// Restart halts and launches a running or halted service.
func (s *ServicesServer) Restart(ctx context.Context, req *pb.ServiceNameRequest) (*pb.ServiceRecord, error) {
	return s.byName(ctx, req, func(svc service.ServicesService) servicesNameVerb { return svc.Restart })
}

// Get reads one of the caller tenant's services.
func (s *ServicesServer) Get(ctx context.Context, req *pb.ServiceNameRequest) (*pb.ServiceRecord, error) {
	return s.byName(ctx, req, func(svc service.ServicesService) servicesNameVerb { return svc.Get })
}

type servicesNameVerb func(context.Context, *service.ServiceNameRequest) (*service.ServiceRecord, error)

func (s *ServicesServer) byName(ctx context.Context, req *pb.ServiceNameRequest, pick func(service.ServicesService) servicesNameVerb) (*pb.ServiceRecord, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}
	rec, err := pick(svc)(ctx, &service.ServiceNameRequest{Name: req.Name})
	if err != nil {
		return nil, toProtoError(err)
	}
	return serviceRecordToProto(rec)
}

// List lists the caller tenant's services.
func (s *ServicesServer) List(ctx context.Context, _ *pb.ListServicesRequest) (*pb.ListServicesResponse, error) {
	svc, err := s.service()
	if err != nil {
		return nil, err
	}
	list, err := svc.List(ctx)
	if err != nil {
		return nil, toProtoError(err)
	}
	out := &pb.ListServicesResponse{Services: make([]*pb.ServiceRecord, 0, len(list.Services))}
	for _, rec := range list.Services {
		p, err := serviceRecordToProto(rec)
		if err != nil {
			return nil, err
		}
		out.Services = append(out.Services, p)
	}
	return out, nil
}

func serviceRecordToProto(rec *service.ServiceRecord) (*pb.ServiceRecord, error) {
	if rec == nil {
		return nil, status.Error(codes.Internal, "the service plane answered no record")
	}
	out := &pb.ServiceRecord{
		Tenant:   rec.Tenant,
		Name:     rec.Name,
		State:    rec.State,
		Verb:     rec.Verb,
		Revision: rec.Revision,
		Restarts: rec.Restarts,
		Grants:   rec.Grants,
		Reason:   rec.Reason,
		KeyId:    rec.KeyID,
	}
	if !rec.UpdatedAt.IsZero() {
		out.UpdatedAtUnixNano = rec.UpdatedAt.UnixNano()
	}
	if rec.Manifest != nil {
		raw, err := json.Marshal(rec.Manifest)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode manifest: %v", err)
		}
		out.ManifestJson = raw
	}
	return out, nil
}
