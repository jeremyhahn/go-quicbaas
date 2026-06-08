//go:build rest

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

package rest

import (
	"net/http"
	"strconv"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
)

// ShardIDHeader is the HTTP header used to specify the target shard ID.
const ShardIDHeader = "X-Shard-ID"

// PlatformShardGuardMiddleware returns HTTP middleware that enforces platform
// shard access control. It extracts the shard ID from the X-Shard-ID request
// header and the caller's admin status from context (set by the authorization
// middleware), then delegates to shards.GuardPlatformShard. Non-admin callers
// receive HTTP 403 Forbidden when targeting the platform shard.
func PlatformShardGuardMiddleware(log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			shardID, ok := extractShardIDFromHeader(r)
			if !ok {
				// No shard ID header present; allow the request through.
				next.ServeHTTP(w, r)
				return
			}

			hasAdmin := hasAdminRoleFromContext(r)

			if err := shards.GuardPlatformShard(shardID, hasAdmin); err != nil {
				log.WithFields(map[string]interface{}{
					"path":       r.URL.Path,
					"method":     r.Method,
					"shard_id":   shardID,
					"ip_address": getClientIP(r),
					"request_id": GetRequestID(r.Context()),
				}).Warn("Platform shard access denied")

				RespondError(w, r, http.StatusForbidden, &PlatformShardForbiddenError{
					ShardID: shardID,
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractShardIDFromHeader reads the shard ID from the X-Shard-ID HTTP header.
// It returns the parsed shard ID and true if the header is present and valid,
// or zero and false otherwise.
func extractShardIDFromHeader(r *http.Request) (uint64, bool) {
	shardStr := r.Header.Get(ShardIDHeader)
	if shardStr == "" {
		return 0, false
	}

	id, err := strconv.ParseUint(shardStr, 10, 64)
	if err != nil {
		return 0, false
	}

	return id, true
}

// hasAdminRoleFromContext checks whether the caller has the admin role by
// inspecting the user roles stored in the request context by the upstream
// authorization middleware.
func hasAdminRoleFromContext(r *http.Request) bool {
	roles := GetUserRoles(r.Context())
	for _, role := range roles {
		if role == qrdbsdk.RoleNameAdmin {
			return true
		}
	}
	return false
}

// PlatformShardForbiddenError is returned when a non-admin caller attempts
// to access the platform shard via the REST API.
type PlatformShardForbiddenError struct {
	ShardID uint64
}

// Error returns a human-readable description of the platform shard access denial.
func (e *PlatformShardForbiddenError) Error() string {
	return "platform shard access denied for shard " + strconv.FormatUint(e.ShardID, 10)
}
