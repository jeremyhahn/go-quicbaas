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
// QuicNotImplementedError is the refusal every QUIC verb rejects with. It is a
// TransportNotSupportedError, so it names the transport, family, method and
// reason, and a refusal never reaches the network.
export class QuicNotImplementedError extends TransportNotSupportedError {
    constructor(family, method, reason) {
        super('quic', family, reason, method);
        this.name = 'QuicNotImplementedError';
    }
}
// quicUnsupported is why most families refuse here. The server's QUIC listener
// (pkg/server/transport_factory.go) speaks qrdb's frame protocol, not REST over
// HTTP/3, and Node's fetch does not negotiate HTTP/3 anyway: a REST client
// aimed at the QUIC endpoint would either fail against the real listener or
// quietly fall back to TCP HTTP while claiming QUIC. Implementing the frame
// protocol in TypeScript waits on an open owner decision on whether and how a
// QUIC client listener is offered to SDK callers.
const quicUnsupported = "the server's QUIC listener speaks qrdb's frame protocol, which this SDK " +
    'does not implement; use the rest transport';
// quicNoOrchestratorRoute is why the orchestrator family refuses: the server's
// QUIC listener serves every unary orchestration, cost and worker-image method
// (go-quicbaas#819), but only over qrdb's frame protocol, which this SDK does
// not implement yet.
const quicNoOrchestratorRoute = "the server's QUIC listener serves the orchestrator's unary methods over " +
    "qrdb's frame protocol, which this SDK does not implement yet; use the rest transport";
