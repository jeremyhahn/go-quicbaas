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
import { unsupportedClient } from './unsupported.js';
import { wholeSetPage, } from './page.js';
import { DeployNotSupportedError, TransportNotSupportedError } from './errors.js';
// refuse rejects one verb of a family this transport cannot carry yet.
function refuse(family, method) {
    throw new TransportNotSupportedError('unix', family, unixUnsupported, method);
}
// unixUnsupported is why every family refuses here. The Unix wire is gRPC
// over AF_UNIX, and the server does serve the orchestrator, auth, projects,
// sdlc and accounts services on it; but speaking gRPC needs an HTTP/2
// gRPC client and generated stubs, which this SDK (no runtime dependencies)
// does not carry, and a browser cannot open a socket at all.
const unixUnsupported = 'the Unix wire is gRPC over AF_UNIX and the SDK has no Node gRPC client or ' +
    'generated stubs; use the rest transport';
class UnixConnectionClient {
    _socketPath;
    _transport = 'unix';
    constructor(socketPath) {
        this._socketPath = socketPath;
    }
    async connect(_address) { refuse('connection', 'connect'); }
    async disconnect() { refuse('connection', 'disconnect'); }
    isConnected() { return false; }
    getAddress() { return this._socketPath; }
    getTransport() { return this._transport; }
}
class UnixOrchestratorClient {
    // =========================================================================
    // The paged listings
    //
    // This transport cannot speak the Unix socket's gRPC yet, so every listing
    // here refuses through its unpaged sibling rather than answering a page it
    // never fetched.
    //
    // Each delegates to its unpaged sibling and reports the whole answer as ONE
    // page (wholeSetPage): `total` is its length and there is no `next_offset`,
    // which is true of what this transport can actually fetch. The day it can
    // page, these methods are where the parameters go.
    // =========================================================================
    async listJobsPage(tenantID, page) {
        return wholeSetPage(await this.listJobs(tenantID, page?.task_id));
    }
    async listTasksPage(tenantID, _page) {
        return wholeSetPage(await this.listTasks(tenantID));
    }
    async listSessionsPage(tenantID, page) {
        const req = { tenantID };
        if (page?.backend !== undefined)
            req.backend = page.backend;
        if (page?.status !== undefined)
            req.status = page.status;
        const resp = await this.listSessions(req);
        return wholeSetPage(resp.sessions ?? []);
    }
    async listFleetNodesPage(tenantID, page) {
        const req = { tenant_id: tenantID };
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
        const resp = await this.listFleetNodes(req);
        return wholeSetPage(resp.nodes ?? []);
    }
    async listWorkflowsPage(tenantID, _page) {
        const resp = await this.listWorkflows(tenantID);
        return wholeSetPage(resp.workflows ?? []);
    }
    async listJobs(_tenantID, _taskID) { return refuse('orchestrator', 'listJobs'); }
    async getJobStatus(_tenantID, _jobID) { return refuse('orchestrator', 'getJobStatus'); }
    async startJob(_req) { return refuse('orchestrator', 'startJob'); }
    async cancelJob(_tenantID, _jobID) { return refuse('orchestrator', 'cancelJob'); }
    async listFleetNodes(_req) { return refuse('orchestrator', 'listFleetNodes'); }
    async getFleetNode(_req) { return refuse('orchestrator', 'getFleetNode'); }
    async listSessions(_req) { return refuse('orchestrator', 'listSessions'); }
    async listTasks(_tenantID) { return refuse('orchestrator', 'listTasks'); }
    async getTask(_tenantID, _taskID) { return refuse('orchestrator', 'getTask'); }
    async listBackends() { return refuse('orchestrator', 'listBackends'); }
    async listRunners() { return refuse('orchestrator', 'listRunners'); }
    async listBackendModels(_agentID, _backendName) { return refuse('orchestrator', 'listBackendModels'); }
    async healthCheck() { return refuse('orchestrator', 'healthCheck'); }
    subscribeJobEvents(_tenantID, _jobID, _onEvent) { return refuse('orchestrator', 'subscribeJobEvents'); }
    async spawnSession(_req) { return refuse('orchestrator', 'spawnSession'); }
    async promptSession(_req) { return refuse('orchestrator', 'promptSession'); }
    async closeSession(_req) { return refuse('orchestrator', 'closeSession'); }
    async rotateSessionAccount(_req) { return refuse('orchestrator', 'rotateSessionAccount'); }
    async getSessionHistory(_req) { return refuse('orchestrator', 'getSessionHistory'); }
    async getSessionConversation(_req) { return refuse('orchestrator', 'getSessionConversation'); }
    async getSpendReport(_req) { return refuse('orchestrator', 'getSpendReport'); }
    async getTenantSpendPolicy(_req) { return refuse('orchestrator', 'getTenantSpendPolicy'); }
    async setTenantSpendPolicy(_req) { return refuse('orchestrator', 'setTenantSpendPolicy'); }
    async getCostSettings(_req) { return refuse('orchestrator', 'getCostSettings'); }
    async setCostSettings(_req) { return refuse('orchestrator', 'setCostSettings'); }
    async getWorkerImageStatus() { return refuse('orchestrator', 'getWorkerImageStatus'); }
    async refreshWorkerImage() { return refuse('orchestrator', 'refreshWorkerImage'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *subscribeSession(_req) { return refuse('orchestrator', 'subscribeSession'); }
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('unix'); }
    async inference(_req) { return refuse('orchestrator', 'inference'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *inferenceStream(_req, _signal) { return refuse('orchestrator', 'inferenceStream'); }
    async createWorkflow(_req) { return refuse('orchestrator', 'createWorkflow'); }
    async listWorkflows(_tenantID) { return refuse('orchestrator', 'listWorkflows'); }
    async getWorkflow(_tenantID, _name) { return refuse('orchestrator', 'getWorkflow'); }
    async updateWorkflow(_name, _def) { return refuse('orchestrator', 'updateWorkflow'); }
    async deleteWorkflow(_tenantID, _name) { return refuse('orchestrator', 'deleteWorkflow'); }
    async createTask(_req) { return refuse('orchestrator', 'createTask'); }
    async claimTask(_req) { return refuse('orchestrator', 'claimTask'); }
    async completeTask(_req) { return refuse('orchestrator', 'completeTask'); }
    async readmitTask(_tenantID, _taskID) { return refuse('orchestrator', 'readmitTask'); }
    async deleteTask(_req) { return refuse('orchestrator', 'deleteTask'); }
    async listAttention(_tenantID, _page) { return refuse('orchestrator', 'listAttention'); }
    async resolveAttention(_req) { return refuse('orchestrator', 'resolveAttention'); }
    async decomposeTask(_req) { return refuse('orchestrator', 'decomposeTask'); }
    async registerFleetNode(_req) { return refuse('orchestrator', 'registerFleetNode'); }
    async removeFleetNode(_req) { return refuse('orchestrator', 'removeFleetNode'); }
    async disableFleetNode(_req) { return refuse('orchestrator', 'disableFleetNode'); }
    async enableFleetNode(_req) { return refuse('orchestrator', 'enableFleetNode'); }
}
class UnixAuthClient {
    async authValidate(_token) { return refuse('auth', 'authValidate'); }
    async authLogin(_credentials) { return refuse('auth', 'authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return refuse('auth', 'webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return refuse('auth', 'webAuthnFinishLogin'); }
    async loginPIV() { return refuse('auth', 'loginPIV'); }
    async logout(_token) { return refuse('auth', 'logout'); }
    async registerBegin(_req) { return refuse('auth', 'registerBegin'); }
    async registerFinish(_req) { return refuse('auth', 'registerFinish'); }
}
// UnixClient is the stub entry point for the Unix domain socket transport.
export class UnixClient {
    connection;
    orchestrator;
    auth;
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
    projects = unsupportedClient('unix', 'projects', unixUnsupported);
    sdlc = unsupportedClient('unix', 'sdlc', unixUnsupported);
    accounts = unsupportedClient('unix', 'accounts', unixUnsupported);
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
    // cluster status/management is exposed over the REST transport; not wired here.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    cluster = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    seal = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    sshKeys = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ssh = null;
    constructor(socketPath, _config = {}) {
        this.connection = new UnixConnectionClient(socketPath);
        this.orchestrator = new UnixOrchestratorClient();
        this.auth = new UnixAuthClient();
    }
    async logout(_token) {
        // Non-REST transports don't manage bearer tokens via the REST logout verb.
        // The caller is responsible for clearing local credentials; this is a no-op
        // so the unified AuthClient surface compiles across every transport.
    }
}
//# sourceMappingURL=unix.js.map