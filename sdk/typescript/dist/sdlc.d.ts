/**
 * The SDLC module of the Software Factory: a project's forge binding and a
 * task's delivery. Tasks, jobs and sessions are the orchestrator client's;
 * the module's records only reference them.
 */
import type { AgentRef } from './agents.js';
/** ForgeConfig names the repository a project delivers to. */
export interface ForgeConfig {
    type: string;
    base_url: string;
    owner: string;
    repo: string;
    labels?: Record<string, string>;
    /** The label an open issue must carry before it becomes a task. */
    trigger_label: string;
    webhook_secret_ref?: string;
    token_secret_ref?: string;
    /**
     * The forge's pinned SSH host public key: one OpenSSH public-key line
     * ("<type> <base64> [comment]"). Required when the project is cloned over
     * ssh — the worker container verifies the forge against this key and never
     * learns one.
     */
    ssh_host_key?: string;
}
/** ReviewPolicy decides when a pull request may land. */
export interface ReviewPolicy {
    required_approvals: number;
    require_independent_reviewer: boolean;
    require_green_checks: boolean;
    /** Nanoseconds; zero takes the default. */
    checks_settle_window?: number;
    /** Forge login → model family, for the independence rule. */
    reviewers?: Record<string, string>;
}
/**
 * ReviewerConfig is one reviewer profile: the agent that reviews a project's
 * pull requests, as its own forge identity. Mirrors pkg/sdlc/review_config.go.
 *
 * The binding's `reviewer` is the project's DEFAULT profile; `reviewers` are
 * alternates an operator chooses per pull request. Seat order is the failover
 * chain: a seat whose Agent cannot take the work passes it to the next seat of
 * the same standing. An empty agent_id on the default means the project is
 * not self-reviewed.
 */
export interface ReviewerConfig {
    /** The profile's name; optional on the default (reads as "default"), required on alternates. */
    name?: string;
    /**
     * Whether the profile's verdicts decide whether a change may land. Unset is
     * true on the default and false on an alternate; an advisory profile's
     * verdict is shown and never counted. Leave it unset to take the role's
     * default: an explicit value is kept through a move between roles.
     */
    counts_toward_merge?: boolean;
    /** The Agent that reviews: it must declare the review role. */
    agent_id?: string;
    /** The forge account the reviewer posts as; distinct per profile. */
    login?: string;
    /** The tenant secret holding this profile's forge token; distinct per profile. */
    token_secret_ref?: string;
    /** The commit status context; empty takes the role's default. */
    status_context?: string;
    /**
     * The http(s) root of the model server an opencode reviewer on a local model
     * (`ollama/<name>`) runs against, as its container reaches it. Required for
     * such a reviewer and refused for any other.
     */
    endpoint?: string;
    /** The fleet node the reviewer must run on; strict — an offline node refuses the review. */
    host_affinity?: string;
    /** Prompt material appended after the reviewer charter for every review this seat runs. At most 64 KiB. */
    prompt_append?: string;
}
/**
 * ReviewerSelection is which reviewer profile reviews one pull request, and
 * whether an operator chose it.
 */
export interface ReviewerSelection {
    project_id: string;
    pull_request: number;
    profile: string;
    /** True when an operator chose `profile` for this pull request. */
    overridden: boolean;
    /** False for an advisory review. */
    counts_toward_merge: boolean;
    status_context: string;
    /** When the override was last written; absent with none. */
    updated_at?: string;
    /**
     * True when setting the override started a review by the chosen seat now.
     * False while a review already runs: switching reviewer cancels nothing, and
     * the next dispatch is by the chosen seat.
     */
    redispatched?: boolean;
}
/** RankingPolicy is a project's contribution to task ordering. */
export interface RankingPolicy {
    default_priority?: number;
    analyzer_name?: string;
    declared_dependents?: string[];
}
/**
 * MergeTrainPolicy is a project's merge train: approved pull requests are
 * queued and landed together, CI running once per train. Absent or zero is ON
 * with the default size, for a project with a reviewer of its own.
 */
export interface MergeTrainPolicy {
    /** Merge approved pull requests one at a time instead. */
    disabled?: boolean;
    /** Changes per train; zero takes the default. */
    max_size?: number;
}
/**
 * ClusteringPolicy is a project's issue clustering: related ready issues are
 * adopted as one task and one pull request. Absent or zero is ON with the
 * default size.
 */
