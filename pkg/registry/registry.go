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

// Package registry implements service discovery on go-qrdb primitives. A
// service instance registers under a lease; resolution is a prefix range
// scan; change notification is a prefix watch. When a node dies, its lease
// expires, qrdb deletes the attached entry, and watchers observe the removal —
// the etcd/Consul service-registry pattern, built entirely on qrdb's
// Lease + Watch + KV SDK surface.
package registry

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

const (
	// keyPrefix namespaces single-tenant (unscoped) registry keys. It lives
	// under qrdb's "__sys:" system namespace so entries are hidden from
	// user-facing KV list/scan and kept out of the tenant key space.
	keyPrefix = "__sys:svc/"

	// tenantKeyPrefix namespaces tenant-scoped registry keys. Following the
	// established multi-tenant convention (see the time-series service), a
	// non-empty tenant from the request context isolates entries under
	// "__sys:svc-t/{tenant}/...".
	tenantKeyPrefix = "__sys:svc-t/"

	// maxTenantLen bounds tenant IDs, matching the platform convention.
	maxTenantLen = 128
)

// cleanupTimeout bounds best-effort lease revokes that run on a fresh context.
const cleanupTimeout = 5 * time.Second

// watchEventBuffer sizes the Watch forwarding channel so a momentarily slow
// consumer does not backpressure the underlying watch stream.
const watchEventBuffer = 64

// validateName rejects empty names and names containing the key separator '/',
// which would let a service or instance name cross into another service's
// namespace during prefix scans and watches.
func validateName(s string) error {
	if s == "" {
		return &InvalidInstanceError{Reason: "service and id are required"}
	}
	if strings.ContainsRune(s, '/') {
		return &InvalidInstanceError{Reason: "service and id must not contain '/'"}
	}
	return nil
}

// tenantFromContext resolves and validates the tenant for the request, using
// the platform-standard context tenant (empty = single-tenant mode). It mirrors
// the validation rules used elsewhere in the stack: alphanumeric, hyphen, and
// underscore only, bounded length.
func tenantFromContext(ctx context.Context) (string, error) {
	tenant := iam.TenantFromContext(ctx)
	if tenant == "" {
		return "", nil
	}
	if len(tenant) > maxTenantLen {
		return "", &InvalidInstanceError{Reason: "tenant ID exceeds maximum length"}
	}
	for _, ch := range tenant {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '-' || ch == '_') {
			return "", &InvalidInstanceError{Reason: "tenant ID contains invalid character; only alphanumeric, hyphens, and underscores are allowed"}
		}
	}
	return tenant, nil
}

// ServiceInstance describes a registered instance of a named service.
type ServiceInstance struct {
	// ID uniquely identifies this instance within its service.
	ID string `json:"id"`

	// Service is the logical service name instances register under.
	Service string `json:"service"`

	// Address is the instance's network address (host:port).
	Address string `json:"address"`

	// Metadata carries optional descriptive key-value pairs.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// EventType distinguishes instance registration from deregistration.
type EventType uint8

const (
	// Put indicates an instance registered or was updated.
	Put EventType = iota

	// Delete indicates an instance deregistered or its lease expired.
	Delete
)

// Event is a service-registry change delivered by Watch.
type Event struct {
	// Type is the kind of change.
	Type EventType

	// Instance is the affected instance. On Delete it is reconstructed from
	// the watch's previous value when available, or carries just the parsed
	// ID and Service derived from the key.
	Instance ServiceInstance
}

// Backend is the qrdb-primitive seam the registry builds on. The production
// implementation (NewClientBackend) adapts a qrdb SDK client; tests supply a
// fake.
type Backend interface {
	// Grant creates a lease with the given TTL in seconds and returns its ID.
	Grant(ctx context.Context, ttlSeconds int64) (int64, error)

	// KeepAlive refreshes a lease's TTL.
	KeepAlive(ctx context.Context, leaseID int64) error

	// Revoke expires a lease, deleting its attached keys.
	Revoke(ctx context.Context, leaseID int64) error

	// PutWithLease stores a key attached to a lease, so the key is deleted
	// automatically when the lease expires.
	PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error

	// Scan returns all key-value pairs under the given prefix.
	Scan(ctx context.Context, prefix string) (map[string][]byte, error)

	// WatchPrefix streams change events for all keys under the prefix.
	WatchPrefix(ctx context.Context, prefix string) (<-chan qrdbtransport.WatchResponse, error)
}

// Registry is a service-discovery registry backed by qrdb primitives. It is
// multi-tenant: each operation scopes its keys to the tenant carried in the
// request context (the platform-standard tenant pattern). With no tenant in
// context it operates in single-tenant mode.
type Registry struct {
	backend Backend
}

// New creates a Registry over the given Backend.
func New(backend Backend) *Registry {
	return &Registry{backend: backend}
}

// Register registers an instance under a lease with the given TTL and returns
// the lease ID. The caller refreshes the lease via Renew; stopping renewals
// (or calling Deregister) removes the instance.
func (r *Registry) Register(ctx context.Context, inst ServiceInstance, ttl time.Duration) (int64, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateName(inst.Service); err != nil {
		return 0, err
	}
	if err := validateName(inst.ID); err != nil {
		return 0, err
	}

	ttlSeconds := int64(ttl.Seconds())
	if ttlSeconds < 1 {
		ttlSeconds = 1
	}

	leaseID, err := r.backend.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, &RegistryError{Op: "grant", Err: err}
	}

	data, err := json.Marshal(inst)
	if err != nil {
		return 0, &RegistryError{Op: "marshal", Err: err}
	}

	if err := r.backend.PutWithLease(ctx, instanceKey(tenant, inst.Service, inst.ID), data, leaseID); err != nil {
		// Best-effort cleanup of the orphaned lease on a fresh context so it
		// runs even when the caller's context is already cancelled.
		cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
		_ = r.backend.Revoke(cleanupCtx, leaseID)
		cancel()
		return 0, &RegistryError{Op: "put", Err: err}
	}
	return leaseID, nil
}

