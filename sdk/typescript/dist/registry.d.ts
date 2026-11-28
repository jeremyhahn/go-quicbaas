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
//# sourceMappingURL=registry.d.ts.map