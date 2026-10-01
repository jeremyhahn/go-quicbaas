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
// service instance registers under a lease; resolution is a prefix range read
// at a known revision; change notification is a prefix watch that can start at
// that revision. When a node dies, its lease expires, qrdb deletes the attached
// entry, and watchers observe the removal — the etcd/Consul service-registry
// pattern, built entirely on qrdb's Lease + Watch + MVCC SDK surface.
//
// The registry is served to remote callers by the service layer
// (pkg/service.RegistryService) on every transport. The tenant every
// operation is scoped to is the AUTHENTICATED tenant the transport's authz
// interceptor places in the request context, never a value the caller
// supplies in a request body.
//
// Storage classification (handbook conventions/data-access.md §3): every key
// here is raw KV under case 5 (internal cluster metadata keyed by name — the
// service and instance names, not a generated ID) and case 7 (the records are
// consumed as a watch stream). Each record is bound to a qrdb lease, which is
// the expiry mechanism; there is no hand-rolled TTL sweep.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
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

	// leasePrefix namespaces the single-tenant lease ownership marks.
	leasePrefix = "__sys:svc-lease/"

	// tenantLeasePrefix namespaces the tenant-scoped lease ownership marks.
	// A mark "__sys:svc-lease-t/{tenant}/{leaseID}" is written under the same
	// lease as the instance it backs, so it expires with the registration and
	// proves, on Renew and Deregister, that the lease is the caller tenant's.
	tenantLeasePrefix = "__sys:svc-lease-t/"

	// maxTenantLen bounds tenant IDs, matching the platform convention.
	maxTenantLen = 128

	// maxNameLen bounds service and instance names so a key stays well inside
	// the store's key-size limit.
	maxNameLen = 253
)

// MinTTL is the shortest lease a registration may hold. A shorter TTL is
// raised to it: a sub-second lease would expire between a client's renewals.
const MinTTL = time.Second

// cleanupTimeout bounds best-effort lease revokes that run on a fresh context.
const cleanupTimeout = 5 * time.Second

// watchEventBuffer sizes the Watch forwarding channel so a momentarily slow
// consumer does not backpressure the underlying watch stream.
const watchEventBuffer = 64

