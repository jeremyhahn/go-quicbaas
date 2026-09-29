/**
 * Paging, sorting and search — ONE request type and ONE answer type for every
 * list endpoint.
 *
 * Every orchestrator listing (jobs, tasks, sessions, agents, workflows,
 * projects, accounts) takes the same four parameters and answers with the same
 * envelope; see docs/software-factory/api.md. Seven resource-specific page
 * types would be seven chances for them to drift, so there is one of each here
 * and the resource only decides which member of the envelope holds the records.
 *
 * THE UNPAGED METHODS STILL EXIST AND STILL WORK, and they are not the same
 * question. `listJobs(tenantID)` sends no `limit`, and the server's default
 * page is 25 — so an unpaged call now answers with the FIRST 25 records and no
 * way to tell that from "there are 25". A caller that needs to know how many
 * there are, or needs the second page, asks `listJobsPage`.
 */
import { QuicBaaSError } from './errors.js';
import type { JobState, SessionState, TaskStatus } from './types.js';
/**
 * PageParams is what a caller asks a list endpoint for. Every field is
 * optional and an absent one takes the server's default: page size 25 (capped
 * at 100), offset 0, the endpoint's natural order, no search.
 */
export interface PageParams {
    /** limit is the page size. The server caps it at 100 rather than refusing. */
    limit?: number;
    /** offset is how many records to skip. */
    offset?: number;
    /** sort is `<field>` or `<field>:desc`, from the endpoint's allow-list. */
    sort?: string;
    /** q is a case-insensitive substring over the endpoint's text fields. */
    q?: string;
}
/** JobPageParams adds the jobs listing's own narrowings. */
export interface JobPageParams extends PageParams {
    /** task_id lists the attempts at one task. */
    task_id?: string;
    /** runner lists the jobs one runner drove — sdlc, triage, review. */
    runner?: string;
    /** project_id lists one project's jobs. */
    project_id?: string;
    /** agent_id lists the jobs one fleet node ran. */
    agent_id?: string;
    /** statuses lists the jobs in any of these states. */
    statuses?: JobState[];
    /**
     * job_ids lists exactly the named jobs — how a page of sessions or tasks
     * resolves the jobs its rows name in ONE call instead of one per row.
     */
    job_ids?: string[];
}
/** TaskPageParams adds the tasks listing's own narrowings. */
export interface TaskPageParams extends PageParams {
    status?: TaskStatus;
    statuses?: TaskStatus[];
    project_id?: string;
    team_name?: string;
    assigned_to?: string;
    created_by?: string;
    /**
     * auto_spawn lists the tasks that asked for immediate dispatch (true) or
     * the ones queued for a claim (false). Absent does not narrow — which is
     * why it is optional rather than defaulting to false.
     */
    auto_spawn?: boolean;
    /** agent_id lists the tasks whose latest attempt ran on one fleet node. */
    agent_id?: string;
    /** job_id lists the tasks one job is an attempt at. */
    job_id?: string;
}
/** SessionPageParams adds the sessions listing's own narrowings. */
export interface SessionPageParams extends PageParams {
    backend?: string;
    status?: SessionState;
    /** project_id lists the sessions opened for one project's work. */
    project_id?: string;
    /** job_id lists the sessions one job opened. */
    job_id?: string;
    /** runner lists the sessions by the role they played — sdlc, triage, review. */
    runner?: string;
    /** agent_id lists the sessions that ran on one fleet node. */
    agent_id?: string;
}
/** FleetNodePageParams adds the fleet listing's own narrowings. */
export interface FleetNodePageParams extends PageParams {
    host?: string;
    status?: string;
    node_type?: string;
    backend?: string;
    model?: string;
}
/** WorkflowPageParams adds the workflow listing's own narrowings. */
export interface WorkflowPageParams extends PageParams {
    /** project_id lists one project's workflows. */
    project_id?: string;
    /** mode lists the workflows of one execution mode — sequential, parallel, dag. */
    mode?: string;
}
/** ProjectPageParams adds the projects listing's own narrowings. */
export interface ProjectPageParams extends PageParams {
    /** autonomous lists the projects that dispatch their own work, or the rest. */
    autonomous?: boolean;
}
/** AccountPageParams adds the accounts listing's own narrowings. */
export interface AccountPageParams extends PageParams {
    provider?: string;
    project_id?: string;
    /** status lists the accounts in one pool state — available, in_use, cooldown. */
    status?: string;
}
/** AttentionPageParams adds the attention inbox's own narrowings. */
export interface AttentionPageParams extends PageParams {
    /** project_id lists one project's attention items. */
    project_id?: string;
    /** kind lists one attention kind: parked_task, agent_offline,
     * account_error, barrier_sealed. */
    kind?: string;
}
/**
 * PageFacets is what a listing counted, per filterable field, over the set its
 * search matched and BEFORE its own filters were applied —
 * `{status: {pending: 12, blocked: 3}}`.
 *
 * The counts are what makes faceted narrowing worth more than a select box:
 * the control says how many records are behind each value before anything is
 * clicked. A listing that counts nothing sends no `facets`, and a caller reads
 * that as "nobody counted" rather than "everything is zero".
 */
