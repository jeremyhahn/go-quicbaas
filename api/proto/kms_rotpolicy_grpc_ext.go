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

// Package proto provides gRPC extensions for the KMS rotation-policy operations.
//
// This file is a temporary extension that adds SetKeyRotationPolicy,
// GetKeyRotationPolicy, and DeleteKeyRotationPolicy to the gRPC service
// without full protobuf code regeneration. When the proto toolchain is
// available, regenerate kms_grpc.pb.go from kms.proto and remove this file.
package proto

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	// KeyManagement_SetKeyRotationPolicy_FullMethodName is the gRPC method full name.
	KeyManagement_SetKeyRotationPolicy_FullMethodName = "/quicbaas.kms.v1.KeyManagement/SetKeyRotationPolicy"

	// KeyManagement_GetKeyRotationPolicy_FullMethodName is the gRPC method full name.
	KeyManagement_GetKeyRotationPolicy_FullMethodName = "/quicbaas.kms.v1.KeyManagement/GetKeyRotationPolicy"

	// KeyManagement_DeleteKeyRotationPolicy_FullMethodName is the gRPC method full name.
	KeyManagement_DeleteKeyRotationPolicy_FullMethodName = "/quicbaas.kms.v1.KeyManagement/DeleteKeyRotationPolicy"
)

// KeyManagementRotationPolicyServer is the server interface for the three
// rotation-policy extension RPCs.
type KeyManagementRotationPolicyServer interface {
	SetKeyRotationPolicy(context.Context, *SetKeyRotationPolicyRequest) (*SetKeyRotationPolicyResponse, error)
	GetKeyRotationPolicy(context.Context, *GetKeyRotationPolicyRequest) (*GetKeyRotationPolicyResponse, error)
	DeleteKeyRotationPolicy(context.Context, *DeleteKeyRotationPolicyRequest) (*DeleteKeyRotationPolicyResponse, error)
}

// RegisterKeyRotationPolicyHandler registers the three rotation-policy gRPC
// handlers on s using the provided implementation. Call this immediately after
// RegisterKeyManagementServer so the methods are reachable via gRPC.
func RegisterKeyRotationPolicyHandler(s grpc.ServiceRegistrar, srv KeyManagementRotationPolicyServer) {
	s.RegisterService(&grpc.ServiceDesc{
		ServiceName: "quicbaas.kms.v1.KeyManagement",
		HandlerType: (*KeyManagementRotationPolicyServer)(nil),
		Methods: []grpc.MethodDesc{
			{
				MethodName: "SetKeyRotationPolicy",
				Handler:    setKeyRotationPolicyHandler,
			},
			{
				MethodName: "GetKeyRotationPolicy",
				Handler:    getKeyRotationPolicyHandler,
			},
			{
				MethodName: "DeleteKeyRotationPolicy",
				Handler:    deleteKeyRotationPolicyHandler,
			},
		},
		Streams: []grpc.StreamDesc{},
	}, srv)
}

func setKeyRotationPolicyHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SetKeyRotationPolicyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeyManagementRotationPolicyServer).SetKeyRotationPolicy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: KeyManagement_SetKeyRotationPolicy_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeyManagementRotationPolicyServer).SetKeyRotationPolicy(ctx, req.(*SetKeyRotationPolicyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func getKeyRotationPolicyHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetKeyRotationPolicyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeyManagementRotationPolicyServer).GetKeyRotationPolicy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: KeyManagement_GetKeyRotationPolicy_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeyManagementRotationPolicyServer).GetKeyRotationPolicy(ctx, req.(*GetKeyRotationPolicyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

func deleteKeyRotationPolicyHandler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteKeyRotationPolicyRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeyManagementRotationPolicyServer).DeleteKeyRotationPolicy(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: KeyManagement_DeleteKeyRotationPolicy_FullMethodName,
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeyManagementRotationPolicyServer).DeleteKeyRotationPolicy(ctx, req.(*DeleteKeyRotationPolicyRequest))
	}
	return interceptor(ctx, in, info, handler)
}

// UnimplementedKeyRotationPolicyServer provides default Unimplemented implementations.
// Embed in concrete server types for forward compatibility.
type UnimplementedKeyRotationPolicyServer struct{}

// SetKeyRotationPolicy returns Unimplemented by default.
func (UnimplementedKeyRotationPolicyServer) SetKeyRotationPolicy(_ context.Context, _ *SetKeyRotationPolicyRequest) (*SetKeyRotationPolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method SetKeyRotationPolicy not implemented")
}

// GetKeyRotationPolicy returns Unimplemented by default.
func (UnimplementedKeyRotationPolicyServer) GetKeyRotationPolicy(_ context.Context, _ *GetKeyRotationPolicyRequest) (*GetKeyRotationPolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method GetKeyRotationPolicy not implemented")
}

// DeleteKeyRotationPolicy returns Unimplemented by default.
func (UnimplementedKeyRotationPolicyServer) DeleteKeyRotationPolicy(_ context.Context, _ *DeleteKeyRotationPolicyRequest) (*DeleteKeyRotationPolicyResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method DeleteKeyRotationPolicy not implemented")
}
