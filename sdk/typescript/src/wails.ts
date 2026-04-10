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
import { ConnectionError } from './errors.js';

// WailsBinding is the shape of a Wails-generated Go struct binding.
// Wails exposes each exported Go method as a property whose value is a
// function that returns a Promise. The exact method names are determined
// at Go compile time, so we use an index signature here.
type WailsBinding = {
  [method: string]: (...args: unknown[]) => Promise<unknown>;
};

// wailsGo returns the window.go namespace cast to a two-level map.
// Wails generates bindings at compile time; we keep the index signature
// here and cast return values at each call site.
function wailsGo(): Record<string, Record<string, WailsBinding[string]>> {
  if (typeof window === 'undefined' || !window.go) {
    throw new ConnectionError('Wails runtime not available');
  }
  return window.go as Record<string, Record<string, WailsBinding[string]>>;
}

// connectionBinding returns the Wails-generated ConnectionBinding proxy.
// Wails places all exported Go structs under window.go.<package>.<StructName>.
function connectionBinding(): WailsBinding {
  return wailsGo().main.ConnectionBinding as unknown as WailsBinding;
}

// orchestratorBinding returns the Wails-generated OrchestratorBinding proxy.
// Wails places all exported Go structs under window.go.<package>.<StructName>.
function orchestratorBinding(): WailsBinding {
  return wailsGo().main.OrchestratorBinding as unknown as WailsBinding;
}

// WailsConnectionClient implements ConnectionClient over Wails IPC.
// Because the Go server is the same process, connect/disconnect are lightweight
// bookkeeping operations that delegate to the Go binding for state management.
class WailsConnectionClient implements ConnectionClient {
  private _connected = false;
  private _address = '';
  private _transport = 'wails';

  async connect(address: string, _transport?: string, _tenantID?: string): Promise<void> {
    await connectionBinding().Connect(address);
    this._address = address;
    this._connected = true;
  }

  async disconnect(): Promise<void> {
    await connectionBinding().Disconnect();
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

// WailsOrchestratorClient implements OrchestratorClient over Wails IPC.
// Each method delegates directly to the Wails-generated Go binding. Wails
// wraps Go function calls in Promises automatically, so all methods are async
// from JavaScript's perspective even though the Go implementation is synchronous
// at the IPC boundary.
class WailsOrchestratorClient implements OrchestratorClient {
  async listJobs(tenantID: string): Promise<JobStatus[]> {
    return orchestratorBinding().ListJobs(tenantID) as Promise<JobStatus[]>;
  }

  async getJobStatus(tenantID: string, jobID: string): Promise<JobStatus> {
    return orchestratorBinding().GetJobStatus(tenantID, jobID) as Promise<JobStatus>;
  }

  async startJob(req: StartJobRequest): Promise<StartJobResponse> {
    return orchestratorBinding().StartJob(req) as Promise<StartJobResponse>;
  }

  async cancelJob(tenantID: string, jobID: string): Promise<void> {
    await orchestratorBinding().CancelJob(tenantID, jobID);
  }

  async listAgents(tenantID: string): Promise<AgentRegistration[]> {
    return orchestratorBinding().ListAgents(tenantID) as Promise<AgentRegistration[]>;
  }

  async getAgent(tenantID: string, agentID: string): Promise<AgentRegistration> {
    return orchestratorBinding().GetAgent(tenantID, agentID) as Promise<AgentRegistration>;
  }

  async listSessions(backend?: string): Promise<SessionInfo[]> {
    return orchestratorBinding().ListSessions(backend ?? '') as Promise<SessionInfo[]>;
  }

  async listTasks(tenantID: string): Promise<Task[]> {
    return orchestratorBinding().ListTasks(tenantID) as Promise<Task[]>;
  }

  async getTask(tenantID: string, taskID: string): Promise<Task> {
    return orchestratorBinding().GetTask(tenantID, taskID) as Promise<Task>;
  }

  async listBackends(): Promise<AgentBackendInfo[]> {
    return orchestratorBinding().ListBackends() as Promise<AgentBackendInfo[]>;
  }

  async healthCheck(): Promise<HealthCheckReport> {
    return orchestratorBinding().HealthCheck() as Promise<HealthCheckReport>;
  }

  // subscribeJobEvents tells the Go binding to start emitting job events and
  // listens for them on the Wails runtime event bus. Returns a cleanup function
  // that unregisters the listener and tells Go to stop streaming.
  subscribeJobEvents(
    tenantID: string,
    jobID: string,
    onEvent: (event: JobEvent) => void,
  ): () => void {
    // Start the server-side streaming goroutine.
    void orchestratorBinding().SubscribeJobEvents(tenantID, jobID);

    const eventName = `job-event:${jobID}`;

    if (!window.runtime) {
      // Return a no-op cleanup when there is no Wails runtime (e.g. tests).
      return () => { /* no-op */ };
    }

    const cleanup = window.runtime.EventsOn(eventName, (...args: unknown[]) => {
      if (args.length > 0) {
        onEvent(args[0] as JobEvent);
      }
    });

    return () => {
      if (typeof cleanup === 'function') {
        cleanup();
      }
      // Signal the Go goroutine to stop streaming for this job.
      const binding = orchestratorBinding();
      if (typeof binding.UnsubscribeJobEvents === 'function') {
        void binding.UnsubscribeJobEvents(tenantID, jobID);
      }
    };
  }
}

// WailsClient is the top-level Client implementation for Wails desktop mode.
// It composes the connection and orchestrator sub-clients.
export class WailsClient implements Client {
  readonly connection: ConnectionClient;
  readonly orchestrator: OrchestratorClient;

  constructor() {
    this.connection = new WailsConnectionClient();
    this.orchestrator = new WailsOrchestratorClient();
  }
}
