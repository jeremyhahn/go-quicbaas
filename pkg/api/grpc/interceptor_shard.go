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
	"strconv"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// Metadata keys for platform shard guard interceptor.
const (
	// ShardIDMetadataKey is the gRPC metadata key for shard ID.
	ShardIDMetadataKey = "x-shard-id"

	// RolesMetadataKey is the gRPC metadata key for user roles.
	RolesMetadataKey = "x-roles"
)

// UnaryPlatformShardGuardInterceptor returns a unary server interceptor that
// enforces platform shard access control. It extracts the shard ID from gRPC
// metadata and the caller's admin status from metadata roles, then delegates
// to shards.GuardPlatformShard. Non-admin callers receive a PermissionDenied
// error when targeting the platform shard.
func UnaryPlatformShardGuardInterceptor(log *logger.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if err := guardPlatformShard(ctx, info.FullMethod, log); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}
}

// StreamPlatformShardGuardInterceptor returns a stream server interceptor that
// enforces platform shard access control. It mirrors the unary interceptor
// logic for streaming RPCs.
func StreamPlatformShardGuardInterceptor(log *logger.Logger) grpc.StreamServerInterceptor {
	return func(srv interface{}, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if err := guardPlatformShard(ss.Context(), info.FullMethod, log); err != nil {
			return err
		}
		return handler(srv, ss)
	}
}

// guardPlatformShard performs the platform shard access control check.
// It returns a gRPC PermissionDenied status error when access is denied,
// or nil when access is allowed or no shard ID is present.
func guardPlatformShard(ctx context.Context, method string, log *logger.Logger) error {
	shardID, ok := extractShardIDFromMetadata(ctx)
	if !ok {
		return nil
	}

	hasAdmin := hasAdminRoleFromMetadata(ctx)

	if err := shards.GuardPlatformShard(shardID, hasAdmin); err != nil {
		log.WithFields(map[string]interface{}{
			"method":   method,
			"shard_id": shardID,
		}).Warn("Platform shard access denied")

		return status.Error(codes.PermissionDenied, err.Error())
	}

	return nil
}

// extractShardIDFromMetadata reads the shard ID from incoming gRPC metadata.
// It returns the parsed shard ID and true if the metadata key is present and
// valid, or zero and false otherwise.
func extractShardIDFromMetadata(ctx context.Context) (uint64, bool) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return 0, false
	}

	values := md.Get(ShardIDMetadataKey)
	if len(values) == 0 {
		return 0, false
	}

	id, err := strconv.ParseUint(values[0], 10, 64)
	if err != nil {
		return 0, false
	}

	return id, true
}

// hasAdminRoleFromMetadata checks whether the caller has the admin role by
// inspecting the roles metadata key in the incoming gRPC context.
func hasAdminRoleFromMetadata(ctx context.Context) bool {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false
	}

	roles := md.Get(RolesMetadataKey)
	for _, role := range roles {
		if role == auth.RoleNameAdmin {
			return true
		}
	}

	return false
}
