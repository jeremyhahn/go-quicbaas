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

import type {
  AgentBackendInfo,
  AgentRegistration,
  HealthCheckReport,
  JobEvent,
  JobStatus,
  SessionInfo,
  StartJobRequest,
  StartJobResponse,
  Task,
} from './types.js';

// ConnectionClient manages the transport-level connection to the QuicBaaS server.
// In Wails desktop mode this is a no-op since the Go process is embedded.
// In REST mode this tracks the base URL and validates connectivity.
export interface ConnectionClient {
  // connect establishes a connection to the given address.
  // transport is optional and defaults to the implementation's native transport.
  connect(address: string, transport?: string, tenantID?: string): Promise<void>;

  // disconnect terminates the connection.
  disconnect(): Promise<void>;

  // isConnected returns true if a connection is currently active.
  isConnected(): boolean;

  // getAddress returns the address of the connected server.
  getAddress(): string;

  // getTransport returns the name of the active transport (e.g. "rest", "wails").
  getTransport(): string;
}

// OrchestratorClient provides all orchestration operations.
// Implementations must delegate to the appropriate transport (Wails IPC or REST).
export interface OrchestratorClient {
  // listJobs returns all jobs for a tenant, sorted by start time descending.
  listJobs(tenantID: string): Promise<JobStatus[]>;

  // getJobStatus returns the current status of a specific job.
  getJobStatus(tenantID: string, jobID: string): Promise<JobStatus>;

  // startJob starts an autonomous orchestration control loop.
  startJob(req: StartJobRequest): Promise<StartJobResponse>;

  // cancelJob cancels a running orchestration job.
  cancelJob(tenantID: string, jobID: string): Promise<void>;

  // listAgents returns all registered agents for a tenant.
  listAgents(tenantID: string): Promise<AgentRegistration[]>;

  // getAgent retrieves a single registered agent by ID.
  getAgent(tenantID: string, agentID: string): Promise<AgentRegistration>;

  // listSessions returns all active sessions, optionally filtered by backend.
  listSessions(backend?: string): Promise<SessionInfo[]>;

  // listTasks returns all tasks for a tenant.
  listTasks(tenantID: string): Promise<Task[]>;

  // getTask retrieves a single task by ID within a tenant.
  getTask(tenantID: string, taskID: string): Promise<Task>;

  // listBackends returns all registered agent backends.
  listBackends(): Promise<AgentBackendInfo[]>;

  // healthCheck performs a health check on all active agent sessions.
  healthCheck(): Promise<HealthCheckReport>;

  // subscribeJobEvents subscribes to real-time events for a running job.
  // Returns a cleanup function that unsubscribes and stops the event stream.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void;
}

// Client is the top-level SDK entry point, combining connection management
// with all orchestration operations.
export interface Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;
}
