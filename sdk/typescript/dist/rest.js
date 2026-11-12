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
import { parseJSONPreservingLargeIntegers } from './json.js';
import { adoptPage, appendCSVParam, appendListParam, appendPageParams, } from './page.js';
import { APIError, ConnectionError, NotConnectedError, DeployNotSupportedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError, } from './errors.js';
import { requireTLSForSOPIN } from './so_pin.js';
// API base path for all orchestrator endpoints.
const API_PREFIX = '/api/v1/orchestrator';
// FLEET_PREFIX is the fleet registry's own REST surface, outside the
// orchestrator prefix.
const FLEET_PREFIX = '/api/v1/fleet';
// WORKER_IMAGE_PATH is the worker image refresh's route: a host verb of the
// SDLC module, served beside the orchestrator surface.
const WORKER_IMAGE_PATH = '/api/v1/sdlc/worker-image';
// sleep resolves after `ms` milliseconds. Used only by the fetch-based SSE
// reconnect loops (subscribeJobEvents) to back off between attempts.
function sleep(ms) {
    return new Promise((resolve) => setTimeout(resolve, ms));
}
// SSE_RECONNECT_DELAY_MS mirrors the browser's own default EventSource retry
// delay, for the fetch-based SSE stream(s) that replace it.
const SSE_RECONNECT_DELAY_MS = 3000;
// resolveToken resolves a bearer token from either a static string or an
// async factory function. Returns an empty string when no token is configured.
export async function resolveToken(token) {
    if (!token)
        return '';
    if (typeof token === 'function')
        return token();
    return token;
}
// utf8Bytes encodes a string to its UTF-8 byte sequence.
function utf8Bytes(s) {
    return new TextEncoder().encode(s);
}
// bytesToBase64 encodes bytes as standard base64 — the wire form Go []byte
// fields decode from JSON strings.
function bytesToBase64(bytes) {
    let binary = '';
    for (let i = 0; i < bytes.length; i++) {
        binary += String.fromCharCode(bytes[i]);
    }
    return btoa(binary);
}
// parseResponseBody reads the body of a 2xx fetch Response and returns the
// parsed JSON, tolerating 204 No Content and empty bodies (returns undefined
// as T). All fetchJSON and fetchSSH success tails delegate here to prevent
// "Unexpected end of JSON input" when servers return 204 or a body-less 2xx.
// The parse keeps 64-bit identities exact: an id past 2^53 arrives as its
// decimal string, while timestamps and other quantities stay numbers (see
// json.ts for the key rule).
async function parseResponseBody(resp) {
    if (resp.status === 204)
        return undefined;
    const text = await resp.text();
    return (text ? parseJSONPreservingLargeIntegers(text) : undefined);
}
// RestConnectionClient manages the server address and validates connectivity
// by performing a health check against the orchestrator endpoint.
class RestConnectionClient {
    _connected = false;
    _address = '';
    _transport = 'rest';
    constructor(address) {
        this._address = address;
    }
    // connect validates that the server is reachable by calling the health endpoint.
    async connect(address, _transport, _tenantID) {
        this._address = address;
        const resp = await fetch(`${address}${API_PREFIX}/health`);
        if (!resp.ok) {
            throw new ConnectionError(`Failed to connect to QuicBaaS at ${address}: ${resp.statusText}`);
        }
        this._connected = true;
    }
    async disconnect() {
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
// sessionFromWire renames the two fields the service and the SDK disagree
// about. It mirrors SessionStatusWire.Response() in the Go client, field for
// field, so the two SDKs hand a caller the same object for the same session.
//
// A field the service does not send stays ABSENT rather than becoming an
// empty string or a zero instant: "the server did not say" and "the server
// said nothing was there" are different answers, and the views render them
// differently.
function sessionFromWire(wire) {
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
    // The ROLE the session played, resolved server-side for the page. It was
    // dropped here while the wire carried it, which left the Role badge blank
    // on every row (go-quicbaas#969).
    if (wire.runner !== undefined)
        session.runner = wire.runner;
    return session;
}
// RestOrchestratorClient implements OrchestratorClient using the QuicBaaS REST API.
// All endpoints are under /api/v1/orchestrator and accept/return JSON.
// Error responses are surfaced as APIError with the HTTP status code.
class RestOrchestratorClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        // Trim trailing slash to allow consistent path concatenation.
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    // authHeaders builds Authorization and tenant headers from config.
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    // fetchJSON performs an authenticated fetch and deserialises the response body.
    // Non-2xx responses are converted to APIError.
    async fetchJSON(path, init) {
        return this.fetchAPI(`${API_PREFIX}${path}`, init);
    }
    async fetchAllListPages(path, itemsKey) {
        const items = [];
        let offset = 0;
        for (;;) {
            const params = new URLSearchParams();
            params.set('limit', '100');
            if (offset > 0)
                params.set('offset', String(offset));
            const pagePath = `${path}${path.includes('?') ? '&' : '?'}${params.toString()}`;
            const raw = await this.fetchJSON(pagePath);
            const page = adoptPage(raw, itemsKey);
            items.push(...page.items);
            if (page.next_offset === undefined)
                return items;
            if (page.next_offset <= page.offset) {
                throw new Error(`${itemsKey} list next_offset did not advance`);
            }
            offset = page.next_offset;
        }
    }
    // fetchAPI is fetchJSON for a path outside the orchestrator prefix — the
    // worker image refresh lives under /api/v1/sdlc and the fleet registry
    // under /api/v1/fleet.
    async fetchAPI(apiPath, init) {
        const url = `${this.baseUrl}${apiPath}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText if the body is not valid JSON.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // =========================================================================
    // The paged listings
    //
    // Each one sends the four shared parameters plus its endpoint's own
    // narrowings, and hands back the envelope the server answered with. The
    // unpaged methods below are unchanged and still useful — they are just no
    // longer the whole collection, because the server pages every listing and
    // defaults to 25 records. See docs/software-factory/api.md.
    // =========================================================================
    // listJobsPage maps to GET /api/v1/orchestrator/jobs with limit/offset/
    // sort/q and the jobs listing's own filters, all applied by the SERVER.
    async listJobsPage(tenantID, page) {
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'task_id', page?.task_id);
        appendListParam(params, 'runner', page?.runner);
        appendListParam(params, 'project_id', page?.project_id);
        appendListParam(params, 'agent_id', page?.agent_id);
        appendCSVParam(params, 'statuses', page?.statuses);
        appendCSVParam(params, 'job_ids', page?.job_ids);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/jobs?${params.toString()}`);
        return adoptPage(raw, 'jobs');
    }
    // listTasksPage maps to GET /api/v1/orchestrator/tasks with the shared
    // paging parameters and the tasks listing's own filters.
    async listTasksPage(tenantID, page) {
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'status', page?.status);
        appendListParam(params, 'project_id', page?.project_id);
        appendListParam(params, 'team_name', page?.team_name);
        appendListParam(params, 'assigned_to', page?.assigned_to);
        appendListParam(params, 'created_by', page?.created_by);
        appendListParam(params, 'auto_spawn', page?.auto_spawn);
        appendListParam(params, 'agent_id', page?.agent_id);
        appendListParam(params, 'job_id', page?.job_id);
        appendListParam(params, 'source_kind', page?.source_kind);
        appendListParam(params, 'source_ref', page?.source_ref);
        appendCSVParam(params, 'statuses', page?.statuses);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/tasks?${params.toString()}`);
        return adoptPage(raw, 'tasks');
    }
    // listSessionsPage maps to GET /api/v1/orchestrator/sessions. The records
    // are renamed through sessionFromWire, exactly as listSessions does, so a
    // paged caller and an unpaged one hold the same session object.
    async listSessionsPage(tenantID, page) {
        if (!tenantID) {
            throw new Error('listSessionsPage: tenantID is required');
        }
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'backend', page?.backend);
        appendListParam(params, 'status', page?.status);
        appendListParam(params, 'project_id', page?.project_id);
        appendListParam(params, 'job_id', page?.job_id);
        appendListParam(params, 'runner', page?.runner);
        appendListParam(params, 'agent_id', page?.agent_id);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/sessions?${params.toString()}`);
        return adoptPage(raw, 'sessions', sessionFromWire);
    }
    // listFleetNodesPage maps to GET /api/v1/fleet with the shared
    // paging parameters and the fleet listing's own filters.
    async listFleetNodesPage(tenantID, page) {
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'host', page?.host);
        appendListParam(params, 'status', page?.status);
        appendListParam(params, 'node_type', page?.node_type);
        appendListParam(params, 'backend', page?.backend);
        appendListParam(params, 'model', page?.model);
        appendPageParams(params, page);
        const raw = await this.fetchAPI(`${FLEET_PREFIX}?${params.toString()}`);
        return adoptPage(raw, 'nodes');
    }
    // listWorkflowsPage maps to GET /api/v1/orchestrator/workflows with the
    // shared paging parameters and the workflow listing's own filters.
    async listWorkflowsPage(tenantID, page) {
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'project_id', page?.project_id);
        appendListParam(params, 'mode', page?.mode);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/workflows?${params.toString()}`);
        return adoptPage(raw, 'workflows');
    }
    // listJobs maps to GET /api/v1/orchestrator/jobs?tenant_id=<tenantID>
    // with an optional &task_id=<taskID>, which the SERVER applies.
    // Server returns {jobs: JobStatus[] | null}; unwrap to bare array.
    async listJobs(tenantID, taskID) {
        let path = `/jobs?tenant_id=${encodeURIComponent(tenantID)}`;
        if (taskID)
            path += `&task_id=${encodeURIComponent(taskID)}`;
        const raw = await this.fetchJSON(path);
        if (Array.isArray(raw))
            return raw;
        return raw.jobs ?? [];
    }
    // getJobStatus maps to GET /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
    async getJobStatus(tenantID, jobID) {
        return this.fetchJSON(`/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`);
    }
    // startJob maps to POST /api/v1/orchestrator/jobs
    async startJob(req) {
        return this.fetchJSON('/jobs', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // cancelJob maps to DELETE /api/v1/orchestrator/jobs/:jobID?tenant_id=<tenantID>
    async cancelJob(tenantID, jobID) {
        await this.fetchJSON(`/jobs/${encodeURIComponent(jobID)}?tenant_id=${encodeURIComponent(tenantID)}`, { method: 'DELETE' });
    }
    // listFleetNodes maps to GET /api/v1/fleet with query parameters.
    async listFleetNodes(req) {
        const params = new URLSearchParams({ tenant_id: req.tenant_id });
        if (req.host)
            params.set('host', req.host);
        if (req.status)
            params.set('status', req.status);
        if (req.node_type)
            params.set('node_type', req.node_type);
        if (req.backend)
            params.set('backend', req.backend);
        if (req.model)
            params.set('model', req.model);
        return this.fetchAPI(`${FLEET_PREFIX}?${params.toString()}`);
    }
    // getFleetNode maps to GET /api/v1/fleet/:nodeID?tenant_id=<tenantID>
    async getFleetNode(req) {
        return this.fetchAPI(`${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`);
    }
    // listSessions maps to GET /api/v1/orchestrator/sessions?tenant_id=<tenantID>
    // Server returns {sessions: [...], timestamp}; normalise missing count field.
    async listSessions(req) {
        if (!req.tenantID) {
            throw new Error('listSessions: tenantID is required');
        }
        const params = new URLSearchParams();
        params.set('tenant_id', req.tenantID);
        if (req.backend)
            params.set('backend', req.backend);
        if (req.status)
            params.set('status', req.status);
        const raw = await this.fetchJSON(`/sessions?${params.toString()}`);
        const sessions = (raw.sessions ?? []).map(sessionFromWire);
        return {
            sessions,
            count: raw.count ?? sessions.length,
        };
    }
    // listTasks maps to GET /api/v1/orchestrator/tasks?tenant_id=<tenantID>
    // Server returns {tasks: Task[]}; unwrap to bare array.
    async listTasks(tenantID) {
        const raw = await this.fetchJSON(`/tasks?tenant_id=${encodeURIComponent(tenantID)}`);
        if (Array.isArray(raw))
            return raw;
        return raw.tasks ?? [];
    }
    // getTask maps to GET /api/v1/orchestrator/tasks/:taskID?tenant_id=<tenantID>
    async getTask(tenantID, taskID) {
        return this.fetchJSON(`/tasks/${encodeURIComponent(taskID)}?tenant_id=${encodeURIComponent(tenantID)}`);
    }
    // listBackends maps to GET /api/v1/orchestrator/backends
    // The REST endpoint returns {"backends": [...]}; unwrap to match the
    // Go SDK contract which exposes a bare slice.
    async listBackends() {
        return this.fetchAllListPages('/backends', 'backends');
    }
    // listRunners maps to GET /api/v1/orchestrator/runners
    // The REST endpoint returns {"runners": [...]}; unwrap to match the Go SDK
    // contract, which exposes a bare slice.
    async listRunners() {
        return this.fetchAllListPages('/runners', 'runners');
    }
    // listBackendModels maps to
    // GET /api/v1/fleet/{nodeID}/backends/{backendName}/models
    // Returns the model names available on the named backend as seen from the
    // specified fleet node. Pass "any" as nodeID to query without host affinity.
    async listBackendModels(nodeID, backendName) {
        const nid = encodeURIComponent(nodeID || 'any');
        const bn = encodeURIComponent(backendName);
        const resp = await this.fetchAPI(`${FLEET_PREFIX}/${nid}/backends/${bn}/models`);
        return resp.models ?? [];
    }
    // healthCheck maps to GET /api/v1/orchestrator/health
    async healthCheck() {
        return this.fetchJSON('/health');
    }
    // subscribeJobEvents opens a Server-Sent Events stream for a running job.
    // Maps to GET /api/v1/orchestrator/jobs/:jobID/events?tenant_id=<tenantID>
    // Returns a cleanup function that stops the stream and any pending
    // reconnect.
    //
    // Native EventSource is deliberately NOT used here, for the same reason
    // subscribeSession does its own fetch-based SSE parsing: it cannot carry a
    // custom Authorization header, so a bearer-token session has no way to
    // authenticate the connection at all. Every request this stream sends
    // therefore arrived at the daemon with no Authorization header and was
    // refused — indistinguishable, from the browser's side, from an expired or
    // rejected session, which is what made "renewed but rejected" reproduce for
    // an operator with running jobs open (their dashboard held an open job-
    // events stream) and NOT for one without any (go-quicbaas UI-login-refused
    // investigation, 2026-09-25). The bearer is resolved fresh on every
    // (re)connect attempt, exactly like every other authenticated call on this
    // client, so a token rotated mid-stream is picked up on the next reconnect
    // rather than the stream being pinned to whatever was valid when it first
    // opened.
    subscribeJobEvents(tenantID, jobID, onEvent, onError) {
        const url = [
            this.baseUrl,
            API_PREFIX,
            `/jobs/${encodeURIComponent(jobID)}/events`,
            `?tenant_id=${encodeURIComponent(tenantID)}`,
        ].join('');
        const controller = new AbortController();
        let closedByCaller = false;
        const run = async () => {
            while (!closedByCaller) {
                let resp;
                try {
                    const auth = await this.authHeaders();
                    resp = await fetch(url, {
                        headers: { Accept: 'text/event-stream', ...auth },
                        signal: controller.signal,
                    });
                }
                catch {
                    // Aborted by the caller, or a network failure. A network failure is
                    // a transient drop — the same case EventSource reconnects from on
                    // its own — so back off and retry rather than giving up.
                    if (closedByCaller)
                        return;
                    await sleep(SSE_RECONNECT_DELAY_MS);
                    continue;
                }
                if (!resp.ok || !resp.body) {
                    // A refusal (403) or a response that is not an event stream at all
                    // is not transient — reported once, never retried, matching
                    // EventSource's CLOSED/give-up semantics.
                    if (!closedByCaller)
                        onError?.();
                    return;
                }
                const reader = resp.body.getReader();
                const decoder = new TextDecoder();
                let buffer = '';
                let currentData = '';
                try {
                    while (true) {
                        const { value, done } = await reader.read();
                        if (done)
                            break;
                        buffer += decoder.decode(value, { stream: true });
                        const lines = buffer.split('\n');
                        buffer = lines.pop() ?? '';
                        for (const line of lines) {
                            if (line.startsWith('data: ')) {
                                currentData = line.slice(6);
                            }
                            else if (line === '' && currentData !== '') {
                                try {
                                    onEvent(JSON.parse(currentData));
                                }
                                catch {
                                    // Discard unparseable frames; the server may send
                                    // keep-alive comments.
                                }
                                currentData = '';
                            }
                        }
                    }
                }
                catch {
                    // Aborted mid-read, or a stream read error — fall through to the
                    // reconnect decision below either way.
                }
                finally {
                    reader.cancel().catch(() => { });
                }
                if (closedByCaller)
                    return;
                // The stream ended — the server closed it, or the connection dropped
                // mid-read. Reconnect the same way EventSource does for a transient
                // drop, rather than treating every disconnect as a give-up.
                await sleep(SSE_RECONNECT_DELAY_MS);
            }
        };
        void run();
        return () => {
            closedByCaller = true;
            controller.abort();
        };
    }
    // spawnSession maps to POST /api/v1/orchestrator/sessions?tenant_id=<t>.
    // tenantID is threaded via query string (server expectation) and not in
    // the JSON body — the server-side SpawnSessionRequest struct has no
    // tenant_id field.
    async spawnSession(req) {
        if (!req.tenantID) {
            throw new Error('spawnSession: tenantID is required');
        }
        const { tenantID, ...body } = req;
        return this.fetchJSON(`/sessions?tenant_id=${encodeURIComponent(tenantID)}`, { method: 'POST', body: JSON.stringify(body) });
    }
    // promptSession maps to POST /api/v1/orchestrator/sessions/:sessionID/prompt
    async promptSession(req) {
        return this.fetchJSON(`/sessions/${encodeURIComponent(req.session_id)}/prompt`, { method: 'POST', body: JSON.stringify(req) });
    }
    // closeSession maps to DELETE /api/v1/orchestrator/sessions/:sessionID
    async closeSession(req) {
        return this.fetchJSON(`/sessions/${encodeURIComponent(req.session_id)}`, { method: 'DELETE' });
    }
    // rotateSessionAccount maps to
    // POST /api/v1/orchestrator/sessions/:sessionID/rotate-account. The path
    // segment is authoritative on the server, so session_id is dropped from the
    // body rather than sent twice.
    async rotateSessionAccount(req) {
        if (!req.session_id) {
            throw new Error('rotateSessionAccount: session_id is required');
        }
        const { session_id, ...body } = req;
        return this.fetchJSON(`/sessions/${encodeURIComponent(session_id)}/rotate-account`, { method: 'POST', body: JSON.stringify(body) });
    }
    // createWorkflow maps to POST /api/v1/orchestrator/workflows
    async createWorkflow(req) {
        return this.fetchJSON('/workflows', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // listWorkflows maps to GET /api/v1/orchestrator/workflows?tenant_id=<tenantID>
    // Server returns {workflows:[...]}; count field may be absent — derive it.
    async listWorkflows(tenantID) {
        const raw = await this.fetchJSON(`/workflows?tenant_id=${encodeURIComponent(tenantID)}`);
        return {
            workflows: raw.workflows ?? [],
            count: raw.count ?? (raw.workflows ?? []).length,
        };
    }
    // getWorkflow maps to GET /api/v1/orchestrator/workflows/:name?tenant_id=<tenantID>
    async getWorkflow(tenantID, name) {
        const resp = await this.fetchJSON(`/workflows/${encodeURIComponent(name)}?tenant_id=${encodeURIComponent(tenantID)}`);
        return resp.workflow;
    }
    // updateWorkflow maps to PUT /api/v1/orchestrator/workflows/:name
    async updateWorkflow(name, def) {
        const resp = await this.fetchJSON(`/workflows/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(def) });
        return resp.workflow;
    }
    // deleteWorkflow maps to DELETE /api/v1/orchestrator/workflows/:name
    async deleteWorkflow(tenantID, name) {
        await this.fetchJSON(`/workflows/${encodeURIComponent(name)}?tenant_id=${encodeURIComponent(tenantID)}`, { method: 'DELETE' });
    }
    // createTask maps to POST /api/v1/orchestrator/tasks
    async createTask(req) {
        return this.fetchJSON('/tasks', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // claimTask maps to POST /api/v1/orchestrator/tasks/:taskID/claim
    async claimTask(req) {
        await this.fetchJSON(`/tasks/${encodeURIComponent(req.task_id)}/claim?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'POST', body: JSON.stringify({ agent_id: req.agent_id }) });
    }
    // completeTask maps to POST /api/v1/orchestrator/tasks/:taskID/complete
    async completeTask(req) {
        await this.fetchJSON(`/tasks/${encodeURIComponent(req.task_id)}/complete?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'POST', body: JSON.stringify({ result: req.result }) });
    }
    // readmitTask maps to POST /api/v1/orchestrator/tasks/:taskID/readmit
    async readmitTask(tenantID, taskID) {
        await this.fetchJSON(`/tasks/${encodeURIComponent(taskID)}/readmit?tenant_id=${encodeURIComponent(tenantID)}`, { method: 'POST' });
    }
    // deleteTask maps to DELETE /api/v1/orchestrator/tasks/:taskID
    async deleteTask(req) {
        return this.fetchJSON(`/tasks/${encodeURIComponent(req.task_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'DELETE' });
    }
    // listAttention maps to GET /api/v1/orchestrator/attention with the
    // shared paging parameters and the inbox's own project/kind narrowings.
    async listAttention(tenantID, page) {
        const params = new URLSearchParams({ tenant_id: tenantID });
        appendListParam(params, 'project_id', page?.project_id);
        appendListParam(params, 'kind', page?.kind);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/attention?${params.toString()}`);
        return adoptPage(raw, 'items');
    }
    // resolveAttention maps to POST /api/v1/orchestrator/attention/:taskID/resolve
    async resolveAttention(req) {
        return this.fetchJSON(`/attention/${encodeURIComponent(req.task_id)}/resolve?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'POST', body: JSON.stringify({ action: req.action, answer: req.answer }) });
    }
    // decomposeTask maps to POST /api/v1/orchestrator/tasks/:taskID/decompose
    async decomposeTask(req) {
        return this.fetchJSON(`/tasks/${encodeURIComponent(req.task_id)}/decompose?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'POST', body: JSON.stringify(req) });
    }
    // registerFleetNode maps to POST /api/v1/fleet
    async registerFleetNode(req) {
        return this.fetchAPI(FLEET_PREFIX, {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // removeFleetNode maps to DELETE /api/v1/fleet/:nodeID?tenant_id=<tenantID>
    async removeFleetNode(req) {
        await this.fetchAPI(`${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}?tenant_id=${encodeURIComponent(req.tenant_id)}`, { method: 'DELETE' });
    }
    // disableFleetNode maps to POST /api/v1/fleet/:nodeID/disable?tenant_id=<tenantID>
    async disableFleetNode(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchAPI(`${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}/disable?${query}`, { method: 'POST' });
    }
    // enableFleetNode maps to POST /api/v1/fleet/:nodeID/enable?tenant_id=<tenantID>
    async enableFleetNode(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchAPI(`${FLEET_PREFIX}/${encodeURIComponent(req.node_id)}/enable?${query}`, { method: 'POST' });
    }
    // deployFleetNode is a desktop-only operation. REST callers must use the
    // CLI (`quicbaas fleet deploy`) or the desktop GUI.
    async deployFleetNode(_spec) {
        throw new DeployNotSupportedError('rest');
    }
    // getSessionHistory maps to GET /api/v1/orchestrator/sessions/:sessionID/events
    // with Accept: application/json and follow=0, which returns a paginated JSON body.
    async getSessionHistory(req) {
        const params = new URLSearchParams({
            from: String(req.from_sequence ?? 0),
            follow: '0',
        });
        if (req.limit && req.limit > 0)
            params.set('limit', String(req.limit));
        if (req.tenant_id)
            params.set('tenant_id', req.tenant_id);
        return this.fetchJSON(`/sessions/${encodeURIComponent(req.session_id)}/events?${params.toString()}`);
    }
    // getSessionConversation maps to
    // GET /api/v1/orchestrator/sessions/:sessionID/history, the paged
    // conversation view.
    //
    // The route says "history" and the method says "conversation" because the
    // SDK already has a getSessionHistory that pages the raw EVENT log: the
    // path names the resource an operator reaches for, the method names the
    // shape it returns.
    async getSessionConversation(req) {
        const params = new URLSearchParams();
        if (req.tenant_id)
            params.set('tenant_id', req.tenant_id);
        if (req.cursor && req.cursor > 0)
            params.set('cursor', String(req.cursor));
        if (req.limit && req.limit > 0)
            params.set('limit', String(req.limit));
        const query = params.toString();
        return this.fetchJSON(`/sessions/${encodeURIComponent(req.session_id)}/history${query ? `?${query}` : ''}`);
    }
    // getSpendReport maps to GET /api/v1/orchestrator/cost.
    //
    // ONE ROUTE, SCOPED BY QUERY, because the ledger's attribution chain is a
    // PATH: a project's spend and a job's spend are the same prefix scan at two
    // depths, and four routes over one scan would be four places for the
    // drill-down's arithmetic to stop agreeing with itself.
    // getWorkerImageStatus maps to GET /api/v1/sdlc/worker-image.
    async getWorkerImageStatus() {
        return this.fetchAPI(WORKER_IMAGE_PATH);
    }
    // refreshWorkerImage maps to POST /api/v1/sdlc/worker-image/refresh, which
    // answers 202 with the status the request left behind, 409 while a pass is
    // running or queued, 503 on a node without the refresh loop.
    async refreshWorkerImage() {
        return this.fetchAPI(`${WORKER_IMAGE_PATH}/refresh`, { method: 'POST' });
    }
    async getSpendReport(req) {
        const params = new URLSearchParams();
        if (req.tenant_id)
            params.set('tenant_id', req.tenant_id);
        if (req.project_id)
            params.set('project_id', req.project_id);
        if (req.task_id)
            params.set('task_id', req.task_id);
        if (req.job_id)
            params.set('job_id', req.job_id);
        if (req.session_id)
            params.set('session_id', req.session_id);
        // Presence, not value: round 0 is a real level, so an unset round_set
        // must leave the parameter off entirely rather than send '0'.
        if (req.round_set)
            params.set('round', String(req.round ?? 0));
        if (req.from)
            params.set('from', req.from);
        if (req.to)
            params.set('to', req.to);
        if (req.host)
            params.set('host', req.host);
        if (req.agent)
            params.set('agent', req.agent);
        const query = params.toString();
        return this.fetchJSON(`/cost${query ? `?${query}` : ''}`);
    }
    // getTenantSpendPolicy maps to GET /api/v1/orchestrator/spend-policy.
    async getTenantSpendPolicy(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/spend-policy?${query}`);
    }
    // setTenantSpendPolicy maps to PUT /api/v1/orchestrator/spend-policy.
    async setTenantSpendPolicy(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/spend-policy?${query}`, {
            method: 'PUT',
            body: JSON.stringify({ limits: req.limits }),
        });
    }
    // getCostSettings maps to GET /api/v1/orchestrator/cost-settings.
    async getCostSettings(req) {
        return this.fetchJSON(`/cost-settings${costSettingsQuery(req.tenant_id)}`);
    }
    // setCostSettings maps to PUT /api/v1/orchestrator/cost-settings. The
    // settings are replaced: an absent rate or engine inherits the
    // configuration.
    async setCostSettings(req) {
        const body = {
            electricity_price_per_kwh: req.electricity_price_per_kwh,
            engine_power_watts: req.engine_power_watts,
        };
        return this.fetchJSON(`/cost-settings${costSettingsQuery(req.tenant_id)}`, {
            method: 'PUT',
            body: JSON.stringify(body),
        });
    }
    // subscribeSession opens a Server-Sent Events stream for a session.
    // Maps to GET /api/v1/orchestrator/sessions/:sessionID/events?from=N&follow=1
    //
    // The SSE stream emits frames in the standard format:
    //   id: <sequence>
    //   event: <kind>
    //   data: <json>
    //
    // Native EventSource is intentionally avoided here because it does not support
    // custom request headers (needed for bearer tokens), AbortSignal, or reliable
    // reconnect with Last-Event-ID across all target browsers. Manual fetch-based
    // SSE parsing gives us all of those.
    async *subscribeSession(req) {
        const params = new URLSearchParams({
            from: String(req.from_sequence ?? 0),
            follow: req.follow ? '1' : '0',
        });
        if (req.tenant_id)
            params.set('tenant_id', req.tenant_id);
        const url = `${this.baseUrl}${API_PREFIX}/sessions/${encodeURIComponent(req.session_id)}/events?${params.toString()}`;
        const auth = await this.authHeaders();
        const res = await fetch(url, {
            headers: { Accept: 'text/event-stream', ...auth },
            signal: req.signal,
        });
        if (!res.ok) {
            let message = res.statusText;
            try {
                const body = (await res.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(res.status, message);
        }
        if (!res.body) {
            throw new APIError(0, 'subscribeSession: response body is null');
        }
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        let currentID = 0;
        let currentData = '';
        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done)
                    return;
                buffer += decoder.decode(value, { stream: true });
                // SSE lines are separated by \n; a blank line dispatches the event.
                const lines = buffer.split('\n');
                // The last element is an incomplete line — keep it in the buffer.
                buffer = lines.pop() ?? '';
                for (const line of lines) {
                    if (line.startsWith('id: ')) {
                        currentID = parseInt(line.slice(4), 10);
                    }
                    else if (line.startsWith('data: ')) {
                        currentData = line.slice(6);
                    }
                    else if (line === '' && currentData !== '') {
                        // Blank line after data dispatches the event.
                        const parsed = JSON.parse(currentData);
                        // Merge the SSE sequence into the envelope so that callers have a
                        // single source of truth regardless of whether sequence is also in
                        // the JSON body.
                        yield { sequence: currentID, ...parsed };
                        currentData = '';
                    }
                }
            }
        }
        finally {
            reader.cancel().catch(() => { });
        }
    }
    // inference maps to POST /api/v1/orchestrator/inference (stream=false).
    // Returns the aggregated response after the backend finishes generating.
    async inference(req) {
        return this.fetchJSON('/inference', {
            method: 'POST',
            body: JSON.stringify({ ...req, stream: false }),
        });
    }
    // inferenceStream maps to POST /api/v1/orchestrator/inference (stream=true).
    // Reads a chunked NDJSON body and yields InferenceChunk objects as they arrive.
    // Each newline-delimited JSON object carries a delta array and a final flag.
    // The iterable completes when the server sends a chunk with final=true or the
    // response body is exhausted. Callers may abort early via the optional signal.
    async *inferenceStream(req, signal) {
        const url = `${this.baseUrl}${API_PREFIX}/inference`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/x-ndjson',
                ...auth,
            },
            body: JSON.stringify({ ...req, stream: true }),
            signal,
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        if (!resp.body) {
            throw new APIError(0, 'inferenceStream: response body is null');
        }
        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buffer = '';
        try {
            while (true) {
                const { value, done } = await reader.read();
                if (done)
                    return;
                buffer += decoder.decode(value, { stream: true });
                // NDJSON: each complete JSON object is separated by a newline.
                const lines = buffer.split('\n');
                // Keep the last (potentially incomplete) line in the buffer.
                buffer = lines.pop() ?? '';
                for (const line of lines) {
                    const trimmed = line.trim();
                    if (trimmed === '')
                        continue;
                    const chunk = JSON.parse(trimmed);
                    yield chunk;
                    if (chunk.final === true)
                        return;
                }
            }
        }
        finally {
            reader.cancel().catch(() => { });
        }
    }
}
// RestDataClient implements DataClient over the QuicBaaS REST API.
// All endpoints are under /api/v1/kv/{type}[/{key}].
class RestDataClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // kvList maps to GET /api/v1/kv/{type}
    // Server response shape: {entity_type, items: [{key, value, timestamp}], total, has_more}
    async kvList(entityType) {
        const raw = await this.fetchJSON(`/api/v1/kv/${encodeURIComponent(entityType)}`);
        const source = raw.items ?? raw.entries ?? [];
        const entries = source.map((e) => ({
            key: (e.key ?? e.string_key) ?? '',
            value: e.value ?? e.data,
        }));
        return { entries };
    }
    // kvGet maps to GET /api/v1/kv/{type}/{key}
    // Server response shape: {entity_type, key, value, found, timestamp}
    async kvGet(entityType, key) {
        const raw = await this.fetchJSON(`/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`);
        return {
            key: raw.key ?? raw.string_key ?? key,
            value: raw.value ?? raw.data,
        };
    }
    // kvPut maps to PUT /api/v1/kv/{type}/{key}.
    // The server expects a {"value": <JSON>} envelope. Sending the raw value
    // directly causes the server to (mis)interpret a top-level "value" field
    // inside the user's payload as the actual stored value, silently dropping
    // the rest of the JSON. Always wrap.
    async kvPut(entityType, key, value) {
        await this.fetchJSON(`/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`, { method: 'PUT', body: JSON.stringify({ value }) });
    }
    // kvDelete maps to DELETE /api/v1/kv/{type}/{key}
    async kvDelete(entityType, key) {
        await this.fetchJSON(`/api/v1/kv/${encodeURIComponent(entityType)}/${encodeURIComponent(key)}`, { method: 'DELETE' });
    }
}
// RestVectorsClient implements VectorsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/vector/namespaces.
class RestVectorsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // listNamespaces maps to GET /api/v1/vector/namespaces.
    // Server returns {namespaces: [{namespace, dimensions, distance_metric, ...}]}
    // — translate to the SDK's {name, dimension, metric} surface.
    async listNamespaces() {
        const raw = await this.fetchJSON('/api/v1/vector/namespaces');
        return (raw.namespaces ?? []).map((ns) => ({
            name: ns.name ?? ns.namespace ?? '',
            dimension: ns.dimension ?? ns.dimensions,
            metric: ns.metric ?? ns.distance_metric,
            vector_count: ns.vector_count,
            created_at: ns.created_at,
        }));
    }
    // createNamespace maps to POST /api/v1/vector/namespaces.
    // Server expects {namespace, dimensions, distance_metric} — translate from
    // the SDK's {name, dimension, metric} surface so callers don't need to
    // know about the wire-format quirks.
    async createNamespace(req) {
        return this.fetchJSON('/api/v1/vector/namespaces', {
            method: 'POST',
            body: JSON.stringify({
                namespace: req.name,
                dimensions: req.dimension,
                distance_metric: req.metric ?? 'cosine',
            }),
        });
    }
    // deleteNamespace maps to DELETE /api/v1/vector/namespaces/{namespace}
    async deleteNamespace(namespace) {
        await this.fetchJSON(`/api/v1/vector/namespaces/${encodeURIComponent(namespace)}`, {
            method: 'DELETE',
        });
    }
    // searchSimilar maps to POST /api/v1/vector/namespaces/{namespace}/search/similar
    async searchSimilar(namespace, req) {
        const raw = await this.fetchJSON(`/api/v1/vector/namespaces/${encodeURIComponent(namespace)}/search/similar`, { method: 'POST', body: JSON.stringify(req) });
        return raw.results ?? [];
    }
}
// RestTimeSeriesClient implements TimeSeriesClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/timeseries.
class RestTimeSeriesClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // write maps to POST /api/v1/timeseries/write
    async write(point) {
        await this.fetchJSON('/api/v1/timeseries/write', {
            method: 'POST',
            body: JSON.stringify({
                metric: point.metric,
                timestamp: point.timestamp,
                value: point.value,
                tags: point.tags ?? {},
            }),
        });
    }
    // query maps to POST /api/v1/timeseries/query
    async query(req) {
        const raw = await this.fetchJSON('/api/v1/timeseries/query', { method: 'POST', body: JSON.stringify(req) });
        return { points: raw.points ?? [], count: raw.count ?? 0 };
    }
    // listMetrics maps to GET /api/v1/timeseries/metrics
    async listMetrics() {
        const raw = await this.fetchJSON('/api/v1/timeseries/metrics');
        return raw.metrics ?? [];
    }
    // deleteSeries maps to DELETE /api/v1/timeseries/series
    async deleteSeries(metric) {
        await this.fetchJSON(`/api/v1/timeseries/series?metric=${encodeURIComponent(metric)}`, {
            method: 'DELETE',
        });
    }
}
// RestObjectsClient implements ObjectsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/objects.
class RestObjectsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // listObjects maps to GET /api/v1/objects (with optional prefix query param)
    async listObjects(prefix) {
        const params = new URLSearchParams();
        if (prefix)
            params.set('prefix', prefix);
        const query = params.toString() ? `?${params.toString()}` : '';
        const raw = await this.fetchJSON(`/api/v1/objects${query}`);
        return { objects: raw.objects ?? [], cursor: raw.cursor };
    }
    // getObject maps to GET /api/v1/objects/{key} returning binary content.
    async getObject(key) {
        const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, { headers: { ...auth } });
        if (!resp.ok) {
            throw new APIError(resp.status, resp.statusText);
        }
        return resp.blob();
    }
    // putObject maps to PUT /api/v1/objects/{key} with binary body.
    async putObject(key, body, contentType = 'application/octet-stream') {
        const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            method: 'PUT',
            headers: { 'Content-Type': contentType, ...auth },
            body,
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const errBody = (await resp.json());
                message = errBody.error ?? errBody.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
    }
    // deleteObject maps to DELETE /api/v1/objects/{key}
    async deleteObject(key) {
        const url = `${this.baseUrl}/api/v1/objects/${encodeURIComponent(key)}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, { method: 'DELETE', headers: { ...auth } });
        if (!resp.ok) {
            throw new APIError(resp.status, resp.statusText);
        }
    }
}
// RestAuthClient implements AuthClient using the QuicBaaS REST auth endpoints.
class RestAuthClient {
    baseUrl;
    constructor(baseUrl) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
    }
    // authValidate POSTs to the session-validate verb with a bearer token. When
    // no token is supplied, skip the network call entirely and return false —
    // the server requires the Authorization header and 400s without it, which is
    // not an error condition, it's just "unauthenticated". Returns false on
    // 400/401 (missing or invalid token); throws APIError on any other non-2xx
    // and on a 2xx whose body does not affirmatively answer the question.
    //
    // NOT /api/v1/auth/validate. On a composed node the xkms module owns the
    // /api/v1/auth/* session plane and registers GET (not POST) at that path, so
    // this POST matched no route at all — and go-qrdb's auth middleware wraps the
    // whole router, so an unregistered method+pattern has no module auth fallback
    // and fell through to mTLS, answering 401 "qrdb/auth: no_peer_cert" for a
    // bearer the daemon had just minted (go-quicbaas#256). quicbaas's own verb is
    // IAM-namespaced to clear that collision, the same move that put renewal at
    // /api/v1/iam/refresh (#221) and PIV login at /api/v1/iam/login/piv.
    //
    // A 2xx is NOT on its own a valid session: ValidateToken answers 200 with
    // {"valid":false} when the service rejects the token (pkg/api/rest/
    // iam_login.go), so reading only the status code would report a dead session
    // as live.
    //
    // Only `valid === true` — the literal boolean — authenticates. This is an
    // authentication boundary, so it fails CLOSED: every other shape is a
    // protocol violation and throws rather than resolving either way. The
    // tempting `body.valid !== false` reads almost the same and is not: it
    // authenticates `{}`, `{"valid":"false"}` (the string, which is truthy),
    // `{"valid":null}`, and a JSON `null` body, and pairing it with a catch that
    // returns true also authenticates an empty 204, an HTML error page from an
    // intermediary, and any truncated response. Each of those is something OTHER
    // than the server affirming this session, and none of them should be
    // indistinguishable from a "yes".
    async authValidate(token) {
        if (!token)
            return false;
        const resp = await fetch(`${this.baseUrl}/api/v1/iam/validate`, {
            method: 'POST',
            credentials: 'include',
            headers: {
                'Content-Type': 'application/json',
                Authorization: `Bearer ${token}`,
            },
        });
        if (resp.status === 400 || resp.status === 401)
            return false;
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        let body;
        try {
            body = await resp.json();
        }
        catch {
            throw new APIError(resp.status, 'session validation returned an unreadable body; expected JSON with a boolean "valid"');
        }
        if (typeof body !== 'object' || body === null) {
            throw new APIError(resp.status, 'session validation returned a non-object body; expected JSON with a boolean "valid"');
        }
        // Own property only. Plain destructuring would walk the prototype chain,
        // so a page that had suffered prototype pollution (Object.prototype.valid
        // = true) would turn a 200 {} into an authenticated session — the response
        // affirming nothing while the client reads a "yes" off the environment.
        if (!Object.prototype.hasOwnProperty.call(body, 'valid')) {
            throw new APIError(resp.status, 'session validation response has no own "valid" field');
        }
        const { valid } = body;
        if (typeof valid !== 'boolean') {
            throw new APIError(resp.status, 'session validation response has no boolean "valid" field');
        }
        return valid;
    }
    async authLogin(credentials) {
        // The server's LoginRequest.Password is a *secure.Buffer populated via a
        // json.Unmarshaler that base64-decodes directly into an mlocked enclave
        // without ever materializing a Go string. Encode the password bytes as
        // base64 so the wire format matches the server decoder.
        const passwordBytes = new TextEncoder().encode(credentials.password);
        let binary = '';
        for (let i = 0; i < passwordBytes.length; i++) {
            binary += String.fromCharCode(passwordBytes[i]);
        }
        const body = {
            username: credentials.username,
            password: btoa(binary),
        };
        const resp = await fetch(`${this.baseUrl}/api/v1/auth/login`, {
            method: 'POST',
            credentials: 'include',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(body),
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
    }
    // webAuthnBeginLogin opens a unified-ceremony FIDO2 login and returns the
    // in-flight challenge (ceremony session + relying-party session + the
    // go-webauthn options envelope).
    // Maps to POST /api/v1/auth/login/begin (type=fido2). The server answers
    // every identity-resolution failure with a uniform 401 so callers cannot
    // enumerate tenants or users.
    async webAuthnBeginLogin(tenantID, email) {
        const resp = await fetch(`${this.baseUrl}/api/v1/auth/login/begin`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ type: 'fido2', hint: email, tenant_id: tenantID }),
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        const begin = (await resp.json());
        const optionsJSON = begin.challenge_metadata?.['options'];
        if (!optionsJSON) {
            throw new APIError(500, 'login/begin returned no WebAuthn challenge options');
        }
        return {
            session_id: begin.session_id,
            rp_session_id: begin.challenge_metadata?.['session_id'] ?? '',
            options: JSON.parse(optionsJSON),
        };
    }
    // webAuthnFinishLogin submits the WebAuthn assertion for an in-flight
    // challenge and returns tokens (ceremony bearer + rotating refresh
    // credential, renewable via /api/v1/iam/refresh).
    // Maps to POST /api/v1/auth/login/finish (type=fido2). The proof wire
    // mirrors the Go CLI's proveCanonicalFIDO2: the lookup key is the raw
    // bytes of the base64url credential id, the signature carries the
    // assertion JSON, and the method metadata echoes the relying-party
    // session id and credential id.
    async webAuthnFinishLogin(challenge, credential) {
        const assertionJSON = JSON.stringify(credential);
        const resp = await fetch(`${this.baseUrl}/api/v1/auth/login/finish`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
                session_id: challenge.session_id,
                type: 'fido2',
                // Go []byte fields decode standard-base64 JSON strings.
                lookup_key: bytesToBase64(utf8Bytes(credential.rawId)),
                signature: bytesToBase64(utf8Bytes(assertionJSON)),
                method_metadata: {
                    session_id: challenge.rp_session_id,
                    credential_id: credential.rawId,
                },
            }),
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        const finish = (await resp.json());
        let expiresIn = 0;
        if (finish.expires_at) {
            const deadline = Date.parse(finish.expires_at);
            if (!Number.isNaN(deadline)) {
                expiresIn = Math.max(0, Math.floor((deadline - Date.now()) / 1000));
            }
        }
        return {
            access_token: finish.bearer,
            refresh_token: finish.refresh_token ?? '',
            token_type: 'Bearer',
            expires_in: expiresIn,
        };
    }
    // loginPIV performs mTLS PIV smartcard login. credentials:'include' and
    // mode:'cors' ensure the browser presents the client certificate during
    // the TLS handshake so the server can read r.TLS.PeerCertificates[0].
    // Maps to POST /api/v1/iam/login/piv (P3.3 de-collision: moved off
    // /api/v1/auth/login/piv, which xkms's module now owns on a composed
    // node as part of its own session-login plane).
    async loginPIV() {
        const resp = await fetch(`${this.baseUrl}/api/v1/iam/login/piv`, {
            method: 'POST',
            credentials: 'include',
            mode: 'cors',
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return resp.json();
    }
    // logout POSTs to the session-revoke verb to revoke the active session token
    // on the server. IAM-namespaced for the same reason authValidate is: xkms's
    // module owns POST /api/v1/auth/logout on a composed node and redeems against
    // its OWN token store, so a quicbaas-minted session sent there was never
    // actually revoked (go-quicbaas#256). Because this method swallows every
    // failure, that miss was silent — sign-out looked clean and the bearer stayed
    // live until its TTL. Caller must clear localStorage tokens after this
    // resolves;
    // the SDK doesn't own that storage. Idempotent — server-side errors and
    // missing tokens are swallowed so the UI can use this as a clean-state
    // primitive without branching on the network outcome.
    async logout(token) {
        const headers = {
            'Content-Type': 'application/json',
            Accept: 'application/json',
        };
        if (token)
            headers.Authorization = `Bearer ${token}`;
        try {
            await fetch(`${this.baseUrl}/api/v1/iam/logout`, {
                method: 'POST',
                credentials: 'include',
                headers,
                body: JSON.stringify({}),
            });
        }
        catch {
            // Network errors during logout shouldn't block the UI from clearing
            // local session state. The token is already a one-shot credential —
            // worst case it stays valid until its TTL expires server-side.
        }
    }
    // registerBegin initiates the self-service registration WebAuthn ceremony.
    // Maps to POST /api/v1/auth/register/begin.
    async registerBegin(req) {
        const resp = await fetch(`${this.baseUrl}/api/v1/auth/register/begin`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
            body: JSON.stringify(req),
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return resp.json();
    }
    // registerFinish completes the self-service registration WebAuthn ceremony.
    // Maps to POST /api/v1/auth/register/finish.
    async registerFinish(req) {
        const resp = await fetch(`${this.baseUrl}/api/v1/auth/register/finish`, {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', Accept: 'application/json' },
            body: JSON.stringify(req),
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return resp.json();
    }
}
// RestRAGClient implements RAGClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/rag.
class RestRAGClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // query maps to POST /api/v1/rag/query
    async query(req) {
        return this.fetchJSON('/api/v1/rag/query', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // buildContext maps to POST /api/v1/rag/context
    async buildContext(req) {
        return this.fetchJSON('/api/v1/rag/context', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // listDocuments maps to GET /api/v1/rag/documents?namespace={namespace}
    async listDocuments(namespace, limit, cursor) {
        const params = new URLSearchParams({ namespace });
        if (limit !== undefined)
            params.set('limit', String(limit));
        if (cursor)
            params.set('cursor', cursor);
        const raw = await this.fetchJSON(`/api/v1/rag/documents?${params.toString()}`);
        return { documents: raw.documents ?? [], next_cursor: raw.next_cursor, total: raw.total ?? 0 };
    }
    // getDocument maps to GET /api/v1/rag/documents/{id}
    async getDocument(id) {
        return this.fetchJSON(`/api/v1/rag/documents/${id}`);
    }
    // deleteDocument maps to DELETE /api/v1/rag/documents/{id}
    async deleteDocument(id) {
        await this.fetchJSON(`/api/v1/rag/documents/${id}`, { method: 'DELETE' });
    }
    // reindexDocument maps to POST /api/v1/rag/documents/{id}/reindex
    async reindexDocument(id, req) {
        return this.fetchJSON(`/api/v1/rag/documents/${id}/reindex`, {
            method: 'POST',
            body: JSON.stringify(req ?? {}),
        });
    }
}
// RestEmbeddingsClient implements EmbeddingsClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/embedding.
class RestEmbeddingsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // generate maps to POST /api/v1/embedding/generate
    async generate(req) {
        return this.fetchJSON('/api/v1/embedding/generate', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // listProviders maps to GET /api/v1/embedding/providers
    async listProviders(req) {
        const params = new URLSearchParams();
        if (req?.include_unavailable)
            params.set('include_unavailable', 'true');
        const query = params.toString() ? `?${params.toString()}` : '';
        return this.fetchJSON(`/api/v1/embedding/providers${query}`);
    }
    // setDefaultProvider maps to PUT /api/v1/embedding/providers/default
    async setDefaultProvider(providerName) {
        await this.fetchJSON('/api/v1/embedding/providers/default', {
            method: 'PUT',
            body: JSON.stringify({ provider: providerName }),
        });
    }
    // setProviderStatus maps to PUT /api/v1/embedding/providers/{name}/status
    async setProviderStatus(name, enabled) {
        return this.fetchJSON(`/api/v1/embedding/providers/${encodeURIComponent(name)}/status`, { method: 'PUT', body: JSON.stringify({ enabled }) });
    }
}
// RestMessagingClient implements MessagingClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/messaging.
class RestMessagingClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // listChannels maps to GET /api/v1/messaging/channels
    // Server returns {channels:[], total, timestamp}; map to SDK's ListChannelsResponse shape:
    //   total_count ← total ?? total_count
    //   has_more    ← has_more (may be absent; default false)
    //   next_cursor ← next_cursor ?? cursor
    async listChannels(req) {
        const params = new URLSearchParams();
        if (req?.limit)
            params.set('limit', String(req.limit));
        if (req?.cursor)
            params.set('cursor', req.cursor);
        if (req?.name_prefix)
            params.set('name_prefix', req.name_prefix);
        const query = params.toString() ? `?${params.toString()}` : '';
        const raw = await this.fetchJSON(`/api/v1/messaging/channels${query}`);
        return {
            channels: raw.channels ?? [],
            total_count: raw.total_count ?? raw.total ?? 0,
            has_more: raw.has_more ?? false,
            next_cursor: raw.next_cursor ?? raw.cursor ?? '',
        };
    }
    // createChannel maps to POST /api/v1/messaging/channels
    async createChannel(req) {
        return this.fetchJSON('/api/v1/messaging/channels', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // deleteChannel maps to DELETE /api/v1/messaging/channels/:name
    async deleteChannel(name) {
        return this.fetchJSON(`/api/v1/messaging/channels/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
    // publish maps to POST /api/v1/messaging/publish/:channel
    async publish(req) {
        const { channel, ...body } = req;
        return this.fetchJSON(`/api/v1/messaging/publish/${encodeURIComponent(channel)}`, { method: 'POST', body: JSON.stringify(body) });
    }
}
// RestSchedulerClient implements SchedulerClient over the QuicBaaS REST API.
// Endpoints live under /api/v1/scheduler.
class RestSchedulerClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    // listJobs maps to GET /api/v1/scheduler/jobs
    // Server returns {jobs:[], has_more, timestamp}; map to full SDK ListJobsResponse shape:
    //   total_count ← total_count (absent in current daemon; derived from jobs.length)
    //   next_cursor ← next_cursor ?? cursor
    //   timestamp   ← ISO string from server
    async listJobs(req) {
        const params = new URLSearchParams();
        if (req?.status)
            params.set('status', req.status);
        if (req?.handler)
            params.set('handler', req.handler);
        if (req?.limit)
            params.set('limit', String(req.limit));
        if (req?.cursor)
            params.set('cursor', req.cursor);
        if (req?.owner_type)
            params.set('owner_type', req.owner_type);
        if (req?.owner_id)
            params.set('owner_id', req.owner_id);
        const query = params.toString() ? `?${params.toString()}` : '';
        const raw = await this.fetchJSON(`/api/v1/scheduler/jobs${query}`);
        const jobs = raw.jobs ?? [];
        return {
            jobs,
            has_more: raw.has_more ?? false,
            total_count: raw.total_count ?? jobs.length,
            next_cursor: raw.next_cursor ?? raw.cursor ?? '',
            timestamp: raw.timestamp ?? '',
        };
    }
    // createJob maps to POST /api/v1/scheduler/jobs
    async createJob(req) {
        return this.fetchJSON('/api/v1/scheduler/jobs', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // deleteJob maps to DELETE /api/v1/scheduler/jobs/:id
    async deleteJob(id) {
        return this.fetchJSON(`/api/v1/scheduler/jobs/${id}`, {
            method: 'DELETE',
        });
    }
    // triggerJob maps to POST /api/v1/scheduler/jobs/:id/trigger
    async triggerJob(id) {
        return this.fetchJSON(`/api/v1/scheduler/jobs/${id}/trigger`, {
            method: 'POST',
        });
    }
    // pauseJob maps to POST /api/v1/scheduler/jobs/:id/pause
    async pauseJob(id) {
        await this.fetchJSON(`/api/v1/scheduler/jobs/${id}/pause`, { method: 'POST' });
    }
    // resumeJob maps to POST /api/v1/scheduler/jobs/:id/resume
    async resumeJob(id) {
        await this.fetchJSON(`/api/v1/scheduler/jobs/${id}/resume`, { method: 'POST' });
    }
    // updateJob maps to PUT /api/v1/scheduler/jobs/:id
    async updateJob(id, req) {
        return this.fetchJSON(`/api/v1/scheduler/jobs/${id}`, {
            method: 'PUT',
            body: JSON.stringify(req),
        });
    }
    // getStatus maps to GET /api/v1/scheduler/status
    async getStatus() {
        return this.fetchJSON('/api/v1/scheduler/status');
    }
    // registerNode maps to POST /api/v1/scheduler/nodes
    async registerNode(req) {
        return this.fetchJSON('/api/v1/scheduler/nodes', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // renewNode maps to POST /api/v1/scheduler/nodes/:id/renew
    async renewNode(nodeId) {
        await this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/renew`, { method: 'POST' });
    }
    // deregisterNode maps to DELETE /api/v1/scheduler/nodes/:id
    async deregisterNode(nodeId) {
        await this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}`, { method: 'DELETE' });
    }
    // getNode maps to GET /api/v1/scheduler/nodes/:id
    async getNode(nodeId) {
        return this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}`);
    }
    // listNodes maps to GET /api/v1/scheduler/nodes
    async listNodes() {
        const body = await this.fetchJSON('/api/v1/scheduler/nodes');
        return body.nodes ?? [];
    }
    // cordonNode maps to POST /api/v1/scheduler/nodes/:id/cordon
    async cordonNode(nodeId) {
        return this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/cordon`, {
            method: 'POST',
        });
    }
    // uncordonNode maps to POST /api/v1/scheduler/nodes/:id/uncordon
    async uncordonNode(nodeId) {
        return this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/uncordon`, {
            method: 'POST',
        });
    }
    // drainNode maps to POST /api/v1/scheduler/nodes/:id/drain
    async drainNode(nodeId) {
        const body = await this.fetchJSON(`/api/v1/scheduler/nodes/${encodeURIComponent(nodeId)}/drain`, { method: 'POST' });
        return body.placements ?? [];
    }
    // place maps to POST /api/v1/scheduler/placements
    async place(req) {
        return this.fetchJSON('/api/v1/scheduler/placements', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // release maps to DELETE /api/v1/scheduler/placements/:id
    async release(workloadId) {
        await this.fetchJSON(`/api/v1/scheduler/placements/${encodeURIComponent(workloadId)}`, {
            method: 'DELETE',
        });
    }
    // getPlacement maps to GET /api/v1/scheduler/placements/:id
    async getPlacement(workloadId) {
        return this.fetchJSON(`/api/v1/scheduler/placements/${encodeURIComponent(workloadId)}`);
    }
    // listPlacements maps to GET /api/v1/scheduler/placements
    async listPlacements(filter = {}) {
        const params = new URLSearchParams();
        if (filter.node_id)
            params.set('node_id', filter.node_id);
        if (filter.group)
            params.set('group', filter.group);
        const query = params.toString() ? `?${params.toString()}` : '';
        const body = await this.fetchJSON(`/api/v1/scheduler/placements${query}`);
        return body.placements ?? [];
    }
}
// RestProjectsClient implements ProjectsClient over the QuicBaaS REST project endpoints.
// All project routes live under /api/v1/projects.
export class RestProjectsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async fetchJSON(path, init = {}) {
        const headers = { 'Content-Type': 'application/json' };
        // resolveToken, not the raw config value: the SPA supplies a token FACTORY
        // so every request re-reads the live access token. Interpolating the config
        // value directly stringified the function itself into the header, and the
        // 401 it earned drove the auth store into a session-ending renewal.
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: { ...headers, ...(init.headers ?? {}) },
        });
        if (!resp.ok) {
            const text = await resp.text().catch(() => resp.statusText);
            throw new APIError(resp.status, text);
        }
        return parseResponseBody(resp);
    }
    // createProject maps to POST /api/v1/projects
    async createProject(req) {
        return this.fetchJSON('/api/v1/projects', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // getProject maps to GET /api/v1/projects/:projectId?tenant_id=...
    async getProject(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/projects/${encodeURIComponent(projectId)}?${query}`);
    }
    // updateProject maps to PUT /api/v1/projects/:projectId?tenant_id=...
    //
    // The tenant goes in the QUERY, not only in the body: the handler derives
    // the tenant and refuses before it reads the body at all
    // (pkg/api/rest/handlers_project.go). Without it every save from the SPA's
    // edit dialog came back 400 — the project edit flow was simply broken.
    async updateProject(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/api/v1/projects/${encodeURIComponent(req.project_id)}?${query}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    // deleteProject maps to DELETE /api/v1/projects/:projectId?tenant_id=...
    async deleteProject(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/projects/${encodeURIComponent(projectId)}?${query}`, { method: 'DELETE' });
    }
    // listProjects maps to GET /api/v1/projects?tenant_id=...
    async listProjects(tenantId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/projects?${query}`);
    }
    // listProjectsPage maps to GET /api/v1/projects with the shared paging
    // parameters, and hands back the envelope the server answered with.
    async listProjectsPage(tenantId, page) {
        const params = new URLSearchParams({ tenant_id: tenantId });
        appendListParam(params, 'autonomous', page?.autonomous);
        appendPageParams(params, page);
        const raw = await this.fetchJSON(`/api/v1/projects?${params.toString()}`);
        return adoptPage(raw, 'projects');
    }
    // grantFleetNodeRole maps to PUT /api/v1/projects/:projectId/permissions/:agentId
    async grantFleetNodeRole(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.node_id)}?${query}`, { method: 'PUT', body: JSON.stringify({ role: req.role }) });
    }
    // revokeFleetNodeRole maps to DELETE /api/v1/projects/:projectId/permissions/:agentId
    async revokeFleetNodeRole(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/api/v1/projects/${encodeURIComponent(req.project_id)}/permissions/${encodeURIComponent(req.node_id)}?${query}`, { method: 'DELETE' });
    }
}
// RestSdlcClient implements SdlcClient over the SDLC module's REST endpoints:
// a project's forge binding and a task's delivery.
export class RestSdlcClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async fetchJSON(path, init = {}) {
        const headers = { 'Content-Type': 'application/json' };
        // resolveToken, not the raw config value: the SPA supplies a token FACTORY
        // so every request re-reads the live access token. Interpolating the config
        // value directly stringified the function itself into the header, and the
        // 401 it earned drove the auth store into a session-ending renewal.
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: { ...headers, ...(init.headers ?? {}) },
        });
        if (!resp.ok) {
            const text = await resp.text().catch(() => resp.statusText);
            throw new APIError(resp.status, text);
        }
        return parseResponseBody(resp);
    }
    bindingPath(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/binding?${query}`;
    }
    // forges maps to GET /api/v1/sdlc/forges
    // The endpoint returns {"forges": [...]}; unwrap to a bare slice.
    async forges() {
        const resp = await this.fetchJSON('/api/v1/sdlc/forges');
        if (Array.isArray(resp)) {
            return resp;
        }
        return resp.forges ?? [];
    }
    // bind maps to PUT /api/v1/sdlc/projects/:projectId/binding?tenant_id=...[&paused=true]
    async bind(tenantId, projectId, binding, paused) {
        const path = this.bindingPath(tenantId, projectId) + (paused ? '&paused=true' : '');
        return this.fetchJSON(path, {
            method: 'PUT',
            body: JSON.stringify({ ...binding, project_id: binding.project_id || projectId }),
        });
    }
    // unbind maps to DELETE /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
    async unbind(tenantId, projectId) {
        await this.fetchJSON(this.bindingPath(tenantId, projectId), { method: 'DELETE' });
    }
    // binding maps to GET /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
    async binding(tenantId, projectId) {
        return this.fetchJSON(this.bindingPath(tenantId, projectId));
    }
    // delivery maps to GET /api/v1/sdlc/tasks/:taskId/delivery?tenant_id=...
    async delivery(tenantId, taskId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/tasks/${encodeURIComponent(taskId)}/delivery?${query}`);
    }
    // onboard maps to POST /api/v1/sdlc/projects/:projectId/onboarding
    // A POST, because it writes to the forge. The read is a different route so
    // that asking what state a repository is in cannot change it.
    async onboard(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/onboarding?${query}`, { method: 'POST' });
    }
    // conformance maps to GET /api/v1/sdlc/projects/:projectId/conformance
    async conformance(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/conformance?${query}`);
    }
    // openWork maps to GET /api/v1/sdlc/projects/:projectId/open-work
    async openWork(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/open-work?${query}`);
    }
    reviewerPath(tenantId, projectId, pullRequest) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return `/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/pulls/${encodeURIComponent(String(pullRequest))}/reviewer?${query}`;
    }
    // reviewer maps to GET /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
    async reviewer(tenantId, projectId, pullRequest) {
        return this.fetchJSON(this.reviewerPath(tenantId, projectId, pullRequest));
    }
    // setReviewer maps to PUT /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
    // The body carries the profile alone: the path names the project and the
    // pull request.
    async setReviewer(tenantId, projectId, pullRequest, profile) {
        return this.fetchJSON(this.reviewerPath(tenantId, projectId, pullRequest), {
            method: 'PUT',
            body: JSON.stringify({ profile }),
        });
    }
    // clearReviewer maps to DELETE /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer
    // The server answers the selection that now holds, not 204.
    async clearReviewer(tenantId, projectId, pullRequest) {
        return this.fetchJSON(this.reviewerPath(tenantId, projectId, pullRequest), {
            method: 'DELETE',
        });
    }
    // fileIssue maps to POST /api/v1/sdlc/projects/:projectId/issues
    // The body carries the filing alone: the path names the project.
    async fileIssue(tenantId, projectId, req) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/issues?${query}`, {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // collaborators maps to GET /api/v1/sdlc/projects/:projectId/collaborators
    async collaborators(tenantId, projectId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/collaborators?${query}`);
    }
    // adoptIssue maps to POST /api/v1/sdlc/projects/:projectId/adopt
    // The path names the project; the body carries the issue, the pull request
    // and the ruling.
    async adoptIssue(tenantId, projectId, request) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/sdlc/projects/${encodeURIComponent(projectId)}/adopt?${query}`, {
            method: 'POST',
            body: JSON.stringify(request),
        });
    }
}
// RestAccountsClient implements AccountsClient over the QuicBaaS REST pooled
// provider account endpoints. All account routes live under
// /api/v1/agent-accounts. See handlers_account.go for the server-side shapes
// this mirrors — AccountRESTResponse never carries credential material, so
// no method here can either.
export class RestAccountsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async fetchJSON(path, init = {}) {
        const headers = { 'Content-Type': 'application/json' };
        // resolveToken, not the raw config value: the SPA supplies a token FACTORY
        // so every request re-reads the live access token. Interpolating the config
        // value directly stringified the function itself into the header, and the
        // 401 it earned drove the auth store into a session-ending renewal.
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: { ...headers, ...(init.headers ?? {}) },
        });
        if (!resp.ok) {
            const text = await resp.text().catch(() => resp.statusText);
            throw new APIError(resp.status, text);
        }
        return parseResponseBody(resp);
    }
    // createAccount maps to POST /api/v1/agent-accounts
    async createAccount(req) {
        return this.fetchJSON('/api/v1/agent-accounts', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // getAccount maps to GET /api/v1/agent-accounts/:accountId?tenant_id=...
    async getAccount(tenantId, accountId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`);
    }
    // listAccounts maps to GET /api/v1/agent-accounts?tenant_id=...&provider=...&project_id=...
    async listAccounts(req) {
        const params = { tenant_id: req.tenant_id };
        if (req.provider)
            params.provider = req.provider;
        if (req.project_id)
            params.project_id = req.project_id;
        const query = new URLSearchParams(params).toString();
        return this.fetchJSON(`/api/v1/agent-accounts?${query}`);
    }
    // listAccountsPage maps to GET /api/v1/agent-accounts with the shared
    // paging parameters and the accounts listing's own filters.
    async listAccountsPage(req) {
        const params = new URLSearchParams({ tenant_id: req.tenant_id });
        appendListParam(params, 'provider', req.provider);
        appendListParam(params, 'project_id', req.project_id);
        appendListParam(params, 'status', req.status);
        appendPageParams(params, req);
        const raw = await this.fetchJSON(`/api/v1/agent-accounts?${params.toString()}`);
        return adoptPage(raw, 'accounts');
    }
    // updateAccount maps to PUT /api/v1/agent-accounts/:accountId?tenant_id=...
    async updateAccount(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        const { tenant_id: _tenantId, account_id: accountId, ...body } = req;
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`, { method: 'PUT', body: JSON.stringify(body) });
    }
    // deleteAccount maps to DELETE /api/v1/agent-accounts/:accountId?tenant_id=...
    async deleteAccount(tenantId, accountId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}?${query}`, { method: 'DELETE' });
    }
    // rotateAccountCredential maps to POST /api/v1/agent-accounts/:accountId/rotate?tenant_id=...
    async rotateAccountCredential(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(req.account_id)}/rotate?${query}`, { method: 'POST', body: JSON.stringify({ credential: req.credential }) });
    }
    // revokeAccount maps to POST /api/v1/agent-accounts/:accountId/revoke?tenant_id=...
    async revokeAccount(req) {
        const query = new URLSearchParams({ tenant_id: req.tenant_id }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(req.account_id)}/revoke?${query}`, { method: 'POST', body: JSON.stringify({ reason: req.reason }) });
    }
    // setDefaultAccount maps to POST /api/v1/agent-accounts/:accountId/default?tenant_id=...
    async setDefaultAccount(tenantId, accountId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}/default?${query}`, { method: 'POST' });
    }
    // getAccountUsage maps to GET /api/v1/agent-accounts/:accountId/usage?tenant_id=...
    async getAccountUsage(tenantId, accountId) {
        const query = new URLSearchParams({ tenant_id: tenantId }).toString();
        return this.fetchJSON(`/api/v1/agent-accounts/${encodeURIComponent(accountId)}/usage?${query}`);
    }
}
// RestAgentsClient implements AgentsClient over the tenant AI agent catalog:
// /api/v1/agents and /api/v1/agent-pools. See handlers_agents.go for the
// server-side shapes this mirrors. Every route takes ?tenant_id=; without it
// the server answers for the caller's own tenant.
class RestAgentsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async fetchJSON(path, tenantId, init = {}) {
        const headers = { 'Content-Type': 'application/json' };
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        const query = tenantId ? `?${new URLSearchParams({ tenant_id: tenantId }).toString()}` : '';
        const resp = await fetch(`${this.baseUrl}${path}${query}`, {
            ...init,
            headers: { ...headers, ...(init.headers ?? {}) },
        });
        if (!resp.ok) {
            const text = await resp.text().catch(() => resp.statusText);
            throw new APIError(resp.status, text);
        }
        return parseResponseBody(resp);
    }
    async listAgents(tenantId) {
        const body = await this.fetchJSON('/api/v1/agents', tenantId);
        return body.agents ?? [];
    }
    async getAgent(agentId, tenantId) {
        return this.fetchJSON(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId);
    }
    async createAgent(agent, tenantId) {
        return this.fetchJSON('/api/v1/agents', tenantId, { method: 'POST', body: JSON.stringify(agent) });
    }
    async updateAgent(agentId, agent, tenantId) {
        return this.fetchJSON(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId, {
            method: 'PUT',
            body: JSON.stringify(agent),
        });
    }
    async deleteAgent(agentId, tenantId) {
        await this.fetchJSON(`/api/v1/agents/${encodeURIComponent(agentId)}`, tenantId, { method: 'DELETE' });
    }
    async agentUsedBy(agentId, tenantId) {
        const body = await this.fetchJSON(`/api/v1/agents/${encodeURIComponent(agentId)}/used-by`, tenantId);
        return body.used_by ?? [];
    }
    async listPools(tenantId) {
        const body = await this.fetchJSON('/api/v1/agent-pools', tenantId);
        return body.pools ?? [];
    }
    async getPool(poolId, tenantId) {
        return this.fetchJSON(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId);
    }
    async createPool(pool, tenantId) {
        return this.fetchJSON('/api/v1/agent-pools', tenantId, { method: 'POST', body: JSON.stringify(pool) });
    }
    async updatePool(poolId, pool, tenantId) {
        return this.fetchJSON(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId, {
            method: 'PUT',
            body: JSON.stringify(pool),
        });
    }
    async deletePool(poolId, tenantId) {
        await this.fetchJSON(`/api/v1/agent-pools/${encodeURIComponent(poolId)}`, tenantId, { method: 'DELETE' });
    }
    async poolUsedBy(poolId, tenantId) {
        const body = await this.fetchJSON(`/api/v1/agent-pools/${encodeURIComponent(poolId)}/used-by`, tenantId);
        return body.used_by ?? [];
    }
}
// RestIAMClient implements IAMClient over the QuicBaaS REST IAM endpoints.
// Paths: /api/v1/tenants/{tenantID}/users|roles|policies and /api/v1/tenants
class RestIAMClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    async listUsers(tenantID) {
        // Server returns {limit, offset, users:[{id, email, name, tenant_id, created, updated, ...}]}.
        // The wire fields differ from the IAMUser SDK type:
        //   name      → username (display name; used as the human-readable identifier)
        //   created   → created_at
        //   updated   → updated_at
        //   role      → roles[] (some responses send a single "role" string)
        // Absent fields are normalised to safe defaults so the UI never renders undefined.
        const raw = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users`);
        const users = (raw.users ?? []).map((u) => ({
            id: u.id ?? '',
            tenant_id: u.tenant_id ?? tenantID,
            // Prefer an explicit username field; fall back to the display name field.
            username: u.username ?? u.name ?? u.email ?? '',
            email: u.email ?? '',
            lifecycle: u.lifecycle,
            roles: u.roles ?? (u.role ? [u.role] : []),
            created_at: u.created_at ?? u.created ?? '',
            updated_at: u.updated_at ?? u.updated ?? '',
        }));
        return { users, count: raw.count ?? users.length };
    }
    async createUser(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users`, { method: 'POST', body: JSON.stringify(req) });
    }
    async deleteUser(tenantID, userID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`, { method: 'DELETE' });
    }
    async listTenants() {
        // Server returns {limit, offset, tenants:[{id, name, created, updated, ...}]}.
        // Wire fields differ from IAMTenant:
        //   created → created_at
        //   updated → updated_at
        const raw = await this.fetchJSON('/api/v1/tenants');
        const tenants = (raw.tenants ?? []).map((t) => ({
            id: t.id ?? '',
            name: t.name ?? '',
            description: t.description ?? '',
            created_at: t.created_at ?? t.created ?? '',
            updated_at: t.updated_at ?? t.updated ?? '',
        }));
        return { tenants, count: raw.count ?? tenants.length };
    }
    async createTenant(req) {
        // The server's POST /api/v1/tenants validates an `id` field as the
        // immutable tenant identifier. The SDK surface keeps a single `name`
        // (the human-friendly label) so callers don't have to think about both.
        // We derive a kebab-case slug from `name` for the wire `id` when the
        // caller doesn't specify one explicitly.
        const reqWithID = req;
        const id = reqWithID.id ?? req.name.trim().toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
        return this.fetchJSON('/api/v1/tenants', {
            method: 'POST',
            body: JSON.stringify({ id, name: req.name, description: req.description }),
        });
    }
    async updateUser(tenantID, userID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    async assignRoleToUser(tenantID, userID, role) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/roles`, 
        // Wire field is "role_id" — the handler decodes this and passes it directly
        // to IdentityStore.AssignRole which keys on the role UUID, not the name.
        { method: 'POST', body: JSON.stringify({ role_id: role }) });
    }
    async revokeRoleFromUser(tenantID, userID, role) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/roles/${encodeURIComponent(role)}`, { method: 'DELETE' });
    }
    async listSessions(opts) {
        const params = new URLSearchParams();
        if (opts?.user_id)
            params.set('user_id', opts.user_id);
        if (opts?.tenant_id)
            params.set('tenant_id', opts.tenant_id);
        const qs = params.toString();
        return this.fetchJSON(`/api/v1/iam/sessions${qs ? `?${qs}` : ''}`);
    }
    async getSession(sessionID) {
        return this.fetchJSON(`/api/v1/iam/sessions/${encodeURIComponent(sessionID)}`);
    }
    async revokeSession(sessionID) {
        await this.fetchJSON(`/api/v1/iam/sessions/${encodeURIComponent(sessionID)}`, { method: 'DELETE' });
    }
    async revokeAllSessions(opts) {
        const params = new URLSearchParams();
        if (opts?.except)
            params.set('except', opts.except);
        if (opts?.user_id)
            params.set('user_id', opts.user_id);
        if (opts?.tenant_id)
            params.set('tenant_id', opts.tenant_id);
        const qs = params.toString();
        await this.fetchJSON(`/api/v1/iam/sessions${qs ? `?${qs}` : ''}`, { method: 'DELETE' });
    }
    async revokeOtherSessions(keepSessionID) {
        return this.fetchJSON('/api/v1/iam/sessions/revoke-others', { method: 'POST', body: JSON.stringify({ keep_session_id: keepSessionID }) });
    }
    userPath(tenantID, userID) {
        return `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`;
    }
    // Maps to GET /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/
    async listWebAuthnCredentials(tenantID, userID) {
        return this.fetchJSON(`${this.userPath(tenantID, userID)}/webauthn/credentials/`);
    }
    // Maps to DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/
    async deleteAllWebAuthnCredentials(tenantID, userID) {
        return this.fetchJSON(`${this.userPath(tenantID, userID)}/webauthn/credentials/`, { method: 'DELETE' });
    }
    // Maps to DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/webauthn/credentials/{credential_id}
    async deleteWebAuthnCredential(tenantID, userID, credentialID) {
        await this.fetchJSON(`${this.userPath(tenantID, userID)}/webauthn/credentials/${encodeURIComponent(credentialID)}`, { method: 'DELETE' });
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/links/
    async createAccountLink(tenantID, userID, req) {
        return this.fetchJSON(`${this.userPath(tenantID, userID)}/links/`, {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // Maps to GET /api/v1/tenants/{tenant_id}/users/{user_id}/links/
    async listAccountLinks(tenantID, userID, opts) {
        const params = new URLSearchParams();
        if (opts?.limit !== undefined)
            params.set('limit', String(opts.limit));
        if (opts?.offset !== undefined)
            params.set('offset', String(opts.offset));
        const qs = params.toString();
        return this.fetchJSON(`${this.userPath(tenantID, userID)}/links/${qs ? `?${qs}` : ''}`);
    }
    // Maps to DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/links/{provider_id}
    async deleteAccountLink(tenantID, userID, providerID) {
        await this.fetchJSON(`${this.userPath(tenantID, userID)}/links/${encodeURIComponent(providerID)}`, {
            method: 'DELETE',
        });
    }
    mfaPath(tenantID, userID) {
        return `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/mfa`;
    }
    // Maps to GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/
    async getMFAStatus(tenantID, userID) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/`);
    }
    // Maps to DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/
    async disableMFA(tenantID, userID) {
        await this.fetchJSON(`${this.mfaPath(tenantID, userID)}/`, { method: 'DELETE' });
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll
    async beginMFAEnrollment(tenantID, userID, method) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/enroll`, { method: 'POST', body: JSON.stringify(method ? { method } : {}) });
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify
    async completeMFAEnrollment(tenantID, userID, enrollmentID, code) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/verify`, { method: 'POST', body: JSON.stringify({ enrollment_id: enrollmentID, code }) });
    }
    // Maps to DELETE /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/enroll/{enrollment_id}
    async cancelMFAEnrollment(tenantID, userID, enrollmentID) {
        await this.fetchJSON(`${this.mfaPath(tenantID, userID)}/enroll/${encodeURIComponent(enrollmentID)}`, { method: 'DELETE' });
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/challenge
    async beginMFAChallenge(tenantID, userID, sessionToken) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/challenge`, { method: 'POST', body: JSON.stringify({ session_token: sessionToken }) });
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/verify-challenge
    async verifyMFAChallenge(tenantID, userID, challengeID, code) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/verify-challenge`, { method: 'POST', body: JSON.stringify({ challenge_id: challengeID, code }) });
    }
    // Maps to GET /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
    async getBackupCodeStatus(tenantID, userID) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/backup-codes`);
    }
    // Maps to POST /api/v1/tenants/{tenant_id}/users/{user_id}/mfa/backup-codes
    async generateBackupCodes(tenantID, userID) {
        return this.fetchJSON(`${this.mfaPath(tenantID, userID)}/backup-codes`, { method: 'POST' });
    }
    async updateTenant(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    async deleteTenant(tenantID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}`, { method: 'DELETE' });
    }
    // Maps to GET /api/v1/tenants/{tenantID}
    async getTenant(tenantID) {
        // Same wire shape as a listTenants element: created/updated → *_at.
        const t = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}`);
        return {
            id: t.id ?? '',
            name: t.name ?? '',
            description: t.description ?? '',
            created_at: t.created_at ?? t.created ?? '',
            updated_at: t.updated_at ?? t.updated ?? '',
        };
    }
    // Maps to GET /api/v1/tenants/{tenantID}/users/{userID}
    async getUser(tenantID, userID) {
        // Same wire shape as a listUsers element; normalised the same way.
        const u = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}`);
        return {
            id: u.id ?? '',
            tenant_id: u.tenant_id ?? tenantID,
            username: u.username ?? u.name ?? u.email ?? '',
            email: u.email ?? '',
            lifecycle: u.lifecycle,
            roles: u.roles ?? (u.role ? [u.role] : []),
            created_at: u.created_at ?? u.created ?? '',
            updated_at: u.updated_at ?? u.updated ?? '',
        };
    }
    // Maps to GET /api/v1/tenants/{tenantID}/users/{userID}/sessions[?limit=N]
    async listUserSessions(tenantID, userID, limit) {
        const qs = limit !== undefined && limit > 0 ? `?limit=${limit}` : '';
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/sessions${qs}`);
    }
    // Maps to DELETE /api/v1/tenants/{tenantID}/users/{userID}/sessions
    async revokeAllUserSessions(tenantID, userID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/sessions`, { method: 'DELETE' });
    }
    // Maps to GET /api/v1/tenants/{tenantID}/apps/?limit=&offset=
    async listApps(tenantID, opts) {
        const params = new URLSearchParams();
        if (opts?.limit !== undefined)
            params.set('limit', String(opts.limit));
        if (opts?.offset !== undefined)
            params.set('offset', String(opts.offset));
        const qs = params.toString();
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/${qs ? `?${qs}` : ''}`);
    }
    // Maps to POST /api/v1/tenants/{tenantID}/apps/ (201)
    async registerApp(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/`, { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to GET /api/v1/tenants/{tenantID}/apps/{clientID}
    async getApp(tenantID, clientID) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/${encodeURIComponent(clientID)}`);
    }
    // Maps to PUT /api/v1/tenants/{tenantID}/apps/{clientID}
    async updateApp(tenantID, clientID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/${encodeURIComponent(clientID)}`, { method: 'PUT', body: JSON.stringify({ ...req, client_id: clientID }) });
    }
    // Maps to DELETE /api/v1/tenants/{tenantID}/apps/{clientID} (204)
    async deleteApp(tenantID, clientID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/${encodeURIComponent(clientID)}`, { method: 'DELETE' });
    }
    // Maps to POST /api/v1/tenants/{tenantID}/apps/{clientID}/rotate-secret
    async rotateAppSecret(tenantID, clientID) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/apps/${encodeURIComponent(clientID)}/rotate-secret`, { method: 'POST' });
    }
    // Maps to PATCH /api/v1/tenants/{tenantID}/quotas (204)
    async updateTenantQuotas(tenantID, quotas) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/quotas`, { method: 'PATCH', body: JSON.stringify(quotas) });
    }
    // Maps to PATCH /api/v1/tenants/{tenantID}/entitlements (204)
    async updateTenantEntitlements(tenantID, entitlements) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/entitlements`, { method: 'PATCH', body: JSON.stringify(entitlements) });
    }
    // Maps to POST /api/v1/tenants/{tenantID}/provision (201)
    async provisionTenant(req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(req.tenant_id)}/provision`, { method: 'POST', body: JSON.stringify(req) });
    }
    async listRoles(tenantID) {
        // Server returns {limit, offset, roles:[...]} — count field absent; derive it.
        const raw = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles`);
        const roles = raw.roles ?? [];
        return { roles, count: raw.count ?? roles.length };
    }
    async reconcileRoles(tenantID) {
        // Server returns {tenant_id, roles:[...], changed} — roles is omitted when
        // nothing drifted; normalize it to an array so callers can iterate freely.
        const raw = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/reconcile`, { method: 'POST' });
        const roles = raw.roles ?? [];
        return { tenant_id: raw.tenant_id ?? tenantID, roles, changed: raw.changed ?? roles.length > 0 };
    }
    async createRole(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles`, { method: 'POST', body: JSON.stringify(req) });
    }
    async updateRole(tenantID, roleID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    // Maps to GET /api/v1/iam/audit?tenant_id=&event_type=&category=&outcome=&actor_id=&resource_id=&start_time=&end_time=&limit=&offset=
    async listAuditEvents(opts) {
        const q = [];
        const add = (name, v) => {
            if (v !== undefined)
                q.push(`${name}=${encodeURIComponent(String(v))}`);
        };
        add('tenant_id', opts?.tenantID);
        add('event_type', opts?.eventType);
        add('category', opts?.category);
        add('outcome', opts?.outcome);
        add('actor_id', opts?.actorID);
        add('resource_id', opts?.resourceID);
        add('start_time', opts?.startTime);
        add('end_time', opts?.endTime);
        add('limit', opts?.limit);
        add('offset', opts?.offset);
        const qs = q.length > 0 ? `?${q.join('&')}` : '';
        return this.fetchJSON(`/api/v1/iam/audit${qs}`);
    }
    // Maps to GET /api/v1/iam/audit/{id}?tenant_id=
    async getAuditEvent(eventID, tenantID) {
        const qs = tenantID !== undefined ? `?tenant_id=${encodeURIComponent(tenantID)}` : '';
        return this.fetchJSON(`/api/v1/iam/audit/${encodeURIComponent(eventID)}${qs}`);
    }
    // Maps to POST /api/v1/iam/audit
    async recordAuditEvent(req) {
        return this.fetchJSON('/api/v1/iam/audit', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // Maps to DELETE /api/v1/iam/audit?before=&tenant_id=
    async deleteAuditEventsBefore(before, tenantID) {
        let qs = `?before=${encodeURIComponent(before)}`;
        if (tenantID !== undefined)
            qs += `&tenant_id=${encodeURIComponent(tenantID)}`;
        return this.fetchJSON(`/api/v1/iam/audit${qs}`, {
            method: 'DELETE',
        });
    }
    // Maps to GET /api/v1/admin/impersonation/?active=true&admin_user_id=
    async listImpersonations(opts) {
        const q = [];
        if (opts?.activeOnly === true)
            q.push('active=true');
        if (opts?.adminUserID !== undefined)
            q.push(`admin_user_id=${encodeURIComponent(opts.adminUserID)}`);
        const qs = q.length > 0 ? `?${q.join('&')}` : '';
        return this.fetchJSON(`/api/v1/admin/impersonation/${qs}`);
    }
    // Maps to POST /api/v1/admin/impersonation/start
    async startImpersonation(req) {
        return this.fetchJSON('/api/v1/admin/impersonation/start', { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to POST /api/v1/admin/impersonation/{sessionID}/end
    async endImpersonation(sessionID) {
        return this.fetchJSON(`/api/v1/admin/impersonation/${encodeURIComponent(sessionID)}/end`, { method: 'POST' });
    }
    // Maps to GET /api/v1/admin/users/{userID}/impersonations
    async listUserImpersonations(userID) {
        return this.fetchJSON(`/api/v1/admin/users/${encodeURIComponent(userID)}/impersonations`);
    }
    // Maps to GET /api/v1/tenants/{tenantID}/roles/{roleID}
    async getRole(tenantID, roleID) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`);
    }
    // Maps to POST /api/v1/tenants/{tenantID}/providers/
    async registerProvider(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/providers/`, { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to GET /api/v1/tenants/{tenantID}/providers/?limit=&offset=&type=
    async listProviders(tenantID, opts) {
        const q = [];
        if (opts?.limit !== undefined)
            q.push(`limit=${encodeURIComponent(String(opts.limit))}`);
        if (opts?.offset !== undefined)
            q.push(`offset=${encodeURIComponent(String(opts.offset))}`);
        if (opts?.type !== undefined)
            q.push(`type=${encodeURIComponent(opts.type)}`);
        const qs = q.length > 0 ? `?${q.join('&')}` : '';
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/providers/${qs}`);
    }
    // Maps to GET /api/v1/tenants/{tenantID}/providers/{providerID}
    async getProvider(tenantID, providerID) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/providers/${encodeURIComponent(providerID)}`);
    }
    // Maps to PUT /api/v1/tenants/{tenantID}/providers/{providerID}
    async updateProvider(tenantID, providerID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/providers/${encodeURIComponent(providerID)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    // Maps to DELETE /api/v1/tenants/{tenantID}/providers/{providerID}
    async deleteProvider(tenantID, providerID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/providers/${encodeURIComponent(providerID)}`, { method: 'DELETE' });
    }
    async deleteRole(tenantID, roleID) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/roles/${encodeURIComponent(roleID)}`, { method: 'DELETE' });
    }
    async listPolicies(tenantID) {
        // Server returns {limit, offset, policies:[...]} — count field absent; derive it.
        const raw = await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies`);
        const policies = raw.policies ?? [];
        return { policies, count: raw.count ?? policies.length };
    }
    async createPolicy(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies`, { method: 'POST', body: JSON.stringify(req) });
    }
    async updatePolicy(tenantID, policyName, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/${encodeURIComponent(policyName)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    async deletePolicy(tenantID, policyName) {
        await this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/${encodeURIComponent(policyName)}`, { method: 'DELETE' });
    }
    // Maps to GET /api/v1/tenants/{tenantID}/policies/{policyName}
    async getPolicy(tenantID, policyName) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/${encodeURIComponent(policyName)}`);
    }
    // Maps to POST /api/v1/tenants/{tenantID}/policies/validate[?name=]
    async validatePolicy(tenantID, req, name) {
        const query = name !== undefined ? `?name=${encodeURIComponent(name)}` : '';
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/validate${query}`, { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to POST /api/v1/tenants/{tenantID}/policies/test
    async testPolicy(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/test`, { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to POST /api/v1/tenants/{tenantID}/policies/simulate
    async simulatePolicy(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/simulate`, { method: 'POST', body: JSON.stringify(req) });
    }
    // Maps to POST /api/v1/tenants/{tenantID}/policies/effective-permissions
    async getEffectivePermissions(tenantID, req) {
        return this.fetchJSON(`/api/v1/tenants/${encodeURIComponent(tenantID)}/policies/effective-permissions`, { method: 'POST', body: JSON.stringify(req) });
    }
    // THE TENANT IS A PATH SEGMENT, and it is not optional. Invitations live in
    // their tenant's own store, so a token issued for one tenant is simply absent
    // from another — isolation is structural rather than a check that could be
    // forgotten. These methods called tenant-less paths that the server never
    // registered, so every one of them 404'd; on a server whose authenticator
    // answers before routing resolves, that surfaced as an auth error and the
    // browser invite flow had never worked at all.
    async getInvite(tenantID, token) {
        return this.fetchJSON(`/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}`, { method: 'GET' });
    }
    async activateFIDO2Begin(tenantID, token) {
        return this.fetchJSON(`/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/fido2/begin`, { method: 'POST' });
    }
    async activateFIDO2Finish(tenantID, token, credential) {
        return this.fetchJSON(`/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/fido2/finish`, { method: 'POST', body: JSON.stringify({ credential }) });
    }
    async activatePIV(tenantID, token, certPEM) {
        return this.fetchJSON(`/api/v1/iam/${encodeURIComponent(tenantID)}/invites/${encodeURIComponent(token)}/activate/piv`, { method: 'POST', body: JSON.stringify({ cert_pem: certPEM }) });
    }
    // gdprPath is the GDPR route prefix of one user in one tenant.
    gdprPath(tenantID, userID) {
        return `/api/v1/tenants/${encodeURIComponent(tenantID)}/users/${encodeURIComponent(userID)}/gdpr`;
    }
    async grantConsent(tenantID, userID, req) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/consent`, {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async listUserConsents(tenantID, userID) {
        const consents = await this.fetchJSON(`${this.gdprPath(tenantID, userID)}/consents`);
        return consents ?? [];
    }
    async getConsent(tenantID, userID, purpose) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/consent/${encodeURIComponent(purpose)}`);
    }
    async withdrawConsent(tenantID, userID, purpose, req) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/consent/${encodeURIComponent(purpose)}/withdraw`, { method: 'PUT', body: JSON.stringify(req ?? {}) });
    }
    async requestDataExport(tenantID, userID, req) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/export`, {
            method: 'POST',
            body: JSON.stringify(req ?? {}),
        });
    }
    async getExportStatus(tenantID, userID, exportID) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/export/${encodeURIComponent(exportID)}`);
    }
    // downloadExport reads the export as JSON: DownloadExport always writes
    // application/json, whatever format the request named.
    async downloadExport(tenantID, userID, exportID) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/export/${encodeURIComponent(exportID)}/download`);
    }
    async requestDeletion(tenantID, userID, req) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/deletion`, {
            method: 'POST',
            body: JSON.stringify(req ?? {}),
        });
    }
    async getDeletionStatus(tenantID, userID, deletionID) {
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/deletion/${encodeURIComponent(deletionID)}`);
    }
    async cancelDeletion(tenantID, userID, deletionID, cancelledBy) {
        const query = cancelledBy ? `?${new URLSearchParams({ cancelled_by: cancelledBy }).toString()}` : '';
        return this.fetchJSON(`${this.gdprPath(tenantID, userID)}/deletion/${encodeURIComponent(deletionID)}${query}`, { method: 'DELETE' });
    }
}
// RestKMSClient implements KMSClient over the QuicBaaS REST KMS endpoints.
// All endpoints are under /api/v1/kms.
class RestKMSClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    async listKeys(limit = 100) {
        // Server returns {keys: [{key_id, version, algorithm, status, created_at}]}.
        // Map key_id → both id and name (UI uses .name primarily, .id as fallback)
        // and translate the unix-second created_at timestamp to an ISO string for
        // consistent rendering across views.
        const raw = await this.fetchJSON(`/api/v1/kms/keys?limit=${limit}`);
        const toISO = (t) => {
            if (typeof t === 'string')
                return t;
            if (typeof t === 'number') {
                // Skip Go zero-time sentinel (-62135596800 = year 1)
                if (t < 0 || !Number.isFinite(t))
                    return '';
                return new Date(t * 1000).toISOString();
            }
            return '';
        };
        const keys = (raw.keys ?? []).map((k) => ({
            id: k.id ?? k.key_id ?? '',
            name: k.name ?? k.key_id ?? '',
            description: k.description ?? '',
            algorithm: k.algorithm ?? '',
            key_size: k.key_size ?? 0,
            status: k.status ?? 'enabled',
            created_at: toISO(k.created_at),
            updated_at: toISO(k.updated_at),
            exportable: k.exportable ?? false,
            tags: k.tags ?? {},
        }));
        return { keys, count: raw.count ?? keys.length };
    }
    async createKey(req) {
        return this.fetchJSON('/api/v1/kms/keys', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async rotateKey(name) {
        return this.fetchJSON(`/api/v1/kms/keys/${encodeURIComponent(name)}/rotate`, { method: 'POST' });
    }
    async deleteKey(name) {
        await this.fetchJSON(`/api/v1/kms/keys/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
    async encrypt(req) {
        return this.fetchJSON(`/api/v1/kms/encrypt/${encodeURIComponent(req.key_id)}`, { method: 'POST', body: JSON.stringify({ plaintext: req.plaintext }) });
    }
    async decrypt(req) {
        return this.fetchJSON(`/api/v1/kms/decrypt/${encodeURIComponent(req.key_id)}`, { method: 'POST', body: JSON.stringify({ ciphertext: req.ciphertext }) });
    }
    async listKeyVersions(name) {
        // Server returns created_at / destroyed_at as Unix-second int64 values.
        // Convert to ISO strings so callers can pass them directly to new Date().
        const toISO = (t) => {
            if (t === undefined || t === null)
                return undefined;
            if (typeof t === 'string')
                return t || undefined;
            if (typeof t === 'number') {
                if (t <= 0 || !Number.isFinite(t))
                    return undefined;
                return new Date(t * 1000).toISOString();
            }
            return undefined;
        };
        const raw = await this.fetchJSON(`/api/v1/kms/keys/${encodeURIComponent(name)}/versions`);
        return (raw.versions ?? []).map((v) => ({
            version: v.version ?? 0,
            status: v.status ?? '',
            created_at: toISO(v.created_at),
            destroyed_at: toISO(v.destroyed_at),
        }));
    }
    async updateKeyStatus(name, status) {
        await this.fetchJSON(`/api/v1/kms/keys/${encodeURIComponent(name)}/status`, { method: 'PUT', body: JSON.stringify({ status }) });
    }
    // setDefaultKey marks the named key as the system default.
    async setDefaultKey(name) {
        return this.fetchJSON(`/api/v1/kms/keys/${encodeURIComponent(name)}/default`, { method: 'PUT' });
    }
}
// RestSecretsClient implements SecretsClient over the QuicBaaS REST secrets endpoints.
// All endpoints are under /api/v1/secrets.
class RestSecretsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const tok = await resolveToken(this.config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchJSON(path, init) {
        const url = `${this.baseUrl}${path}`;
        const auth = await this.authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Use statusText when body is not parseable.
            }
            throw new APIError(resp.status, message);
        }
        return parseResponseBody(resp);
    }
    async listSecrets(limit = 100) {
        const raw = await this.fetchJSON(`/api/v1/secrets?limit=${limit}`);
        const wire = raw.secrets ?? [];
        const secrets = wire.map((w) => ({
            path: w.path ?? w.secret_id ?? w.name ?? '',
            version: w.version ?? w.current_version ?? 0,
            created_at: w.created_at ?? '',
            updated_at: w.updated_at ?? '',
        }));
        return { secrets, count: raw.count ?? raw.total_count ?? secrets.length };
    }
    async storeSecret(req) {
        await this.fetchJSON(`/api/v1/secrets/${encodeURIComponent(req.path)}`, { method: 'POST', body: JSON.stringify({ data: req.data }) });
    }
    async getSecret(path) {
        const raw = await this.fetchJSON(`/api/v1/secrets/${encodeURIComponent(path)}`);
        let data = {};
        if (typeof raw.data === 'string') {
            try {
                const decoded = atob(raw.data);
                const parsed = JSON.parse(decoded);
                if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
                    data = parsed;
                }
                else {
                    data = { value: parsed };
                }
            }
            catch {
                data = { value: raw.data };
            }
        }
        else if (raw.data && typeof raw.data === 'object') {
            data = raw.data;
        }
        return {
            path: raw.path ?? raw.secret_id ?? path,
            data,
            version: raw.version ?? raw.current_version ?? 0,
            created_at: raw.created_at ?? '',
            updated_at: raw.updated_at ?? '',
        };
    }
    async deleteSecret(path) {
        await this.fetchJSON(`/api/v1/secrets/${encodeURIComponent(path)}`, { method: 'DELETE' });
    }
    async rotateSecret(path) {
        return this.fetchJSON(`/api/v1/secrets/${encodeURIComponent(path)}?action=rotate`, { method: 'POST' });
    }
    // listVersions returns all versions of a secret at the given path.
    async listVersions(path) {
        const raw = await this.fetchJSON(`/api/v1/secrets-versions/${encodeURIComponent(path)}`);
        return { versions: raw.versions ?? [], count: raw.count ?? 0 };
    }
}
function newFetchHelper(baseUrl, config) {
    const trimmed = baseUrl.replace(/\/$/, '');
    const authHeaders = async () => {
        const headers = {};
        const tok = await resolveToken(config.token);
        if (tok)
            headers['Authorization'] = `Bearer ${tok}`;
        if (config.tenantID)
            headers['X-Tenant-ID'] = config.tenantID;
        return headers;
    };
    const send = async (path, init) => {
        const url = `${trimmed}${path}`;
        const auth = await authHeaders();
        const resp = await fetch(url, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            let message = resp.statusText;
            try {
                const body = (await resp.json());
                message = body.error ?? body.message ?? message;
            }
            catch {
                // Body not JSON: fall back to statusText.
            }
            throw new APIError(resp.status, message);
        }
        return resp;
    };
    return {
        async fetchJSON(path, init) {
            const resp = await send(path, init);
            return parseResponseBody(resp);
        },
        async fetchVoid(path, init) {
            await send(path, init);
        },
    };
}
// unixToISO converts a Go unix-second int64 timestamp to an ISO-8601 string.
// Shared by RestCAClient, RestTrustClient, and RestCryptoClient, all of
// which receive int64 unix timestamps on the wire (Go's time.Time.Unix()).
function unixToISO(t) {
    if (t === undefined || t === null || !Number.isFinite(t) || t <= 0)
        return '';
    return new Date(t * 1000).toISOString();
}
// ---------------------------------------------------------------------------
// RestCAClient — /api/v1/certificate/* and /api/v1/xkms/ca/* surface.
// ---------------------------------------------------------------------------
// RestCAClient implements CAClient over the QuicBaaS certificate authority
// REST endpoints. Mirrors sdk/go/transport/rest/certificate_handler.go,
// including its GenerateCRL/IsRevoked routing quirk: those two verbs live
// under /api/v1/xkms/ca/* (the underlying xkms CA implementation) rather
// than /api/v1/certificate/*, because no equivalent route exists on the
// quicbaas-native Certificate service.
export class RestCAClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async issueCertificate(req) {
        const wire = await this.http.fetchJSON('/api/v1/certificate/issue', { method: 'POST', body: JSON.stringify(req) });
        return {
            certificate_pem: wire.certificate_pem,
            private_key_pem: wire.private_key_pem,
            ca_certificate_pem: wire.ca_certificate_pem,
            chain_pem: wire.chain_pem,
            serial_number: wire.serial_number,
            not_before: unixToISO(wire.not_before),
            not_after: unixToISO(wire.not_after),
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async signCSR(req) {
        const wire = await this.http.fetchJSON('/api/v1/certificate/sign-csr', { method: 'POST', body: JSON.stringify(req) });
        return {
            certificate_pem: wire.certificate_pem,
            ca_certificate_pem: wire.ca_certificate_pem,
            chain_pem: wire.chain_pem,
            serial_number: wire.serial_number,
            not_before: unixToISO(wire.not_before),
            not_after: unixToISO(wire.not_after),
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async revokeCertificate(serialNumber) {
        const wire = await this.http.fetchJSON('/api/v1/certificate/revoke', { method: 'POST', body: JSON.stringify({ serial_number: serialNumber }) });
        return { success: wire.success, timestamp: unixToISO(wire.timestamp) };
    }
    async verifyCertificate(certificatePEM) {
        const wire = await this.http.fetchJSON('/api/v1/certificate/verify', { method: 'POST', body: JSON.stringify({ certificate_pem: certificatePEM }) });
        return { valid: wire.valid, error_message: wire.error_message, timestamp: unixToISO(wire.timestamp) };
    }
    async getCACertificate() {
        const wire = await this.http.fetchJSON('/api/v1/certificate/ca');
        return {
            ca_certificate_pem: wire.ca_certificate_pem,
            ca_bundle_pem: wire.ca_bundle_pem,
            identity: wire.identity,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async caInfo() {
        const wire = await this.http.fetchJSON('/api/v1/certificate/ca/info');
        return {
            common_name: wire.common_name,
            subject: wire.subject,
            issuer: wire.issuer,
            serial_number: wire.serial_number,
            not_before: unixToISO(wire.not_before),
            not_after: unixToISO(wire.not_after),
            is_ca: wire.is_ca,
            key_type: wire.key_type,
            key_bits: wire.key_bits,
            signature_algorithm: wire.signature_algorithm,
            certificate_pem: wire.certificate_pem,
            ca_bundle_pem: wire.ca_bundle_pem,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async getCABundle() {
        const wire = await this.http.fetchJSON('/api/v1/certificate/ca/bundle');
        return { bundle_pem: wire.bundle_pem, timestamp: unixToISO(wire.timestamp) };
    }
    async listCertificates(req = {}) {
        const params = {};
        if (req.common_name)
            params.common_name = req.common_name;
        if (req.include_revoked)
            params.include_revoked = 'true';
        if (req.include_expired)
            params.include_expired = 'true';
        if (req.limit !== undefined)
            params.limit = String(req.limit);
        const query = new URLSearchParams(params).toString();
        const wire = await this.http.fetchJSON(`/api/v1/certificate/ca/list${query ? `?${query}` : ''}`);
        const certificates = (wire.certificates ?? []).map((c) => ({
            common_name: c.common_name,
            subject: c.subject,
            issuer: c.issuer,
            serial_number: c.serial_number,
            not_before: unixToISO(c.not_before),
            not_after: unixToISO(c.not_after),
            is_ca: c.is_ca,
            key_usage: c.key_usage,
            dns_names: c.dns_names,
        }));
        return { certificates, total_count: wire.total_count ?? certificates.length, timestamp: unixToISO(wire.timestamp) };
    }
    async generateCRL() {
        // The CRL route lives under the xkms-native CA (see class doc comment).
        // The wire field is base64 of the already PEM-encoded CRL text.
        const wire = await this.http.fetchJSON('/api/v1/xkms/ca/crl', { method: 'POST', body: JSON.stringify({}) });
        let decoded = '';
        try {
            decoded = atob(wire.crl_pem);
        }
        catch {
            decoded = wire.crl_pem;
        }
        if (!decoded.includes('-----BEGIN')) {
            // Defensive fallback: wrap raw DER bytes in PEM headers ourselves,
            // matching the Go SDK's rest.certificate_handler.go behaviour.
            const rewrapped = wire.crl_pem.replace(/(.{64})/g, '$1\n');
            decoded = `-----BEGIN X509 CRL-----\n${rewrapped}\n-----END X509 CRL-----\n`;
        }
        return { crl_pem: decoded };
    }
    async isRevoked(serialNumber) {
        return this.http.fetchJSON(`/api/v1/xkms/ca/revoked/${encodeURIComponent(serialNumber)}`);
    }
}
// ---------------------------------------------------------------------------
// RestTrustClient — /api/v1/trust/* surface.
// ---------------------------------------------------------------------------
// pemToDERBase64 extracts the base64 payload from a single PEM block,
// stripping the header/footer lines and all whitespace. The result is
// exactly the base64 encoding of the DER bytes the block contains — no
// re-encoding is required because that's what PEM already is.
function pemToDERBase64(pem) {
    return pem
        .replace(/-----BEGIN [^-]+-----/g, '')
        .replace(/-----END [^-]+-----/g, '')
        .replace(/\s+/g, '');
}
// RestTrustClient implements TrustClient over the QuicBaaS trust composition
// REST endpoints (/api/v1/trust/*). See handlers_trust.go.
export class RestTrustClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async crossSignedRootPool() {
        const wire = await this.http.fetchJSON('/api/v1/trust/cross-signed-roots');
        const root_pems = (wire.root_pems ?? []).map((b64) => {
            try {
                return atob(b64);
            }
            catch {
                return b64;
            }
        });
        return { root_pems };
    }
    async autoImportAIA(req) {
        const chain_der = req.chain_pems.map(pemToDERBase64);
        return this.http.fetchJSON('/api/v1/trust/auto-import-aia', { method: 'POST', body: JSON.stringify({ chain_der }) });
    }
    async publishDANE(req) {
        const cert_der = pemToDERBase64(req.cert_pem);
        return this.http.fetchJSON('/api/v1/trust/publish-dane', { method: 'POST', body: JSON.stringify({ name: req.name, port: req.port, cert_der }) });
    }
}
// ---------------------------------------------------------------------------
// RestCryptoClient — /api/v1/crypto/* surface.
// ---------------------------------------------------------------------------
// RestCryptoClient implements CryptoClient over the QuicBaaS crypto
// primitives REST endpoints (/api/v1/crypto/*). See handlers_crypto.go.
// All binary fields are base64 strings on the wire; this client passes them
// through unchanged (no re-encoding), except for unix timestamps which are
// converted to ISO-8601 strings for consistency with the rest of the SDK.
export class RestCryptoClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async encrypt(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/encrypt/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({ plaintext: req.plaintext, context: req.context, nonce: req.nonce }),
        });
        return {
            key_id: wire.key_id,
            key_version: wire.key_version,
            ciphertext: wire.ciphertext,
            nonce: wire.nonce,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async decrypt(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/decrypt/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({
                ciphertext: req.ciphertext,
                context: req.context,
                nonce: req.nonce,
                key_version: req.key_version,
            }),
        });
        return {
            key_id: wire.key_id,
            key_version: wire.key_version,
            plaintext: wire.plaintext,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async rewrap(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/rewrap/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({ ciphertext: req.ciphertext, context: req.context, nonce: req.nonce }),
        });
        return {
            key_id: wire.key_id,
            old_key_version: wire.old_key_version,
            new_key_version: wire.new_key_version,
            ciphertext: wire.ciphertext,
            nonce: wire.nonce,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async sign(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/sign/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({
                message: req.message,
                hash_algorithm: req.hash_algorithm,
                pre_hashed: req.pre_hashed,
            }),
        });
        return {
            key_id: wire.key_id,
            key_version: wire.key_version,
            signature: wire.signature,
            hash_algorithm: wire.hash_algorithm,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async verify(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/verify/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({
                message: req.message,
                signature: req.signature,
                hash_algorithm: req.hash_algorithm,
                key_version: req.key_version,
            }),
        });
        return {
            key_id: wire.key_id,
            key_version: wire.key_version,
            valid: wire.valid,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async hash(req) {
        const wire = await this.http.fetchJSON('/api/v1/crypto/hash', {
            method: 'POST',
            body: JSON.stringify({ data: req.data, algorithm: req.algorithm }),
        });
        return { algorithm: wire.algorithm, hash: wire.hash, timestamp: unixToISO(wire.timestamp) };
    }
    async random(numBytes = 32) {
        const wire = await this.http.fetchJSON('/api/v1/crypto/random', { method: 'POST', body: JSON.stringify({ num_bytes: numBytes }) });
        return { random_bytes: wire.random_bytes, timestamp: unixToISO(wire.timestamp) };
    }
    async dataKey(req) {
        const wire = await this.http.fetchJSON(`/api/v1/crypto/datakey/${encodeURIComponent(req.key_name)}`, {
            method: 'POST',
            body: JSON.stringify({
                key_size_bits: req.key_size_bits,
                algorithm: req.algorithm,
                context: req.context,
            }),
        });
        return {
            key_id: wire.key_id,
            plaintext: wire.plaintext,
            ciphertext_blob: wire.ciphertext_blob,
            timestamp: unixToISO(wire.timestamp),
        };
    }
}
// ---------------------------------------------------------------------------
// RestDEKClient — /api/v1/kms/deks/* surface.
// ---------------------------------------------------------------------------
// RestDEKClient implements DEKClient over the QuicBaaS Data Encryption Key
// REST endpoints (/api/v1/kms/deks/*). See handlers_kms_dek.go. DEK path
// identifiers may contain '/' (e.g. "tenant/app/key"); every method encodes
// the path with encodeURIComponent, matching RestSecretsClient's convention
// — chi's wildcard route decodes it back to the original path server-side.
// Unlike RestCAClient/RestTrustClient/RestCryptoClient, DEK response
// timestamps arrive pre-formatted as RFC3339 strings (json.Marshal of a Go
// time.Time), so no unix-to-ISO conversion is applied here.
export class RestDEKClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async createDEK(req) {
        return this.http.fetchJSON('/api/v1/kms/deks', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async getDEK(path, version) {
        const query = version !== undefined ? `?version=${encodeURIComponent(String(version))}` : '';
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}${query}`);
    }
    async listDEKs(req = {}) {
        const params = {};
        if (req.prefix)
            params.prefix = req.prefix;
        if (req.recursive)
            params.recursive = 'true';
        if (req.limit !== undefined)
            params.limit = String(req.limit);
        if (req.cursor)
            params.cursor = req.cursor;
        const query = new URLSearchParams(params).toString();
        const wire = await this.http.fetchJSON(`/api/v1/kms/deks${query ? `?${query}` : ''}`);
        return { ...wire, deks: wire.deks ?? [] };
    }
    async rotateDEK(path, req = {}) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/rotate`, { method: 'POST', body: JSON.stringify(req) });
    }
    async deleteDEK(path, req = {}) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}`, { method: 'DELETE', body: JSON.stringify(req) });
    }
    async getDEKStatus(path) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/status`);
    }
    async setDefaultDEK(path) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/set-default`, { method: 'POST' });
    }
    async setDEKSafety(path, req) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/safety`, { method: 'PUT', body: JSON.stringify(req) });
    }
    async clearDEKSafety(path) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/safety`, { method: 'DELETE' });
    }
    async startReencryption(path, req = {}) {
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/reencrypt`, { method: 'POST', body: JSON.stringify(req) });
    }
    async getReencryptionProgress(path, operationId, verbose) {
        const params = {};
        if (operationId)
            params.operation_id = operationId;
        if (verbose)
            params.verbose = 'true';
        const query = new URLSearchParams(params).toString();
        return this.http.fetchJSON(`/api/v1/kms/deks/${encodeURIComponent(path)}/reencrypt${query ? `?${query}` : ''}`);
    }
}
/**
 * RestAttestationClient manages the attestation verifier's endorsements and
 * reference values over REST (/api/v1/attestation/*).
 */
