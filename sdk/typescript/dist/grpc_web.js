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
import { QuicBaaSError, DeployNotSupportedError } from './errors.js';
// GrpcWebNotImplementedError is thrown when any gRPC-Web method is called
// before the transport library is wired in.
export class GrpcWebNotImplementedError extends QuicBaaSError {
    constructor(method) {
        super(`grpc-web transport: ${method} not implemented. ` +
            'Install @improbable-eng/grpc-web or grpc-web and wire the generated proto client.');
        this.name = 'GrpcWebNotImplementedError';
    }
}
// notImpl is a helper that produces a rejected Promise with a clear error.
function notImpl(method) {
    throw new GrpcWebNotImplementedError(method);
}
// grpcWebUnsupported is why projects, sdlc and accounts refuse here. The
// server serves ProjectService, SDLCService and AccountService as native gRPC
// (pkg/server/lifecycle.go), but nothing serves the gRPC-Web framing a browser
// needs, and this SDK carries no gRPC-Web client or generated stubs.
const grpcWebUnsupported = 'the server serves these services as native gRPC only (no gRPC-Web endpoint) ' +
    'and the SDK has no gRPC-Web client; use the rest transport';
class GrpcWebConnectionClient {
    _transport = 'grpc-web';
    _address;
    constructor(address) {
        this._address = address;
    }
    async connect(_address) { notImpl('connect'); }
    async disconnect() { notImpl('disconnect'); }
    isConnected() { return false; }
    getAddress() { return this._address; }
    getTransport() { return this._transport; }
}
class GrpcWebOrchestratorClient {
    // All methods delegate to notImpl until the gRPC-Web library is wired in.
    // =========================================================================
    // The paged listings
    //
    // gRPC CARRIES NO PAGING FIELDS. Its request messages are generated
    // from api/proto/orchestrator.proto, which has none, so there is no page to
    // ask for until the proto grows them (go-quicbaas#968 follow-up).
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
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('grpc-web'); }
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
class GrpcWebAuthClient {
    async authValidate(_token) { return notImpl('authValidate'); }
    async authLogin(_credentials) { return notImpl('authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return notImpl('webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return notImpl('webAuthnFinishLogin'); }
    async loginPIV() { return notImpl('loginPIV'); }
    async logout(_token) { return notImpl('logout'); }
    async registerBegin(_req) { return notImpl('registerBegin'); }
    async registerFinish(_req) { return notImpl('registerFinish'); }
}
// GrpcWebBarrierClient is a stub BarrierClient that throws
// GrpcWebNotImplementedError on every call. The barrier surface lives on
// REST today; future gRPC-Web wiring can replace this stub.
export class GrpcWebBarrierClient {
    async status() { return notImpl('barrier.status'); }
    async initialize(_req) { return notImpl('barrier.initialize'); }
    async unseal(_req) { return notImpl('barrier.unseal'); }
    async seal(_req) { return notImpl('barrier.seal'); }
    async initializeShamir(_req) { return notImpl('barrier.initializeShamir'); }
    async unsealWithShare(_req) { return notImpl('barrier.unsealWithShare'); }
    async unsealWithShares(_req) { return notImpl('barrier.unsealWithShares'); }
    async rekey(_req) { return notImpl('barrier.rekey'); }
    async generateRecoveryKeys(_req) { return notImpl('barrier.generateRecoveryKeys'); }
    async recoverWithKeys(_req) { return notImpl('barrier.recoverWithKeys'); }
    async deleteRecoveryKeys() { return notImpl('barrier.deleteRecoveryKeys'); }
    async recoveryStatus() { return notImpl('barrier.recoveryStatus'); }
    async generateRootToken(_req) { return notImpl('barrier.generateRootToken'); }
    async enableEncryption(_req) { return notImpl('barrier.enableEncryption'); }
}
// GrpcWebCeremonyClient is a stub CeremonyClient that throws
// GrpcWebNotImplementedError on every call.
export class GrpcWebCeremonyClient {
    async initialize(_req) { return notImpl('ceremony.initialize'); }
    async status() { return notImpl('ceremony.status'); }
    async claimCertBegin(_req) { return notImpl('ceremony.claimCertBegin'); }
    async claimCertComplete(_req) { return notImpl('ceremony.claimCertComplete'); }
    async claimShareBegin(_req) { return notImpl('ceremony.claimShareBegin'); }
    async claimShareComplete(_req) { return notImpl('ceremony.claimShareComplete'); }
    async signCSR(_req) { return notImpl('ceremony.signCSR'); }
    async enrollExternalOfficer(_req) { return notImpl('ceremony.enrollExternalOfficer'); }
}
// GrpcWebClient is the stub entry point for the gRPC-Web transport.
// Replace the stub implementations above with real gRPC-Web calls once the
// proto client library is installed.
export class GrpcWebClient {
    connection;
    orchestrator;
    auth;
    // Data sub-clients are not implemented for gRPC-Web — all methods throw
    // GrpcWebNotImplementedError until the transport is wired in.
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
    projects = unsupportedClient('grpc-web', 'projects', grpcWebUnsupported);
    sdlc = unsupportedClient('grpc-web', 'sdlc', grpcWebUnsupported);
    accounts = unsupportedClient('grpc-web', 'accounts', grpcWebUnsupported);
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
    constructor(baseUrl, _config = {}) {
        this.connection = new GrpcWebConnectionClient(baseUrl);
        this.orchestrator = new GrpcWebOrchestratorClient();
        this.auth = new GrpcWebAuthClient();
        this.barrier = new GrpcWebBarrierClient();
        this.ceremony = new GrpcWebCeremonyClient();
    }
    async logout(_token) {
        // Non-REST transports don't manage bearer tokens via the REST logout verb.
        // The caller is responsible for clearing local credentials; this is a no-op
        // so the unified AuthClient surface compiles across every transport.
    }
}
//# sourceMappingURL=grpc_web.js.map