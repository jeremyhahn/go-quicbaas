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

package orchestrator

import (
	"context"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Fleet placement specs (go-quicbaas#1681): where a class of work may run
// across the fleet's hosts, and the read-only report of where it runs
// instead. Not the scheduler's per-workload placement (client/scheduler).
// Every request's TenantID is optional; empty means the caller's tenant.

// FleetPlacementProfile is a role's resource profile.
type FleetPlacementProfile struct {
	CPUCores    float64   `json:"cpu_cores"`
	MemoryBytes int64     `json:"memory_bytes"`
	Measured    bool      `json:"measured"`
	SampleSize  int       `json:"sample_size,omitempty"`
	UpdatedAt   time.Time `json:"updated_at,omitempty"`
}

// FleetPlacementSpec declares where a role's work may run. A spec names its
// target by HostCount or by Hosts, never both. Generation is the one the
// caller read (zero to create); the server stores it plus one, and a stale
// one is refused as a conflict.
type FleetPlacementSpec struct {
	TenantID             string                 `json:"tenant_id"`
	Name                 string                 `json:"name"`
	Role                 string                 `json:"role"`
	HostCount            int                    `json:"host_count,omitempty"`
	Hosts                []string               `json:"hosts,omitempty"`
	Profile              *FleetPlacementProfile `json:"profile,omitempty"`
	RequiredCapabilities []string               `json:"required_capabilities,omitempty"`
	Actuate              bool                   `json:"actuate"`
	Generation           uint64                 `json:"generation"`
}

// FleetPlacementRemedy is the move a finding calls for.
type FleetPlacementRemedy struct {
	Action       string   `json:"action"`
	From         string   `json:"from,omitempty"`
	Destinations []string `json:"destinations,omitempty"`
}

// FleetPlacementFinding is one way the running work departs from a spec.
type FleetPlacementFinding struct {
	Kind    string               `json:"kind"`
	Field   string               `json:"field,omitempty"`
	Host    string               `json:"host,omitempty"`
	WorkID  string               `json:"work_id,omitempty"`
	Missing []string             `json:"missing,omitempty"`
	Remedy  FleetPlacementRemedy `json:"remedy"`
}

// FleetPlacementDrift is the read-only report of a spec's role work against
// the hosts the spec admits. ObservedWork is how much work was compared: a
// clean report over none is not a clean report over forty.
type FleetPlacementDrift struct {
	TenantID        string                  `json:"tenant_id"`
	Spec            string                  `json:"spec"`
	Role            string                  `json:"role"`
	Generation      uint64                  `json:"generation"`
	Disposition     string                  `json:"disposition"`
	ObservedWork    int                     `json:"observed_work"`
	AdmissibleHosts []string                `json:"admissible_hosts"`
	Findings        []FleetPlacementFinding `json:"findings"`
}

// DeclareFleetPlacementSpecRequest creates or replaces a spec.
type DeclareFleetPlacementSpecRequest struct {
	TenantID string              `json:"tenant_id,omitempty"`
	Spec     *FleetPlacementSpec `json:"spec"`
}

// FleetPlacementSpecRequest names one spec.
type FleetPlacementSpecRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	Name     string `json:"name"`
}

// ListFleetPlacementSpecsRequest lists a tenant's specs.
type ListFleetPlacementSpecsRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
}

// ListFleetPlacementSpecsResponse is a tenant's specs, ordered by name.
type ListFleetPlacementSpecsResponse struct {
	Specs []*FleetPlacementSpec `json:"specs"`
	Count int                   `json:"count"`
}

// WithdrawFleetPlacementSpecResponse confirms a withdrawal.
type WithdrawFleetPlacementSpecResponse struct {
	Name      string `json:"name"`
	Withdrawn bool   `json:"withdrawn"`
}

// FleetPlacementConsentMove is the one move a consent request asks about:
// a piece of work of a class, under a spec generation, leaving a host.
type FleetPlacementConsentMove struct {
	SpecName       string `json:"spec_name"`
	SpecGeneration uint64 `json:"spec_generation"`
	Class          string `json:"class"`
	WorkID         string `json:"work_id"`
	SourceHost     string `json:"source_host"`
}

// FleetPlacementConsentRequest is a per-move consent request and its answer.
// State is pending, granted, refused or incoherent; By is the authenticated
// subject that answered it.
type FleetPlacementConsentRequest struct {
	ID         string                    `json:"id"`
	TenantID   string                    `json:"tenant_id"`
	Move       FleetPlacementConsentMove `json:"move"`
	State      string                    `json:"state"`
	By         string                    `json:"by,omitempty"`
	Reason     string                    `json:"reason,omitempty"`
	RaisedAt   time.Time                 `json:"raised_at"`
	AnsweredAt time.Time                 `json:"answered_at,omitempty"`
}

// ListFleetPlacementConsentsRequest lists a tenant's consent requests,
// optionally only those in one state.
type ListFleetPlacementConsentsRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	State    string `json:"state,omitempty"`
}