export type PageFacets = Record<string, Record<string, number>>;
/**
 * PageResult is one page of a listing, normalised.
 *
 * The records are under `items` whatever the endpoint calls them on the wire
 * (`jobs`, `tasks`, `sessions`, …): one envelope type for every resource is
 * what keeps this file from growing a type per listing, and a consumer that
 * wants the name has the method it called.
 */
export interface PageResult<T> {
    /** items is this page's records, never undefined. */
    items: T[];
    /** total is how many matched the filters and search, BEFORE the page was cut. */
    total: number;
    /** limit and offset are the values the server APPLIED, not the ones asked for. */
    limit: number;
    offset: number;
    /**
     * next_offset is where the following page starts, ABSENT on the last page.
     * Its absence is how a caller knows it has reached the end; do not compute
     * it from `total`.
     */
    next_offset?: number;
    /**
     * facets is the per-field count of the matched set, ABSENT when the endpoint
     * did not count — an older daemon, or a listing with nothing to facet. A
     * caller renders its declared vocabulary without counts rather than
     * rendering nothing.
     */
    facets?: PageFacets;
}
/**
 * MalformedPageError is thrown when a list endpoint answers something that is
 * not a listing at all — a null body, a string, an object with no records
 * under the key the endpoint uses.
 *
 * IT THROWS RATHER THAN REPORTING AN EMPTY PAGE, because "the server sent
 * nothing usable" and "this tenant has no jobs" are different answers and a
 * table rendering "0 of 0" for the first one tells the operator a falsehood.
 */
export declare class MalformedPageError extends QuicBaaSError {
    /** itemsKey is the envelope member the records were expected under. */
    readonly itemsKey: string;
    constructor(itemsKey: string, received: string);
}
/**
 * appendPageParams writes the four shared parameters onto a query string.
 *
 * An absent field is NOT written: `limit=0` and an absent limit both mean the
 * default to the server, but sending nothing keeps the URL honest about what
 * the caller actually asked for. An empty search is likewise omitted rather
 * than sent as `q=`.
 */
export declare function appendPageParams(params: URLSearchParams, page?: PageParams): URLSearchParams;
/** appendListParam writes one optional scalar filter, skipping empties. */
export declare function appendListParam(params: URLSearchParams, name: string, value: string | boolean | undefined): URLSearchParams;
/**
 * appendCSVParam writes a repeated filter the way every transport spells it —
 * comma-separated, as `statuses` and `job_ids` are read server-side. An empty
 * list narrows NOTHING and is omitted rather than sent as an empty value.
 */
export declare function appendCSVParam(params: URLSearchParams, name: string, values: readonly string[] | undefined): URLSearchParams;
/**
 * adoptPage normalises whatever a list endpoint answered into a PageResult.
 *
 * Three shapes are accepted, because three are real:
 *
 *   {jobs: [...], total, limit, offset, next_offset}  a paged answer
 *   {jobs: [...]}                                     a daemon older than
 *                                                     this SDK, which pages
 *                                                     nothing
 *   [...]                                             the same, unwrapped
 *
 * The last two are reported as ONE page holding the whole set — total is its
 * length — which is true of them and is what a table needs to render. Anything
 * else is a MalformedPageError.
 *
 * `map` converts each record, for the listings whose wire shape differs from
 * the SDK's (sessions rename `state` to `status`).
 */
export declare function adoptPage<T>(raw: unknown, itemsKey: string, map?: (item: never) => T): PageResult<T>;
/**
 * adoptFacets reads the counts out of a listing answer, keeping only the
 * numbers. A missing member, or one that is not a map of maps of numbers,
 * reports undefined — which a caller reads as "nobody counted" and answers by
 * offering its declared vocabulary uncounted.
 *
 * IT DOES NOT THROW. A malformed `facets` costs the operator the counts beside
 * the chips; refusing the whole page over it would cost them the records.
 */
export declare function adoptFacets(raw: unknown): PageFacets | undefined;
/**
 * wholeSetPage reports a complete collection as a single page.
 *
 * It is the honest answer from a transport that cannot page — one whose wire
 * format carries no paging fields, or a daemon older than this SDK. The caller
 * is told it has everything (`total` is the length and there is no
 * `next_offset`) rather than being handed a first page it would ask a second
 * of and never receive.
 */
export declare function wholeSetPage<T>(items: T[]): PageResult<T>;
//# sourceMappingURL=page.d.ts.map