// validateName rejects empty names, names containing the key separator '/'
// (which would let a service or instance name cross into another service's
// namespace during prefix reads and watches), and over-long names.
func validateName(field, s string) error {
	if s == "" {
		return &InvalidInstanceError{Reason: field + " is required"}
	}
	if len(s) > maxNameLen {
		return &InvalidInstanceError{Reason: field + " exceeds " + strconv.Itoa(maxNameLen) + " bytes"}
	}
	if strings.ContainsRune(s, '/') {
		return &InvalidInstanceError{Reason: field + " must not contain '/'"}
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
		if (ch < 'a' || ch > 'z') && (ch < 'A' || ch > 'Z') && (ch < '0' || ch > '9') && ch != '-' && ch != '_' {
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

// String returns the wire name of the event type.
func (t EventType) String() string {
	if name, ok := eventTypeNames[t]; ok {
		return name
	}
	return "UNKNOWN"
}

// eventTypeNames maps event types to their wire names.
var eventTypeNames = map[EventType]string{
	Put:    "PUT",
	Delete: "DELETE",
}

// Event is a service-registry change delivered by Watch.
type Event struct {
	// Type is the kind of change.
	Type EventType

	// Instance is the affected instance. On Delete it is reconstructed from
	// the watch's previous value when available, or carries just the parsed
	// ID and Service derived from the key.
	Instance ServiceInstance

	// Revision is the store revision of the change. Watching again from
	// Revision+1 resumes the stream with nothing lost and nothing repeated.
	Revision int64

	// Err is set only on the final event of a watch the store ended:
	// *WatchCompactedError or *WatchEndedError. The channel closes after it.
	// A watch ended by its context closes the channel with no such event.
	Err error
}

// Backend is the qrdb-primitive seam the registry builds on. The production
// implementation (NewClientBackend) adapts a qrdb SDK client; tests supply a
// fake.
type Backend interface {
	// Grant creates a lease with the given TTL in seconds and returns its ID.
	Grant(ctx context.Context, ttlSeconds int64) (int64, error)

	// KeepAlive refreshes a lease's TTL and returns the TTL it was reset to.
	KeepAlive(ctx context.Context, leaseID int64) (int64, error)

	// Revoke expires a lease, deleting its attached keys.
	Revoke(ctx context.Context, leaseID int64) error

	// PutWithLease stores a key attached to a lease, so the key is deleted
	// automatically when the lease expires.
	PutWithLease(ctx context.Context, key string, value []byte, leaseID int64) error

	// Exists reports whether a key is present.
	Exists(ctx context.Context, key string) (bool, error)

	// Range returns every key-value pair under the prefix together with the
	// store revision the read was served at.
	Range(ctx context.Context, prefix string) (map[string][]byte, int64, error)

	// WatchPrefix streams change events for all keys under the prefix,
	// starting at startRevision (0 = from now).
	WatchPrefix(ctx context.Context, prefix string, startRevision int64) (<-chan qrdbtransport.WatchResponse, error)
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

// leaseOwner is the value of a lease ownership mark. It records what the
// lease backs, which is what an operator inspecting the store needs; the
// ownership proof itself is the mark's presence under the tenant's prefix.
type leaseOwner struct {
	Service string `json:"service"`
	ID      string `json:"id"`
}

// Register registers an instance under a lease with the given TTL and returns
// the lease ID. The caller refreshes the lease via Renew; stopping renewals
// (or calling Deregister) removes the instance. A TTL below MinTTL is raised
// to MinTTL.
func (r *Registry) Register(ctx context.Context, inst ServiceInstance, ttl time.Duration) (int64, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return 0, err
	}
	if err := validateName("service", inst.Service); err != nil {
		return 0, err
	}
	if err := validateName("id", inst.ID); err != nil {
		return 0, err
	}
	if inst.Address == "" {
		return 0, &InvalidInstanceError{Reason: "address is required"}
	}

	if ttl < MinTTL {
		ttl = MinTTL
	}
	ttlSeconds := int64(ttl / time.Second)

	data, err := json.Marshal(inst)
	if err != nil {
		return 0, &RegistryError{Op: "marshal", Err: err}
	}
	owner, err := json.Marshal(leaseOwner{Service: inst.Service, ID: inst.ID})
	if err != nil {
		return 0, &RegistryError{Op: "marshal", Err: err}
	}

	leaseID, err := r.backend.Grant(ctx, ttlSeconds)
	if err != nil {
		return 0, &RegistryError{Op: "grant", Err: err}
	}

	// The ownership mark is written first: an instance must never be visible
	// under a lease its tenant cannot later renew or revoke.
	if err := r.backend.PutWithLease(ctx, leaseKey(tenant, leaseID), owner, leaseID); err != nil {
		return 0, &RegistryError{Op: "put", Err: errors.Join(err, r.revokeOrphan(leaseID))}
	}
	if err := r.backend.PutWithLease(ctx, instanceKey(tenant, inst.Service, inst.ID), data, leaseID); err != nil {
		return 0, &RegistryError{Op: "put", Err: errors.Join(err, r.revokeOrphan(leaseID))}
	}
	return leaseID, nil
}

// revokeOrphan revokes a lease a failed Register left behind, on a fresh
// context so it runs even when the caller's context is already cancelled.
// A revoke failure is reported alongside the write failure; the lease's own
// TTL still removes the orphan.
func (r *Registry) revokeOrphan(leaseID int64) error {
	cleanupCtx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	if err := r.backend.Revoke(cleanupCtx, leaseID); err != nil {
		return &RegistryError{Op: "revoke orphaned lease", Err: err}
	}
	return nil
}

// Renew refreshes the lease backing one of the caller tenant's registrations
// (a heartbeat) and returns the TTL, in seconds, it was reset to.
func (r *Registry) Renew(ctx context.Context, leaseID int64) (int64, error) {
	if err := r.authorizeLease(ctx, leaseID); err != nil {
		return 0, err
	}
	ttl, err := r.backend.KeepAlive(ctx, leaseID)
	if err != nil {
		return 0, &RegistryError{Op: "keepalive", Err: err}
	}
	return ttl, nil
}

// Deregister removes one of the caller tenant's registrations by revoking its
// lease, which deletes the attached entry and notifies watchers.
func (r *Registry) Deregister(ctx context.Context, leaseID int64) error {
	if err := r.authorizeLease(ctx, leaseID); err != nil {
		return err
	}
	if err := r.backend.Revoke(ctx, leaseID); err != nil {
		return &RegistryError{Op: "revoke", Err: err}
	}
	return nil
}

// authorizeLease proves the lease backs a registration of the caller's
// tenant by the presence of its ownership mark. Without this, any caller able
// to reach the registry could renew or revoke another tenant's lease by ID.
func (r *Registry) authorizeLease(ctx context.Context, leaseID int64) error {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return err
	}
	if leaseID <= 0 {
		return &InvalidInstanceError{Reason: "lease ID must be positive"}
	}
	owned, err := r.backend.Exists(ctx, leaseKey(tenant, leaseID))
	if err != nil {
		return &RegistryError{Op: "exists", Err: err}
	}
	if !owned {
		return &LeaseNotFoundError{LeaseID: leaseID}
	}
	return nil
}

// Resolve returns every currently registered instance of a service, ordered
// by instance ID, and the store revision they were read at. Watching from
// that revision plus one observes every later change with nothing missed.
func (r *Registry) Resolve(ctx context.Context, service string) ([]ServiceInstance, int64, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return nil, 0, err
	}
	if err := validateName("service", service); err != nil {
		return nil, 0, err
	}
	pairs, revision, err := r.backend.Range(ctx, servicePrefix(tenant, service))
	if err != nil {
		return nil, 0, &RegistryError{Op: "range", Err: err}
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
	sort.Slice(instances, func(i, j int) bool { return instances[i].ID < instances[j].ID })
	return instances, revision, nil
}

// Watch streams registration changes for a service, starting at fromRevision
// (0 = from now). The channel is closed when ctx is cancelled, or after a
// final event carrying Err when the store ends the watch.
func (r *Registry) Watch(ctx context.Context, service string, fromRevision int64) (<-chan Event, error) {
	tenant, err := tenantFromContext(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateName("service", service); err != nil {
		return nil, err
	}
	if fromRevision < 0 {
		return nil, &InvalidInstanceError{Reason: "from revision must not be negative"}
	}
	prefix := servicePrefix(tenant, service)
	src, err := r.backend.WatchPrefix(ctx, prefix, fromRevision)
	if err != nil {
		if compacted := asCompacted(service, fromRevision, err); compacted != nil {
			return nil, compacted
		}
		return nil, &RegistryError{Op: "watch", Err: err}
	}

	// Buffer so a momentarily slow consumer does not block the forwarding
	// goroutine and backpressure the underlying watch stream.
	out := make(chan Event, watchEventBuffer)
	go func() {
		defer close(out)
		for resp := range src {
			if resp.Canceled {
				final := Event{Err: watchCancelError(service, fromRevision, resp.Err())}
				select {
				case <-ctx.Done():
				case out <- final:
				}
				return
			}
			for _, ev := range resp.Events {
				event, ok := translateEvent(tenant, service, ev, resp.Revision)
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

// asCompacted reports a store watch failure caused by a compacted start
// revision as the registry's typed WatchCompactedError, and returns nil for
// any other failure. Every transport classifies compaction through here, so
// a caller's resync logic matches one error type wherever it is connected.
func asCompacted(service string, fromRevision int64, err error) *WatchCompactedError {
	classified := qrdbtransport.ClassifyWatchError(err)
	var compacted *qrdbtransport.WatchCompactedError
	if errors.As(classified, &compacted) {
		return &WatchCompactedError{Service: service, FromRevision: fromRevision, Err: classified}
	}
	return nil
}

// watchCancelError maps the store's cancellation of a running watch onto the
// registry's typed watch errors.
func watchCancelError(service string, fromRevision int64, err error) error {
	if err == nil {
		err = &qrdbtransport.WatchCanceledError{}
	}
	if compacted := asCompacted(service, fromRevision, err); compacted != nil {
		return compacted
	}
	return &WatchEndedError{Service: service, Err: err}
}

// translateEvent converts a qrdb watch event into a registry Event.
// headerRevision is the revision of the watch response carrying the event,
// used when the event's own key-value carries no modification revision.
func translateEvent(tenant, service string, ev *qrdbtransport.WatchEvent, headerRevision int64) (Event, bool) {
	if ev == nil {
		return Event{}, false
	}
	revision := headerRevision
	if ev.Kv != nil && ev.Kv.ModRevision > 0 {
		revision = ev.Kv.ModRevision
	}
	switch ev.Type {
	case qrdbtransport.EventPut:
		if ev.Kv == nil {
			return Event{}, false
		}
		var inst ServiceInstance
		if err := json.Unmarshal(ev.Kv.Value, &inst); err != nil {
			return Event{}, false
		}
		return Event{Type: Put, Instance: inst, Revision: revision}, true
	case qrdbtransport.EventDelete:
		// On delete the current value is gone; recover the instance from the
		// previous value when the watch carried it, else derive ID from key.
		if ev.PrevKv != nil {
			var inst ServiceInstance
			if err := json.Unmarshal(ev.PrevKv.Value, &inst); err == nil {
				return Event{Type: Delete, Instance: inst, Revision: revision}, true
			}
		}
		key := ""
		if ev.Kv != nil {
			key = string(ev.Kv.Key)
		} else if ev.PrevKv != nil {
			key = string(ev.PrevKv.Key)
		}
		return Event{Type: Delete, Revision: revision, Instance: ServiceInstance{
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

// leaseKey builds the qrdb key of a lease's ownership mark for a tenant.
func leaseKey(tenant string, leaseID int64) string {
	id := strconv.FormatInt(leaseID, 10)
	if tenant == "" {
		return leasePrefix + id
	}
	return tenantLeasePrefix + tenant + "/" + id
}

// instanceIDFromKey extracts the instance ID from a full instance key.
func instanceIDFromKey(tenant, service, key string) string {
	return strings.TrimPrefix(key, servicePrefix(tenant, service))
}
