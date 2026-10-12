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

// ---------------------------------------------------------------------------
// Scheduler domain types
// ---------------------------------------------------------------------------

export interface SchedulerJob {
  id: number;
  name: string;
  description?: string;
  type: string;
  schedule?: string;
  handler: string;
  payload?: unknown;
  priority: number;
  timeout: number;
  max_retries: number;
  status: string;
  last_run?: string;
  next_run?: string;
  run_count: number;
  fail_count: number;
  last_error?: string;
  labels?: Record<string, string>;
  created_at: string;
  updated_at: string;
  owner_type?: string;
  owner_id?: string;
  target_tenant_id?: string;
}

export interface ListJobsRequest {
  status?: string;
  handler?: string;
  limit?: number;
  cursor?: string;
  owner_type?: string;
  owner_id?: string;
}

export interface ListJobsResponse {
  jobs: SchedulerJob[];
  next_cursor: string;
  has_more: boolean;
  total_count: number;
  timestamp: string;
}

export interface CreateJobRequest {
  name: string;
  description?: string;
  type: string;
  schedule?: string;
  handler: string;
  payload?: unknown;
  priority?: number;
  timeout_seconds?: number;
  max_retries?: number;
  labels?: Record<string, string>;
  owner_type?: string;
  owner_id?: string;
  target_tenant_id?: string;
}

export interface CreateJobResponse {
  job: SchedulerJob;
  timestamp: string;
}

export interface DeleteJobResponse {
  job_id: number;
  deleted: boolean;
  timestamp: string;
}

export interface TriggerJobResponse {
  job_id: number;
  status: string;
  timestamp: string;
}

export interface SchedulerStatus {
  running: boolean;
  stats?: {
    job_count: number;
    running_jobs: number;
    success_count: number;
    failure_count: number;
    started_at: string;
  };
  timestamp: string;
}

// ---------------------------------------------------------------------------
// Workload placement types (mirror pkg/scheduler/placement JSON)
// ---------------------------------------------------------------------------

// PlacementNodeFacts are a node's caller-supplied facts.
export interface PlacementNodeFacts {
  node_id: string;
  capacity?: Record<string, number>;
  labels?: Record<string, string>;
}

// PlacementNodeStatus is a node's cordon and drain state.
export interface PlacementNodeStatus {
  unschedulable: boolean;
  draining: boolean;
}

// PlacementNodeInfo is a live node with its lease and allocation.
export interface PlacementNodeInfo {
  facts: PlacementNodeFacts;
  status: PlacementNodeStatus;
  // lease_id is the node's int64 registry lease. One past 2^53 arrives as its
  // exact decimal string; a smaller one as a number.
  lease_id: number | string;
  ttl_seconds: number;
  allocated?: Record<string, number>;
  placements: number;
  registered_at: string;
}

// RegisterNodeRequest registers a node's facts under a lease.
export interface RegisterNodeRequest {
  facts: PlacementNodeFacts;
  ttl_seconds: number;
}

// PlacementConstraint is a hard requirement on a node label.
export interface PlacementConstraint {
  key: string;
  operator: 'In' | 'NotIn' | 'Exists' | 'DoesNotExist';
  values?: string[];
}

// PlacementAffinityTerm relates a workload to nodes hosting another group.
export interface PlacementAffinityTerm {
  group: string;
  required?: boolean;
  weight?: number;
}

// PlacementSpreadConstraint spreads a group across a topology label.
export interface PlacementSpreadConstraint {
  topology_key: string;
  max_skew: number;
  required?: boolean;
}

// PlacementRequest asks for one workload to be placed.
export interface PlacementRequest {
  workload_id: string;
  group?: string;
  labels?: Record<string, string>;
  resources?: Record<string, number>;
  constraints?: PlacementConstraint[];
  affinity?: PlacementAffinityTerm[];
  anti_affinity?: PlacementAffinityTerm[];
  spread?: PlacementSpreadConstraint;
  strategy?: 'binpack' | 'spread';
}

