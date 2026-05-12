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

package quicbaas

import (
	pkgservice "github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Service-Prefixed Type Aliases
//
// These re-export types from pkg/service with a "Service" prefix so that
// SDK consumers (e.g. go-xkms) can reference them unambiguously from
// the top-level quicbaas package without colliding with identically-named
// types from other sub-packages.
// =============================================================================

// ServiceQuicBaaS is the main QuicBaaS unified service facade interface.
type ServiceQuicBaaS = pkgservice.QuicBaaS

// =============================================================================
// KV Request/Response Types (service-prefixed)
// =============================================================================

type (
	// ServicePutRequest is a service-prefixed alias for service.PutRequest.
	ServicePutRequest = pkgservice.PutRequest

	// ServicePutResponse is a service-prefixed alias for service.PutResponse.
	ServicePutResponse = pkgservice.PutResponse

	// ServiceGetRequest is a service-prefixed alias for service.GetRequest.
	ServiceGetRequest = pkgservice.GetRequest

	// ServiceGetResponse is a service-prefixed alias for service.GetResponse.
	ServiceGetResponse = pkgservice.GetResponse

	// ServiceDeleteRequest is a service-prefixed alias for service.DeleteRequest.
	ServiceDeleteRequest = pkgservice.DeleteRequest

	// ServiceDeleteResponse is a service-prefixed alias for service.DeleteResponse.
	ServiceDeleteResponse = pkgservice.DeleteResponse

	// ServiceListRequest is a service-prefixed alias for service.ListRequest.
	ServiceListRequest = pkgservice.ListRequest

	// ServiceListResponse is a service-prefixed alias for service.ListResponse.
	ServiceListResponse = pkgservice.ListResponse
)

// =============================================================================
// Watch Types (service-prefixed)
// =============================================================================

type (
	// ServiceWatchRequest is a service-prefixed alias for service.WatchRequest.
	ServiceWatchRequest = pkgservice.WatchRequest

	// ServiceWatchResponse is a service-prefixed alias for service.WatchResponse.
	ServiceWatchResponse = pkgservice.WatchResponse

	// ServiceWatchEvent is a service-prefixed alias for service.WatchEvent.
	ServiceWatchEvent = pkgservice.WatchEvent
)

// =============================================================================
// Auth Types (service-prefixed)
// =============================================================================

type (
	// ServiceValidateTokenRequest is a service-prefixed alias for service.ValidateTokenRequest.
	ServiceValidateTokenRequest = pkgservice.ValidateTokenRequest

	// ServiceValidateTokenResponse is a service-prefixed alias for service.ValidateTokenResponse.
	ServiceValidateTokenResponse = pkgservice.ValidateTokenResponse

	// ServiceRefreshTokenRequest is a service-prefixed alias for service.RefreshTokenRequest.
	ServiceRefreshTokenRequest = pkgservice.RefreshTokenRequest

	// ServiceRefreshTokenResponse is a service-prefixed alias for service.RefreshTokenResponse.
	ServiceRefreshTokenResponse = pkgservice.RefreshTokenResponse

	// ServiceLogoutRequest is a service-prefixed alias for service.LogoutRequest.
	ServiceLogoutRequest = pkgservice.LogoutRequest

	// ServiceLogoutResponse is a service-prefixed alias for service.LogoutResponse.
	ServiceLogoutResponse = pkgservice.LogoutResponse
)

// =============================================================================
// Cluster Types (service-prefixed)
// =============================================================================

type (
	// ServiceClusterStatusRequest is a service-prefixed alias for service.ClusterStatusRequest.
	ServiceClusterStatusRequest = pkgservice.ClusterStatusRequest

	// ServiceClusterStatusResponse is a service-prefixed alias for service.ClusterStatusResponse.
	ServiceClusterStatusResponse = pkgservice.ClusterStatusResponse

	// ServiceGetLeaderRequest is a service-prefixed alias for service.GetLeaderRequest.
	ServiceGetLeaderRequest = pkgservice.GetLeaderRequest

	// ServiceGetLeaderResponse is a service-prefixed alias for service.GetLeaderResponse.
	ServiceGetLeaderResponse = pkgservice.GetLeaderResponse

	// ServiceNodeInfo is a service-prefixed alias for service.NodeInfo.
	ServiceNodeInfo = pkgservice.NodeInfo

	// ServiceShardInfo is a service-prefixed alias for service.ShardInfo.
	ServiceShardInfo = pkgservice.ShardInfo
)

// =============================================================================
// Entity Type (service-prefixed)
// =============================================================================

// ServiceEntity is a service-prefixed alias for service.Entity.
type ServiceEntity = pkgservice.Entity

// =============================================================================
// Health Status Constants (service-prefixed)
// =============================================================================

const (
	// ServiceHealthStatusHealthy indicates the component is fully operational.
	ServiceHealthStatusHealthy = pkgservice.HealthStatusHealthy

	// ServiceHealthStatusDegraded indicates the component is operational but impaired.
	ServiceHealthStatusDegraded = pkgservice.HealthStatusDegraded

	// ServiceHealthStatusUnhealthy indicates the component is not operational.
	ServiceHealthStatusUnhealthy = pkgservice.HealthStatusUnhealthy
)

// =============================================================================
// Watch Event Type Constants (service-prefixed)
// =============================================================================

const (
	// ServiceWatchEventTypePut indicates a key was created or updated.
	ServiceWatchEventTypePut = pkgservice.WatchEventTypePut

	// ServiceWatchEventTypeDelete indicates a key was deleted.
	ServiceWatchEventTypeDelete = pkgservice.WatchEventTypeDelete

	// ServiceWatchEventTypeUnspecified is the default/unset value.
	ServiceWatchEventTypeUnspecified = pkgservice.WatchEventTypeUnspecified
)
