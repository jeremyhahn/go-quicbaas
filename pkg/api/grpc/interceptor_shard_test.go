// Copyright 2026 Jeremy Hahn
// SPDX-License-Identifier: Apache-2.0
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// noopUnaryHandler is a handler that returns a fixed response.
func noopUnaryHandler(_ context.Context, _ interface{}) (interface{}, error) {
	return "ok", nil
}

// noopStreamHandler is a handler that returns nil.
func noopStreamHandler(_ interface{}, _ grpc.ServerStream) error {
	return nil
}

func TestUnaryPlatformShardGuardInterceptor_NoMetadata_Allows(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	ctx := context.Background()
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected 'ok', got: %v", resp)
	}
}

func TestUnaryPlatformShardGuardInterceptor_TenantShard_Allows(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(ShardIDMetadataKey, "100")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err != nil {
		t.Fatalf("expected no error for tenant shard, got: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected 'ok', got: %v", resp)
	}
}

func TestUnaryPlatformShardGuardInterceptor_PlatformShard_AdminAllowed(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(
		ShardIDMetadataKey, "1",
		RolesMetadataKey, qrdbsdk.RoleNameAdmin,
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err != nil {
		t.Fatalf("expected no error for admin on platform shard, got: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected 'ok', got: %v", resp)
	}
}

func TestUnaryPlatformShardGuardInterceptor_PlatformShard_NonAdminDenied(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(
		ShardIDMetadataKey, "1",
		RolesMetadataKey, "readwrite",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err == nil {
		t.Fatalf("expected error for non-admin on platform shard, got nil")
	}
	if resp != nil {
		t.Fatalf("expected nil response on denied access, got: %v", resp)
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %T", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got: %v", st.Code())
	}
}

func TestUnaryPlatformShardGuardInterceptor_InvalidShardID_Allows(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(ShardIDMetadataKey, "not-a-number")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	resp, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err != nil {
		t.Fatalf("expected no error for invalid shard ID, got: %v", err)
	}
	if resp != "ok" {
		t.Fatalf("expected 'ok', got: %v", resp)
	}
}

func TestUnaryPlatformShardGuardInterceptor_PlatformShard_NoRoles_Denied(t *testing.T) {
	interceptor := UnaryPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(ShardIDMetadataKey, "1")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	info := &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}

	_, err := interceptor(ctx, nil, info, noopUnaryHandler)
	if err == nil {
		t.Fatalf("expected error for platform shard with no roles")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %T", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got: %v", st.Code())
	}
}

func TestStreamPlatformShardGuardInterceptor_PlatformShard_NonAdminDenied(t *testing.T) {
	interceptor := StreamPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(
		ShardIDMetadataKey, "1",
		RolesMetadataKey, "readonly",
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	ss := &shardMockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}

	err := interceptor(nil, ss, info, noopStreamHandler)
	if err == nil {
		t.Fatalf("expected error for non-admin on platform shard stream")
	}

	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %T", err)
	}
	if st.Code() != codes.PermissionDenied {
		t.Fatalf("expected PermissionDenied, got: %v", st.Code())
	}
}

func TestStreamPlatformShardGuardInterceptor_PlatformShard_AdminAllowed(t *testing.T) {
	interceptor := StreamPlatformShardGuardInterceptor(testLogger(t))

	md := metadata.Pairs(
		ShardIDMetadataKey, "1",
		RolesMetadataKey, qrdbsdk.RoleNameAdmin,
	)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	ss := &shardMockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}

	err := interceptor(nil, ss, info, noopStreamHandler)
	if err != nil {
		t.Fatalf("expected no error for admin on platform shard stream, got: %v", err)
	}
}

func TestStreamPlatformShardGuardInterceptor_NoMetadata_Allows(t *testing.T) {
	interceptor := StreamPlatformShardGuardInterceptor(testLogger(t))

	ctx := context.Background()
	ss := &shardMockServerStream{ctx: ctx}
	info := &grpc.StreamServerInfo{FullMethod: "/test.Service/StreamMethod"}

	err := interceptor(nil, ss, info, noopStreamHandler)
	if err != nil {
		t.Fatalf("expected no error when no metadata, got: %v", err)
	}
}

func TestExtractShardIDFromMetadata_ValidID(t *testing.T) {
	md := metadata.Pairs(ShardIDMetadataKey, "42")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	id, ok := extractShardIDFromMetadata(ctx)
	if !ok {
		t.Fatal("expected ok=true for valid shard ID")
	}
	if id != 42 {
		t.Fatalf("expected shard ID 42, got: %d", id)
	}
}

func TestExtractShardIDFromMetadata_MissingKey(t *testing.T) {
	md := metadata.Pairs("other-key", "value")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, ok := extractShardIDFromMetadata(ctx)
	if ok {
		t.Fatal("expected ok=false for missing shard ID key")
	}
}

func TestExtractShardIDFromMetadata_InvalidValue(t *testing.T) {
	md := metadata.Pairs(ShardIDMetadataKey, "abc")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, ok := extractShardIDFromMetadata(ctx)
	if ok {
		t.Fatal("expected ok=false for invalid shard ID value")
	}
}

func TestExtractShardIDFromMetadata_NoIncomingMetadata(t *testing.T) {
	ctx := context.Background()

	_, ok := extractShardIDFromMetadata(ctx)
	if ok {
		t.Fatal("expected ok=false when no incoming metadata")
	}
}

func TestHasAdminRoleFromMetadata_AdminPresent(t *testing.T) {
	md := metadata.Pairs(RolesMetadataKey, qrdbsdk.RoleNameAdmin)
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if !hasAdminRoleFromMetadata(ctx) {
		t.Fatal("expected true when admin role is present")
	}
}

func TestHasAdminRoleFromMetadata_NoAdminRole(t *testing.T) {
	md := metadata.Pairs(RolesMetadataKey, "readwrite")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if hasAdminRoleFromMetadata(ctx) {
		t.Fatal("expected false when admin role is not present")
	}
}

func TestHasAdminRoleFromMetadata_NoMetadata(t *testing.T) {
	ctx := context.Background()

	if hasAdminRoleFromMetadata(ctx) {
		t.Fatal("expected false when no metadata present")
	}
}

func TestHasAdminRoleFromMetadata_EmptyRoles(t *testing.T) {
	md := metadata.MD{}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	if hasAdminRoleFromMetadata(ctx) {
		t.Fatal("expected false when roles metadata is empty")
	}
}

func TestGuardPlatformShard_ReservedShard_Allows(t *testing.T) {
	// Reserved shards (2-99) are not the platform shard, so should be allowed.
	err := shards.GuardPlatformShard(50, false)
	if err != nil {
		t.Fatalf("expected no error for reserved shard, got: %v", err)
	}
}

// shardMockServerStream implements grpc.ServerStream for testing stream interceptors.
type shardMockServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (m *shardMockServerStream) Context() context.Context {
	return m.ctx
}