// ListFleetPlacementConsentsResponse holds a tenant's consent requests.
type ListFleetPlacementConsentsResponse struct {
	Requests []*FleetPlacementConsentRequest `json:"requests"`
	Count    int                             `json:"count"`
}

// AnswerFleetPlacementConsentRequest grants or refuses a pending consent
// request. It names no answerer: the answer is the authenticated caller's.
type AnswerFleetPlacementConsentRequest struct {
	TenantID string `json:"tenant_id,omitempty"`
	ID       string `json:"id"`
	State    string `json:"state"`
	Reason   string `json:"reason,omitempty"`
}

// DeclareFleetPlacementSpec creates or replaces a spec.
func (c *client) DeclareFleetPlacementSpec(ctx context.Context, req *DeclareFleetPlacementSpecRequest) (*FleetPlacementSpec, error) {
	const method = "DeclareFleetPlacementSpec"
	if req == nil || req.Spec == nil {
		return nil, errors.NewInvalidInput("orchestrator."+method, "a spec is required")
	}
	if req.Spec.Name == "" {
		return nil, errors.NewInvalidInput("orchestrator."+method, "the spec's name is required")
	}
	return fleetPlacementCall[FleetPlacementSpec](ctx, c, method, req)
}

// GetFleetPlacementSpec returns one spec.
func (c *client) GetFleetPlacementSpec(ctx context.Context, req *FleetPlacementSpecRequest) (*FleetPlacementSpec, error) {
	const method = "GetFleetPlacementSpec"
	if err := requireFleetPlacementName(method, req); err != nil {
		return nil, err
	}
	return fleetPlacementCall[FleetPlacementSpec](ctx, c, method, req)
}

// ListFleetPlacementSpecs returns a tenant's specs.
func (c *client) ListFleetPlacementSpecs(ctx context.Context, req *ListFleetPlacementSpecsRequest) (*ListFleetPlacementSpecsResponse, error) {
	if req == nil {
		req = &ListFleetPlacementSpecsRequest{}
	}
	return fleetPlacementCall[ListFleetPlacementSpecsResponse](ctx, c, "ListFleetPlacementSpecs", req)
}

// WithdrawFleetPlacementSpec removes a spec.
func (c *client) WithdrawFleetPlacementSpec(ctx context.Context, req *FleetPlacementSpecRequest) (*WithdrawFleetPlacementSpecResponse, error) {
	const method = "WithdrawFleetPlacementSpec"
	if err := requireFleetPlacementName(method, req); err != nil {
		return nil, err
	}
	return fleetPlacementCall[WithdrawFleetPlacementSpecResponse](ctx, c, method, req)
}

// GetFleetPlacementDrift reports a spec's drift. A report that cannot be
// complete is refused (precondition failed) rather than answered partially.
func (c *client) GetFleetPlacementDrift(ctx context.Context, req *FleetPlacementSpecRequest) (*FleetPlacementDrift, error) {
	const method = "GetFleetPlacementDrift"
	if err := requireFleetPlacementName(method, req); err != nil {
		return nil, err
	}
	return fleetPlacementCall[FleetPlacementDrift](ctx, c, method, req)
}

// ListFleetPlacementConsents lists a tenant's per-move consent requests.
func (c *client) ListFleetPlacementConsents(ctx context.Context, req *ListFleetPlacementConsentsRequest) (*ListFleetPlacementConsentsResponse, error) {
	if req == nil {
		req = &ListFleetPlacementConsentsRequest{}
	}
	return fleetPlacementCall[ListFleetPlacementConsentsResponse](ctx, c, "ListFleetPlacementConsents", req)
}

// AnswerFleetPlacementConsent grants or refuses a pending consent request.
func (c *client) AnswerFleetPlacementConsent(ctx context.Context, req *AnswerFleetPlacementConsentRequest) (*FleetPlacementConsentRequest, error) {
	const method = "AnswerFleetPlacementConsent"
	if req == nil {
		return nil, errors.NewInvalidInput("orchestrator."+method, "request is required")
	}
	if req.ID == "" {
		return nil, errors.NewInvalidInput("orchestrator."+method, "consent request id is required")
	}
	if req.State != "granted" && req.State != "refused" {
		return nil, errors.NewInvalidInput("orchestrator."+method, "state must be granted or refused")
	}
	return fleetPlacementCall[FleetPlacementConsentRequest](ctx, c, method, req)
}

func requireFleetPlacementName(method string, req *FleetPlacementSpecRequest) error {
	if req == nil || req.Name == "" {
		return errors.NewInvalidInput("orchestrator."+method, "a spec name is required")
	}
	return nil
}

func fleetPlacementCall[T any](ctx context.Context, c *client, method string, req interface{}) (*T, error) {
	op := "orchestrator." + method
	if ctx == nil {
		return nil, errors.NewInvalidInput(op, "context is required")
	}
	resp, err := c.transport.Request(ctx, serviceName, method, req)
	if err != nil {
		return nil, err
	}
	out, ok := resp.(*T)
	if !ok {
		return nil, errors.NewInternal(op, "invalid response type")
	}
	return out, nil
}
