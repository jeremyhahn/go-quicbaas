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
	"strings"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	servicesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/services"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isServicesService reports whether the service name targets the service
// plane. This branch also serves the Unix transport, which delegates to gRPC.
func isServicesService(svc string) bool {
	return strings.HasPrefix(svc, servicesclient.ServiceName)
}

type servicesNameRPC func(pb.ServicesServiceClient, context.Context, *pb.ServiceNameRequest, ...grpc.CallOption) (*pb.ServiceRecord, error)

type servicesManifestRPC func(pb.ServicesServiceClient, context.Context, *pb.DeployServiceRequest, ...grpc.CallOption) (*pb.ServiceRecord, error)

var servicesNameRPCs = map[string]servicesNameRPC{
	"Start":   pb.ServicesServiceClient.Start,
	"Stop":    pb.ServicesServiceClient.Stop,
	"Resume":  pb.ServicesServiceClient.Resume,
	"Restart": pb.ServicesServiceClient.Restart,
	"Get":     pb.ServicesServiceClient.Get,
}

var servicesManifestRPCs = map[string]servicesManifestRPC{
	"Deploy":   pb.ServicesServiceClient.Deploy,
	"Redeploy": pb.ServicesServiceClient.Redeploy,
}

// handleServicesRequest converts a services SDK request into the generated
// ServicesService call and the reply back into the SDK type.
func handleServicesRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	op := "grpc.services." + method
	client := pb.NewServicesServiceClient(conn)
	if rpc, ok := servicesManifestRPCs[method]; ok {
		r, ok := req.(*servicesclient.DeployRequest)
		if !ok || r == nil || r.Manifest == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.DeployRequest with a manifest")
		}
		rec, err := rpc(client, ctx, &pb.DeployServiceRequest{Manifest: &pb.SignedServiceManifest{
			Payload: r.Manifest.Payload, KeyId: r.Manifest.KeyID, Signature: r.Manifest.Signature,
		}})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		return servicesRecordFromProto(rec), nil
	}
	if rpc, ok := servicesNameRPCs[method]; ok {
		r, ok := req.(*servicesclient.NameRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.NameRequest")
		}
		rec, err := rpc(client, ctx, &pb.ServiceNameRequest{Name: r.Name})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		return servicesRecordFromProto(rec), nil
	}
	switch method {
	case "Undeploy":
		r, ok := req.(*servicesclient.NameRequest)
		if !ok || r == nil {
			return nil, errors.NewInvalidInput(op, "invalid request type, expected *services.NameRequest")
		}
		if _, err := client.Undeploy(ctx, &pb.ServiceNameRequest{Name: r.Name}); err != nil {
			return nil, convertGRPCError(err, op)
		}
		return nil, nil
	case "List":
		resp, err := client.List(ctx, &pb.ListServicesRequest{})
		if err != nil {
			return nil, convertGRPCError(err, op)
		}
		out := &servicesclient.ListResponse{Services: make([]servicesclient.Record, 0, len(resp.Services))}
		for _, rec := range resp.Services {
			out.Services = append(out.Services, *servicesRecordFromProto(rec))
		}
		return out, nil
	}
	return nil, errors.NewUnimplemented(op, "method not exposed by ServicesService")
}

func servicesRecordFromProto(rec *pb.ServiceRecord) *servicesclient.Record {
	out := &servicesclient.Record{
		Tenant: rec.Tenant, Name: rec.Name, State: rec.State, Verb: rec.Verb,
		Revision: rec.Revision, Restarts: rec.Restarts, Grants: rec.Grants,
		Reason: rec.Reason, KeyID: rec.KeyId, Manifest: rec.ManifestJson,
	}
	if rec.UpdatedAtUnixNano != 0 {
		out.UpdatedAt = time.Unix(0, rec.UpdatedAtUnixNano).UTC()
	}
	return out
}
