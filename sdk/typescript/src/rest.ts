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

import type { Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
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
import { APIError, ConnectionError, NotConnectedError } from './errors.js';

// API base path for all orchestrator endpoints.
const API_PREFIX = '/api/v1/orchestrator';

// RestConnectionClient manages the server address and validates connectivity
// by performing a health check against the orchestrator endpoint.
class RestConnectionClient implements ConnectionClient {
  private _connected = false;
  private _address = '';
  private readonly _transport = 'rest';

  constructor(address: string) {
    this._address = address;
  }

  // connect validates that the server is reachable by calling the health endpoint.
  async connect(address: string, _transport?: string, _tenantID?: string): Promise<void> {
    this._address = address;
    const resp = await fetch(`${address}${API_PREFIX}/health`);
    if (!resp.ok) {
      throw new ConnectionError(
        `Failed to connect to QuicBaaS at ${address}: ${resp.statusText}`,
      );
    }
    this._connected = true;
  }

  async disconnect(): Promise<void> {
    this._connected = false;
  }

  isConnected(): boolean {
    return this._connected;
  }

  getAddress(): string {
    return this._address;
  }

  getTransport(): string {
    return this._transport;
  }
}

// RestOrchestratorClient implements OrchestratorClient using the QuicBaaS REST API.
// All endpoints are under /api/v1/orchestrator and accept/return JSON.
// Error responses are surfaced as APIError with the HTTP status code.
class RestOrchestratorClient implements OrchestratorClient {
  private readonly baseUrl: string;

  constructor(baseUrl: string) {
    // Trim trailing slash to allow consistent path concatenation.
    this.baseUrl = baseUrl.replace(/\/$/, '');
  }

  // fetch performs an authenticated fetch and deserialises the response body.
  // Non-2xx responses are converted to APIError.
  private async fetch<T>(path: string, init?: RequestInit): Promise<T> {
    const url = `${this.baseUrl}${API_PREFIX}${path}`;
    const resp = await fetch(url, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        Accept: 'application/json',
        ...(init?.headers ?? {}),
      },
    });

    if (!resp.ok) {
      let message = resp.statusText;
      try {
        const body = (await resp.json()) as { error?: string; message?: string };
        message = body.error ?? body.message ?? message;
      } catch {
        // Use statusText if the body is not valid JSON.
      }
      throw new APIError(resp.status, message);
    }

    return resp.json() as Promise<T>;
  }

  // listJobs maps to GET /api/v1/orchestrator/jobs?tenant_id=<tenantID>
  async listJobs(tenantID: string): Promise<JobStatus[]> {
    return this.fetch<JobStatus[]>(`/jobs?tenant_id=${encodeURIComponent(tenantID)}`);
  }

  // getJobStatus maps to GET /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
  async getJobStatus(tenantID: string, jobID: string): Promise<JobStatus> {
    return this.fetch<JobStatus>(
      `/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // startJob maps to POST /api/v1/orchestrator/jobs
  async startJob(req: StartJobRequest): Promise<StartJobResponse> {
    return this.fetch<StartJobResponse>('/jobs', {
      method: 'POST',
      body: JSON.stringify(req),
    });
  }

  // cancelJob maps to DELETE /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
  async cancelJob(tenantID: string, jobID: string): Promise<void> {
    await this.fetch<void>(
      `/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`,
      { method: 'DELETE' },
    );
  }

  // listAgents maps to GET /api/v1/orchestrator/agents?tenant_id=<tenantID>
  async listAgents(tenantID: string): Promise<AgentRegistration[]> {
    return this.fetch<AgentRegistration[]>(
      `/agents?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // getAgent maps to GET /api/v1/orchestrator/agents/:agentID?tenant_id=<tenantID>
  async getAgent(tenantID: string, agentID: string): Promise<AgentRegistration> {
    return this.fetch<AgentRegistration>(
      `/agents/${encodeURIComponent(agentID)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // listSessions maps to GET /api/v1/orchestrator/sessions[?backend=<backend>]
  async listSessions(backend?: string): Promise<SessionInfo[]> {
    const query = backend ? `?backend=${encodeURIComponent(backend)}` : '';
    return this.fetch<SessionInfo[]>(`/sessions${query}`);
  }

  // listTasks maps to GET /api/v1/orchestrator/tasks?tenant_id=<tenantID>
  async listTasks(tenantID: string): Promise<Task[]> {
    return this.fetch<Task[]>(`/tasks?tenant_id=${encodeURIComponent(tenantID)}`);
  }

  // getTask maps to GET /api/v1/orchestrator/tasks/:taskID?tenant_id=<tenantID>
  async getTask(tenantID: string, taskID: string): Promise<Task> {
    return this.fetch<Task>(
      `/tasks/${encodeURIComponent(taskID)}?tenant_id=${encodeURIComponent(tenantID)}`,
    );
  }

  // listBackends maps to GET /api/v1/orchestrator/backends
  async listBackends(): Promise<AgentBackendInfo[]> {
    return this.fetch<AgentBackendInfo[]>('/backends');
  }

  // healthCheck maps to GET /api/v1/orchestrator/health
  async healthCheck(): Promise<HealthCheckReport> {
    return this.fetch<HealthCheckReport>('/health');
  }

  // subscribeJobEvents opens a Server-Sent Events stream for a running job.
  // Maps to GET /api/v1/orchestrator/jobs/:jobID/events?tenant_id=<tenantID>
  // Returns a cleanup function that closes the EventSource connection.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void {
    const url = [
      this.baseUrl,
      API_PREFIX,
      `/jobs/${encodeURIComponent(jobID)}/events`,
      `?tenant_id=${encodeURIComponent(tenantID)}`,
    ].join('');

    const source = new EventSource(url);

    source.onmessage = (e: MessageEvent<string>) => {
      try {
        const event = JSON.parse(e.data) as JobEvent;
        onEvent(event);
      } catch {
        // Discard unparseable frames; the server may send keep-alive comments.
      }
    };

    // Surface EventSource errors as connection errors in the console.
    // The caller decides whether to retry via the cleanup/re-subscribe pattern.
    source.onerror = () => {
      if (source.readyState === EventSource.CLOSED) {
        source.close();
      }
    };

    return () => source.close();
  }
}

// RestClient is the top-level Client implementation for REST / browser mode.
// It composes the connection and orchestrator sub-clients and shares the
// base URL between them.
export class RestClient implements Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;

  constructor(baseUrl: string) {
    this.connection = new RestConnectionClient(baseUrl);
    this.orchestrator = new RestOrchestratorClient(baseUrl);
  }
}

// Re-export errors used in this module so callers can catch typed errors
// without importing from the errors module directly.
export { APIError, ConnectionError, NotConnectedError };
