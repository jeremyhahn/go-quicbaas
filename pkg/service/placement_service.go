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
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
)

// PlacementService is the scheduler's workload placement surface, reached on
// the wire as the placement operations of Async().Scheduler(). It registers
// caller-supplied node facts under go-qrdb leases, manages cordon and drain,
// and decides and records where workloads run (pkg/scheduler/placement).
//
// Every call is scoped to the tenant of the authenticated request, exactly
// as the job surface is.
type PlacementService interface {
	// RegisterNode registers or refreshes a node's facts under a lease.
	RegisterNode(ctx context.Context, req *RegisterPlacementNodeRequest) (*placement.NodeInfo, error)

	// RenewNode heartbeats a node's lease.
	RenewNode(ctx context.Context, nodeID string) error

	// DeregisterNode revokes a node's lease and clears its status.
	DeregisterNode(ctx context.Context, nodeID string) error

	// GetNode returns a live node.
	GetNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error)

	// ListNodes returns every live node.
	ListNodes(ctx context.Context) ([]*placement.NodeInfo, error)

	// CordonNode excludes a node from new placements.
	CordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error)

	// UncordonNode returns a node to service, clearing cordon and drain.
	UncordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error)

	// DrainNode marks a node draining and returns the placements to move.
	DrainNode(ctx context.Context, nodeID string) ([]*placement.Placement, error)

	// Place decides and records a workload's node.
	Place(ctx context.Context, req *placement.PlacementRequest) (*placement.PlaceResult, error)

	// Release removes a workload's placement.
	Release(ctx context.Context, workloadID string) error

	// GetPlacement returns a workload's placement.
	GetPlacement(ctx context.Context, workloadID string) (*placement.Placement, error)

	// ListPlacements returns the placements matching a filter.
	ListPlacements(ctx context.Context, filter *placement.PlacementFilter) ([]*placement.Placement, error)
}

// RegisterPlacementNodeRequest registers a node's facts under a lease.
type RegisterPlacementNodeRequest struct {
	// Facts are the node's caller-supplied facts.
	Facts placement.NodeFacts `json:"facts"`

	// TTLSeconds is the lease time-to-live. The node heartbeats with
	// RenewNode within it or drops out of placement.
	TTLSeconds int64 `json:"ttl_seconds"`
}

// ErrPlacementNilRequest is returned for a nil request.
var ErrPlacementNilRequest = &errors.QBError{
	Code: errors.ErrInvalidInput,
	Op:   "PlacementService",
	Err:  stderrors.New("request must not be nil"),
}

// placementService adapts a placement.Placer to PlacementService, resolving
// the tenant from the authenticated request and classifying the engine's
// typed errors into QuicBaaS error codes every transport maps.
type placementService struct {
	resolve func() (*placement.Placer, error)
}

// NewPlacementService returns a PlacementService over placer.
func NewPlacementService(placer *placement.Placer) PlacementService {
	return &placementService{resolve: func() (*placement.Placer, error) { return placer, nil }}
}

// newLazyPlacementService returns a PlacementService whose engine is built
// over provider's shard client on first use. A failed resolution is not
// cached, so a transient shard lookup failure is retried by the next call.
func newLazyPlacementService(provider QRDBClientProvider, shardID uint64) PlacementService {
	var (
		mu     sync.Mutex
		placer *placement.Placer
	)
	return &placementService{resolve: func() (*placement.Placer, error) {
		mu.Lock()
		defer mu.Unlock()
		if placer != nil {
			return placer, nil
		}
		client, err := provider.ForShard(shardID)
		if err != nil {
			return nil, &errors.QBError{Code: errors.ErrUnavailable, Op: "PlacementService", Err: err}
		}
		// The placement store is system-tenant state: a provider backed by
		// an embedded node binds its system service principal.
		var binder qrdbservice.Binder
		if b, ok := provider.(qrdbservice.Binder); ok {
			binder = b
		}
		placer = placement.NewFromClient(client, binder)
		return placer, nil
	}}
}

// begin resolves the tenant and the engine for one call.
func (s *placementService) begin(ctx context.Context, op string) (string, *placement.Placer, error) {
	tenant, err := placementTenant(ctx, op)
	if err != nil {
		return "", nil, err
	}
	placer, err := s.resolve()
	if err != nil {
		return "", nil, err
	}
	return tenant, placer, nil
}

// placementTenant resolves the request's tenant the way the job surface does
// (schedulerTenantResolveID): the authenticated tenant only (pkg/tenantctx),
// refused when none is bound, never named by the request. The system tenant
// is served only when bound explicitly, and maps onto the engine's system
// namespace (schedulerEngineTenant).
func placementTenant(ctx context.Context, op string) (string, error) {
	return schedulerTenantResolveID(ctx, "placement."+op, "")
}

