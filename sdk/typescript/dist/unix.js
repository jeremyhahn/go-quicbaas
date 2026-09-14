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
import { QuicBaaSError, DeployNotSupportedError } from './errors.js';
// UnixNotImplementedError is thrown by all methods until the Unix socket
// adapter is wired into this file.
export class UnixNotImplementedError extends QuicBaaSError {
    constructor(method) {
        super(`unix transport: ${method} not implemented. ` +
            'Add a Node.js net.Socket adapter that speaks the go-quicbaas framing protocol.');
        this.name = 'UnixNotImplementedError';
    }
}
function notImpl(method) {
    throw new UnixNotImplementedError(method);
}
class UnixConnectionClient {
    _socketPath;
    _transport = 'unix';
    constructor(socketPath) {
        this._socketPath = socketPath;
    }
    async connect(_address) { notImpl('connect'); }
    async disconnect() { notImpl('disconnect'); }
    isConnected() { return false; }
    getAddress() { return this._socketPath; }
    getTransport() { return this._transport; }
}
class UnixOrchestratorClient {
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
    async deployAgent(_spec) { throw new DeployNotSupportedError('unix'); }
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
class UnixAuthClient {
    async authValidate(_token) { return notImpl('authValidate'); }
    async authLogin(_credentials) { return notImpl('authLogin'); }
    async webAuthnBeginLogin(_tenantID, _email) { return notImpl('webAuthnBeginLogin'); }
    async webAuthnFinishLogin(_challenge, _credential) { return notImpl('webAuthnFinishLogin'); }
    async loginPIV() { return notImpl('loginPIV'); }
    async logout(_token) { return notImpl('logout'); }
    async registerBegin(_req) { return notImpl('registerBegin'); }
    async registerFinish(_req) { return notImpl('registerFinish'); }
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
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    barrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    tenantBarrier = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    ceremony = null;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    bootstrap = null;
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