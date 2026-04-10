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
import { ConnectionError } from './errors.js';
// wailsGo returns the window.go namespace cast to a two-level map.
// Wails generates bindings at compile time; we keep the index signature
// here and cast return values at each call site.
function wailsGo() {
    if (typeof window === 'undefined' || !window.go) {
        throw new ConnectionError('Wails runtime not available');
    }
    return window.go;
}
// connectionBinding returns the Wails-generated ConnectionBinding proxy.
// Wails places all exported Go structs under window.go.<package>.<StructName>.
function connectionBinding() {
    return wailsGo().main.ConnectionBinding;
}
// orchestratorBinding returns the Wails-generated OrchestratorBinding proxy.
// Wails places all exported Go structs under window.go.<package>.<StructName>.
function orchestratorBinding() {
    return wailsGo().main.OrchestratorBinding;
}
// WailsConnectionClient implements ConnectionClient over Wails IPC.
// Because the Go server is the same process, connect/disconnect are lightweight
// bookkeeping operations that delegate to the Go binding for state management.
class WailsConnectionClient {
    _connected = false;
    _address = '';
    _transport = 'wails';
    async connect(address, _transport, _tenantID) {
        await connectionBinding().Connect(address);
        this._address = address;
        this._connected = true;
    }
    async disconnect() {
        await connectionBinding().Disconnect();
        this._connected = false;
    }
    isConnected() {
        return this._connected;
    }
    getAddress() {
        return this._address;
    }
    getTransport() {
        return this._transport;
    }
}
// WailsOrchestratorClient implements OrchestratorClient over Wails IPC.
// Each method delegates directly to the Wails-generated Go binding. Wails
// wraps Go function calls in Promises automatically, so all methods are async
// from JavaScript's perspective even though the Go implementation is synchronous
// at the IPC boundary.
class WailsOrchestratorClient {
    async listJobs(tenantID) {
        return orchestratorBinding().ListJobs(tenantID);
    }
    async getJobStatus(tenantID, jobID) {
        return orchestratorBinding().GetJobStatus(tenantID, jobID);
    }
    async startJob(req) {
        return orchestratorBinding().StartJob(req);
    }
    async cancelJob(tenantID, jobID) {
        await orchestratorBinding().CancelJob(tenantID, jobID);
    }
    async listAgents(tenantID) {
        return orchestratorBinding().ListAgents(tenantID);
    }
    async getAgent(tenantID, agentID) {
        return orchestratorBinding().GetAgent(tenantID, agentID);
    }
    async listSessions(backend) {
        return orchestratorBinding().ListSessions(backend ?? '');
    }
    async listTasks(tenantID) {
        return orchestratorBinding().ListTasks(tenantID);
    }
    async getTask(tenantID, taskID) {
        return orchestratorBinding().GetTask(tenantID, taskID);
    }
    async listBackends() {
        return orchestratorBinding().ListBackends();
    }
    async healthCheck() {
        return orchestratorBinding().HealthCheck();
    }
    // subscribeJobEvents tells the Go binding to start emitting job events and
    // listens for them on the Wails runtime event bus. Returns a cleanup function
    // that unregisters the listener and tells Go to stop streaming.
    subscribeJobEvents(tenantID, jobID, onEvent) {
        // Start the server-side streaming goroutine.
        void orchestratorBinding().SubscribeJobEvents(tenantID, jobID);
        const eventName = `job-event:${jobID}`;
        if (!window.runtime) {
            // Return a no-op cleanup when there is no Wails runtime (e.g. tests).
            return () => { };
        }
        const cleanup = window.runtime.EventsOn(eventName, (...args) => {
            if (args.length > 0) {
                onEvent(args[0]);
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
export class WailsClient {
    connection;
    orchestrator;
    constructor() {
        this.connection = new WailsConnectionClient();
        this.orchestrator = new WailsOrchestratorClient();
    }
}
//# sourceMappingURL=wails.js.map