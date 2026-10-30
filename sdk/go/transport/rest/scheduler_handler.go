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

package rest

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler/placement"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/scheduler"
)

// The scheduler surface over REST (and QUIC, which is REST over HTTP/3).
// schedulerRESTHandler presents the /api/v1/scheduler routes as the
// server's own SchedulerService and PlacementService, and
// scheduler.Dispatch maps them to the SDK types — the same mapping the gRPC
// and embedded transports use. Paths come from mapSchedulerMethod, the one
// route table for this surface.

// isSchedulerService reports whether service targets Async().Scheduler().
func isSchedulerService(service string) bool {
	return service == scheduler.ServiceName
}

// schedulerRESTHandler adapts the REST routes to the service interfaces.
type schedulerRESTHandler struct {
	t *Transport
}

// newSchedulerRESTHandler binds the handler to a transport.
func newSchedulerRESTHandler(t *Transport) *schedulerRESTHandler {
	return &schedulerRESTHandler{t: t}
}

// Compile-time proof the handler serves both halves.
var (
	_ service.SchedulerService = (*schedulerRESTHandler)(nil)
	_ service.PlacementService = (*schedulerRESTHandler)(nil)
)

// handleRequest serves one scheduler method.
func (h *schedulerRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	return scheduler.Dispatch(ctx, scheduler.Backends{Jobs: h, Placement: h}, method, req)
}

// call issues the route mapSchedulerMethod names for method, substituting
// id and appending query.
func (h *schedulerRESTHandler) call(ctx context.Context, method, id string, query url.Values, body, result interface{}) error {
	path, verb := h.t.mapSchedulerMethod(method)
	path = strings.Replace(path, "{id}", url.PathEscape(id), 1)
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return h.t.doJSONRequest(ctx, verb, path, body, result)
}

// schedulerJobQuery carries a by-ID request's explicit tenant, which the
// server reads as tenant_id when the authenticated context carries none, and
// the scope that names the namespace holding the job.
func schedulerJobQuery(tenantID string, scope service.SchedulerOwnerType) url.Values {
	query := url.Values{}
	if tenantID != "" {
		query.Set("tenant_id", tenantID)
	}
	if scope != "" {
		query.Set("scope", string(scope))
	}
	return query
}

// jsonPayload renders a job payload for the REST body, whose payload field
// is raw JSON: a JSON payload is sent as-is, any other bytes as a JSON
// string.
func jsonPayload(payload []byte) (json.RawMessage, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if json.Valid(payload) {
		return json.RawMessage(payload), nil
	}
	quoted, err := json.Marshal(string(payload))
	if err != nil {
		return nil, err
	}
	return quoted, nil
}

// restJob decodes a job from the REST JSON, whose payload is raw JSON
// rather than the service type's base64 bytes.
type restJob struct {
	service.SchedulerJob
	Payload json.RawMessage `json:"payload,omitempty"`
}

// toService returns the service job.
func (j *restJob) toService() *service.SchedulerJob {
	if j == nil {
		return nil
	}
	job := j.SchedulerJob
	job.Payload = []byte(j.Payload)
	return &job
}

// restJobEnvelope is the {job, timestamp} body of the job routes.
type restJobEnvelope struct {
	Job       *restJob  `json:"job"`
	Timestamp time.Time `json:"timestamp"`
}