export interface ClusteringPolicy {
    /** Make every ready issue its own task. */
    disabled?: boolean;
    /** Issues per task, the anchor included; zero takes the default. */
    max_size?: number;
}
/** ProjectBinding attaches a forge repository and its delivery policy to a project. */
export interface ProjectBinding {
    project_id: string;
    forge: ForgeConfig;
    /** The registered SDLC the project follows: github-flow, trunk-based, gitflow. */
    strategy: string;
    /** The Agent or pool that writes the code; its Agents must declare the coding role. */
    worker: AgentRef;
    /** Prompt material appended after the worker charter for every worker session. At most 64 KiB. */
    worker_prompt_append?: string;
    /** The Agent or pool that triages issues; absent takes the node default, then the worker. */
    triage?: AgentRef;
    /** Prompt material appended after the triage charter for every triage session. At most 64 KiB. */
    triage_prompt_append?: string;
    /** Commands that must pass before the work is offered for review, in order. */
    verification?: string[];
    review: ReviewPolicy;
    /**
     * The project's default reviewer profile; absent or with no agent_id, the
     * project is not self-reviewed. A bind REPLACES the whole record, so a
     * client must write back every field it read, this one included.
     */
    reviewer?: ReviewerConfig;
    /** Alternate reviewer profiles, chosen per pull request by an operator. */
    reviewers?: ReviewerConfig[];
    /** How many review rounds before escalation; zero takes the default. */
    max_review_rounds?: number;
    ranking: RankingPolicy;
    /**
     * What provisioning writes to the forge and what conformance checks. Every
     * field defaults, so a binding that omits this still onboards correctly.
     */
    onboarding?: OnboardingConfig;
    /**
     * The merge train. A bind REPLACES the whole record, so a client must write
     * back what it read: dropping this would turn a disabled train back on.
     */
    merge_train?: MergeTrainPolicy;
    /** Issue clustering in triage. */
    clustering?: ClusteringPolicy;
    /**
     * Forge logins, beyond the repository owner and the node's configured
     * rulers, whose comment on a parked issue is an owner ruling. Each must be
     * the owner or a collaborator of the repository; see `collaborators`.
     */
    rulers?: string[];
    created_at?: string;
    updated_at?: string;
}
/** FileIssueRequest files one issue on a bound project's repository. */
export interface FileIssueRequest {
    /** One line, at most 255 characters. */
    title: string;
    body?: string;
    /** Label names the repository already defines, e.g. task:ready. */
    labels?: string[];
    /** The issue this one was split out of; omit for none. */
    parent?: number;
}
/** ForgeIssue is an issue as the forge stored it. */
export interface ForgeIssue {
    number: number;
    title: string;
    body?: string;
    state?: string;
    labels?: string[];
    author?: string;
    url?: string;
}
/** ForgeUser is one account with access to a repository. */
export interface ForgeUser {
    login: string;
    full_name?: string;
}
/** CollaboratorsResponse lists the repository's owner first, then every collaborator. */
export interface CollaboratorsResponse {
    users: ForgeUser[];
    count: number;
}
/** DeliveryPullRequest is the pull request an attempt opened. */
export interface DeliveryPullRequest {
    number: number;
    url?: string;
    state: 'open' | 'merged' | 'closed';
    title?: string;
    base?: string;
}
/** ReviewRecord is one reviewer's verdict on one head. */
export interface ReviewRecord {
    reviewer: string;
    model_family?: string;
    verdict: 'approve' | 'request_changes' | 'error';
    head_sha: string;
    external_id?: string;
    round: number;
    findings?: string;
    submitted_at: string;
}
/** CheckRecord is the latest state of one check context on one head. */
export interface CheckRecord {
    name: string;
    head_sha: string;
    status: 'pending' | 'success' | 'failure' | 'cancelled';
    forge_run_id?: string;
    finished_at?: string;
}
/** Delivery records the git side of one attempt at a task. */
export interface Delivery {
    task_id: string;
    project_id: string;
    attempt: number;
    branch: string;
    /** The ref the branch was cut from. */
    base: string;
    /** The ref the branch lands on; a strategy may cut from one and target another. */
    target: string;
    /** The branch plan's review decision, recorded when the branch was cut. */
    requires_pull_request: boolean;
    /** Whether the branch goes once it has landed. */
    delete_after_merge?: boolean;
    head_sha?: string;
    work_dir?: string;
    pull_request: DeliveryPullRequest;
    reviews?: ReviewRecord[];
    checks?: CheckRecord[];
    version: number;
    created_at: string;
    updated_at: string;
}
/**
 * OnboardingConfig declares what provisioning writes to the forge and what
 * conformance checks for one bound repository. Every field defaults, so a
 * binding that says nothing about onboarding still onboards correctly.
 * Mirrors pkg/sdlc/onboarding_config.go.
 */