// Renew refreshes the lease backing a registration (heartbeat).
func (r *Registry) Renew(ctx context.Context, leaseID int64) error {
	if err := r.backend.KeepAlive(ctx, leaseID); err != nil {
		return &RegistryError{Op: "keepalive", Err: err}
	}
	return nil
}

// Deregister removes a registration by revoking its lease, which deletes the
// attached entry and notifies watchers.
func (r *Registry) Deregister(ctx context.Context, leaseID int64) error {
	if err := r.backend.Revoke(ctx, leaseID); err != nil {
		return &RegistryError{Op: "revoke", Err: err}
	}
	return nil
}

// Resolve returns all currently registered instances of a service.
func (r *Registry) Resolve(ctx context.Context, service string) ([]ServiceInstance, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateName(service); err != nil {
		return nil, err
	}
	pairs, err := r.backend.Scan(ctx, servicePrefix(tenant, service))
	if err != nil {
		return nil, &RegistryError{Op: "scan", Err: err}
	}
	instances := make([]ServiceInstance, 0, len(pairs))
	for _, data := range pairs {
		var inst ServiceInstance
		if err := json.Unmarshal(data, &inst); err != nil {
			// Skip malformed entries rather than failing the whole resolve.
			continue
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// Watch streams registration changes for a service. The channel is closed
// when ctx is cancelled.
func (r *Registry) Watch(ctx context.Context, service string) (<-chan Event, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateName(service); err != nil {
		return nil, err
	}
	prefix := servicePrefix(tenant, service)
	src, err := r.backend.WatchPrefix(ctx, prefix)
	if err != nil {
		return nil, &RegistryError{Op: "watch", Err: err}
	}

	// Buffer so a momentarily slow consumer does not block the forwarding
	// goroutine and backpressure the underlying watch stream.
	out := make(chan Event, watchEventBuffer)
	go func() {
		defer close(out)
		for resp := range src {
			for _, ev := range resp.Events {
				event, ok := translateEvent(tenant, service, ev)
				if !ok {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case out <- event:
				}
			}
		}
	}()
	return out, nil
}

// translateEvent converts a qrdb watch event into a registry Event.
func translateEvent(tenant, service string, ev *qrdbtransport.WatchEvent) (Event, bool) {
	switch ev.Type {
	case qrdbtransport.EventPut:
		if ev.Kv == nil {
			return Event{}, false
		}
		var inst ServiceInstance
		if err := json.Unmarshal(ev.Kv.Value, &inst); err != nil {
			return Event{}, false
		}
		return Event{Type: Put, Instance: inst}, true
	case qrdbtransport.EventDelete:
		// On delete the current value is gone; recover the instance from the
		// previous value when the watch carried it, else derive ID from key.
		if ev.PrevKv != nil {
			var inst ServiceInstance
			if err := json.Unmarshal(ev.PrevKv.Value, &inst); err == nil {
				return Event{Type: Delete, Instance: inst}, true
			}
		}
		key := ""
		if ev.Kv != nil {
			key = string(ev.Kv.Key)
		} else if ev.PrevKv != nil {
			key = string(ev.PrevKv.Key)
		}
		return Event{Type: Delete, Instance: ServiceInstance{
			Service: service,
			ID:      instanceIDFromKey(tenant, service, key),
		}}, true
	default:
		return Event{}, false
	}
}

// instanceKey builds the qrdb key for a service instance.
func instanceKey(tenant, service, id string) string {
	return servicePrefix(tenant, service) + id
}

// servicePrefix builds the qrdb key prefix for a tenant's service instances.
// An empty tenant uses the unscoped single-tenant namespace; a non-empty
// tenant isolates the entries under the per-tenant namespace.
func servicePrefix(tenant, service string) string {
	if tenant == "" {
		return keyPrefix + service + "/"
	}
	return tenantKeyPrefix + tenant + "/" + service + "/"
}

// instanceIDFromKey extracts the instance ID from a full instance key.
func instanceIDFromKey(tenant, service, key string) string {
	return strings.TrimPrefix(key, servicePrefix(tenant, service))
}
