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
import { McpSession } from './mcp_rpc.js';
import { adoptPage, wholeSetPage, } from './page.js';
import { QuicBaaSError, DeployNotSupportedError, TransportNotSupportedError } from './errors.js';
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
    async listJobs(_tenantID, _taskID) { return notImpl('listJobs'); }
    async getJobStatus(_tenantID, _jobID) { return notImpl('getJobStatus'); }
    async startJob(_req) { return notImpl('startJob'); }
    async cancelJob(_tenantID, _jobID) { return notImpl('cancelJob'); }
    async listFleetNodes(_req) { return notImpl('listFleetNodes'); }
    async getFleetNode(_req) { return notImpl('getFleetNode'); }
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
    async getTenantSpendPolicy(_req) { return notImpl('getTenantSpendPolicy'); }
    async setTenantSpendPolicy(_req) { return notImpl('setTenantSpendPolicy'); }
    async getCostSettings(_req) { return notImpl('getCostSettings'); }
    async setCostSettings(_req) { return notImpl('setCostSettings'); }
    async getWorkerImageStatus() { return notImpl('getWorkerImageStatus'); }
    async refreshWorkerImage() { return notImpl('refreshWorkerImage'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *subscribeSession(_req) { return notImpl('subscribeSession'); }
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('mcp'); }
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
    async listAttention(_tenantID, _page) { return notImpl('listAttention'); }
    async resolveAttention(_req) { return notImpl('resolveAttention'); }
    async decomposeTask(_req) { return notImpl('decomposeTask'); }
    async registerFleetNode(_req) { return notImpl('registerFleetNode'); }
    async removeFleetNode(_req) { return notImpl('removeFleetNode'); }
    async disableFleetNode(_req) { return notImpl('disableFleetNode'); }
    async enableFleetNode(_req) { return notImpl('enableFleetNode'); }
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
// McpAccountsClient carries the accounts family over the quicbaas_agent_account_*
// tools (pkg/api/mcp/account_tools.go). Each tool takes the REST request's
// fields flat and answers with the same JSON the REST handler writes, so the
// requests pass through and the responses are the shared accounts types.
// mcpRefusal is the error for a request argument a tool does not read:
// dropping it would answer as if the server had applied it.
function mcpRefusal(family, method, reason) {
    return new TransportNotSupportedError('mcp', family, `${reason}; use the rest transport`, method);
}
function hasEntries(value) {
    return value !== undefined && Object.keys(value).length > 0;
}
export class McpProjectsClient {
    session;
    constructor(session) {
        this.session = session;
    }
    // createProject refuses metadata: project_create does not read it.
    async createProject(req) {
        if (hasEntries(req.metadata)) {
            throw mcpRefusal('projects', 'createProject', 'the project_create tool does not read metadata');
        }
        const resp = await this.session.callTool('project_create', req);
        return { project: resp.Project };
    }
    async getProject(tenantId, projectId) {
        const resp = await this.session.callTool('project_get', { tenant_id: tenantId, project_id: projectId });
        return { project: resp.Project };
    }
    // updateProject refuses metadata and clear_description: project_update
    // reads neither. Any defined metadata is refused, {} included: on an update
    // {} means "clear the metadata", and dropping it would report a success that
    // changed nothing.
    async updateProject(req) {
        if (req.metadata !== undefined) {
            throw mcpRefusal('projects', 'updateProject', 'the project_update tool does not read metadata');
        }
        if (req.clear_description) {
            throw mcpRefusal('projects', 'updateProject', 'the project_update tool does not read clear_description');
        }
        const resp = await this.session.callTool('project_update', req);
        return { project: resp.Project };
    }
    async deleteProject(tenantId, projectId) {
        const resp = await this.session.callTool('project_delete', { tenant_id: tenantId, project_id: projectId });
        return { deleted: resp.Deleted };
    }
    async listProjects(tenantId) {
        const page = await this.listProjectsPage(tenantId);
        return { projects: page.items, total: page.total };
    }
    // listProjectsPage refuses the autonomous filter: project_list takes the
    // paging arguments but no autonomous switch.
    async listProjectsPage(tenantId, page) {
        if (page?.autonomous !== undefined) {
            throw mcpRefusal('projects', 'listProjectsPage', 'the project_list tool has no autonomous filter');
        }
        const raw = await this.session.callTool('project_list', {
            tenant_id: tenantId,
            limit: page?.limit,
            offset: page?.offset,
            sort: page?.sort,
            q: page?.q,
        });
        return adoptPage(raw, 'projects');
    }
    async grantFleetNodeRole(req) {
        const resp = await this.session.callTool('project_grant_fleet_node', req);
        return { project_id: resp.ProjectID, node_id: resp.FleetNodeID, role: resp.Role, timestamp: resp.Timestamp };
    }
    async revokeFleetNodeRole(req) {
        const resp = await this.session.callTool('project_revoke_fleet_node', req);
        return { project_id: resp.ProjectID, node_id: resp.FleetNodeID, timestamp: resp.Timestamp };
    }
}
// McpSdlcClient calls the sdlc_* tools. They answer the same service values
// the REST handlers do, so every result but forges passes through unchanged.
export class McpSdlcClient {
    session;
    constructor(session) {
        this.session = session;
    }
    async forges() {
        const resp = await this.session.callTool('sdlc_forges', {});
        return resp.forges ?? [];
    }
    async bind(tenantId, projectId, binding) {
        return this.session.callTool('sdlc_bind', {
            tenant_id: tenantId,
            project_id: projectId,
            binding: { ...binding, project_id: binding.project_id || projectId },
        });
    }
    async unbind(tenantId, projectId) {
        await this.session.callTool('sdlc_unbind', { tenant_id: tenantId, project_id: projectId });
    }
    async binding(tenantId, projectId) {
        return this.session.callTool('sdlc_binding', { tenant_id: tenantId, project_id: projectId });
    }
    async delivery(tenantId, taskId) {
        return this.session.callTool('sdlc_delivery', { tenant_id: tenantId, task_id: taskId });
    }
    async onboard(tenantId, projectId) {
        return this.session.callTool('sdlc_onboard', { tenant_id: tenantId, project_id: projectId });
    }
    async conformance(tenantId, projectId) {
        return this.session.callTool('sdlc_conformance', { tenant_id: tenantId, project_id: projectId });
    }
    async openWork(tenantId, projectId) {
        return this.session.callTool('sdlc_open_work', { tenant_id: tenantId, project_id: projectId });
    }
    async reviewer(tenantId, projectId, pullRequest) {
        return this.session.callTool('sdlc_reviewer', { tenant_id: tenantId, project_id: projectId, pull_request: pullRequest });
    }
    async setReviewer(tenantId, projectId, pullRequest, profile) {
        return this.session.callTool('sdlc_reviewer_set', {
            tenant_id: tenantId,
            project_id: projectId,
            pull_request: pullRequest,
            profile,
        });
    }
    async clearReviewer(tenantId, projectId, pullRequest) {
        return this.session.callTool('sdlc_reviewer_clear', { tenant_id: tenantId, project_id: projectId, pull_request: pullRequest });
    }
    async fileIssue(tenantId, projectId, req) {
        return this.session.callTool('sdlc_issue_create', { ...req, tenant_id: tenantId, project_id: projectId });
    }
    async collaborators(tenantId, projectId) {
        return this.session.callTool('sdlc_collaborators', { tenant_id: tenantId, project_id: projectId });
    }
    async adoptIssue(tenantId, projectId, request) {
        return this.session.callTool('sdlc_adopt_issue', { ...request, tenant_id: tenantId, project_id: projectId });
    }
}
export class McpAccountsClient {
    session;
    constructor(session) {
        this.session = session;
    }
    async createAccount(req) {
        return this.session.callTool('quicbaas_agent_account_create', req);
    }
    async getAccount(tenantId, accountId) {
        return this.session.callTool('quicbaas_agent_account_get', { tenant_id: tenantId, account_id: accountId });
    }
    async listAccounts(req) {
        return this.session.callTool('quicbaas_agent_account_list', req);
    }
    // listAccountsPage refuses a status filter: the list tool takes provider,
    // project_id and the paging arguments but no status, and answering the
    // unfiltered page as if it were filtered would be a lie.
    async listAccountsPage(req) {
        if (req.status !== undefined && req.status !== '') {
            throw new TransportNotSupportedError('mcp', 'accounts', 'the quicbaas_agent_account_list tool has no status filter; use the rest transport', 'listAccountsPage');
        }
        const raw = await this.session.callTool('quicbaas_agent_account_list', req);
        return adoptPage(raw, 'accounts');
    }
    async updateAccount(req) {
        return this.session.callTool('quicbaas_agent_account_update', req);
    }
    async deleteAccount(tenantId, accountId) {
        return this.session.callTool('quicbaas_agent_account_delete', { tenant_id: tenantId, account_id: accountId });
    }
    async rotateAccountCredential(req) {
        return this.session.callTool('quicbaas_agent_account_rotate', req);
    }
    async revokeAccount(req) {
        return this.session.callTool('quicbaas_agent_account_revoke', req);
    }
    async setDefaultAccount(tenantId, accountId) {
        return this.session.callTool('quicbaas_agent_account_set_default', { tenant_id: tenantId, account_id: accountId });
    }
    async getAccountUsage(tenantId, accountId) {
        return this.session.callTool('quicbaas_agent_account_usage', { tenant_id: tenantId, account_id: accountId });
    }
}
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
    projects;
    sdlc;
    accounts;
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
    constructor(endpoint, config = {}) {
        this.connection = new McpConnectionClient(endpoint);
        const session = new McpSession(endpoint, config);
        this.projects = new McpProjectsClient(session);
        this.sdlc = new McpSdlcClient(session);
        this.accounts = new McpAccountsClient(session);
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