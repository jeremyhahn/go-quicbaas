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
import { QuicBaaSError, DeployNotSupportedError, McpError, TransportNotSupportedError } from './errors.js';
import { requireTLSForSOPIN } from './so_pin.js';
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
function sessionFromMcpWire(wire) {
    const session = {
        session_id: wire.session_id,
        backend: wire.backend ?? '',
        status: wire.state ?? '',
        created_at: wire.created_at ?? 0,
        last_active: wire.last_activity ?? 0,
    };
    if (wire.model !== undefined)
        session.model = wire.model;
    if (wire.container_id !== undefined)
        session.container_id = wire.container_id;
    if (wire.job_id !== undefined)
        session.job_id = wire.job_id;
    if (wire.runner !== undefined)
        session.runner = wire.runner;
    if (wire.agent_id !== undefined)
        session.agent_id = wire.agent_id;
    return session;
}
// addPageArgs copies the shared page parameters a caller set into a tool's
// arguments, the MCP counterpart of appendPageParams.
function addPageArgs(args, page) {
    if (page?.limit !== undefined)
        args.limit = page.limit;
    if (page?.offset !== undefined)
        args.offset = page.offset;
    if (page?.sort !== undefined)
        args.sort = page.sort;
    if (page?.q !== undefined)
        args.q = page.q;
}
// asksForOnePage reports whether a caller of an unpaged verb named a page; one
// that did gets that page, one that did not gets the whole set.
function asksForOnePage(page) {
    return page.limit !== undefined || page.offset !== undefined;
}
// atOffset is a listing request moved to the page starting at `offset`; the
// first page sends no offset, as a caller that never paged would.
function atOffset(page, offset) {
    return offset === 0 ? page : { ...page, offset };
}
// walkPages fetches the page at each `next_offset` until a page names none,
// returning every record. A page that does not advance the offset, or that is
// empty, ends the walk rather than looping on a server that miscounts.
async function walkPages(fetchPage) {
    const items = [];
    let offset = 0;
    for (;;) {
        const page = await fetchPage(offset);
        items.push(...page.items);
        const next = page.next_offset;
        if (next === undefined || next <= offset || page.items.length === 0)
            return items;
        offset = next;
    }
}
// unixNanos converts the RFC 3339 instant a tool answers into the Unix
// nanoseconds the REST response carries; precision below a millisecond is
// lost. An instant that does not parse is 0.
function unixNanos(rfc3339) {
    const ms = Date.parse(rfc3339);
    return Number.isFinite(ms) ? ms * 1e6 : 0;
}
class McpOrchestratorClient {
    session;
    constructor(session) {
        this.session = session;
    }
    // =========================================================================
    // The paged listings
    //
    // Each forwards its filters and the shared page parameters to the tool and
    // keeps the server's page envelope (`total`, `limit`, `offset`,
    // `next_offset`), exactly as the REST transport's page methods do, so a
    // caller sees the same page whichever transport it rides.
    // =========================================================================
    async listJobsPage(tenantID, page) {
        const args = { tenant_id: tenantID };
        if (page?.task_id !== undefined)
            args.task_id = page.task_id;
        if (page?.runner !== undefined)
            args.runner = page.runner;
        if (page?.project_id !== undefined)
            args.project_id = page.project_id;
        if (page?.agent_id !== undefined)
            args.agent_id = page.agent_id;
        if (page?.statuses !== undefined)
            args.statuses = page.statuses;
        if (page?.job_ids !== undefined)
            args.job_ids = page.job_ids;
        addPageArgs(args, page);
        const raw = await this.session.callTool('quicbaas_orchestrator_list_jobs', args);
        return adoptPage(raw, 'jobs');
    }
    async listTasksPage(tenantID, _page) {
        return wholeSetPage(await this.listTasks(tenantID));
    }
    async listSessionsPage(tenantID, page) {
        return this.sessionsPage({ ...page, tenantID });
    }
    async listFleetNodesPage(tenantID, page) {
        return this.fleetNodesPage({ ...page, tenant_id: tenantID });
    }
    async listWorkflowsPage(tenantID, page) {
        const args = { tenant_id: tenantID };
        if (page?.project_id !== undefined)
            args.project_id = page.project_id;
        if (page?.mode !== undefined)
            args.mode = page.mode;
        addPageArgs(args, page);
        const raw = await this.session.callTool('quicbaas_orchestrator_list_workflows', args);
        return adoptPage(raw, 'workflows');
    }
    // =========================================================================
    // Jobs
    // =========================================================================
    async listJobs(tenantID, taskID) {
        const filter = taskID === undefined ? {} : { task_id: taskID };
        return walkPages((offset) => this.listJobsPage(tenantID, atOffset(filter, offset)));
    }
    async getJobStatus(tenantID, jobID) {
        return this.session.callTool('quicbaas_orchestrator_get_job_status', {
            tenant_id: tenantID,
            job_id: jobID,
        });
    }
    // startJob runs under the caller's own tenant: the start_job tool takes no
    // tenant_id, so req.tenant_id is not sent.
    async startJob(req) {
        if (req.workflow_name) {
            throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no workflow_name');
        }
        if (req.workflow_file) {
            throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no workflow_file');
        }
        if (req.poll_interval) {
            throw mcpRefusal('orchestrator', 'startJob', 'the start_job tool takes no poll_interval');
        }
        const args = {
            description: req.description,
            project_id: req.project_id,
            max_iterations: req.max_iterations,
        };
        if (req.lead !== undefined)
            args.lead = req.lead;
        if (req.runner !== undefined)
            args.runner = req.runner;
        if (req.prompt_append !== undefined)
            args.prompt_append = req.prompt_append;
        const resp = await this.session.callTool('quicbaas_orchestrator_start_job', args);
        return { job_id: resp.job_id, timestamp: unixNanos(resp.timestamp) };
    }
    async cancelJob(tenantID, jobID) {
        await this.session.callTool('quicbaas_orchestrator_cancel_job', {
            tenant_id: tenantID,
            job_id: jobID,
        });
    }
    subscribeJobEvents(_tenantID, _jobID, _onEvent) {
        throw mcpRefusal('orchestrator', 'subscribeJobEvents', 'the server has no streaming job-event tool');
    }
    // =========================================================================
    // Fleet
    // =========================================================================
    async listFleetNodes(req) {
        const nodes = asksForOnePage(req)
            ? (await this.fleetNodesPage(req)).items
            : await walkPages((offset) => this.fleetNodesPage(atOffset(req, offset)));
        return { nodes, count: nodes.length };
    }
    async fleetNodesPage(req) {
        if (req.model) {
            throw mcpRefusal('orchestrator', 'listFleetNodes', 'the fleet_list tool has no model filter');
        }
        const args = { tenant_id: req.tenant_id };
        if (req.host !== undefined)
            args.host = req.host;
        if (req.status !== undefined)
            args.status = req.status;
        if (req.node_type !== undefined)
            args.node_type = req.node_type;
        if (req.backend !== undefined)
            args.backend = req.backend;
        addPageArgs(args, req);
        const raw = await this.session.callTool('quicbaas_fleet_list', args);
        return adoptPage(raw, 'nodes');
    }
    async getFleetNode(req) {
        const resp = await this.session.callTool('quicbaas_fleet_status', {
            tenant_id: req.tenant_id,
            node_id: req.node_id,
        });
        return resp.node;
    }
    async registerFleetNode(req) {
        if (req.model) {
            throw mcpRefusal('orchestrator', 'registerFleetNode', 'the fleet_register tool takes no model');
        }
        if (hasEntries(req.metadata)) {
            throw mcpRefusal('orchestrator', 'registerFleetNode', 'the fleet_register tool takes no metadata');
        }
        const resp = await this.session.callTool('quicbaas_fleet_register', {
            tenant_id: req.tenant_id,
            name: req.name,
            host: req.host,
            backend: req.backend,
            node_type: req.node_type,
        });
        return { node: resp.node };
    }
    async removeFleetNode(req) {
        await this.session.callTool('quicbaas_fleet_remove', {
            tenant_id: req.tenant_id,
            node_id: req.node_id,
        });
    }
    async disableFleetNode(req) {
        return this.session.callTool('quicbaas_fleet_disable', {
            tenant_id: req.tenant_id,
            node_id: req.node_id,
        });
    }
    async enableFleetNode(req) {
        return this.session.callTool('quicbaas_fleet_enable', {
            tenant_id: req.tenant_id,
            node_id: req.node_id,
        });
    }
    async deployFleetNode(_spec) { throw new DeployNotSupportedError('mcp'); }
    // =========================================================================
    // Backends, runners and health
    // =========================================================================
    async listBackends() {
        return this.allPages('quicbaas_orchestrator_list_backends', 'backends');
    }
    async listRunners() {
        return this.allPages('quicbaas_orchestrator_list_runners', 'runners');
    }
    async listBackendModels(_agentID, _backendName) {
        throw mcpRefusal('orchestrator', 'listBackendModels', 'the server has no tool listing a fleet node backend\'s models');
    }
    async healthCheck() {
        const resp = await this.session.callTool('quicbaas_orchestrator_health', {});
        return { ...resp, timestamp: unixNanos(resp.timestamp) };
    }
    // =========================================================================
    // Sessions
    // =========================================================================
    async listSessions(req) {
        const sessions = asksForOnePage(req)
            ? (await this.sessionsPage(req)).items
            : await walkPages((offset) => this.sessionsPage(atOffset(req, offset)));
        return { sessions, count: sessions.length };
    }
    async sessionsPage(req) {
        const args = { tenant_id: req.tenantID };
        if (req.backend !== undefined)
            args.backend = req.backend;
        if (req.status !== undefined)
            args.status = req.status;
        if (req.project_id !== undefined)
            args.project_id = req.project_id;
        if (req.job_id !== undefined)
            args.job_id = req.job_id;
        if (req.runner !== undefined)
            args.runner = req.runner;
        if (req.agent_id !== undefined)
            args.agent_id = req.agent_id;
        addPageArgs(args, req);
        const raw = await this.session.callTool('quicbaas_orchestrator_list_sessions', args);
        return adoptPage(raw, 'sessions', sessionFromMcpWire);
    }
    async spawnSession(req) {
        if (hasEntries(req.env)) {
            throw mcpRefusal('orchestrator', 'spawnSession', 'the spawn tool takes no env');
        }
        if (req.sandboxed) {
            throw mcpRefusal('orchestrator', 'spawnSession', 'the spawn tool takes sandbox_mode, not the deprecated sandboxed');
        }
        const { tenantID, env: _env, sandboxed: _sandboxed, ...fields } = req;
        return this.session.callTool('quicbaas_orchestrator_spawn', {
            ...fields,
            tenant_id: tenantID,
        });
    }
    // promptSession answers the server's asynchronous turn acknowledgement, as
    // the REST route's 202 does; the turn's output arrives on the session's
    // event history.
    async promptSession(req) {
        return this.session.callTool('quicbaas_orchestrator_prompt', {
            tenant_id: req.tenant_id ?? '',
            session_id: req.session_id,
            prompt: req.prompt,
        });
    }
    async closeSession(req) {
        const resp = await this.session.callTool('quicbaas_orchestrator_close', {
            tenant_id: req.tenant_id ?? '',
            session_id: req.session_id,
        });
        return { session_id: resp.session_id, closed: true };
    }
    async rotateSessionAccount(req) {
        const args = {
            tenant_id: req.tenant_id ?? '',
            session_id: req.session_id,
        };
        if (req.reason !== undefined)
            args.reason = req.reason;
        if (req.hold_outgoing !== undefined)
            args.hold_outgoing = req.hold_outgoing;
        if (req.available_at !== undefined)
            args.available_at = req.available_at;
        return this.session.callTool('quicbaas_orchestrator_rotate_account', args);
    }
    async getSessionHistory(req) {
        const args = {
            tenant_id: req.tenant_id ?? '',
            session_id: req.session_id,
            from_sequence: req.from_sequence ?? 0,
        };
        if (req.limit && req.limit > 0)
            args.limit = req.limit;
        return this.session.callTool('quicbaas_orchestrator_session_history', args);
    }
    async getSessionConversation(req) {
        const args = {
            tenant_id: req.tenant_id ?? '',
            session_id: req.session_id,
        };
        if (req.cursor !== undefined)
            args.cursor = req.cursor;
        if (req.limit !== undefined)
            args.limit = req.limit;
        return this.session.callTool('quicbaas_orchestrator_session_conversation', args);
    }
    // eslint-disable-next-line @typescript-eslint/require-await, require-yield
    async *subscribeSession(_req) {
        throw mcpRefusal('orchestrator', 'subscribeSession', 'the server has no streaming session-event tool');
    }
    // =========================================================================
    // Spend and cost settings
    // =========================================================================
    async getSpendReport(req) {
        const args = { tenant_id: req.tenant_id ?? '' };
        for (const key of ['project_id', 'task_id', 'job_id', 'session_id', 'from', 'to', 'host', 'agent']) {
            if (req[key])
                args[key] = req[key];
        }
        // Presence, not value: round 0 is a real level, so an unset round_set
        // must leave the argument off entirely.
        if (req.round_set)
            args.round = req.round ?? 0;
        return this.session.callTool('quicbaas_orchestrator_cost', args);
    }
    async getTenantSpendPolicy(req) {
        return this.session.callTool('quicbaas_orchestrator_spend_policy_get', {
            tenant_id: req.tenant_id,
        });
    }
    async setTenantSpendPolicy(req) {
        return this.session.callTool('quicbaas_orchestrator_spend_policy_set', {
            tenant_id: req.tenant_id,
            limits: req.limits,
        });
    }
    async getCostSettings(req) {
        return this.session.callTool('quicbaas_orchestrator_cost_settings_get', {
            tenant_id: req.tenant_id ?? '',
        });
    }
    async setCostSettings(req) {
        const args = { tenant_id: req.tenant_id ?? '' };
        if (req.electricity_price_per_kwh !== undefined) {
            args.electricity_price_per_kwh = req.electricity_price_per_kwh;
        }
        if (req.engine_power_watts !== undefined)
            args.engine_power_watts = req.engine_power_watts;
        return this.session.callTool('quicbaas_orchestrator_cost_settings_set', args);
    }
    // =========================================================================
    // Worker image
    // =========================================================================
    async getWorkerImageStatus() {
        return this.session.callTool('quicbaas_sdlc_worker_image', {});
    }
    async refreshWorkerImage() {
        return this.session.callTool('quicbaas_sdlc_worker_image_refresh', {});
    }
    // =========================================================================
    // Inference
    // =========================================================================
    async inference(req) {
        if (req.host_affinity_strict) {
            throw mcpRefusal('orchestrator', 'inference', 'the inference tool takes no host_affinity_strict');
        }
        const args = { backend: req.backend, prompt: req.prompt };
        if (req.model !== undefined)
            args.model = req.model;
        if (req.system_prompt !== undefined)
            args.system_prompt = req.system_prompt;
        if (req.host_affinity !== undefined)
            args.host_affinity = req.host_affinity;
        if (req.metadata !== undefined)
            args.metadata = req.metadata;
        const resp = await this.session.callTool('quicbaas_orchestrator_inference', args);
        const out = { content: resp.content ?? [] };
        if (resp.model !== undefined)
            out.model = resp.model;
        if (resp.stop_reason !== undefined)
            out.stop_reason = resp.stop_reason;
        return out;
    }
    // eslint-disable-next-line @typescript-eslint/require-await, require-yield
    async *inferenceStream(_req, _signal) {
        throw mcpRefusal('orchestrator', 'inferenceStream', 'the server has no streaming inference tool');
    }
    // =========================================================================
    // Workflows
    // =========================================================================
    async listWorkflows(tenantID) {
        const workflows = await walkPages((offset) => this.listWorkflowsPage(tenantID, atOffset({}, offset)));
        return { workflows, count: workflows.length };
    }
    async createWorkflow(_req) {
        throw mcpRefusal('orchestrator', 'createWorkflow', 'the server has no workflow-create tool');
    }
    async getWorkflow(_tenantID, _name) {
        throw mcpRefusal('orchestrator', 'getWorkflow', 'the server has no workflow-get tool');
    }
    async updateWorkflow(_name, _def) {
        throw mcpRefusal('orchestrator', 'updateWorkflow', 'the server has no workflow-update tool');
    }
    async deleteWorkflow(_tenantID, _name) {
        throw mcpRefusal('orchestrator', 'deleteWorkflow', 'the server has no workflow-delete tool');
    }
    // =========================================================================
    // Tasks and attention
    // =========================================================================
    async listTasks(_tenantID) {
        throw mcpRefusal('orchestrator', 'listTasks', 'the server has no task-list tool');
    }
    async getTask(_tenantID, _taskID) {
        throw mcpRefusal('orchestrator', 'getTask', 'the server has no task-get tool');
    }
    // createTask builds the Task from the request and the fields the task_create
    // tool answers (id, status, runner, spawned job, dispatch reason). The tool
    // answers no server-stamped times or version, so those are zero.
    async createTask(req) {
        const tool = 'quicbaas_orchestrator_task_create';
        if (req.host_affinity_strict) {
            throw mcpRefusal('orchestrator', 'createTask', 'the task_create tool takes no host_affinity_strict');
        }
        const resp = await this.session.callTool(tool, { ...req });
        // The tool reports a task it persisted but could not dispatch as a soft
        // error; the REST route answers that as a failure, and so does this.
        if (resp.error)
            throw new McpError(tool, resp.error);
        const task = {
            id: resp.task_id,
            tenant_id: req.tenant_id,
            team_name: req.team_name,
            subject: resp.subject,
            description: req.description ?? '',
            status: resp.status,
            assigned_to: '',
            created_by: '',
            dependencies: [],
            blocks: [],
            result: null,
            metadata: req.metadata ?? {},
            created_at: 0,
            updated_at: 0,
            claimed_at: 0,
            completed_at: 0,
            version: 0,
            auto_spawn: resp.auto_spawn,
        };
        if (resp.spawned_job_id)
            task.spawned_job_id = resp.spawned_job_id;
        if (resp.runner)
            task.runner = resp.runner;
        if (resp.dispatch_reason)
            task.dispatch_reason = resp.dispatch_reason;
        if (req.prompt_append)
            task.prompt_append = req.prompt_append;
        return { task };
    }
    async claimTask(_req) {
        throw mcpRefusal('orchestrator', 'claimTask', 'the server has no task-claim tool');
    }
    async completeTask(_req) {
        throw mcpRefusal('orchestrator', 'completeTask', 'the server has no task-complete tool');
    }
    async readmitTask(tenantID, taskID) {
        await this.session.callTool('quicbaas_orchestrator_task_readmit', {
            tenant_id: tenantID,
            task_id: taskID,
        });
    }
    async deleteTask(req) {
        const resp = await this.session.callTool('quicbaas_orchestrator_task_delete', { tenant_id: req.tenant_id, task_id: req.task_id });
        return { task_id: resp.task_id, deleted: resp.deleted };
    }
    // decomposeTask is refused: quicbaas_orchestrator_decompose splits free
    // text into work items, which is not decomposing a stored task.
    async decomposeTask(_req) {
        throw mcpRefusal('orchestrator', 'decomposeTask', 'the server has no tool that decomposes a stored task');
    }
    async listAttention(tenantID, page) {
        const args = { tenant_id: tenantID };
        if (page?.project_id !== undefined)
            args.project_id = page.project_id;
        if (page?.kind !== undefined)
            args.kind = page.kind;
        if (page?.limit !== undefined)
            args.limit = page.limit;
        if (page?.offset !== undefined)
            args.offset = page.offset;
        if (page?.sort !== undefined)
            args.sort = page.sort;
        if (page?.q !== undefined)
            args.q = page.q;
        const raw = await this.session.callTool('quicbaas_orchestrator_attention_list', args);
        return adoptPage(raw, 'items');
    }
    async resolveAttention(req) {
        const args = {
            tenant_id: req.tenant_id,
            task_id: req.task_id,
            action: req.action,
        };
        if (req.answer !== undefined)
            args.answer = req.answer;
        return this.session.callTool('quicbaas_orchestrator_attention_resolve', args);
    }
    // allPages walks a paged listing tool to its end, as the REST transport's
    // unpaged listings do, so the unpaged verbs answer the whole set.
    async allPages(tool, key) {
        return walkPages(async (offset) => {
            const resp = await this.session.callTool(tool, { offset });
            return {
                items: resp[key] ?? [],
                next_offset: resp.next_offset,
            };
        });
    }
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
// secretArg renders a barrier credential as the plaintext string an MCP tool
// argument has to be, as the Go MCP transport's secretArg does: JSON carries
// no bytes, and the server reads the argument as the credential itself.
function secretArg(secret) {
    return new TextDecoder().decode(secret);
}
// McpBarrierClient calls the qrdb_barrier_* MCP tools, with the tool names
// and argument keys of the Go SDK's MCP transport.
export class McpBarrierClient {
    session;
    constructor(session) {
        this.session = session;
    }
    async status() {
        return this.session.callTool('qrdb_barrier_status', {});
    }
    async initialize(req) {
        await this.session.callTool('qrdb_barrier_initialize', { secret: secretArg(req.secret) });
    }
    async unseal(req) {
        await this.session.callTool('qrdb_barrier_unseal', { secret: secretArg(req.secret) });
    }
    async seal(req) {
        requireTLSForSOPIN('mcp', this.session.endpoint, req?.so_pin);
        await this.session.callTool('qrdb_barrier_seal', req?.so_pin ? { so_pin: req.so_pin } : {});
    }
    // The MCP tool never returns share material (officers claim shares through
    // the qrdb share-claim flow), so shares is empty unless the server sends it.
    async initializeShamir(req) {
        const reply = await this.session.callTool('qrdb_barrier_shamir_initialize', {
            secret: secretArg(req.secret),
            threshold: req.threshold,
            total_shares: req.total_shares,
        });
        return { shares: reply.shares ?? [], threshold: reply.threshold ?? 0, total_shares: reply.total_shares ?? 0 };
    }
    async unsealWithShare(req) {
        return this.session.callTool('qrdb_barrier_shamir_unseal_share', { share: req.share });
    }
    async unsealWithShares(req) {
        await this.session.callTool('qrdb_barrier_shamir_unseal_shares', { shares: req.shares });
    }
    // Like initializeShamir, the rekey tool reports the new quorum but no shares.
    async rekey(req) {
        const reply = await this.session.callTool('qrdb_barrier_rekey', {
            threshold: req.threshold,
            total: req.total,
        });
        return { shares: reply.shares ?? [], threshold: reply.threshold ?? 0, total_shares: reply.total_shares ?? 0 };
    }
    // The recovery tool reports the quorum but never the recovery keys.
    async generateRecoveryKeys(req) {
        const reply = await this.session.callTool('qrdb_barrier_recovery_generate', {
            threshold: req.threshold,
            total: req.total,
        });
        return { keys: reply.keys ?? [], threshold: reply.threshold ?? 0, total: reply.total ?? 0 };
    }
    async recoverWithKeys(req) {
        await this.session.callTool('qrdb_barrier_recovery_recover', { keys: req.keys });
    }
    async deleteRecoveryKeys() {
        await this.session.callTool('qrdb_barrier_recovery_delete', {});
    }
    // The tool names the flag has_recovery_keys; the SDK type names it has_keys.
    async recoveryStatus() {
        const reply = await this.session.callTool('qrdb_barrier_recovery_status', {});
        return { has_keys: reply.has_recovery_keys === true };
    }
    async generateRootToken(req) {
        const reply = await this.session.callTool('qrdb_barrier_root_token', { shares: req.shares });
        return { token: reply.token ?? '', created_at: reply.created_at ?? '' };
    }
    // A tenant request goes to the tenant tool: the system tool ignores
    // tenant_id and would migrate the system barrier. The tools do not report
    // the scope, so it is the scope of the tool that was called.
    async enableEncryption(req) {
        const args = {
            target_strategy: req.strategy,
            mechanism: req.mechanism ?? '',
            availability: req.availability ?? '',
            passphrase: req.passphrase === undefined ? '' : secretArg(req.passphrase),
            shamir_threshold: req.shamir_threshold ?? 0,
            shamir_total: req.shamir_total ?? 0,
        };
        let tool = 'qrdb_barrier_enable_encryption';
        let scope = 'system';
        if (req.tenant_id) {
            tool = 'qrdb_tenant_barrier_enable_encryption';
            scope = 'tenant';
            args.tenant_id = req.tenant_id;
        }
        const reply = await this.session.callTool(tool, args);
        return { status: reply.status ?? '', scope };
    }
}
// decodeBase64Text decodes a base64 tool field into UTF-8 text, refusing
// malformed base64 as an McpError naming the tool.
function decodeBase64Text(tool, b64) {
    let binary;
    try {
        binary = atob(b64);
    }
    catch {
        throw new McpError(tool, 'share is not valid base64');
    }
    return new TextDecoder().decode(Uint8Array.from(binary, (c) => c.charCodeAt(0)));
}
// McpCeremonyClient calls the qrdb_ceremony_* MCP tools, with the tool names
// and argument keys of the Go SDK's MCP transport. claimShareBegin and
// claimShareComplete call the qrdb_ceremony_begin_claim_share /
// qrdb_ceremony_complete_claim_share pair (pkg/api/mcp/tools_ceremony.go): the
// officer signs the begin nonce, and the server binds both calls to the
// session's mTLS certificate thumbprint, which never travels in the arguments.
export class McpCeremonyClient {
    session;
    constructor(session) {
        this.session = session;
    }
    async initialize(req) {
        requireTLSForSOPIN('mcp', this.session.endpoint, req.so_pin);
        const args = { so_pin: req.so_pin, user_pin: req.user_pin, threshold: req.threshold };
        if (req.credential_seal_strategy)
            args.credential_seal_strategy = req.credential_seal_strategy;
        if (req.officers && req.officers.length > 0) {
            args.officers = req.officers.map((o) => ({ username: o.username, csr_pem: o.csr_pem }));
        }
        if (req.hostname)
            args.hostname = req.hostname;
        if (req.sans && req.sans.length > 0)
            args.sans = req.sans;
        return this.session.callTool('qrdb_ceremony_initialize', args);
    }
    async status() {
        return this.session.callTool('qrdb_ceremony_status', {});
    }
    async claimCertBegin(req) {
        const reply = await this.session.callTool('qrdb_ceremony_claim_cert_begin', { username: req.username });
        return { nonce_hex: reply.nonce_hex ?? '', expires_at: reply.expires_at ?? '' };
    }
    async claimCertComplete(req) {
        return this.session.callTool('qrdb_ceremony_claim_cert_complete', {
            username: req.username,
            nonce_hex: req.nonce_hex,
            signature: req.signature,
        });
    }
    async claimShareBegin(req) {
        const reply = await this.session.callTool('qrdb_ceremony_begin_claim_share', { username: req.username });
        return { nonce_hex: reply.nonce_hex ?? '', expires_at: reply.expires_at ?? '' };
    }
    // claimShareComplete sends the base64 signature as the tool's signature
    // argument and decodes the tool's base64 share back into share_json.
    async claimShareComplete(req) {
        const tool = 'qrdb_ceremony_complete_claim_share';
        const reply = await this.session.callTool(tool, {
            username: req.username,
            nonce_hex: req.nonce_hex,
            signature: req.signature,
        });
        return { share_index: reply.share_index ?? 0, share_json: decodeBase64Text(tool, reply.share ?? '') };
    }
    async signCSR(req) {
        requireTLSForSOPIN('mcp', this.session.endpoint, req.so_pin);
        return this.session.callTool('qrdb_ceremony_sign_csr', {
            username: req.username,
            so_pin: req.so_pin,
            csr_pem: req.csr_pem,
            role: req.role,
        });
    }
    async enrollExternalOfficer(req) {
        const args = {
            username: req.username,
            share_index: req.share_index,
            auth_factor: req.auth_factor,
        };
        if (req.cert_pem)
            args.cert_pem = req.cert_pem;
        if (req.pubkey_pem)
            args.pubkey_pem = req.pubkey_pem;
        if (req.share_bytes)
            args.share_bytes = req.share_bytes;
        return this.session.callTool('qrdb_ceremony_enroll_external_officer', args);
    }
}
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
    async bind(tenantId, projectId, binding, paused) {
        return this.session.callTool('sdlc_bind', {
            tenant_id: tenantId,
            project_id: projectId,
            binding: { ...binding, project_id: binding.project_id || projectId },
            ...(paused ? { paused: true } : {}),
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
        this.orchestrator = new McpOrchestratorClient(session);
        this.auth = new McpAuthClient();
        this.barrier = new McpBarrierClient(session);
        this.ceremony = new McpCeremonyClient(session);
    }
    async logout(_token) {
        // Non-REST transports don't manage bearer tokens via the REST logout verb.
        // The caller is responsible for clearing local credentials; this is a no-op
        // so the unified AuthClient surface compiles across every transport.
    }
}
//# sourceMappingURL=mcp.js.map