function frameOnly(family, method) {
    throw new QuicNotImplementedError(family, method, quicUnsupported);
}
function noQuicRoute(method) {
    throw new QuicNotImplementedError('orchestrator', method, quicNoOrchestratorRoute);
}
class QuicConnectionClient {
    _endpoint;
    _transport = 'quic';
    constructor(endpoint) {
        this._endpoint = endpoint;
    }
    async connect(_address) { frameOnly('connection', 'connect'); }
    async disconnect() { frameOnly('connection', 'disconnect'); }
    isConnected() { return false; }
    getAddress() { return this._endpoint; }
    getTransport() { return this._transport; }
}
class QuicOrchestratorClient {
    // =========================================================================
    // The paged listings
    //
    // This transport is not wired to a QUIC/WebTransport client yet, so
    // every listing here refuses through its unpaged sibling rather than
    // answering a page it never fetched.
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
    async listJobs(_tenantID, _taskID) { return noQuicRoute('listJobs'); }
    async getJobStatus(_tenantID, _jobID) { return noQuicRoute('getJobStatus'); }
    async startJob(_req) { return noQuicRoute('startJob'); }
    async cancelJob(_tenantID, _jobID) { return noQuicRoute('cancelJob'); }
    async listFleetNodes(_req) { return noQuicRoute('listFleetNodes'); }
    async getFleetNode(_req) { return noQuicRoute('getFleetNode'); }
    async listSessions(_req) { return noQuicRoute('listSessions'); }
    async listTasks(_tenantID) { return noQuicRoute('listTasks'); }
    async getTask(_tenantID, _taskID) { return noQuicRoute('getTask'); }
    async listBackends() { return noQuicRoute('listBackends'); }
    async listRunners() { return noQuicRoute('listRunners'); }
    async listBackendModels(_agentID, _backendName) { return noQuicRoute('listBackendModels'); }
    async healthCheck() { return noQuicRoute('healthCheck'); }
    subscribeJobEvents(_tenantID, _jobID, _onEvent) { return noQuicRoute('subscribeJobEvents'); }
    async spawnSession(_req) { return noQuicRoute('spawnSession'); }
    async promptSession(_req) { return noQuicRoute('promptSession'); }
    async closeSession(_req) { return noQuicRoute('closeSession'); }
    async rotateSessionAccount(_req) { return noQuicRoute('rotateSessionAccount'); }
    async getSessionHistory(_req) { return noQuicRoute('getSessionHistory'); }
    async getSessionConversation(_req) { return noQuicRoute('getSessionConversation'); }
    async getSpendReport(_req) { return noQuicRoute('getSpendReport'); }
    async getTenantSpendPolicy(_req) { return noQuicRoute('getTenantSpendPolicy'); }
    async setTenantSpendPolicy(_req) { return noQuicRoute('setTenantSpendPolicy'); }
    async getCostSettings(_req) { return noQuicRoute('getCostSettings'); }
    async setCostSettings(_req) { return noQuicRoute('setCostSettings'); }
    async getWorkerImageStatus() { return noQuicRoute('getWorkerImageStatus'); }
    async refreshWorkerImage() { return noQuicRoute('refreshWorkerImage'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *subscribeSession(_req) { return noQuicRoute('subscribeSession'); }
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('quic'); }
    async inference(_req) { return noQuicRoute('inference'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *inferenceStream(_req, _signal) { return noQuicRoute('inferenceStream'); }
    async createWorkflow(_req) { return noQuicRoute('createWorkflow'); }
    async listWorkflows(_tenantID) { return noQuicRoute('listWorkflows'); }
    async getWorkflow(_tenantID, _name) { return noQuicRoute('getWorkflow'); }
    async updateWorkflow(_name, _def) { return noQuicRoute('updateWorkflow'); }
    async deleteWorkflow(_tenantID, _name) { return noQuicRoute('deleteWorkflow'); }
    async createTask(_req) { return noQuicRoute('createTask'); }
    async claimTask(_req) { return noQuicRoute('claimTask'); }
    async completeTask(_req) { return noQuicRoute('completeTask'); }
    async readmitTask(_tenantID, _taskID) { return noQuicRoute('readmitTask'); }
    async deleteTask(_req) { return noQuicRoute('deleteTask'); }
    async listAttention(_tenantID, _page) { return noQuicRoute('listAttention'); }
    async resolveAttention(_req) { return noQuicRoute('resolveAttention'); }
    async decomposeTask(_req) { return noQuicRoute('decomposeTask'); }
    async registerFleetNode(_req) { return noQuicRoute('registerFleetNode'); }
    async removeFleetNode(_req) { return noQuicRoute('removeFleetNode'); }
    async disableFleetNode(_req) { return noQuicRoute('disableFleetNode'); }
    async enableFleetNode(_req) { return noQuicRoute('enableFleetNode'); }
}
class QuicAuthClient {
    async authValidate(_token) { return frameOnly('auth', 'authValidate'); }
    async authLogin(_credentials) { return frameOnly('auth', 'authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return frameOnly('auth', 'webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return frameOnly('auth', 'webAuthnFinishLogin'); }
    async loginPIV() { return frameOnly('auth', 'loginPIV'); }
    async logout(_token) { return frameOnly('auth', 'logout'); }
    async registerBegin(_req) { return frameOnly('auth', 'registerBegin'); }
    async registerFinish(_req) { return frameOnly('auth', 'registerFinish'); }
}
// QuicBarrierClient refuses every verb: go-qrdb serves barrier.* on the QUIC
// listener's frame protocol, which this SDK does not speak.
export class QuicBarrierClient {
    async status() { return frameOnly('barrier', 'status'); }
    async initialize(_req) { return frameOnly('barrier', 'initialize'); }
    async unseal(_req) { return frameOnly('barrier', 'unseal'); }
    async seal(_req) { return frameOnly('barrier', 'seal'); }
    async initializeShamir(_req) { return frameOnly('barrier', 'initializeShamir'); }
    async unsealWithShare(_req) { return frameOnly('barrier', 'unsealWithShare'); }
    async unsealWithShares(_req) { return frameOnly('barrier', 'unsealWithShares'); }
    async rekey(_req) { return frameOnly('barrier', 'rekey'); }
    async generateRecoveryKeys(_req) { return frameOnly('barrier', 'generateRecoveryKeys'); }
    async recoverWithKeys(_req) { return frameOnly('barrier', 'recoverWithKeys'); }
    async deleteRecoveryKeys() { return frameOnly('barrier', 'deleteRecoveryKeys'); }
    async recoveryStatus() { return frameOnly('barrier', 'recoveryStatus'); }
    async generateRootToken(_req) { return frameOnly('barrier', 'generateRootToken'); }
    async enableEncryption(_req) { return frameOnly('barrier', 'enableEncryption'); }
}
// QuicCeremonyClient refuses every verb: go-qrdb serves ceremony.* on the QUIC
// listener's frame protocol, which this SDK does not speak.
export class QuicCeremonyClient {
    async initialize(_req) { return frameOnly('ceremony', 'initialize'); }
    async status() { return frameOnly('ceremony', 'status'); }
    async claimCertBegin(_req) { return frameOnly('ceremony', 'claimCertBegin'); }
    async claimCertComplete(_req) { return frameOnly('ceremony', 'claimCertComplete'); }
    async claimShareBegin(_req) { return frameOnly('ceremony', 'claimShareBegin'); }
    async claimShareComplete(_req) { return frameOnly('ceremony', 'claimShareComplete'); }
    async signCSR(_req) { return frameOnly('ceremony', 'signCSR'); }
    async enrollExternalOfficer(_req) { return frameOnly('ceremony', 'enrollExternalOfficer'); }
}
// QuicClient is the entry point for the QUIC transport. It sends nothing: every
// verb refuses with a typed reason (see quicUnsupported).
export class QuicClient {
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
    projects = unsupportedClient('quic', 'projects', quicUnsupported);
    sdlc = unsupportedClient('quic', 'sdlc', quicUnsupported);
    accounts = unsupportedClient('quic', 'accounts', quicUnsupported);
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
        this.connection = new QuicConnectionClient(endpoint);
        this.orchestrator = new QuicOrchestratorClient();
        this.auth = new QuicAuthClient();
        this.barrier = new QuicBarrierClient();
        this.ceremony = new QuicCeremonyClient();
    }
    async logout(_token) {
        // Non-REST transports don't manage bearer tokens via the REST logout verb.
        // The caller is responsible for clearing local credentials; this is a no-op
        // so the unified AuthClient surface compiles across every transport.
    }
}
//# sourceMappingURL=quic.js.map