export class RestAttestationClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    // Maps to PUT /api/v1/attestation/endorsements/{name}
    async putEndorsement(name, req) {
        return this.http.fetchJSON(`/api/v1/attestation/endorsements/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    // Maps to GET /api/v1/attestation/endorsements[?kind=]
    async listEndorsements(kind) {
        const query = kind ? `?kind=${encodeURIComponent(kind)}` : '';
        const wire = await this.http.fetchJSON(`/api/v1/attestation/endorsements${query}`);
        return { ...wire, endorsements: wire?.endorsements ?? [] };
    }
    // Maps to DELETE /api/v1/attestation/endorsements/{name}
    async deleteEndorsement(name) {
        return this.http.fetchVoid(`/api/v1/attestation/endorsements/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
    // Maps to PUT /api/v1/attestation/reference-values/{name}
    async putReferenceValues(name, signedCoRIM) {
        const req = { signed_corim: signedCoRIM };
        return this.http.fetchJSON(`/api/v1/attestation/reference-values/${encodeURIComponent(name)}`, { method: 'PUT', body: JSON.stringify(req) });
    }
    // Maps to GET /api/v1/attestation/reference-values/{name}
    async getReferenceValues(name) {
        return this.http.fetchJSON(`/api/v1/attestation/reference-values/${encodeURIComponent(name)}`);
    }
    // Maps to GET /api/v1/attestation/reference-values
    async listReferenceValues() {
        const wire = await this.http.fetchJSON('/api/v1/attestation/reference-values');
        return { ...wire, reference_values: wire?.reference_values ?? [] };
    }
    // Maps to DELETE /api/v1/attestation/reference-values/{name}
    async deleteReferenceValues(name) {
        return this.http.fetchVoid(`/api/v1/attestation/reference-values/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
}
// ---------------------------------------------------------------------------
// RestDynamicSecretsClient — /api/v1/secrets/dynamic/* surface.
// ---------------------------------------------------------------------------
// RestDynamicSecretsClient implements DynamicSecretsClient over the
// QuicBaaS dynamic secrets REST endpoints (/api/v1/secrets/dynamic/*). See
// handlers_dynamic_secrets.go. Like RestCAClient/RestTrustClient, the wire
// responses carry Go int64 unix-second timestamps; every method converts
// them to ISO-8601 strings before returning to the caller.
export class RestDynamicSecretsClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async createRole(req) {
        const wire = await this.http.fetchJSON('/api/v1/secrets/dynamic/roles', { method: 'POST', body: JSON.stringify(req) });
        return {
            name: wire.name,
            generator_type: wire.generator_type,
            config: wire.config,
            default_ttl: wire.default_ttl,
            max_ttl: wire.max_ttl,
            created_at: wire.created_at !== undefined ? unixToISO(wire.created_at) : undefined,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async getRole(name) {
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/roles/${encodeURIComponent(name)}`);
        return {
            name: wire.name,
            generator_type: wire.generator_type,
            config: wire.config,
            default_ttl: wire.default_ttl,
            max_ttl: wire.max_ttl,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async listRoles(prefix) {
        const query = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/roles${query}`);
        const roles = (wire.roles ?? []).map((r) => ({
            name: r.name,
            generator_type: r.generator_type,
            config: r.config,
            default_ttl: r.default_ttl,
            max_ttl: r.max_ttl,
            timestamp: unixToISO(r.timestamp),
        }));
        return { roles, count: wire.count ?? roles.length, timestamp: unixToISO(wire.timestamp) };
    }
    async deleteRole(name) {
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/roles/${encodeURIComponent(name)}`, { method: 'DELETE' });
        return { success: wire.success, deleted_at: unixToISO(wire.deleted_at), timestamp: unixToISO(wire.timestamp) };
    }
    async generateCredential(roleName, req = {}) {
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/credentials/${encodeURIComponent(roleName)}`, {
            method: 'POST',
            body: JSON.stringify(req),
        });
        return {
            credential_id: wire.credential_id,
            username: wire.username,
            password: wire.password,
            secret: wire.secret,
            lease_id: wire.lease_id,
            lease_ttl: wire.lease_ttl,
            lease_expiry: unixToISO(wire.lease_expiry),
            renewable: wire.renewable,
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async renewLease(leaseId, req = {}) {
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/leases/${encodeURIComponent(leaseId)}/renew`, {
            method: 'POST',
            body: JSON.stringify(req),
        });
        return {
            lease_id: wire.lease_id,
            lease_ttl: wire.lease_ttl,
            lease_expiry: unixToISO(wire.lease_expiry),
            renew_count: wire.renew_count,
            renewed_at: unixToISO(wire.renewed_at),
            timestamp: unixToISO(wire.timestamp),
        };
    }
    async revokeLease(leaseId) {
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/leases/${encodeURIComponent(leaseId)}`, { method: 'DELETE' });
        return { success: wire.success, revoked_at: unixToISO(wire.revoked_at), timestamp: unixToISO(wire.timestamp) };
    }
    async listLeases(prefix) {
        const query = prefix ? `?prefix=${encodeURIComponent(prefix)}` : '';
        const wire = await this.http.fetchJSON(`/api/v1/secrets/dynamic/leases${query}`);
        const leases = (wire.leases ?? []).map((l) => ({
            lease_id: l.lease_id,
            path: l.path,
            ttl: l.ttl,
            expiry: unixToISO(l.expiry),
            renewable: l.renewable,
            renew_count: l.renew_count,
            metadata: l.metadata,
        }));
        return { leases, count: wire.count ?? leases.length, timestamp: unixToISO(wire.timestamp) };
    }
}
// ---------------------------------------------------------------------------
// RestBarrierClient — canonical /api/v1/barrier surface.
// ---------------------------------------------------------------------------
// RestBarrierClient implements BarrierClient by routing to the QuicBaaS
// barrier REST endpoints. The shape mirrors go-qrdb's BarrierClient surface.
export class RestBarrierClient {
    baseUrl;
    http;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl;
        this.http = newFetchHelper(baseUrl, config);
    }
    async status() {
        return this.http.fetchJSON('/api/v1/barrier/status');
    }
    async initialize(req) {
        await this.http.fetchVoid('/api/v1/barrier/initialize', {
            method: 'POST',
            body: JSON.stringify({ secret: bytesToBase64(req.secret) }),
        });
    }
    async unseal(req) {
        await this.http.fetchVoid('/api/v1/barrier/unseal', {
            method: 'POST',
            body: JSON.stringify({ secret: bytesToBase64(req.secret) }),
        });
    }
    async seal(req) {
        requireTLSForSOPIN('rest', this.baseUrl, req?.so_pin);
        await this.http.fetchVoid('/api/v1/barrier/seal', {
            method: 'POST',
            body: JSON.stringify({ so_pin: req?.so_pin }),
        });
    }
    async initializeShamir(req) {
        return this.http.fetchJSON('/api/v1/barrier/shamir/initialize', {
            method: 'POST',
            body: JSON.stringify({ ...req, secret: bytesToBase64(req.secret) }),
        });
    }
    async unsealWithShare(req) {
        return this.http.fetchJSON('/api/v1/barrier/shamir/unseal-share', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async unsealWithShares(req) {
        await this.http.fetchVoid('/api/v1/barrier/shamir/unseal-shares', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async rekey(req) {
        return this.http.fetchJSON('/api/v1/barrier/rekey', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async generateRecoveryKeys(req) {
        return this.http.fetchJSON('/api/v1/barrier/recovery/generate', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async recoverWithKeys(req) {
        await this.http.fetchVoid('/api/v1/barrier/recovery/recover', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async deleteRecoveryKeys() {
        await this.http.fetchVoid('/api/v1/barrier/recovery', { method: 'DELETE' });
    }
    async recoveryStatus() {
        return this.http.fetchJSON('/api/v1/barrier/recovery/status');
    }
    async generateRootToken(req) {
        return this.http.fetchJSON('/api/v1/barrier/root-token', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // enableEncryption posts to the go-qrdb wire mount, whose body is the
    // canonical transport.BarrierEnableEncryptionRequest: tenant_id there
    // selects the tenant barrier, and the reply acknowledges the started
    // migration.
    async enableEncryption(req) {
        const { passphrase, ...rest } = req;
        const body = passphrase === undefined ? rest : { ...rest, passphrase: bytesToBase64(passphrase) };
        return this.http.fetchJSON('/v1/barrier/enable-encryption', {
            method: 'POST',
            body: JSON.stringify(body),
        });
    }
}
// ---------------------------------------------------------------------------
// RestTenantBarrierClient — /api/v1/tenants/{id}/barrier surface.
// ---------------------------------------------------------------------------
// RestTenantBarrierClient implements TenantBarrierClient by routing to the
// broker's per-tenant barrier REST endpoints. Each call is scoped to a tenant
// ID; the surface is distinct from RestBarrierClient, which drives the
// process-wide system barrier.
export class RestTenantBarrierClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    base(tenantId) {
        return `/api/v1/tenants/${encodeURIComponent(tenantId)}/barrier`;
    }
    async tenantBarrierInit(tenantId, req) {
        const { passphrase, ...rest } = req;
        const body = passphrase === undefined ? rest : { ...rest, passphrase: bytesToBase64(passphrase) };
        // The shamir strategy returns 200 with the minted shares; the none and
        // passphrase strategies return 204 No Content. fetchJSON yields undefined
        // on a 204, so the union return type already covers both cases.
        return this.http.fetchJSON(`${this.base(tenantId)}/init`, {
            method: 'POST',
            body: JSON.stringify(body),
        });
    }
    async tenantBarrierUnseal(tenantId, key) {
        await this.http.fetchVoid(`${this.base(tenantId)}/unseal`, {
            method: 'POST',
            body: JSON.stringify({ key: bytesToBase64(key) }),
        });
    }
    async tenantBarrierUnsealShare(tenantId, share) {
        return this.http.fetchJSON(`${this.base(tenantId)}/unseal-share`, {
            method: 'POST',
            body: JSON.stringify({ share }),
        });
    }
    async tenantBarrierSeal(tenantId) {
        await this.http.fetchVoid(`${this.base(tenantId)}/seal`, { method: 'POST' });
    }
    async tenantBarrierStatus(tenantId) {
        return this.http.fetchJSON(`${this.base(tenantId)}/status`);
    }
}
// ---------------------------------------------------------------------------
// RestCeremonyClient — /api/v1/init surface.
// ---------------------------------------------------------------------------
// RestCeremonyClient implements CeremonyClient against the QuicBaaS
// ceremony REST endpoints (mounted under /api/v1/init).
export class RestCeremonyClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async initialize(req) {
        return this.http.fetchJSON('/api/v1/init/initialize', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async status() {
        return this.http.fetchJSON('/api/v1/init/status');
    }
    async claimCertBegin(req) {
        return this.http.fetchJSON('/api/v1/init/claim-cert/begin', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async claimCertComplete(req) {
        return this.http.fetchJSON('/api/v1/init/claim-cert/complete', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async claimShareBegin(req) {
        return this.http.fetchJSON('/api/v1/init/officer-share/begin', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async claimShareComplete(req) {
        return this.http.fetchJSON('/api/v1/init/officer-share/complete', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async signCSR(req) {
        return this.http.fetchJSON('/api/v1/init/sign-csr', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async enrollExternalOfficer(req) {
        return this.http.fetchJSON('/api/v1/init/enroll-external-officer', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
}
// ---------------------------------------------------------------------------
// RestClusterClient — /api/v1/cluster surface.
// ---------------------------------------------------------------------------
// RestClusterClient implements ClusterClient against the QuicBaaS Raft cluster
// REST endpoints. status() issues an authenticated GET so the bearer token is
// attached via the shared FetchHelper (the same auth path every other
// authenticated client uses) — a raw fetch() would omit the token and 401.
export class RestClusterClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async status() {
        return this.http.fetchJSON('/api/v1/cluster/status', {
            method: 'GET',
        });
    }
}
// ---------------------------------------------------------------------------
// RestBootstrapClient — /api/v1/bootstrap surface.
// ---------------------------------------------------------------------------
// RestBootstrapClient implements BootstrapClient against the QuicBaaS
// trust bootstrap REST endpoints. Each method is a thin wrapper around the
// matching POST /api/v1/bootstrap/{method} route. A nil request fetches
// the full CA bundle without filters.
export class RestBootstrapClient {
    http;
    constructor(baseUrl, config = {}) {
        this.http = newFetchHelper(baseUrl, config);
    }
    async fetchMethod(method, req) {
        const body = req ? JSON.stringify(req) : '{}';
        return this.http.fetchJSON(`/api/v1/bootstrap/${method}`, {
            method: 'POST',
            body,
        });
    }
    async auto(req) {
        return this.fetchMethod('auto', req);
    }
    async dane(req) {
        return this.fetchMethod('dane', req);
    }
    async noise(req) {
        return this.fetchMethod('noise', req);
    }
    async spki(req) {
        return this.fetchMethod('spki', req);
    }
}
// ---------------------------------------------------------------------------
// RestSealClient — deprecated alias preserved for one release.
//
// This class warns once per construction and delegates directly to the new
// RestBarrierClient. The legacy /api/v1/seal endpoints remain available on
// the daemon (with Deprecation/Sunset headers) so existing callers that
// instantiate RestSealClient out-of-band continue to work, but new code
// should migrate to client.barrier directly.
// ---------------------------------------------------------------------------
let sealDeprecationWarned = false;
// emitSealDeprecationWarning prints a single console warning describing the
// rename. Repeated constructions are no-ops to avoid log spam.
function emitSealDeprecationWarning() {
    if (sealDeprecationWarned) {
        return;
    }
    sealDeprecationWarned = true;
    // eslint-disable-next-line no-console
    console.warn('SealClient is deprecated, use BarrierClient (client.barrier). ' +
        'The Seal* surface will be removed one release after the unification ships.');
}
class RestSealClient {
    http;
    constructor(baseUrl, config = {}) {
        emitSealDeprecationWarning();
        this.http = newFetchHelper(baseUrl, config);
    }
    async status() {
        return this.http.fetchJSON('/api/v1/seal/status');
    }
    async unseal(req) {
        return this.http.fetchJSON('/api/v1/seal/unseal', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async seal() {
        await this.http.fetchVoid('/api/v1/seal/seal', { method: 'POST' });
    }
    async rekey(req) {
        return this.http.fetchJSON('/api/v1/seal/rekey', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    async rotateRootKey() {
        return this.http.fetchJSON('/api/v1/seal/rotate-root', {
            method: 'POST',
        });
    }
}
// throwSSHError maps an HTTP response onto a typed SSH error and throws it.
// The caller must ensure resp.ok is false before calling this.
async function throwSSHError(resp) {
    let body = {};
    try {
        body = (await resp.json());
    }
    catch {
        // Fall through with empty body — use statusText.
    }
    const message = body.error ?? resp.statusText;
    const resource = body.resource;
    switch (resp.status) {
        case 404:
            throw new SSHNotFoundError(message, resource);
        case 400:
            throw new SSHInvalidError(message, resource);
        case 503:
            throw new SSHUnavailableError(message);
        case 409:
            // Distinguish host-key mismatch (has expected/received) from generic conflict.
            if (body.host !== undefined && body.expected !== undefined && body.received !== undefined) {
                throw new SSHHostMismatchError(body.host, body.expected, body.received, message);
            }
            throw new SSHDuplicateError(message, resource);
        default:
            if (resp.status >= 500) {
                throw new SSHServerError(resp.status, message, resource);
            }
            throw new SSHError(resp.status, message, resource);
    }
}
// =============================================================================
// RestSSHKeysClient
// =============================================================================
// RestSSHKeysClient implements SSHKeysClient using the QuicBaaS REST API.
// Legacy flat methods hit /api/v1/ssh/keys with server-side scope/owner defaults.
// Scoped methods use the full /api/v1/ssh/keys/{scope}/{owner}/{name} surface.
export class RestSSHKeysClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const token = await resolveToken(this.config.token);
        if (token)
            headers.Authorization = `Bearer ${token}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchSSH(path, init) {
        const auth = await this.authHeaders();
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            await throwSSHError(resp);
        }
        return parseResponseBody(resp);
    }
    // list returns SSH keys using server-side scope/owner defaults.
    // Backward-compatible: returns {keys: SSHKey[]} for Wails callers.
    // The new REST surface returns {items: SSHKey[]} — both fields are populated.
    async list() {
        try {
            const raw = await this.fetchSSH('/api/v1/ssh/keys');
            return { items: raw.items ?? [], keys: raw.items ?? [] };
        }
        catch {
            // Silent fallback preserves old behaviour for Wails/Svelte list views.
            return { keys: [], items: [] };
        }
    }
    // create generates a new SSH key and returns its metadata.
    async create(req) {
        return this.fetchSSH('/api/v1/ssh/keys', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // get fetches a single key. For backward compat, uses the first
    // matching key from the list when scope/owner are not provided.
    async get(name) {
        // The new surface requires scope/owner/name. Without them we query
        // the list and find by name, matching the legacy flat behaviour.
        const resp = await this.fetchSSH('/api/v1/ssh/keys');
        const found = (resp.items ?? []).find((k) => k.name === name);
        if (!found) {
            throw new SSHNotFoundError(`ssh key not found: ${name}`);
        }
        return found;
    }
    // remove deletes a key. Without scope/owner, mirrors the legacy behaviour
    // by finding the key via list first, then deleting by triple.
    async remove(name) {
        const key = await this.get(name);
        const scope = key.scope ?? 'user';
        const owner = key.owner ?? '';
        await this.fetchSSH(`/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
    // listScoped enumerates keys filtered by scope/owner.
    async listScoped(query) {
        const params = new URLSearchParams();
        if (query?.scope)
            params.set('scope', query.scope);
        if (query?.owner)
            params.set('owner', query.owner);
        const qs = params.toString() ? `?${params.toString()}` : '';
        const raw = await this.fetchSSH(`/api/v1/ssh/keys${qs}`);
        return { items: raw.items ?? [], keys: raw.items ?? [] };
    }
    // getScoped fetches a single key by the full scope/owner/name triple.
    async getScoped(scope, owner, name) {
        return this.fetchSSH(`/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`);
    }
    // deleteScoped removes a key by the full scope/owner/name triple.
    async deleteScoped(scope, owner, name) {
        await this.fetchSSH(`/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}`, { method: 'DELETE' });
    }
    // exportPublic returns the authorized_keys-format public key for the given key.
    async exportPublic(scope, owner, name) {
        return this.fetchSSH(`/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/public`);
    }
    // sign signs a raw digest with the named key.
    async sign(scope, owner, name, req) {
        return this.fetchSSH(`/api/v1/ssh/keys/${encodeURIComponent(scope)}/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/sign`, { method: 'POST', body: JSON.stringify(req) });
    }
    // import_ imports a PEM-encoded private key. Trailing underscore avoids
    // the reserved word 'import'.
    async import_(req) {
        return this.fetchSSH('/api/v1/ssh/keys/import', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
}
// =============================================================================
// RestKnownHostsClient
// =============================================================================
// RestKnownHostsClient implements SSHKnownHostsClient over the QuicBaaS REST API.
// Paths: /api/v1/ssh/known-hosts
export class RestKnownHostsClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const token = await resolveToken(this.config.token);
        if (token)
            headers.Authorization = `Bearer ${token}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchSSH(path, init) {
        const auth = await this.authHeaders();
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            await throwSSHError(resp);
        }
        return parseResponseBody(resp);
    }
    // add pins a host-key fingerprint.
    async add(req) {
        await this.fetchSSH('/api/v1/ssh/known-hosts', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // list enumerates pinned host keys, optionally filtered by scope/owner.
    async list(query) {
        const params = new URLSearchParams();
        if (query?.scope)
            params.set('scope', query.scope);
        if (query?.owner)
            params.set('owner', query.owner);
        const qs = params.toString() ? `?${params.toString()}` : '';
        return this.fetchSSH(`/api/v1/ssh/known-hosts${qs}`);
    }
    // remove removes a pinned host key.
    async remove(host, query) {
        const params = new URLSearchParams();
        if (query?.scope)
            params.set('scope', query.scope);
        if (query?.owner)
            params.set('owner', query.owner);
        const qs = params.toString() ? `?${params.toString()}` : '';
        await this.fetchSSH(`/api/v1/ssh/known-hosts/${encodeURIComponent(host)}${qs}`, { method: 'DELETE' });
    }
    // verify checks the presented fingerprint against the stored pin.
    // Throws SSHHostMismatchError on mismatch, SSHNotFoundError when not pinned.
    async verify(host, req) {
        await this.fetchSSH(`/api/v1/ssh/known-hosts/${encodeURIComponent(host)}/verify`, { method: 'POST', body: JSON.stringify(req) });
    }
}
// =============================================================================
// RestAuthorizedKeysClient
// =============================================================================
// RestAuthorizedKeysClient implements SSHAuthorizedKeysClient over the QuicBaaS REST API.
// Paths: /api/v1/ssh/authorized
export class RestAuthorizedKeysClient {
    baseUrl;
    config;
    constructor(baseUrl, config = {}) {
        this.baseUrl = baseUrl.replace(/\/$/, '');
        this.config = config;
    }
    async authHeaders() {
        const headers = {};
        const token = await resolveToken(this.config.token);
        if (token)
            headers.Authorization = `Bearer ${token}`;
        if (this.config.tenantID)
            headers['X-Tenant-ID'] = this.config.tenantID;
        return headers;
    }
    async fetchSSH(path, init) {
        const auth = await this.authHeaders();
        const resp = await fetch(`${this.baseUrl}${path}`, {
            ...init,
            headers: {
                'Content-Type': 'application/json',
                Accept: 'application/json',
                ...auth,
                ...(init?.headers ?? {}),
            },
        });
        if (!resp.ok) {
            await throwSSHError(resp);
        }
        return parseResponseBody(resp);
    }
    // emit formats a public key into an authorized_keys line.
    async emit(req) {
        return this.fetchSSH('/api/v1/ssh/authorized', {
            method: 'POST',
            body: JSON.stringify(req),
        });
    }
    // verify checks whether a fingerprint matches an authorized entry.
    // Returns the matching SSHAuthorizedEntry on success.
    async verify(req) {
        const entry = await this.fetchSSH('/api/v1/ssh/authorized/verify', {
            method: 'POST',
            body: JSON.stringify(req),
        });
        return { entry };
    }
}
// =============================================================================
// RestSSHClient — top-level SSH namespace
// =============================================================================
// RestSSHClient groups the three SSH sub-clients behind a single property.
// Exposed as client.ssh on RestClient.
class RestSSHClient {
    keys;
    knownHosts;
    authorized;
    constructor(baseUrl, config = {}) {
        this.keys = new RestSSHKeysClient(baseUrl, config);
        this.knownHosts = new RestKnownHostsClient(baseUrl, config);
        this.authorized = new RestAuthorizedKeysClient(baseUrl, config);
    }
}
// readErrorMessage attempts to extract a structured error message
// from the response body, falling back to the HTTP status text.
async function readErrorMessage(resp) {
    try {
        const data = (await resp.json());
        return data.error ?? data.message ?? resp.statusText;
    }
    catch {
        return resp.statusText;
    }
}
// RestClient is the top-level Client implementation for REST / browser mode.
// It composes all sub-clients and shares the base URL and config between them.
export class RestClient {
    connection;
    orchestrator;
    auth;
    data;
    vectors;
    timeseries;
    objects;
    rag;
    embeddings;
    messaging;
    scheduler;
    projects;
    sdlc;
    accounts;
    agents;
    iam;
    kms;
    secrets;
    ca;
    trust;
    crypto;
    dek;
    attestation;
    dynamicSecrets;
    barrier;
    tenantBarrier;
    ceremony;
    bootstrap;
    cluster;
    /** @deprecated Use barrier. The legacy seal accessor is kept for one release. */
    seal;
    /**
     * sshKeys is the legacy backward-compat accessor.
     * Points to the same RestSSHKeysClient instance as ssh.keys.
     */
    sshKeys;
    /**
     * ssh exposes the full scoped SSH surface (keys, knownHosts, authorized).
     * client.ssh.keys is the same instance as client.sshKeys.
     */
    ssh;
    constructor(baseUrl, config = {}) {
        this.connection = new RestConnectionClient(baseUrl);
        this.orchestrator = new RestOrchestratorClient(baseUrl, config);
        this.auth = new RestAuthClient(baseUrl);
        this.data = new RestDataClient(baseUrl, config);
        this.vectors = new RestVectorsClient(baseUrl, config);
        this.timeseries = new RestTimeSeriesClient(baseUrl, config);
        this.objects = new RestObjectsClient(baseUrl, config);
        this.rag = new RestRAGClient(baseUrl, config);
        this.embeddings = new RestEmbeddingsClient(baseUrl, config);
        this.messaging = new RestMessagingClient(baseUrl, config);
        this.scheduler = new RestSchedulerClient(baseUrl, config);
        this.projects = new RestProjectsClient(baseUrl, config);
        this.sdlc = new RestSdlcClient(baseUrl, config);
        this.accounts = new RestAccountsClient(baseUrl, config);
        this.agents = new RestAgentsClient(baseUrl, config);
        this.iam = new RestIAMClient(baseUrl, config);
        this.kms = new RestKMSClient(baseUrl, config);
        this.secrets = new RestSecretsClient(baseUrl, config);
        this.ca = new RestCAClient(baseUrl, config);
        this.trust = new RestTrustClient(baseUrl, config);
        this.crypto = new RestCryptoClient(baseUrl, config);
        this.dek = new RestDEKClient(baseUrl, config);
        this.attestation = new RestAttestationClient(baseUrl, config);
        this.dynamicSecrets = new RestDynamicSecretsClient(baseUrl, config);
        this.barrier = new RestBarrierClient(baseUrl, config);
        this.tenantBarrier = new RestTenantBarrierClient(baseUrl, config);
        this.ceremony = new RestCeremonyClient(baseUrl, config);
        this.bootstrap = new RestBootstrapClient(baseUrl, config);
        this.cluster = new RestClusterClient(baseUrl, config);
        // RestSealClient is a deprecated alias retained for one release. Construction
        // is lazy via a getter so apps that never touch `client.seal` (which is the
        // expected case after the unification) don't pay for the deprecation
        // warning on every client instantiation. New code must use client.barrier;
        // this accessor will be removed one release post-cutover.
        let _seal = null;
        Object.defineProperty(this, 'seal', {
            get: () => {
                if (_seal === null)
                    _seal = new RestSealClient(baseUrl, config);
                return _seal;
            },
            enumerable: true,
        });
        // Build the scoped SSH client group first, then alias sshKeys to ssh.keys
        // so both accessors point at the same RestSSHKeysClient instance.
        this.ssh = new RestSSHClient(baseUrl, config);
        this.sshKeys = this.ssh.keys;
    }
}
// Re-export errors used in this module so callers can catch typed errors
// without importing from the errors module directly.
export { APIError, ConnectionError, NotConnectedError, SSHError, SSHNotFoundError, SSHDuplicateError, SSHInvalidError, SSHUnavailableError, SSHServerError, SSHHostMismatchError, };
// costSettingsQuery scopes a cost-settings request to a tenant when one is
// named; the server otherwise answers for the caller's own tenant.
function costSettingsQuery(tenantID) {
    return tenantID ? `?${new URLSearchParams({ tenant_id: tenantID }).toString()}` : '';
}
//# sourceMappingURL=rest.js.map