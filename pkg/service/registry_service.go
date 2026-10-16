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

package service

import (
	"context"
	stderrors "errors"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
)

// RegistryService is the service registry served on every transport: a
// lease-based directory of service-instance endpoints. An instance registers
// under a lease and stays listed while its owner renews the lease; when the
// owner stops (or its host dies) the lease expires, the entry disappears and
// every watcher observes the removal.
//
// The service is a thin layer over pkg/registry, which holds the storage
// logic on go-qrdb's Lease, MVCC and Watch primitives. What this layer adds
// is the transport contract: request validation, the typed errors every
// transport maps onto its own status codes, and the shard the registry lives
// on. The tenant is the authenticated tenant the transport's authz
// interceptor placed in the request context; no request field names one.
type RegistryService interface {
	// Register lists an instance under a new lease and returns the lease.
	Register(ctx context.Context, req *RegisterServiceInstanceRequest) (*RegisterServiceInstanceResponse, error)

	// Renew refreshes the lease backing one of the caller's registrations.
	Renew(ctx context.Context, req *RenewServiceLeaseRequest) (*RenewServiceLeaseResponse, error)

	// Deregister removes one of the caller's registrations by revoking its
	// lease.
	Deregister(ctx context.Context, req *DeregisterServiceInstanceRequest) error

	// Resolve returns a service's registered instances and the revision
	// they were read at.
	Resolve(ctx context.Context, req *ResolveServiceRequest) (*ResolveServiceResponse, error)

	// Watch streams a service's registration changes from a revision. The
	// channel closes when ctx ends, or after a final event whose Err names
	// why the store ended the watch.
	Watch(ctx context.Context, req *WatchServiceRequest) (<-chan RegistryEvent, error)
}

// RegistryServiceInstance is a registered instance of a named service.
type RegistryServiceInstance = registry.ServiceInstance

// RegistryEvent is one registration change delivered by Watch.
type RegistryEvent = registry.Event

// RegistryEventType distinguishes a registration from a deregistration.
type RegistryEventType = registry.EventType

// Registry event types.
const (
	// RegistryEventPut reports an instance registered or updated.
	RegistryEventPut = registry.Put

	// RegistryEventDelete reports an instance deregistered or expired.
	RegistryEventDelete = registry.Delete
)

// RegistryMinTTLSeconds is the shortest lease a registration may hold.
const RegistryMinTTLSeconds = int64(registry.MinTTL / time.Second)

// RegistryMaxTTLSeconds is the longest lease a registration may hold. A
// longer lease would keep a dead host's endpoint listed for longer than any
// health check tolerates, defeating the point of a lease.
const RegistryMaxTTLSeconds = int64(24 * time.Hour / time.Second)

// RegisterServiceInstanceRequest registers an instance.
type RegisterServiceInstanceRequest struct {
	// Instance is the instance to list.
	Instance RegistryServiceInstance `json:"instance"`

	// TTLSeconds is the lease TTL. It must lie in
	// [RegistryMinTTLSeconds, RegistryMaxTTLSeconds].
	TTLSeconds int64 `json:"ttl_seconds"`
}