// placementErrorCode classifies an engine error. The first match wins.
var placementErrorCode = []struct {
	match func(error) bool
	code  errors.ErrorCode
}{
	{func(err error) bool { var e *placement.InvalidRequestError; return stderrors.As(err, &e) }, errors.ErrInvalidInput},
	{func(err error) bool { var e *placement.NodeNotFoundError; return stderrors.As(err, &e) }, errors.ErrNotFound},
	{func(err error) bool { var e *placement.PlacementNotFoundError; return stderrors.As(err, &e) }, errors.ErrNotFound},
	{func(err error) bool { var e *placement.PlacementConflictError; return stderrors.As(err, &e) }, errors.ErrPreconditionFailed},
	{func(err error) bool { var e *placement.NoFeasibleNodeError; return stderrors.As(err, &e) }, errors.ErrPreconditionFailed},
	{func(err error) bool { var e *placement.ContendedError; return stderrors.As(err, &e) }, errors.ErrAborted},
	{func(err error) bool { var e *placement.CorruptRecordError; return stderrors.As(err, &e) }, errors.ErrDataLoss},
}

// classifyPlacementError wraps an engine error in a QBError carrying the
// code its type maps to, keeping the typed error reachable through
// errors.As.
func classifyPlacementError(op string, err error) error {
	if err == nil {
		return nil
	}
	for _, entry := range placementErrorCode {
		if entry.match(err) {
			return &errors.QBError{Code: entry.code, Op: "PlacementService." + op, Err: err}
		}
	}
	return &errors.QBError{Code: errors.ErrInternal, Op: "PlacementService." + op, Err: err}
}

func (s *placementService) RegisterNode(ctx context.Context, req *RegisterPlacementNodeRequest) (*placement.NodeInfo, error) {
	if req == nil {
		return nil, ErrPlacementNilRequest
	}
	tenant, placer, err := s.begin(ctx, "RegisterNode")
	if err != nil {
		return nil, err
	}
	info, err := placer.RegisterNode(ctx, tenant, &req.Facts, time.Duration(req.TTLSeconds)*time.Second)
	return info, classifyPlacementError("RegisterNode", err)
}

func (s *placementService) RenewNode(ctx context.Context, nodeID string) error {
	tenant, placer, err := s.begin(ctx, "RenewNode")
	if err != nil {
		return err
	}
	return classifyPlacementError("RenewNode", placer.RenewNode(ctx, tenant, nodeID))
}

func (s *placementService) DeregisterNode(ctx context.Context, nodeID string) error {
	tenant, placer, err := s.begin(ctx, "DeregisterNode")
	if err != nil {
		return err
	}
	return classifyPlacementError("DeregisterNode", placer.DeregisterNode(ctx, tenant, nodeID))
}

func (s *placementService) GetNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	tenant, placer, err := s.begin(ctx, "GetNode")
	if err != nil {
		return nil, err
	}
	info, err := placer.GetNode(ctx, tenant, nodeID)
	return info, classifyPlacementError("GetNode", err)
}

func (s *placementService) ListNodes(ctx context.Context) ([]*placement.NodeInfo, error) {
	tenant, placer, err := s.begin(ctx, "ListNodes")
	if err != nil {
		return nil, err
	}
	nodes, err := placer.ListNodes(ctx, tenant)
	return nodes, classifyPlacementError("ListNodes", err)
}

func (s *placementService) CordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	tenant, placer, err := s.begin(ctx, "CordonNode")
	if err != nil {
		return nil, err
	}
	info, err := placer.CordonNode(ctx, tenant, nodeID)
	return info, classifyPlacementError("CordonNode", err)
}

func (s *placementService) UncordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	tenant, placer, err := s.begin(ctx, "UncordonNode")
	if err != nil {
		return nil, err
	}
	info, err := placer.UncordonNode(ctx, tenant, nodeID)
	return info, classifyPlacementError("UncordonNode", err)
}

func (s *placementService) DrainNode(ctx context.Context, nodeID string) ([]*placement.Placement, error) {
	tenant, placer, err := s.begin(ctx, "DrainNode")
	if err != nil {
		return nil, err
	}
	moved, err := placer.DrainNode(ctx, tenant, nodeID)
	return moved, classifyPlacementError("DrainNode", err)
}

func (s *placementService) Place(ctx context.Context, req *placement.PlacementRequest) (*placement.PlaceResult, error) {
	if req == nil {
		return nil, ErrPlacementNilRequest
	}
	tenant, placer, err := s.begin(ctx, "Place")
	if err != nil {
		return nil, err
	}
	res, err := placer.Place(ctx, tenant, req)
	return res, classifyPlacementError("Place", err)
}

func (s *placementService) Release(ctx context.Context, workloadID string) error {
	tenant, placer, err := s.begin(ctx, "Release")
	if err != nil {
		return err
	}
	return classifyPlacementError("Release", placer.Release(ctx, tenant, workloadID))
}

func (s *placementService) GetPlacement(ctx context.Context, workloadID string) (*placement.Placement, error) {
	tenant, placer, err := s.begin(ctx, "GetPlacement")
	if err != nil {
		return nil, err
	}
	pl, err := placer.GetPlacement(ctx, tenant, workloadID)
	return pl, classifyPlacementError("GetPlacement", err)
}

func (s *placementService) ListPlacements(ctx context.Context, filter *placement.PlacementFilter) ([]*placement.Placement, error) {
	tenant, placer, err := s.begin(ctx, "ListPlacements")
	if err != nil {
		return nil, err
	}
	out, err := placer.ListPlacements(ctx, tenant, filter)
	return out, classifyPlacementError("ListPlacements", err)
}