// Placement is a recorded placement decision.
export interface Placement {
  workload_id: string;
  group?: string;
  node_id: string;
  resources?: Record<string, number>;
  labels?: Record<string, string>;
  score: number;
  created_at: string;
  orphaned?: boolean;
}

// PlaceResult is the outcome of place.
export interface PlaceResult {
  placement: Placement;
  existing: boolean;
}

// PlacementFilter narrows listPlacements.
export interface PlacementFilter {
  node_id?: string;
  group?: string;
}

// ---------------------------------------------------------------------------
// SchedulerClient interface
// ---------------------------------------------------------------------------

// SchedulerClient exposes scheduled job management operations.
// Maps to /api/v1/scheduler/* REST endpoints.
export interface SchedulerClient {
  // listJobs returns all scheduled jobs, optionally filtered.
  // GET /api/v1/scheduler/jobs
  listJobs(req?: ListJobsRequest): Promise<ListJobsResponse>;

  // createJob registers a new scheduled job.
  // POST /api/v1/scheduler/jobs
  createJob(req: CreateJobRequest): Promise<CreateJobResponse>;

  // deleteJob removes a scheduled job by numeric ID.
  // DELETE /api/v1/scheduler/jobs/:id
  deleteJob(id: number): Promise<DeleteJobResponse>;

  // triggerJob runs a job immediately, ignoring its schedule.
  // POST /api/v1/scheduler/jobs/:id/trigger
  triggerJob(id: number): Promise<TriggerJobResponse>;

  // pauseJob suspends execution of a scheduled job without deleting it.
  // POST /api/v1/scheduler/jobs/:id/pause
  pauseJob(id: number): Promise<void>;

  // resumeJob resumes a previously paused job.
  // POST /api/v1/scheduler/jobs/:id/resume
  resumeJob(id: number): Promise<void>;

  // updateJob replaces mutable fields on a scheduled job.
  // PUT /api/v1/scheduler/jobs/:id
  updateJob(id: number, req: Partial<CreateJobRequest>): Promise<SchedulerJob>;

  // getStatus returns the scheduler engine status.
  // GET /api/v1/scheduler/status
  getStatus(): Promise<SchedulerStatus>;

  // registerNode registers or refreshes a node's facts under a lease.
  // POST /api/v1/scheduler/nodes
  registerNode(req: RegisterNodeRequest): Promise<PlacementNodeInfo>;

  // renewNode heartbeats a node's lease.
  // POST /api/v1/scheduler/nodes/:id/renew
  renewNode(nodeId: string): Promise<void>;

  // deregisterNode revokes a node's lease and clears its status.
  // DELETE /api/v1/scheduler/nodes/:id
  deregisterNode(nodeId: string): Promise<void>;

  // getNode returns a live node.
  // GET /api/v1/scheduler/nodes/:id
  getNode(nodeId: string): Promise<PlacementNodeInfo>;

  // listNodes returns every live node.
  // GET /api/v1/scheduler/nodes
  listNodes(): Promise<PlacementNodeInfo[]>;

  // cordonNode excludes a node from new placements.
  // POST /api/v1/scheduler/nodes/:id/cordon
  cordonNode(nodeId: string): Promise<PlacementNodeInfo>;

  // uncordonNode returns a node to service.
  // POST /api/v1/scheduler/nodes/:id/uncordon
  uncordonNode(nodeId: string): Promise<PlacementNodeInfo>;

  // drainNode drains a node and returns the placements to move.
  // POST /api/v1/scheduler/nodes/:id/drain
  drainNode(nodeId: string): Promise<Placement[]>;

  // place decides and records the node a workload runs on.
  // POST /api/v1/scheduler/placements
  place(req: PlacementRequest): Promise<PlaceResult>;

  // release removes a workload's placement.
  // DELETE /api/v1/scheduler/placements/:id
  release(workloadId: string): Promise<void>;

  // getPlacement returns a workload's placement.
  // GET /api/v1/scheduler/placements/:id
  getPlacement(workloadId: string): Promise<Placement>;

  // listPlacements returns placements, optionally filtered by node or group.
  // GET /api/v1/scheduler/placements
  listPlacements(filter?: PlacementFilter): Promise<Placement[]>;
}