export interface OnboardingConfig {
    webhook_events?: string[];
    protected_branch?: string;
    required_status_contexts?: string[];
    forbidden_paths?: string[];
    forbidden_paths_ref?: string;
    /** Applied on CREATION only; an existing label's colour is never changed. */
    label_colors?: Record<string, string>;
}
/**
 * ConformanceFinding is one thing that is wrong, and what to do about it.
 * Every finding carries a remedy: the repairs differ, and "not conformant"
 * without a reason is useless. Mirrors pkg/sdlc/onboarding.go.
 */
export interface ConformanceFinding {
    /**
     * class is the failure mode: binding_invalid, webhook_missing,
     * webhook_inactive, webhook_misdirected, webhook_events_missing,
     * label_missing, stale_workflow, protection_missing,
     * protection_context_missing, check_unsupported, check_failed.
     */
    class: string;
    check: string;
    subject?: string;
    detail: string;
    remedy: string;
}
/** ConformanceCheckResult is one check's verdict. */
export interface ConformanceCheckResult {
    name: string;
    /** pass | fail | unsupported | not_applicable | error */
    status: string;
    findings: number;
}
/**
 * ConformanceReport answers "is this project correctly onboarded, and if not,
 * what is wrong".
 */
export interface ConformanceReport {
    project_id: string;
    tenant_id?: string;
    repo?: string;
    /**
     * conformant is true only when every check that RAN passed. A check that
     * errored makes the report non-conformant; one the platform does not
     * support, or the binding does not ask for, does not.
     */
    conformant: boolean;
    webhook_url?: string;
    checks: ConformanceCheckResult[];
    findings?: ConformanceFinding[];
    checked_at: string;
}
/**
 * OnboardingReport is what a provisioning pass did, plus the conformance the
 * repository is in afterwards.
 */
export interface OnboardingReport {
    project_id: string;
    tenant_id?: string;
    repo?: string;
    webhook_url?: string;
    webhook_ensured: boolean;
    /** Empty on a repeat run: the idempotency evidence an operator sees. */
    labels_created?: string[];
    conformance?: ConformanceReport;
}
/** OpenPullRequest is one open pull request the project's agent opened. */
export interface OpenPullRequest {
    number: number;
    title: string;
    head_ref: string;
    head_sha: string;
    /** success, failure, pending or none. */
    ci_state: 'success' | 'failure' | 'pending' | 'none';
    /** approved, changes_requested or awaiting_review. */
    review_state: 'approved' | 'changes_requested' | 'awaiting_review';
    url?: string;
    created: string;
    /** How long the pull request had been open when the report was read. */
    age_seconds: number;
}
/** OpenIssue is one open issue carrying a label the factory writes. */
export interface OpenIssue {
    number: number;
    title: string;
    labels: string[];
    url?: string;
    created: string;
    /** How long the issue had been open when the report was read. */
    age_seconds: number;
}
/**
 * OpenWork is one bound project's open work on its forge: the open pull
 * requests its agent opened and the open issues carrying a factory label.
 */
