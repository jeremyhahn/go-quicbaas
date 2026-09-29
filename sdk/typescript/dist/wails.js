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
import { adoptPage, } from './page.js';
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
// fleetDeployBinding returns the Wails-generated FleetDeployBinding proxy.
// Wails exposes the Go struct at window.go.main.FleetDeployBinding.
function fleetDeployBinding() {
    return wailsGo().main.FleetDeployBinding;
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
// pageFields renders the shared paging parameters as the members a Go SDK
// request struct carries (page.Params: limit/offset/sort/q). An absent field
// is omitted so the request says only what the caller asked for.
function pageFields(page) {
    const fields = {};
    if (page === undefined)
        return fields;
    if (typeof page.limit === 'number' && page.limit > 0)
        fields['limit'] = page.limit;
    if (typeof page.offset === 'number' && page.offset > 0)
        fields['offset'] = page.offset;
    if (page.sort !== undefined && page.sort !== '')
        fields['sort'] = page.sort;
    if (page.q !== undefined && page.q !== '')
        fields['q'] = page.q;
    return fields;
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
    // =========================================================================
    // The paged listings
    //
    // THE PARAMETERS GO OVER THE BINDING; THE ENVELOPE DOES NOT COME BACK. A
    // Wails binding takes the SDK request struct verbatim, and those structs
    // embed the paging parameters, so limit/offset/sort/q reach the daemon and
    // the SERVER does the paging, ordering and searching. What the bindings
    // return, though, is a projected ARRAY — the envelope's `total` is dropped
    // on the Go side (cmd/quicbaas/bindings_orchestrator.go) — so an answer
    // without one is reported as a single whole page rather than as a first
    // page of an unknown number.
    //
    // The consequence, written down rather than discovered: on the desktop a
    // table shows one page and no pager until those bindings return the
    // envelope. adoptPage already handles both shapes, so the day they do,
    // these methods page for real without another edit here.
    // =========================================================================
    async listJobsPage(tenantID, page) {
        const req = { tenant_id: tenantID, ...pageFields(page) };
        if (page?.task_id !== undefined)
            req.task_id = page.task_id;
        if (page?.runner !== undefined)
            req.runner = page.runner;
        if (page?.project_id !== undefined)
            req.project_id = page.project_id;
        if (page?.statuses !== undefined)
            req.statuses = page.statuses;
        if (page?.job_ids !== undefined)
            req.job_ids = page.job_ids;
        if (page?.agent_id !== undefined)
            req.agent_id = page.agent_id;
        return adoptPage(await orchestratorBinding().ListJobs(req), 'jobs');
    }
    async listTasksPage(tenantID, page) {
        const req = { tenant_id: tenantID, ...pageFields(page) };
        if (page?.status !== undefined)
            req.status = page.status;
        if (page?.statuses !== undefined)
            req.statuses = page.statuses;
        if (page?.project_id !== undefined)
            req.project_id = page.project_id;
        if (page?.team_name !== undefined)
            req.team_name = page.team_name;
        if (page?.assigned_to !== undefined)
            req.assigned_to = page.assigned_to;
        if (page?.created_by !== undefined)
            req.created_by = page.created_by;
        if (page?.auto_spawn !== undefined)
            req.auto_spawn = page.auto_spawn;
        if (page?.agent_id !== undefined)
            req.agent_id = page.agent_id;
        if (page?.job_id !== undefined)
            req.job_id = page.job_id;
        return adoptPage(await orchestratorBinding().ListTasks(req), 'tasks');
    }
    async listSessionsPage(tenantID, page) {
        if (!tenantID) {
            throw new Error('listSessionsPage: tenantID is required');
        }
        const req = { tenantID, ...pageFields(page) };
        if (page?.backend !== undefined)
            req.backend = page.backend;
        if (page?.status !== undefined)
            req.status = page.status;
        if (page?.project_id !== undefined)
            req.project_id = page.project_id;
        if (page?.job_id !== undefined)
            req.job_id = page.job_id;
        if (page?.runner !== undefined)
            req.runner = page.runner;
        if (page?.agent_id !== undefined)
            req.agent_id = page.agent_id;
        return adoptPage(await orchestratorBinding().ListSessions(req), 'sessions');
    }
    async listFleetNodesPage(tenantID, page) {
        const req = { tenant_id: tenantID, ...pageFields(page) };
        if (page?.host !== undefined)
            req.host = page.host;
        if (page?.status !== undefined)
            req.status = page.status;
        if (page?.node_type !== undefined)
            req.node_type = page.node_type;
        if (page?.backend !== undefined)
            req.backend = page.backend;
        if (page?.model !== undefined)
            req.model = page.model;
        return adoptPage(await orchestratorBinding().ListFleetNodes(req), 'nodes');
    }
    async listWorkflowsPage(tenantID, page) {
        const b = orchestratorBinding();
        if (typeof b.ListWorkflows !== 'function') {
            throw new Error('wails: ListWorkflows not implemented');
        }
        const workflowFilters = {};
        if (page?.project_id !== undefined)
            workflowFilters['project_id'] = page.project_id;
        if (page?.mode !== undefined)
            workflowFilters['mode'] = page.mode;
        const answered = await b.ListWorkflows({
            tenant_id: tenantID,
            ...workflowFilters,
            ...pageFields(page),
        });
        return adoptPage(answered, 'workflows');
    }
    async listJobs(tenantID, taskID) {
        const req = { tenant_id: tenantID, task_id: taskID };
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
    async listFleetNodes(req) {
        return orchestratorBinding().ListFleetNodes(req);
    }
    async getFleetNode(req) {
        return orchestratorBinding().GetFleetNode(req);
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
    async listRunners() {
        return orchestratorBinding().ListRunners();
    }
    async listBackendModels(nodeID, backendName) {
        return orchestratorBinding().ListBackendModels({ node_id: nodeID, backend_name: backendName });
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
    // rotateSessionAccount is not yet exposed by the Wails SessionBinding.
    // Use the REST transport for account rotation in desktop mode until the
    // Go-side binding grows a RotateAccount method.
    async rotateSessionAccount(_req) {
        throw new Error('rotateSessionAccount: use REST transport for account rotation in desktop mode');
    }
    // deployFleetNode forwards the spec to the Wails FleetDeployBinding.Deploy
    // method. The Go-side binding orchestrates the SSH+SFTP+systemd workflow
    // and returns a structured result.
    async deployFleetNode(spec) {
        return fleetDeployBinding().Deploy(spec);
    }
    // getSessionHistory fetches a page of historical session events.
    // The Go binding forwards the request struct verbatim to the SDK.
    async getSessionHistory(req) {
        return sessionBinding().GetHistory(req);
    }
    // getSessionConversation fetches a page of a session's conversation.
    // The Go binding forwards the request struct verbatim to the SDK.
    async getSessionConversation(req) {
        return sessionBinding().GetConversation(req);
    }
    // getSpendReport delegates to OrchestratorBinding.GetSpendReport. The Go
    // binding forwards the request struct verbatim and returns the SDK type
    // unreshaped, so priced_entries — what says whether equivalent_micros is a
    // measured figure — survives the IPC hop.
    async getSpendReport(req) {
        return orchestratorBinding().GetSpendReport(req);
    }
    // getTenantSpendPolicy / setTenantSpendPolicy delegate to the binding verbs
    // of the same name, which forward the request struct verbatim.
    async getTenantSpendPolicy(req) {
        return orchestratorBinding().GetTenantSpendPolicy(req);
    }
    async setTenantSpendPolicy(req) {
        return orchestratorBinding().SetTenantSpendPolicy(req);
    }
    // getCostSettings / setCostSettings delegate to the binding verbs of the
    // same names, which pass the SDK types straight through.
    async getCostSettings(req) {
        return orchestratorBinding().GetCostSettings(req);
    }
    async setCostSettings(req) {
        return orchestratorBinding().SetCostSettings(req);
    }
    // getWorkerImageStatus delegates to OrchestratorBinding.GetWorkerImageStatus,
    // which reaches the Go SDK's SDLC client and returns its type unreshaped.
    async getWorkerImageStatus() {
        return orchestratorBinding().GetWorkerImageStatus();
    }
    // refreshWorkerImage delegates to OrchestratorBinding.RefreshWorkerImage.
    async refreshWorkerImage() {
        return orchestratorBinding().RefreshWorkerImage();
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
    // readmitTask delegates to OrchestratorBinding.ReadmitTask.
    //
    // The binding takes the SDK request struct, like every other task verb on
    // it; passing the two ids positionally reached a Go method that does not
    // exist in that shape and threw on every click in desktop mode.
    async readmitTask(tenantID, taskID) {
        const b = orchestratorBinding();
        if (typeof b.ReadmitTask !== 'function') {
            throw new Error('wails: ReadmitTask not implemented');
        }
        await b.ReadmitTask({ tenant_id: tenantID, task_id: taskID });
    }
    // deleteTask delegates to OrchestratorBinding.DeleteTask.
    async deleteTask(req) {
        const b = orchestratorBinding();
        if (typeof b.DeleteTask !== 'function') {
            throw new Error('wails: DeleteTask not implemented');
        }
        return b.DeleteTask(req);
    }
    // listAttention delegates to OrchestratorBinding.AttentionList, which
    // returns the full paged envelope (items/total/facets), not a bare array —
    // adoptPage reads it either way.
    async listAttention(tenantID, page) {
        const b = orchestratorBinding();
        if (typeof b.AttentionList !== 'function') {
            throw new Error('wails: AttentionList not implemented');
        }
        const req = { tenant_id: tenantID, ...pageFields(page) };
        if (page?.project_id !== undefined)
            req.project_id = page.project_id;
        if (page?.kind !== undefined)
            req.kind = page.kind;
        return adoptPage(await b.AttentionList(req), 'items');
    }
    // resolveAttention delegates to OrchestratorBinding.AttentionResolve.
    async resolveAttention(req) {
        const b = orchestratorBinding();
        if (typeof b.AttentionResolve !== 'function') {
            throw new Error('wails: AttentionResolve not implemented');
        }
        return b.AttentionResolve(req);
    }
    // decomposeTask delegates to OrchestratorBinding.DecomposeTask.
    async decomposeTask(req) {
        const b = orchestratorBinding();
        if (typeof b.DecomposeTask !== 'function') {
            throw new Error('wails: DecomposeTask not implemented');
        }
        return b.DecomposeTask(req);
    }
    // registerFleetNode delegates to OrchestratorBinding.RegisterFleetNode.
    async registerFleetNode(req) {
        const b = orchestratorBinding();
        if (typeof b.RegisterFleetNode !== 'function') {
            throw new Error('wails: RegisterFleetNode not implemented');
        }
        return b.RegisterFleetNode(req);
    }
    // removeFleetNode delegates to OrchestratorBinding.RemoveFleetNode.
    async removeFleetNode(req) {
        const b = orchestratorBinding();
        if (typeof b.RemoveFleetNode !== 'function') {
            throw new Error('wails: RemoveFleetNode not implemented');
        }
        await b.RemoveFleetNode(req);
    }
    // disableFleetNode delegates to OrchestratorBinding.DisableFleetNode.
    async disableFleetNode(req) {
        const b = orchestratorBinding();
        if (typeof b.DisableFleetNode !== 'function') {
            throw new Error('wails: DisableFleetNode not implemented');
        }
        return b.DisableFleetNode(req);
    }
    // enableFleetNode delegates to OrchestratorBinding.EnableFleetNode.
    async enableFleetNode(req) {
        const b = orchestratorBinding();
        if (typeof b.EnableFleetNode !== 'function') {
            throw new Error('wails: EnableFleetNode not implemented');
        }
        return b.EnableFleetNode(req);
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
    async webAuthnFinishLogin(_challenge, _credential) {
        throw new Error('webAuthnFinishLogin: use REST transport for WebAuthn in desktop mode');
    }
    async loginPIV() {
        throw new Error('loginPIV: PIV requires a browser TLS handshake; not supported in desktop mode');
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
    sdlc = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    accounts = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    agents = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    iam = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    kms = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    secrets = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ca = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    trust = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    crypto = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    dek = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    dynamicSecrets = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    barrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    tenantBarrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ceremony = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    bootstrap = null;
    // cluster status/management resolves through the REST transport in desktop
    // mode (the embedded server runs in-process); no Wails Go binding yet.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    cluster = null;
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