// RegisterServiceInstanceResponse carries the lease backing a registration.
type RegisterServiceInstanceResponse struct {
	// LeaseID identifies the lease; Renew and Deregister take it. It is
	// carried as a JSON string so no JSON client truncates it to a float.
	LeaseID int64 `json:"lease_id,string"`

	// TTLSeconds is the lease TTL that was granted.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// RenewServiceLeaseRequest renews a registration's lease.
type RenewServiceLeaseRequest struct {
	// LeaseID is the lease Register returned.
	LeaseID int64 `json:"lease_id,string"`
}

// RenewServiceLeaseResponse reports the renewed lease.
type RenewServiceLeaseResponse struct {
	// LeaseID echoes the renewed lease.
	LeaseID int64 `json:"lease_id,string"`

	// TTLSeconds is the TTL the lease was reset to.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// DeregisterServiceInstanceRequest removes a registration.
type DeregisterServiceInstanceRequest struct {
	// LeaseID is the lease Register returned.
	LeaseID int64 `json:"lease_id,string"`
}

// ResolveServiceRequest looks up a service.
type ResolveServiceRequest struct {
	// Service is the service name.
	Service string `json:"service"`
}

// ResolveServiceResponse lists a service's instances.
type ResolveServiceResponse struct {
	// Instances are the registered instances, ordered by ID.
	Instances []RegistryServiceInstance `json:"instances"`

	// Revision is the store revision the instances were read at. Watching
	// from Revision+1 observes every later change.
	Revision int64 `json:"revision"`
}

// WatchServiceRequest opens a watch.
type WatchServiceRequest struct {
	// Service is the service name.
	Service string `json:"service"`

	// FromRevision is the first revision to deliver; 0 starts from now.
	FromRevision int64 `json:"from_revision,omitempty"`
}

// =============================================================================
// Typed Errors
// =============================================================================

// Typed sentinel errors for the registry service's own request validation.
// Failures inside the registry keep their pkg/registry types
// (InvalidInstanceError, LeaseNotFoundError, WatchCompactedError, ...),
// wrapped in a QBError carrying the transport status code.
var (
	ErrRegistryNilRequest = &errors.QBError{Code: errors.ErrInvalidInput, Op: "RegistryService", Err: errRegistryNilRequest}
	ErrRegistryTTLRange   = &errors.QBError{Code: errors.ErrInvalidInput, Op: "RegistryService.Register", Err: errRegistryTTLRange}
)

var (
	errRegistryNilRequest = stderrors.New("request must not be nil")
	errRegistryTTLRange   = stderrors.New("ttl_seconds must lie between the registry's minimum and maximum lease TTL")
)

// registryShardID is the qrdb shard the registry lives on: the platform
// shard every other system-namespace record uses.
const registryShardID = 1

// registryErrorCode maps a pkg/registry failure onto the status code every
// transport reports for it.
func registryErrorCode(err error) errors.ErrorCode {
	var invalid *registry.InvalidInstanceError
	var notFound *registry.LeaseNotFoundError
	var compacted *registry.WatchCompactedError
	var requiresRaft *registry.RegistryRequiresRaftError
	switch {
	case stderrors.As(err, &requiresRaft):
		return errors.ErrPreconditionFailed
	case stderrors.As(err, &invalid):
		return errors.ErrInvalidInput
	case stderrors.As(err, &notFound):
		return errors.ErrNotFound
	case stderrors.As(err, &compacted):
		return errors.ErrOutOfRange
	default:
		return errors.ErrUnavailable
	}
}

// registryError wraps a pkg/registry failure in the QBError transports map.
func registryError(op string, err error) error {
	return &errors.QBError{Code: registryErrorCode(err), Op: op, Err: err}
}

// =============================================================================
// Implementation
// =============================================================================

// registryBackendFunc resolves the registry for one call. The production
// resolver builds it over the qrdb client of the platform shard; tests
// supply an in-memory registry.
type registryBackendFunc func(ctx context.Context) (*registry.Registry, error)

// registryService is the production RegistryService.
type registryService struct {
	resolve registryBackendFunc
}

// newRegistryService creates a RegistryService over the given resolver.
func newRegistryService(resolve registryBackendFunc) RegistryService {
	return &registryService{resolve: resolve}
}

// NewRegistryServiceFromBackend creates a RegistryService over a registry
// storage backend. It is how an embedder (or a test) runs the service over a
// backend of its choosing; the server builds it over the platform shard.
func NewRegistryServiceFromBackend(backend registry.Backend) RegistryService {
	reg := registry.New(backend)
	return newRegistryService(func(context.Context) (*registry.Registry, error) { return reg, nil })
}

// registry resolves the registry, reporting an unavailable store as a typed
// error rather than a nil dereference. A standalone qrdb node, which serves
// no lease or watch plane, is reported as a failed precondition.
func (s *registryService) registry(ctx context.Context, op string) (*registry.Registry, error) {
	reg, err := s.resolve(ctx)
	if err != nil {
		return nil, registryError(op, err)
	}
	return reg, nil
}

// Register lists an instance under a new lease.
func (s *registryService) Register(ctx context.Context, req *RegisterServiceInstanceRequest) (*RegisterServiceInstanceResponse, error) {
	const op = "RegistryService.Register"
	if req == nil {
		return nil, ErrRegistryNilRequest
	}
	if req.TTLSeconds < RegistryMinTTLSeconds || req.TTLSeconds > RegistryMaxTTLSeconds {
		return nil, ErrRegistryTTLRange
	}
	reg, err := s.registry(ctx, op)
	if err != nil {
		return nil, err
	}
	leaseID, err := reg.Register(ctx, req.Instance, time.Duration(req.TTLSeconds)*time.Second)
	if err != nil {
		return nil, registryError(op, err)
	}
	return &RegisterServiceInstanceResponse{LeaseID: leaseID, TTLSeconds: req.TTLSeconds}, nil
}

// Renew refreshes a registration's lease.
func (s *registryService) Renew(ctx context.Context, req *RenewServiceLeaseRequest) (*RenewServiceLeaseResponse, error) {
	const op = "RegistryService.Renew"
	if req == nil {
		return nil, ErrRegistryNilRequest
	}
	reg, err := s.registry(ctx, op)
	if err != nil {
		return nil, err
	}
	ttl, err := reg.Renew(ctx, req.LeaseID)
	if err != nil {
		return nil, registryError(op, err)
	}
	return &RenewServiceLeaseResponse{LeaseID: req.LeaseID, TTLSeconds: ttl}, nil
}

// Deregister removes a registration.
func (s *registryService) Deregister(ctx context.Context, req *DeregisterServiceInstanceRequest) error {
	const op = "RegistryService.Deregister"
	if req == nil {
		return ErrRegistryNilRequest
	}
	reg, err := s.registry(ctx, op)
	if err != nil {
		return err
	}
	if err := reg.Deregister(ctx, req.LeaseID); err != nil {
		return registryError(op, err)
	}
	return nil
}

// Resolve lists a service's instances.
func (s *registryService) Resolve(ctx context.Context, req *ResolveServiceRequest) (*ResolveServiceResponse, error) {
	const op = "RegistryService.Resolve"
	if req == nil {
		return nil, ErrRegistryNilRequest
	}
	reg, err := s.registry(ctx, op)
	if err != nil {
		return nil, err
	}
	instances, revision, err := reg.Resolve(ctx, req.Service)
	if err != nil {
		return nil, registryError(op, err)
	}
	return &ResolveServiceResponse{Instances: instances, Revision: revision}, nil
}

// Watch streams a service's registration changes.
func (s *registryService) Watch(ctx context.Context, req *WatchServiceRequest) (<-chan RegistryEvent, error) {
	const op = "RegistryService.Watch"
	if req == nil {
		return nil, ErrRegistryNilRequest
	}
	reg, err := s.registry(ctx, op)
	if err != nil {
		return nil, err
	}
	events, err := reg.Watch(ctx, req.Service, req.FromRevision)
	if err != nil {
		return nil, registryError(op, err)
	}
	return events, nil
}
