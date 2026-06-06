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

// Package proto provides gRPC extensions for the DeleteKeyPolicy operation.
//
// This file is a temporary extension that adds DeleteKeyPolicy to the gRPC
// service without full protobuf code regeneration. When the proto toolchain is
// available, regenerate kms_grpc.pb.go from kms.proto and remove this file.
package proto

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// KeyManagement_DeleteKeyPolicy_FullMethodName is the gRPC method full name
	// for the DeleteKeyPolicy operation.
	KeyManagement_DeleteKeyPolicy_FullMethodName = "/quicbaas.kms.v1.KeyManagement/DeleteKeyPolicy"
)

// KeyManagementDeleteKeyPolicyServer is the minimal server interface for the
// DeleteKeyPolicy extension. Concrete KMS server types implement this alongside
// the generated KeyManagementServer.
type KeyManagementDeleteKeyPolicyServer interface {
	DeleteKeyPolicy(context.Context, *DeleteKeyPolicyRequest) (*DeleteKeyPolicyResponse, error)
}

// RegisterDeleteKeyPolicyHandler registers the DeleteKeyPolicy gRPC handler on
// s using the provided implementation. Call this immediately after
// RegisterKeyManagementServer so the method is reachable via gRPC.
func RegisterDeleteKeyPolicyHandler(s grpc.ServiceRegistrar, srv KeyManagementDeleteKeyPolicyServer) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "quicbaas.kms.v1.KeyManagement",
		HandlerType: (*KeyManagementDeleteKeyPolicyServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "DeleteKeyPolicy",
				Handler:    deleteKeyPolicyHandler,
			},
		},
		Streams: []grpc.StreamDesc{},
	}, srv)
}

func deleteKeyPolicyHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteKeyPolicyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeyManagementDeleteKeyPolicyServer).DeleteKeyPolicy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: KeyManagement_DeleteKeyPolicy_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeyManagementDeleteKeyPolicyServer).DeleteKeyPolicy(ctx, req.(*DeleteKeyPolicyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// UnimplementedDeleteKeyPolicyServer provides a default implementation that
// returns codes.Unimplemented. Embed in concrete server types to satisfy the
// KeyManagementDeleteKeyPolicyServer interface without breaking forward
// compatibility.
type UnimplementedDeleteKeyPolicyServer struct{}

// DeleteKeyPolicy returns an Unimplemented error by default.
func (UnimplementedDeleteKeyPolicyServer) DeleteKeyPolicy(_ context.Context, _ *DeleteKeyPolicyRequest) (*DeleteKeyPolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method DeleteKeyPolicy not implemented")
}
