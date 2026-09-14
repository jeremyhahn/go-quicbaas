/**
 * The SDLC module of the Software Factory: a project's forge binding and a
 * task's delivery. Tasks, jobs and sessions are the orchestrator client's;
 * the module's records only reference them.
 */
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
/** WorkerConfig names the agent that writes the code for this project. */
export interface WorkerConfig {
    /** The ACP agent backend the worker session runs on, e.g. "claude". */
    backend: string;
    /** The model it runs; empty takes the backend's own default. */
    model: string;
    /** The container image the worker runs in; naming one requires the sandbox. */
    image?: string;
}
/** RankingPolicy is a project's contribution to task ordering. */
export interface RankingPolicy {
    default_priority?: number;
    analyzer_name?: string;
    declared_dependents?: string[];
}
/** ProjectBinding attaches a forge repository and its delivery policy to a project. */
export interface ProjectBinding {
    project_id: string;
    forge: ForgeConfig;
    /** The registered SDLC the project follows: github-flow, trunk-based, gitflow. */
    strategy: string;
    /** The agent that writes the code. */
    worker: WorkerConfig;
    /** Commands that must pass before the work is offered for review, in order. */
    verification?: string[];
    review: ReviewPolicy;
    /** How many review rounds before escalation; zero takes the default. */
    max_review_rounds?: number;
    ranking: RankingPolicy;
    /**
     * What provisioning writes to the forge and what conformance checks. Every
     * field defaults, so a binding that omits this still onboards correctly.
     */
    onboarding?: OnboardingConfig;
    created_at?: string;
    updated_at?: string;
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
}
//# sourceMappingURL=sdlc.d.ts.map