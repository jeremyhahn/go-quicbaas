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
import { wholeSetPage, } from './page.js';
import { QuicBaaSError, DeployNotSupportedError } from './errors.js';
// McpNotImplementedError is thrown by all methods until the MCP protocol
// client is wired into this file.
export class McpNotImplementedError extends QuicBaaSError {
    constructor(method) {
        super(`mcp transport: ${method} not implemented. ` +
            'Wire an MCP client library and the go-quicbaas MCP tool definitions.');
        this.name = 'McpNotImplementedError';
    }
}
function notImpl(method) {
    throw new McpNotImplementedError(method);
}
class McpConnectionClient {
    _endpoint;
    _transport = 'mcp';
    constructor(endpoint) {
        this._endpoint = endpoint;
    }
    async connect(_address) { notImpl('connect'); }
    async disconnect() { notImpl('disconnect'); }
    isConnected() { return false; }
    getAddress() { return this._endpoint; }
    getTransport() { return this._transport; }
}
class McpOrchestratorClient {
    // =========================================================================
    // The paged listings
    //
    // This transport is not wired to an MCP client yet, so every listing
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
    async listAgentsPage(tenantID, page) {
        const req = { tenant_id: tenantID };
        if (page?.host !== undefined)
            req.host = page.host;
        if (page?.status !== undefined)
            req.status = page.status;
        if (page?.agent_type !== undefined)
            req.agent_type = page.agent_type;
        if (page?.backend !== undefined)
            req.backend = page.backend;
        if (page?.model !== undefined)
            req.model = page.model;
        const resp = await this.listAgents(req);
        return wholeSetPage(resp.agents ?? []);
    }
    async listWorkflowsPage(tenantID, _page) {
        const resp = await this.listWorkflows(tenantID);
        return wholeSetPage(resp.workflows ?? []);
    }
    async listJobs(_tenantID, _taskID) { return notImpl('listJobs'); }
    async getJobStatus(_tenantID, _jobID) { return notImpl('getJobStatus'); }
    async startJob(_req) { return notImpl('startJob'); }
    async cancelJob(_tenantID, _jobID) { return notImpl('cancelJob'); }
    async listAgents(_req) { return notImpl('listAgents'); }
    async getAgentStatus(_req) { return notImpl('getAgentStatus'); }
    async listSessions(_req) { return notImpl('listSessions'); }
    async listTasks(_tenantID) { return notImpl('listTasks'); }
    async getTask(_tenantID, _taskID) { return notImpl('getTask'); }
    async listBackends() { return notImpl('listBackends'); }
    async listRunners() { return notImpl('listRunners'); }
    async listBackendModels(_agentID, _backendName) { return notImpl('listBackendModels'); }
    async healthCheck() { return notImpl('healthCheck'); }
    subscribeJobEvents(_tenantID, _jobID, _onEvent) { return notImpl('subscribeJobEvents'); }
    async spawnSession(_req) { return notImpl('spawnSession'); }
    async promptSession(_req) { return notImpl('promptSession'); }
    async closeSession(_req) { return notImpl('closeSession'); }
    async rotateSessionAccount(_req) { return notImpl('rotateSessionAccount'); }
    async getSessionHistory(_req) { return notImpl('getSessionHistory'); }
    async getSessionConversation(_req) { return notImpl('getSessionConversation'); }
    async getSpendReport(_req) { return notImpl('getSpendReport'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *subscribeSession(_req) { return notImpl('subscribeSession'); }
    async deployAgent(_spec) { throw new DeployNotSupportedError('mcp'); }
    async inference(_req) { return notImpl('inference'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *inferenceStream(_req, _signal) { return notImpl('inferenceStream'); }
    async createWorkflow(_req) { return notImpl('createWorkflow'); }
    async listWorkflows(_tenantID) { return notImpl('listWorkflows'); }
    async getWorkflow(_tenantID, _name) { return notImpl('getWorkflow'); }
    async updateWorkflow(_name, _def) { return notImpl('updateWorkflow'); }
    async deleteWorkflow(_tenantID, _name) { return notImpl('deleteWorkflow'); }
    async createTask(_req) { return notImpl('createTask'); }
    async claimTask(_req) { return notImpl('claimTask'); }
    async completeTask(_req) { return notImpl('completeTask'); }
    async readmitTask(_tenantID, _taskID) { return notImpl('readmitTask'); }
    async deleteTask(_req) { return notImpl('deleteTask'); }
    async decomposeTask(_req) { return notImpl('decomposeTask'); }
    async registerAgent(_req) { return notImpl('registerAgent'); }
    async removeAgent(_req) { return notImpl('removeAgent'); }
    async disableAgent(_req) { return notImpl('disableAgent'); }
    async enableAgent(_req) { return notImpl('enableAgent'); }
}
class McpAuthClient {
    async authValidate(_token) { return notImpl('authValidate'); }
    async authLogin(_credentials) { return notImpl('authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return notImpl('webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return notImpl('webAuthnFinishLogin'); }
    async loginPIV() { return notImpl('loginPIV'); }
    async logout(_token) { return notImpl('logout'); }
    async registerBegin(_req) { return notImpl('registerBegin'); }
    async registerFinish(_req) { return notImpl('registerFinish'); }
}
// McpBarrierClient is a stub BarrierClient that throws McpNotImplementedError
// on every call. Real MCP tool wiring will replace this stub.
export class McpBarrierClient {
    async status() { return notImpl('barrier.status'); }
    async initialize(_req) { return notImpl('barrier.initialize'); }
    async unseal(_req) { return notImpl('barrier.unseal'); }
    async seal() { return notImpl('barrier.seal'); }
    async initializeShamir(_req) { return notImpl('barrier.initializeShamir'); }
    async unsealWithShare(_req) { return notImpl('barrier.unsealWithShare'); }
    async unsealWithShares(_req) { return notImpl('barrier.unsealWithShares'); }
    async shamirListShares() { return notImpl('barrier.shamirListShares'); }
    async shamirDeleteShare(_req) { return notImpl('barrier.shamirDeleteShare'); }
    async shamirDeleteAllShares() { return notImpl('barrier.shamirDeleteAllShares'); }
    async shamirVerify() { return notImpl('barrier.shamirVerify'); }
    async rekey(_req) { return notImpl('barrier.rekey'); }
    async generateRecoveryKeys(_req) { return notImpl('barrier.generateRecoveryKeys'); }
    async recoverWithKeys(_req) { return notImpl('barrier.recoverWithKeys'); }
    async deleteRecoveryKeys() { return notImpl('barrier.deleteRecoveryKeys'); }
    async recoveryStatus() { return notImpl('barrier.recoveryStatus'); }
    async generateRootToken(_req) { return notImpl('barrier.generateRootToken'); }
}
// McpCeremonyClient is a stub CeremonyClient that throws
// McpNotImplementedError on every call.
export class McpCeremonyClient {
    async initialize(_req) { return notImpl('ceremony.initialize'); }
    async status() { return notImpl('ceremony.status'); }
    async claimCertBegin(_req) { return notImpl('ceremony.claimCertBegin'); }
    async claimCertComplete(_req) { return notImpl('ceremony.claimCertComplete'); }
    async claimShare(_req) { return notImpl('ceremony.claimShare'); }
    async signCSR(_req) { return notImpl('ceremony.signCSR'); }
    async enrollExternalOfficer(_req) { return notImpl('ceremony.enrollExternalOfficer'); }
}
// McpBootstrapClient is a stub BootstrapClient that throws
// McpNotImplementedError on every call.
export class McpBootstrapClient {
    async auto(_req) { return notImpl('bootstrap.auto'); }
    async dane(_req) { return notImpl('bootstrap.dane'); }
    async noise(_req) { return notImpl('bootstrap.noise'); }
    async spki(_req) { return notImpl('bootstrap.spki'); }
}
// McpClient is the stub entry point for the MCP transport.
export class McpClient {
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    projects = null;
    sdlc = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    accounts = null;
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
    barrier;
    // Per-tenant barrier is a REST-only surface; non-REST transports stub it.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    tenantBarrier = null;
    ceremony;
    bootstrap;
    // cluster status/management is exposed over the REST transport; not wired here.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    cluster = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    seal = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    sshKeys = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ssh = null;
    constructor(endpoint, _config = {}) {
        this.connection = new McpConnectionClient(endpoint);
        this.orchestrator = new McpOrchestratorClient();
        this.auth = new McpAuthClient();
        this.barrier = new McpBarrierClient();
        this.ceremony = new McpCeremonyClient();
        this.bootstrap = new McpBootstrapClient();
    }
    async logout(_token) {
        // Non-REST transports don't manage bearer tokens via the REST logout verb.
        // The caller is responsible for clearing local credentials; this is a no-op
        // so the unified AuthClient surface compiles across every transport.
    }
}
//# sourceMappingURL=mcp.js.map