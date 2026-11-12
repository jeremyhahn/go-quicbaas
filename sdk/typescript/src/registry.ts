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

// ---------------------------------------------------------------------------
// Service registry domain types.
//
// Mirrors pkg/api/rest/handlers_registry.go (/api/v1/registry/*) and the
// types in pkg/service/registry_service.go and pkg/registry. A service
// instance is registered under a TTL lease; the caller renews the lease to
// keep the instance visible and revokes it to deregister.
//
// Wire note: the server writes lease_id as a JSON string holding an int64,
// so it stays a string here (a JavaScript number cannot hold every int64).
//
// The SSE route GET /api/v1/registry/services/{service}/watch has no method
// on this client.
// ---------------------------------------------------------------------------

/** RegistryServiceInstance is one addressable instance of a service. */
export interface RegistryServiceInstance {
  id: string;
  service: string;
  address: string;
  metadata?: Record<string, string>;
}

/** RegistryRegisterRequest is the body of POST /api/v1/registry/instances. */
export interface RegistryRegisterRequest {
  instance: RegistryServiceInstance;
  ttl_seconds: number;
}

/** RegistryLeaseResponse is the lease a register or renew returns. */
export interface RegistryLeaseResponse {
  /** The lease id: an int64 the server writes as a JSON string. */
  lease_id: string;
  ttl_seconds: number;
}

/** RegistryResolveResponse lists a service's live instances. */
export interface RegistryResolveResponse {
  instances: RegistryServiceInstance[];
  revision: number;
}

/** RegistryClient registers, renews, deregisters and resolves service instances. */
export interface RegistryClient {
  /** Registers an instance under a lease of ttlSeconds. */
  register(instance: RegistryServiceInstance, ttlSeconds: number): Promise<RegistryLeaseResponse>;
  /** Renews a lease. */
  renew(leaseId: string): Promise<RegistryLeaseResponse>;
  /** Revokes a lease, removing its instance. */
  deregister(leaseId: string): Promise<void>;
  /** Lists the live instances of a service. */
  resolve(service: string): Promise<RegistryResolveResponse>;
}