// restJobBody is the create and update request body.
type restJobBody struct {
	Name           string            `json:"name,omitempty"`
	Description    string            `json:"description,omitempty"`
	OwnerType      string            `json:"owner_type,omitempty"`
	OwnerID        string            `json:"owner_id,omitempty"`
	TargetTenantID string            `json:"target_tenant_id,omitempty"`
	Type           string            `json:"type,omitempty"`
	Schedule       string            `json:"schedule,omitempty"`
	ExecuteAt      string            `json:"execute_at,omitempty"`
	Delay          time.Duration     `json:"delay,omitempty"`
	Handler        string            `json:"handler,omitempty"`
	Payload        json.RawMessage   `json:"payload,omitempty"`
	Priority       int32             `json:"priority,omitempty"`
	TimeoutSeconds int64             `json:"timeout_seconds,omitempty"`
	Timeout        time.Duration     `json:"timeout,omitempty"`
	MaxRetries     int32             `json:"max_retries,omitempty"`
	NodeAffinity   string            `json:"node_affinity,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
}

// rfc3339 renders a non-zero time as RFC 3339, else "".
func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

// CreateJob calls POST /api/v1/scheduler/jobs.
func (h *schedulerRESTHandler) CreateJob(ctx context.Context, req *service.CreateSchedulerJobRequest) (*service.CreateSchedulerJobResponse, error) {
	payload, err := jsonPayload(req.Payload)
	if err != nil {
		return nil, err
	}
	var out restJobEnvelope
	if err := h.call(ctx, "CreateJob", "", nil, &restJobBody{
		Name:           req.Name,
		Description:    req.Description,
		OwnerType:      string(req.OwnerType),
		OwnerID:        req.OwnerID,
		TargetTenantID: req.TargetTenantID,
		Type:           string(req.Type),
		Schedule:       req.Schedule,
		ExecuteAt:      rfc3339(req.ExecuteAt),
		Delay:          req.Delay,
		Handler:        req.Handler,
		Payload:        payload,
		Priority:       req.Priority,
		Timeout:        req.Timeout,
		MaxRetries:     req.MaxRetries,
		NodeAffinity:   req.NodeAffinity,
		Labels:         req.Labels,
	}, &out); err != nil {
		return nil, err
	}
	return &service.CreateSchedulerJobResponse{Job: out.Job.toService(), Timestamp: out.Timestamp}, nil
}

// GetJob calls GET /api/v1/scheduler/jobs/{id}.
func (h *schedulerRESTHandler) GetJob(ctx context.Context, req *service.GetSchedulerJobRequest) (*service.GetSchedulerJobResponse, error) {
	var out restJobEnvelope
	if err := h.call(ctx, "GetJob", strconv.FormatUint(req.JobID, 10), schedulerJobQuery(req.TenantID, req.Scope), nil, &out); err != nil {
		return nil, err
	}
	return &service.GetSchedulerJobResponse{Job: out.Job.toService(), Timestamp: out.Timestamp}, nil
}

// UpdateJob calls PUT /api/v1/scheduler/jobs/{id}. The route takes the
// timeout in whole seconds.
func (h *schedulerRESTHandler) UpdateJob(ctx context.Context, req *service.UpdateSchedulerJobRequest) (*service.UpdateSchedulerJobResponse, error) {
	payload, err := jsonPayload(req.Payload)
	if err != nil {
		return nil, err
	}
	var out restJobEnvelope
	if err := h.call(ctx, "UpdateJob", strconv.FormatUint(req.JobID, 10), schedulerJobQuery(req.TenantID, req.Scope), &restJobBody{
		Name:           req.Name,
		Description:    req.Description,
		Schedule:       req.Schedule,
		ExecuteAt:      rfc3339(req.ExecuteAt),
		Handler:        req.Handler,
		Payload:        payload,
		Priority:       req.Priority,
		TimeoutSeconds: int64(req.Timeout / time.Second),
		MaxRetries:     req.MaxRetries,
		Labels:         req.Labels,
	}, &out); err != nil {
		return nil, err
	}
	return &service.UpdateSchedulerJobResponse{Job: out.Job.toService(), Timestamp: out.Timestamp}, nil
}

// DeleteJob calls DELETE /api/v1/scheduler/jobs/{id}.
func (h *schedulerRESTHandler) DeleteJob(ctx context.Context, req *service.DeleteSchedulerJobRequest) (*service.DeleteSchedulerJobResponse, error) {
	query := schedulerJobQuery(req.TenantID, req.Scope)
	if req.Force {
		query.Set("force", "true")
	}
	var out service.DeleteSchedulerJobResponse
	if err := h.call(ctx, "DeleteJob", strconv.FormatUint(req.JobID, 10), query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListJobs calls GET /api/v1/scheduler/jobs with the filters as query
// parameters.
func (h *schedulerRESTHandler) ListJobs(ctx context.Context, req *service.ListSchedulerJobsRequest) (*service.ListSchedulerJobsResponse, error) {
	query := url.Values{}
	setIf := func(key, value string) {
		if value != "" {
			query.Set(key, value)
		}
	}
	setIf("owner_type", string(req.OwnerType))
	setIf("owner_id", req.OwnerID)
	setIf("target_tenant_id", req.TargetTenantID)
	setIf("status", string(req.Status))
	setIf("handler", req.Handler)
	setIf("cursor", req.Cursor)
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	var out struct {
		Jobs       []*restJob `json:"jobs"`
		NextCursor string     `json:"next_cursor"`
		HasMore    bool       `json:"has_more"`
		TotalCount int        `json:"total_count"`
		Timestamp  time.Time  `json:"timestamp"`
	}
	if err := h.call(ctx, "ListJobs", "", query, nil, &out); err != nil {
		return nil, err
	}
	resp := &service.ListSchedulerJobsResponse{NextCursor: out.NextCursor, HasMore: out.HasMore, TotalCount: out.TotalCount, Timestamp: out.Timestamp}
	for _, job := range out.Jobs {
		resp.Jobs = append(resp.Jobs, job.toService())
	}
	return resp, nil
}

// statusTransition is the body of the trigger, pause, resume and cancel
// routes.
type statusTransition struct {
	JobID     uint64                     `json:"job_id"`
	Status    service.SchedulerJobStatus `json:"status"`
	OldStatus service.SchedulerJobStatus `json:"old_status"`
	NewStatus service.SchedulerJobStatus `json:"new_status"`
	Timestamp time.Time                  `json:"timestamp"`
}

// transition posts a job status change.
func (h *schedulerRESTHandler) transition(ctx context.Context, method string, jobID uint64, query url.Values, body interface{}) (*statusTransition, error) {
	var out statusTransition
	if err := h.call(ctx, method, strconv.FormatUint(jobID, 10), query, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// TriggerJob calls POST /api/v1/scheduler/jobs/{id}/trigger.
func (h *schedulerRESTHandler) TriggerJob(ctx context.Context, req *service.TriggerSchedulerJobRequest) (*service.TriggerSchedulerJobResponse, error) {
	out, err := h.transition(ctx, "TriggerJob", req.JobID, schedulerJobQuery(req.TenantID, req.Scope), nil)
	if err != nil {
		return nil, err
	}
	return &service.TriggerSchedulerJobResponse{JobID: out.JobID, Status: out.Status, Timestamp: out.Timestamp}, nil
}

// PauseJob calls POST /api/v1/scheduler/jobs/{id}/pause.
func (h *schedulerRESTHandler) PauseJob(ctx context.Context, req *service.PauseSchedulerJobRequest) (*service.PauseSchedulerJobResponse, error) {
	out, err := h.transition(ctx, "PauseJob", req.JobID, schedulerJobQuery(req.TenantID, req.Scope), nil)
	if err != nil {
		return nil, err
	}
	return &service.PauseSchedulerJobResponse{JobID: out.JobID, OldStatus: out.OldStatus, NewStatus: out.NewStatus, Timestamp: out.Timestamp}, nil
}

// ResumeJob calls POST /api/v1/scheduler/jobs/{id}/resume.
func (h *schedulerRESTHandler) ResumeJob(ctx context.Context, req *service.ResumeSchedulerJobRequest) (*service.ResumeSchedulerJobResponse, error) {
	out, err := h.transition(ctx, "ResumeJob", req.JobID, schedulerJobQuery(req.TenantID, req.Scope), nil)
	if err != nil {
		return nil, err
	}
	return &service.ResumeSchedulerJobResponse{JobID: out.JobID, OldStatus: out.OldStatus, NewStatus: out.NewStatus, Timestamp: out.Timestamp}, nil
}

// CancelJob calls POST /api/v1/scheduler/jobs/{id}/cancel.
func (h *schedulerRESTHandler) CancelJob(ctx context.Context, req *service.CancelSchedulerJobRequest) (*service.CancelSchedulerJobResponse, error) {
	out, err := h.transition(ctx, "CancelJob", req.JobID, schedulerJobQuery(req.TenantID, req.Scope), map[string]string{"reason": req.Reason})
	if err != nil {
		return nil, err
	}
	return &service.CancelSchedulerJobResponse{JobID: out.JobID, OldStatus: out.OldStatus, NewStatus: out.NewStatus, Timestamp: out.Timestamp}, nil
}

// GetJobHistory calls GET /api/v1/scheduler/jobs/{id}/history.
func (h *schedulerRESTHandler) GetJobHistory(ctx context.Context, req *service.GetSchedulerJobHistoryRequest) (*service.GetSchedulerJobHistoryResponse, error) {
	query := schedulerJobQuery(req.TenantID, req.Scope)
	if req.Limit > 0 {
		query.Set("limit", strconv.Itoa(req.Limit))
	}
	if req.Cursor != "" {
		query.Set("cursor", req.Cursor)
	}
	var out service.GetSchedulerJobHistoryResponse
	if err := h.call(ctx, "GetJobHistory", strconv.FormatUint(req.JobID, 10), query, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetStatus calls GET /api/v1/scheduler/status.
func (h *schedulerRESTHandler) GetStatus(ctx context.Context) (*service.GetSchedulerStatusResponse, error) {
	var out service.GetSchedulerStatusResponse
	if err := h.call(ctx, "GetStatus", "", nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RegisterNode calls POST /api/v1/scheduler/nodes.
func (h *schedulerRESTHandler) RegisterNode(ctx context.Context, req *service.RegisterPlacementNodeRequest) (*placement.NodeInfo, error) {
	var out placement.NodeInfo
	if err := h.call(ctx, "RegisterNode", "", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// RenewNode calls POST /api/v1/scheduler/nodes/{id}/renew.
func (h *schedulerRESTHandler) RenewNode(ctx context.Context, nodeID string) error {
	var out scheduler.RenewNodeResponse
	return h.call(ctx, "RenewNode", nodeID, nil, nil, &out)
}

// DeregisterNode calls DELETE /api/v1/scheduler/nodes/{id}.
func (h *schedulerRESTHandler) DeregisterNode(ctx context.Context, nodeID string) error {
	var out scheduler.DeregisterNodeResponse
	return h.call(ctx, "DeregisterNode", nodeID, nil, nil, &out)
}

// node issues a node-keyed route returning the node.
func (h *schedulerRESTHandler) node(ctx context.Context, method, nodeID string) (*placement.NodeInfo, error) {
	var out placement.NodeInfo
	if err := h.call(ctx, method, nodeID, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetNode calls GET /api/v1/scheduler/nodes/{id}.
func (h *schedulerRESTHandler) GetNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return h.node(ctx, "GetNode", nodeID)
}

// CordonNode calls POST /api/v1/scheduler/nodes/{id}/cordon.
func (h *schedulerRESTHandler) CordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return h.node(ctx, "CordonNode", nodeID)
}

// UncordonNode calls POST /api/v1/scheduler/nodes/{id}/uncordon.
func (h *schedulerRESTHandler) UncordonNode(ctx context.Context, nodeID string) (*placement.NodeInfo, error) {
	return h.node(ctx, "UncordonNode", nodeID)
}

// ListNodes calls GET /api/v1/scheduler/nodes.
func (h *schedulerRESTHandler) ListNodes(ctx context.Context) ([]*placement.NodeInfo, error) {
	var out scheduler.ListNodesResponse
	if err := h.call(ctx, "ListNodes", "", nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Nodes, nil
}

// DrainNode calls POST /api/v1/scheduler/nodes/{id}/drain.
func (h *schedulerRESTHandler) DrainNode(ctx context.Context, nodeID string) ([]*placement.Placement, error) {
	var out scheduler.DrainNodeResponse
	if err := h.call(ctx, "DrainNode", nodeID, nil, nil, &out); err != nil {
		return nil, err
	}
	return out.Placements, nil
}

// Place calls POST /api/v1/scheduler/placements.
func (h *schedulerRESTHandler) Place(ctx context.Context, req *placement.PlacementRequest) (*placement.PlaceResult, error) {
	var out placement.PlaceResult
	if err := h.call(ctx, "Place", "", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Release calls DELETE /api/v1/scheduler/placements/{id}.
func (h *schedulerRESTHandler) Release(ctx context.Context, workloadID string) error {
	var out scheduler.ReleaseResponse
	return h.call(ctx, "Release", workloadID, nil, nil, &out)
}

// GetPlacement calls GET /api/v1/scheduler/placements/{id}.
func (h *schedulerRESTHandler) GetPlacement(ctx context.Context, workloadID string) (*placement.Placement, error) {
	var out placement.Placement
	if err := h.call(ctx, "GetPlacement", workloadID, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ListPlacements calls GET /api/v1/scheduler/placements?node_id=&group=.
func (h *schedulerRESTHandler) ListPlacements(ctx context.Context, filter *placement.PlacementFilter) ([]*placement.Placement, error) {
	query := url.Values{}
	if filter != nil {
		if filter.NodeID != "" {
			query.Set("node_id", filter.NodeID)
		}
		if filter.Group != "" {
			query.Set("group", filter.Group)
		}
	}
	var out scheduler.ListPlacementsResponse
	if err := h.call(ctx, "ListPlacements", "", query, nil, &out); err != nil {
		return nil, err
	}
	return out.Placements, nil
}
