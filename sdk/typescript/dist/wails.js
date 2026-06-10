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
function connectionBinding() {
    return wailsGo().main.ConnectionBinding;
}
// orchestratorBinding returns the Wails-generated OrchestratorBinding proxy.
function orchestratorBinding() {
    return wailsGo().main.OrchestratorBinding;
}
// sessionBinding returns the Wails-generated SessionBinding proxy.
// Phase 6 Go side: window.go.main.SessionBinding.
function sessionBinding() {
    return wailsGo().main.SessionBinding;
}
// agentDeployBinding returns the Wails-generated AgentDeployBinding proxy.
// Wails exposes the Go struct at window.go.main.AgentDeployBinding.
function agentDeployBinding() {
    return wailsGo().main.AgentDeployBinding;
}
// sshKeysBinding returns the Wails-generated SSHKeysBinding proxy. The
// Go struct lives at window.go.main.SSHKeysBinding and mirrors the
// sshkey.Client interface.
function sshKeysBinding() {
    return wailsGo().main.SSHKeysBinding;
}
// WailsSSHKeysClient implements SSHKeysClient over the Wails IPC. Each
// call forwards to the Go binding, which dispatches through the shared
// SDK client owned by the ConnectionBinding.
//
// The Go binding (cmd/quicbaas/bindings_ssh.go) only exposes the legacy
// flat surface: List, Create, Get, Delete with server-side scope/owner
// defaults. The full scope/owner/name methods are available via REST only
// (use RestClient.ssh.keys for those).
class WailsSSHKeysClient {
    async list() {
        return sshKeysBinding().List();
    }
    async create(req) {
        return sshKeysBinding().Create(req);
    }
    async get(name) {
        return sshKeysBinding().Get(name);
    }
    async remove(name) {
        await sshKeysBinding().Delete(name);
    }
    // ---- Scoped methods not available via Wails binding ----
    // Use the REST transport (RestClient.ssh.keys) for these operations.
    async listScoped(_query) {
        throw new Error('wails: listScoped not available — use REST transport');
    }
    async getScoped(_scope, _owner, _name) {
        throw new Error('wails: getScoped not available — use REST transport');
    }
    async deleteScoped(_scope, _owner, _name) {
        throw new Error('wails: deleteScoped not available — use REST transport');
    }
    async exportPublic(_scope, _owner, _name) {
        throw new Error('wails: exportPublic not available — use REST transport');
    }
    async sign(_scope, _owner, _name, _req) {
        throw new Error('wails: sign not available — use REST transport');
    }
    async import_(_req) {
        throw new Error('wails: import_ not available — use REST transport');
    }
}
// WailsKnownHostsClient is a stub that surfaces the interface contract for
// Wails mode. Known-hosts operations are not exposed via the Go Wails binding;
// use the REST transport (RestClient.ssh.knownHosts) for production use.
class WailsKnownHostsClient {
    async add(_req) {
        throw new Error('wails: ssh.knownHosts not available — use REST transport');
    }
    async list(_query) {
        throw new Error('wails: ssh.knownHosts not available — use REST transport');
    }
    async remove(_host, _query) {
        throw new Error('wails: ssh.knownHosts not available — use REST transport');
    }
    async verify(_host, _req) {
        throw new Error('wails: ssh.knownHosts not available — use REST transport');
    }
}
// WailsAuthorizedKeysClient is a stub that surfaces the interface contract for
// Wails mode. Authorized-keys operations are not exposed via the Go Wails
// binding; use the REST transport (RestClient.ssh.authorized) for production use.
class WailsAuthorizedKeysClient {
    async emit(_req) {
        throw new Error('wails: ssh.authorized not available — use REST transport');
    }
    async verify(_req) {
        throw new Error('wails: ssh.authorized not available — use REST transport');
    }
}
// WailsSSHClient groups the three Wails SSH sub-client stubs.
class WailsSSHClient {
    keys;
    knownHosts;
    authorized;
    constructor(keys) {
        this.keys = keys;
        this.knownHosts = new WailsKnownHostsClient();
        this.authorized = new WailsAuthorizedKeysClient();
    }
}
// WailsConnectionClient implements ConnectionClient over Wails IPC.
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
//
// Every Wails Go binding accepts the corresponding SDK request struct
// verbatim — Wails serialises the TS object as JSON and Go unmarshals it
// into the typed request. Methods on the OrchestratorClient interface that
// expose flat argument lists (e.g. `listJobs(tenantID)`) construct the
// request object here so the interface contract is unchanged while the
// wire shape matches what Go expects.
class WailsOrchestratorClient {
    async listJobs(tenantID) {
        const req = { tenant_id: tenantID };
        const resp = (await orchestratorBinding().ListJobs(req));
        // The Go binding returns the projected job array; older shims may
        // have returned a ListJobsResponse. Normalise to JobStatus[].
        if (Array.isArray(resp))
            return resp;
        return resp.jobs ?? [];
    }
    async getJobStatus(tenantID, jobID) {
        const req = { tenant_id: tenantID, job_id: jobID };
        return orchestratorBinding().GetJobStatus(req);
    }
    async startJob(req) {
        return orchestratorBinding().StartJob(req);
    }
    async cancelJob(tenantID, jobID) {
        const req = { tenant_id: tenantID, job_id: jobID };
        await orchestratorBinding().CancelJob(req);
    }
    async listAgents(req) {
        return orchestratorBinding().ListAgents(req);
    }
    async getAgentStatus(req) {
        return orchestratorBinding().GetAgent(req);
    }
    async listSessions(req) {
        if (!req.tenantID) {
            throw new Error('listSessions: tenantID is required');
        }
        return orchestratorBinding().ListSessions(req);
    }
    async listTasks(tenantID) {
        const req = { tenant_id: tenantID };
        const resp = (await orchestratorBinding().ListTasks(req));
        if (Array.isArray(resp))
            return resp;
        return resp.tasks ?? [];
    }
    async getTask(tenantID, taskID) {
        const req = { tenant_id: tenantID, task_id: taskID };
        return orchestratorBinding().GetTask(req);
    }
    async listBackends() {
        return orchestratorBinding().ListBackends();
    }
    async listBackendModels(agentID, backendName) {
        return orchestratorBinding().ListBackendModels({ agent_id: agentID, backend_name: backendName });
    }
    async healthCheck() {
        return orchestratorBinding().HealthCheck();
    }
    // subscribeJobEvents tells the Go binding to start emitting job events and
    // listens for them on the Wails runtime event bus. Returns a cleanup function
    // that unregisters the listener and tells Go to stop streaming.
    subscribeJobEvents(tenantID, jobID, onEvent) {
        const req = { tenant_id: tenantID, job_id: jobID };
        void orchestratorBinding().SubscribeJobEvents(req);
        const eventName = `job-event:${jobID}`;
        if (!window.runtime) {
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
            const binding = orchestratorBinding();
            if (typeof binding.UnsubscribeJobEvents === 'function') {
                void binding.UnsubscribeJobEvents(req);
            }
        };
    }
    // spawnSession creates a new agent session via the Wails SessionBinding.
    async spawnSession(req) {
        return sessionBinding().Spawn(req);
    }
    // promptSession sends a prompt to an existing session via the Wails SessionBinding.
    async promptSession(req) {
        return sessionBinding().Prompt(req);
    }
    // closeSession terminates a session via the Wails SessionBinding.
    async closeSession(req) {
        return sessionBinding().Close(req);
    }
    // deployAgent forwards the spec to the Wails AgentDeployBinding.Deploy
    // method. The Go-side binding orchestrates the SSH+SFTP+systemd workflow
    // and returns a structured result.
    async deployAgent(spec) {
        return agentDeployBinding().Deploy(spec);
    }
    // getSessionHistory fetches a page of historical session events.
    // The Go binding forwards the request struct verbatim to the SDK.
    async getSessionHistory(req) {
        return sessionBinding().GetHistory(req);
    }
    // createWorkflow delegates to OrchestratorBinding.CreateWorkflow if available.
    async createWorkflow(req) {
        const b = orchestratorBinding();
        if (typeof b.CreateWorkflow !== 'function') {
            throw new Error('wails: CreateWorkflow not implemented');
        }
        return b.CreateWorkflow(req);
    }
    // listWorkflows delegates to OrchestratorBinding.ListWorkflows if available.
    async listWorkflows(tenantID) {
        const b = orchestratorBinding();
        if (typeof b.ListWorkflows !== 'function') {
            throw new Error('wails: ListWorkflows not implemented');
        }
        return b.ListWorkflows({ tenant_id: tenantID });
    }
    // getWorkflow delegates to OrchestratorBinding.GetWorkflow if available.
    async getWorkflow(tenantID, name) {
        const b = orchestratorBinding();
        if (typeof b.GetWorkflow !== 'function') {
            throw new Error('wails: GetWorkflow not implemented');
        }
        return b.GetWorkflow({ tenant_id: tenantID, name });
    }
    // updateWorkflow delegates to OrchestratorBinding.UpdateWorkflow if available.
    async updateWorkflow(name, def) {
        const b = orchestratorBinding();
        if (typeof b.UpdateWorkflow !== 'function') {
            throw new Error('wails: UpdateWorkflow not implemented');
        }
        return b.UpdateWorkflow({ name, definition: def });
    }
    // deleteWorkflow delegates to OrchestratorBinding.DeleteWorkflow if available.
    async deleteWorkflow(tenantID, name) {
        const b = orchestratorBinding();
        if (typeof b.DeleteWorkflow !== 'function') {
            throw new Error('wails: DeleteWorkflow not implemented');
        }
        await b.DeleteWorkflow({ tenant_id: tenantID, name });
    }
    // createTask delegates to OrchestratorBinding.CreateTask.
    async createTask(req) {
        const b = orchestratorBinding();
        if (typeof b.CreateTask !== 'function') {
            throw new Error('wails: CreateTask not implemented');
        }
        return b.CreateTask(req);
    }
    // claimTask delegates to OrchestratorBinding.ClaimTask.
    async claimTask(req) {
        const b = orchestratorBinding();
        if (typeof b.ClaimTask !== 'function') {
            throw new Error('wails: ClaimTask not implemented');
        }
        await b.ClaimTask(req);
    }
    // completeTask delegates to OrchestratorBinding.CompleteTask.
    async completeTask(req) {
        const b = orchestratorBinding();
        if (typeof b.CompleteTask !== 'function') {
            throw new Error('wails: CompleteTask not implemented');
        }
        await b.CompleteTask(req);
    }
    // decomposeTask delegates to OrchestratorBinding.DecomposeTask.
    async decomposeTask(req) {
        const b = orchestratorBinding();
        if (typeof b.DecomposeTask !== 'function') {
            throw new Error('wails: DecomposeTask not implemented');
        }
        return b.DecomposeTask(req);
    }
    // registerAgent delegates to OrchestratorBinding.RegisterAgent.
    async registerAgent(req) {
        const b = orchestratorBinding();
        if (typeof b.RegisterAgent !== 'function') {
            throw new Error('wails: RegisterAgent not implemented');
        }
        return b.RegisterAgent(req);
    }
    // removeAgent delegates to OrchestratorBinding.RemoveAgent.
    async removeAgent(req) {
        const b = orchestratorBinding();
        if (typeof b.RemoveAgent !== 'function') {
            throw new Error('wails: RemoveAgent not implemented');
        }
        await b.RemoveAgent(req);
    }
    // disableAgent delegates to OrchestratorBinding.DisableAgent.
    async disableAgent(req) {
        const b = orchestratorBinding();
        if (typeof b.DisableAgent !== 'function') {
            throw new Error('wails: DisableAgent not implemented');
        }
        return b.DisableAgent(req);
    }
    // enableAgent delegates to OrchestratorBinding.EnableAgent.
    async enableAgent(req) {
        const b = orchestratorBinding();
        if (typeof b.EnableAgent !== 'function') {
            throw new Error('wails: EnableAgent not implemented');
        }
        return b.EnableAgent(req);
    }
    // subscribeSession returns an async iterable of session events via Wails IPC.
    //
    // The Go-side binding (Phase 6) is registered as SessionBinding.Subscribe.
    // It returns the historical replay slice first, then emits new events via
    // the Wails event bus under the topic "session-event:<sessionID>".
    async *subscribeSession(req) {
        // Fetch historical events from the Go binding.
        const history = (await sessionBinding().Subscribe(req));
        for (const h of history) {
            yield h;
        }
        if (!req.follow)
            return;
        // Queue to buffer events that arrive while the generator is suspended.
        const queue = [];
        // resolve holds the pending Promise resolver so we can wake the generator
        // when a new event arrives.
        let wakeResolve = null;
        const eventTopic = `session-event:${req.session_id}`;
        if (!window.runtime) {
            // No Wails runtime available (e.g. test environment without the stub).
            return;
        }
        const unsub = window.runtime.EventsOn(eventTopic, (...args) => {
            if (args.length > 0) {
                queue.push(args[0]);
                const r = wakeResolve;
                wakeResolve = null;
                r?.();
            }
        });
        try {
            while (true) {
                if (req.signal?.aborted)
                    return;
                if (queue.length > 0) {
                    yield queue.shift();
                    continue;
                }
                // Wait until the next event arrives or the signal fires.
                await new Promise((resolve) => {
                    wakeResolve = resolve;
                    // If the signal fires while we are waiting, wake immediately.
                    req.signal?.addEventListener('abort', () => {
                        const r = wakeResolve;
                        wakeResolve = null;
                        r?.();
                    }, { once: true });
                });
                if (req.signal?.aborted)
                    return;
            }
        }
        finally {
            unsub();
        }
    }
    // inference delegates to OrchestratorBinding.Inference via Wails IPC.
    // The Go-side binding calls into the orchestrator service directly (in-process).
    async inference(req) {
        const b = orchestratorBinding();
        if (typeof b.Inference !== 'function') {
            throw new Error('wails: Inference not implemented — update the Go Wails binding');
        }
        return b.Inference(req);
    }
    // inferenceStream is not natively streamable over Wails IPC (no chunked HTTP).
    // Falls back to aggregate inference and yields a single chunk so callers
    // using the streaming API work transparently in desktop mode.
    async *inferenceStream(req, _signal) {
        const resp = await this.inference(req);
        yield {
            delta: resp.content,
            model: resp.model,
            stop_reason: resp.stop_reason,
            final: true,
        };
    }
}
// WailsAuthClient is a no-op AuthClient for desktop mode.
// In Wails, auth is managed by the OS session; validate always returns true.
// WebAuthn / PIV flows require a browser TLS handshake and are not applicable
// to the embedded desktop transport — those methods delegate to notImpl.
class WailsAuthClient {
    async authValidate(_token) {
        return true;
    }
    async authLogin(_credentials) {
        // Wails desktop mode runs embedded — no login required.
    }
    async webAuthnBeginLogin(_tenantID, _email) {
        // Wails desktop mode delegates to the embedded REST transport for WebAuthn.
        // Use the REST transport client when WebAuthn is needed in desktop mode.
        throw new Error('webAuthnBeginLogin: use REST transport for WebAuthn in desktop mode');
    }
    async webAuthnFinishLogin(_tenantID, _email, _credential) {
        throw new Error('webAuthnFinishLogin: use REST transport for WebAuthn in desktop mode');
    }
    async loginPIV() {
        throw new Error('loginPIV: PIV requires a browser TLS handshake; not supported in desktop mode');
    }
    async listPublicTenants() {
        // Desktop mode: return the implicit default tenant.
        return [{ id: 'default', name: 'default', display_name: 'Default' }];
    }
    async logout(_token) {
        // Wails desktop mode runs embedded — there is no remote bearer token to
        // revoke. The host application owns the OS-level session; this is a no-op
        // so the caller can use a single logout() method across transports.
    }
    async registerBegin(_req) {
        throw new Error('registerBegin: use REST transport for self-service registration in desktop mode');
    }
    async registerFinish(_req) {
        throw new Error('registerFinish: use REST transport for self-service registration in desktop mode');
    }
}
// WailsClient is the top-level Client implementation for Wails desktop mode.
export class WailsClient {
    connection;
    orchestrator;
    auth;
    // Data sub-clients delegate to REST over the embedded server. The Wails
    // desktop mode runs the full QuicBaaS server in-process so REST calls
    // resolve against localhost without a network round-trip.
    // These are intentionally null until Wails Go bindings are generated;
    // desktop users can also access data surfaces through the REST transport.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    data = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    vectors = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    timeseries = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    objects = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    rag = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    embeddings = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    messaging = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    scheduler = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    projects = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    iam = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    kms = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    secrets = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    barrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    tenantBarrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ceremony = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    bootstrap = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    seal = null;
    /**
     * sshKeys is the legacy backward-compat accessor.
     * Points to ssh.keys — the same WailsSSHKeysClient instance.
     */
    sshKeys;
    /**
     * ssh exposes the full scoped SSH interface.
     * In Wails mode, only ssh.keys (List/Create/Get/Delete) is backed by the
     * Go binding. ssh.knownHosts and ssh.authorized throw in Wails mode;
     * use the REST transport for those.
     */
    ssh;
    constructor() {
        this.connection = new WailsConnectionClient();
        this.orchestrator = new WailsOrchestratorClient();
        this.auth = new WailsAuthClient();
        const keysClient = new WailsSSHKeysClient();
        this.ssh = new WailsSSHClient(keysClient);
        this.sshKeys = this.ssh.keys;
    }
}
//# sourceMappingURL=wails.js.map