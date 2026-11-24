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
import { DeployNotSupportedError, TransportNotSupportedError } from './errors.js';
// grpcWebNotImplemented is why every gRPC-Web verb refuses. The server's
// gRPC-Web endpoint comes from go-qrdb, whose pinned release has no sdk/go
// option to turn it on, and the client docs/SPEC.md (multiprotocol-api) names,
// @connectrpc/connect-web, is not wired into this SDK.
const grpcWebNotImplemented = 'go-qrdb serves no gRPC-Web endpoint yet: the pinned go-qrdb release has no sdk/go option ' +
    'that turns it on, and this SDK has no @connectrpc/connect-web client yet; use the rest transport';
// GrpcWebNotImplementedError is the gRPC-Web transport's refusal of one verb.
// It is a TransportNotSupportedError naming `family.verb`, like
// QuicNotImplementedError, so a caller that catches the base class handles it.
export class GrpcWebNotImplementedError extends TransportNotSupportedError {
    constructor(family, method, reason = grpcWebNotImplemented) {
        super('grpc-web', family, reason, method);
        this.name = 'GrpcWebNotImplementedError';
    }
}
// notImpl throws the gRPC-Web refusal for family.method.
function notImpl(family, method) {
    throw new GrpcWebNotImplementedError(family, method);
}
// grpcWebNotImplementedClient returns a client of type T whose every verb
// rejects with GrpcWebNotImplementedError naming `family.verb`. It is the
// gRPC-Web refusal docs/SPEC.md (multiprotocol-api) requires until the
// go-qrdb option lands. `then` and symbol keys read as undefined, so
// awaiting or inspecting the client does not call a verb.
function grpcWebNotImplementedClient(family) {
    return new Proxy({}, {
        get(_target, prop) {
            if (typeof prop !== 'string' || prop === 'then')
                return undefined;
            return async () => {
                throw new GrpcWebNotImplementedError(family, prop);
            };
        },
    });
}
class GrpcWebConnectionClient {
    _transport = 'grpc-web';
    _address;
    constructor(address) {
        this._address = address;
    }
    async connect(_address) { notImpl('connection', 'connect'); }
    async disconnect() { notImpl('connection', 'disconnect'); }
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
    async listJobs(_tenantID, _taskID) { return notImpl('orchestrator', 'listJobs'); }
    async getJobStatus(_tenantID, _jobID) { return notImpl('orchestrator', 'getJobStatus'); }
    async startJob(_req) { return notImpl('orchestrator', 'startJob'); }
    async cancelJob(_tenantID, _jobID) { return notImpl('orchestrator', 'cancelJob'); }
    async listFleetNodes(_req) { return notImpl('orchestrator', 'listFleetNodes'); }
    async getFleetNode(_req) { return notImpl('orchestrator', 'getFleetNode'); }
    async listSessions(_req) { return notImpl('orchestrator', 'listSessions'); }
    async listTasks(_tenantID) { return notImpl('orchestrator', 'listTasks'); }
    async getTask(_tenantID, _taskID) { return notImpl('orchestrator', 'getTask'); }
    async listBackends() { return notImpl('orchestrator', 'listBackends'); }
    async listRunners() { return notImpl('orchestrator', 'listRunners'); }
    async listBackendModels(_agentID, _backendName) { return notImpl('orchestrator', 'listBackendModels'); }
    async healthCheck() { return notImpl('orchestrator', 'healthCheck'); }
    subscribeJobEvents(_tenantID, _jobID, _onEvent) { return notImpl('orchestrator', 'subscribeJobEvents'); }
    async spawnSession(_req) { return notImpl('orchestrator', 'spawnSession'); }
    async promptSession(_req) { return notImpl('orchestrator', 'promptSession'); }
    async closeSession(_req) { return notImpl('orchestrator', 'closeSession'); }
    async rotateSessionAccount(_req) { return notImpl('orchestrator', 'rotateSessionAccount'); }
    async getSessionHistory(_req) { return notImpl('orchestrator', 'getSessionHistory'); }
    async getSessionConversation(_req) { return notImpl('orchestrator', 'getSessionConversation'); }
    async getSpendReport(_req) { return notImpl('orchestrator', 'getSpendReport'); }
    async getTenantSpendPolicy(_req) { return notImpl('orchestrator', 'getTenantSpendPolicy'); }
    async setTenantSpendPolicy(_req) { return notImpl('orchestrator', 'setTenantSpendPolicy'); }
    async getCostSettings(_req) { return notImpl('orchestrator', 'getCostSettings'); }
    async setCostSettings(_req) { return notImpl('orchestrator', 'setCostSettings'); }
    async getWorkerImageStatus() { return notImpl('orchestrator', 'getWorkerImageStatus'); }
    async refreshWorkerImage() { return notImpl('orchestrator', 'refreshWorkerImage'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *subscribeSession(_req) { return notImpl('orchestrator', 'subscribeSession'); }
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('grpc-web'); }
    async inference(_req) { return notImpl('orchestrator', 'inference'); }
    // eslint-disable-next-line @typescript-eslint/require-await
    async *inferenceStream(_req, _signal) { return notImpl('orchestrator', 'inferenceStream'); }
    async createWorkflow(_req) { return notImpl('orchestrator', 'createWorkflow'); }
    async listWorkflows(_tenantID) { return notImpl('orchestrator', 'listWorkflows'); }
    async getWorkflow(_tenantID, _name) { return notImpl('orchestrator', 'getWorkflow'); }
    async updateWorkflow(_name, _def) { return notImpl('orchestrator', 'updateWorkflow'); }
    async deleteWorkflow(_tenantID, _name) { return notImpl('orchestrator', 'deleteWorkflow'); }
    async createTask(_req) { return notImpl('orchestrator', 'createTask'); }
    async claimTask(_req) { return notImpl('orchestrator', 'claimTask'); }
    async completeTask(_req) { return notImpl('orchestrator', 'completeTask'); }
    async readmitTask(_tenantID, _taskID) { return notImpl('orchestrator', 'readmitTask'); }
    async deleteTask(_req) { return notImpl('orchestrator', 'deleteTask'); }
    async listAttention(_tenantID, _page) { return notImpl('orchestrator', 'listAttention'); }
    async resolveAttention(_req) { return notImpl('orchestrator', 'resolveAttention'); }
    async decomposeTask(_req) { return notImpl('orchestrator', 'decomposeTask'); }
    async registerFleetNode(_req) { return notImpl('orchestrator', 'registerFleetNode'); }
    async removeFleetNode(_req) { return notImpl('orchestrator', 'removeFleetNode'); }
    async disableFleetNode(_req) { return notImpl('orchestrator', 'disableFleetNode'); }
    async enableFleetNode(_req) { return notImpl('orchestrator', 'enableFleetNode'); }
}
class GrpcWebAuthClient {
    async authValidate(_token) { return notImpl('auth', 'authValidate'); }
    async authLogin(_credentials) { return notImpl('auth', 'authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return notImpl('auth', 'webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return notImpl('auth', 'webAuthnFinishLogin'); }
    async loginPIV() { return notImpl('auth', 'loginPIV'); }
    async logout(_token) { return notImpl('auth', 'logout'); }
    async registerBegin(_req) { return notImpl('auth', 'registerBegin'); }
    async registerFinish(_req) { return notImpl('auth', 'registerFinish'); }
}
// GrpcWebBarrierClient is a stub BarrierClient that throws
// GrpcWebNotImplementedError on every call. The barrier surface lives on
// REST today; future gRPC-Web wiring can replace this stub.
export class GrpcWebBarrierClient {
    async status() { return notImpl('barrier', 'status'); }
    async initialize(_req) { return notImpl('barrier', 'initialize'); }
    async unseal(_req) { return notImpl('barrier', 'unseal'); }
    async seal(_req) { return notImpl('barrier', 'seal'); }
    async initializeShamir(_req) { return notImpl('barrier', 'initializeShamir'); }
    async unsealWithShare(_req) { return notImpl('barrier', 'unsealWithShare'); }
    async unsealWithShares(_req) { return notImpl('barrier', 'unsealWithShares'); }
    async rekey(_req) { return notImpl('barrier', 'rekey'); }
    async generateRecoveryKeys(_req) { return notImpl('barrier', 'generateRecoveryKeys'); }
    async recoverWithKeys(_req) { return notImpl('barrier', 'recoverWithKeys'); }
    async deleteRecoveryKeys() { return notImpl('barrier', 'deleteRecoveryKeys'); }
    async recoveryStatus() { return notImpl('barrier', 'recoveryStatus'); }
    async generateRootToken(_req) { return notImpl('barrier', 'generateRootToken'); }
    async enableEncryption(_req) { return notImpl('barrier', 'enableEncryption'); }
}
// GrpcWebCeremonyClient is a stub CeremonyClient that throws
// GrpcWebNotImplementedError on every call.
export class GrpcWebCeremonyClient {
    async initialize(_req) { return notImpl('ceremony', 'initialize'); }
    async status() { return notImpl('ceremony', 'status'); }
    async claimCertBegin(_req) { return notImpl('ceremony', 'claimCertBegin'); }
    async claimCertComplete(_req) { return notImpl('ceremony', 'claimCertComplete'); }
    async claimShareBegin(_req) { return notImpl('ceremony', 'claimShareBegin'); }
    async claimShareComplete(_req) { return notImpl('ceremony', 'claimShareComplete'); }
    async signCSR(_req) { return notImpl('ceremony', 'signCSR'); }
    async enrollExternalOfficer(_req) { return notImpl('ceremony', 'enrollExternalOfficer'); }
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
    projects = grpcWebNotImplementedClient('projects');
    sdlc = grpcWebNotImplementedClient('sdlc');
    accounts = grpcWebNotImplementedClient('accounts');
    prompts = grpcWebNotImplementedClient('prompts');
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