export interface OpenWork {
    project_id: string;
    /** The bound repository as "owner/repo". */
    repo: string;
    /** The forge account the binding's token authenticates as. */
    agent: string;
    pull_requests: OpenPullRequest[];
    issues: OpenIssue[];
    read_at: string;
}
/** SdlcClient is the SDLC module's client surface. */
export interface SdlcClient {
    /**
     * forges reports the forge platforms this build has an adapter for, so a
     * caller offers an unavailable one disabled with the reason rather than
     * hiding it or discovering the refusal only on bind.
     * GET /api/v1/sdlc/forges
     */
    forges(): Promise<string[]>;
    /**
     * bind attaches a forge binding to a project, replacing any earlier one.
     * PUT /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
     */
    bind(tenantId: string, projectId: string, binding: ProjectBinding): Promise<ProjectBinding>;
    /**
     * unbind detaches the project's binding.
     * DELETE /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
     */
    unbind(tenantId: string, projectId: string): Promise<void>;
    /**
     * binding reads the project's binding.
     * GET /api/v1/sdlc/projects/:projectId/binding?tenant_id=...
     */
    binding(tenantId: string, projectId: string): Promise<ProjectBinding>;
    /**
     * delivery reads what the task's attempt published.
     * GET /api/v1/sdlc/tasks/:taskId/delivery?tenant_id=...
     */
    delivery(tenantId: string, taskId: string): Promise<Delivery>;
    /**
     * onboard provisions the bound repository's forge configuration — the
     * webhook that delivers to this node and the label vocabulary the binding
     * declares — idempotently. It is the repair for a conformance finding;
     * conformance itself only reports.
     * POST /api/v1/sdlc/projects/:projectId/onboarding?tenant_id=...
     */
    onboard(tenantId: string, projectId: string): Promise<OnboardingReport>;
    /**
     * conformance reports whether the project is correctly onboarded and what
     * is wrong if it is not. Read-only: it changes nothing.
     * GET /api/v1/sdlc/projects/:projectId/conformance?tenant_id=...
     */
    conformance(tenantId: string, projectId: string): Promise<ConformanceReport>;
    /**
     * openWork lists a bound project's open work on its forge: the open pull
     * requests its agent opened, with CI and review state, and the open issues
     * carrying a factory label. Read-only.
     * GET /api/v1/sdlc/projects/:projectId/open-work?tenant_id=...
     */
    openWork(tenantId: string, projectId: string): Promise<OpenWork>;
    /**
     * reviewer reports which reviewer profile reviews a pull request: the
     * operator's override when one is set, the binding's default otherwise.
     * GET /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer?tenant_id=...
     */
    reviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    /**
     * setReviewer chooses which of the binding's profiles reviews one pull
     * request. An operator verb; nothing a forge sends can set it.
     * PUT /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer?tenant_id=...
     */
    setReviewer(tenantId: string, projectId: string, pullRequest: number, profile: string): Promise<ReviewerSelection>;
    /**
     * clearReviewer removes a pull request's override, so the binding's default
     * reviews it again.
     * DELETE /api/v1/sdlc/projects/:projectId/pulls/:number/reviewer?tenant_id=...
     */
    clearReviewer(tenantId: string, projectId: string, pullRequest: number): Promise<ReviewerSelection>;
    /**
     * fileIssue files an issue on the bound repository through the binding's
     * forge token. An audited operator verb gated sdlc:issue_create.
     * POST /api/v1/sdlc/projects/:projectId/issues?tenant_id=...
     */
    fileIssue(tenantId: string, projectId: string, req: FileIssueRequest): Promise<ForgeIssue>;
    /**
     * collaborators lists the accounts that may rule on the bound repository:
     * its owner first, then every collaborator. Read-only.
     * GET /api/v1/sdlc/projects/:projectId/collaborators?tenant_id=...
     */
    collaborators(tenantId: string, projectId: string): Promise<CollaboratorsResponse>;
    /**
     * adoptIssue hands one forge issue back to the factory: its task is
     * created — on the named pull request's own head branch when one is named —
     * its holds come off, the trigger label goes on and the ruling is posted.
     * Refused (409) while a live task still holds the issue.
     * POST /api/v1/sdlc/projects/:projectId/adopt?tenant_id=...
     */
    adoptIssue(tenantId: string, projectId: string, request: AdoptIssueRequest): Promise<Adoption>;
}
/** AdoptIssueRequest names the issue an operator adopts. */
export interface AdoptIssueRequest {
    /** The issue number on the project's repository. */
    issue: number;
    /** An open pull request the task resumes, by number. */
    pull_request?: number;
    /** The operator's instruction, appended to the contract and posted on the issue. */
    ruling?: string;
}
/** Adoption is what an adoption did. */
export interface Adoption {
    /** The task created for the issue. */
    task_id: string;
    issue: number;
    /** The pull request the task resumes, absent for none. */
    pull_request?: number;
    /** The pull request's head branch the delivery works on. */
    branch?: string;
    /** The finished task that held the issue before, if any. */
    superseded?: string;
}
//# sourceMappingURL=sdlc.d.ts.map