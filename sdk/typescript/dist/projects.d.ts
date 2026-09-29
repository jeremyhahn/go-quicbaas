import type { SpendLimits } from './spend.js';
import type { AgentRef } from './agents.js';
import type { PageResult, ProjectPageParams } from './page.js';
/** ProjectRole defines the level of access an agent has within a project. */
export type ProjectRole = 'lead' | 'worker' | 'observer' | 'denied';
/**
 * ContextRepo names one repository the worker clones beside the project
 * checkout. The daemon never clones it.
 */
export interface ContextRepo {
    /** name is the directory the repository is cloned into (one path segment). */
    name: string;
    /** repo_url is the remote the worker clones from. */
    repo_url: string;
    /** ref is the branch, tag or commit the worker checks out. */
    ref: string;
}
/**
 * WorkspaceTier is the storage tier of a project's sandboxed checkout. The
 * empty string keeps the host bind mount and behaves exactly like
 * "persistent"; "ephemeral" puts the checkout on a sized tmpfs inside the
 * container. See docs/software-factory/storage-tiers.md.
 */
export type WorkspaceTier = '' | 'persistent' | 'ephemeral';
/**
 * SessionRotationPolicy is what a persistent worker session does when the
 * pooled provider account under it rotates: "resume" continues the same
 * conversation in the replacement session, "fresh" starts a new one on the
 * same branch. A project that never chose one stores nothing and runs
 * "resume". See docs/software-factory/sdlc.md.
 */
export type SessionRotationPolicy = 'resume' | 'fresh';
/**
 * Project is the canonical project entity returned by the server.
 *
 * THE SHAPE IS FLAT, and it is flat because the server's is: every transport
 * serializes `pkg/api/rest.ProjectRESTResponse`, whose git, workspace, quota
 * and permission fields are all top-level `git_*` / `workspace_*` /
 * `gpu_minutes` / `agent_roles` keys. A nested mirror of it here read
 * `project.git.repo_url` off a body that never carried one, so every existing
 * project's Git settings edited as blank (go-quicbaas#797). One shape, and it
 * is the one on the wire.
 */
/** RetiredSigningKey is one entry of a project's trusted signing history. */
export interface RetiredSigningKey {
    /** public_key is the retired key's public half, one OpenSSH line. */
    public_key: string;
    /** name is the KMS name the key was held under. */
    name?: string;
    /** retired_at (RFC 3339) is when the key stopped being the project's. */
    retired_at: string;
}
export interface Project {
    id: string;
    tenant_id: string;
    name: string;
    description?: string;
    /** workspace_agent_id is the agent that owns the workspace. */
    workspace_agent_id?: string;
    /** workspace_path is the workspace path on the agent host. */
    workspace_path?: string;
    host_affinity?: string;
    /** default_lead_agent is the Agent or pool a job in this project leads with when it names none. */
    default_lead_agent?: AgentRef;
    /** default_workflow is the tenant-scoped workflow a Task auto-spawns into. */
    default_workflow?: string;
    /** autonomous reports whether autonomous development is on for this project. */
    autonomous?: boolean;
    /**
     * parked reports that the project's intake is closed: nothing that has not
     * started is dispatched, adopted or triaged, and work in flight finishes.
     * parked_at (RFC 3339) and parked_by say when and by whom.
     */
    parked?: boolean;
    parked_at?: string;
    parked_by?: string;
    /** context_repos are cloned by the worker beside the project checkout. */
    context_repos?: ContextRepo[];
    /** git_repo_url is the remote git repository URL. */
    git_repo_url?: string;
    /** git_default_branch is the default git branch (e.g. "main"). */
    git_default_branch?: string;
    /** git_current_sha is the commit SHA last recorded by an agent job run. */
    git_current_sha?: string;
    /** git_author_name / git_author_email are the commit identity of the workspace. */
    git_author_name?: string;
    git_author_email?: string;
    /** git_signing_enabled reports whether commits are signed; an enabled policy names a key. */
    git_signing_enabled?: boolean;
    /** git_signing_algorithm is ED25519 (default) or ECDSA-P256 (FIPS). */
    git_signing_algorithm?: string;
    /** git_signing_key_* name the KMS-held signing key. Never material. */
    git_signing_key_name?: string;
    git_signing_key_scope?: string;
    git_signing_key_owner?: string;
    /** git_ssh_key_* name the KMS key an ssh:// git_repo_url is reached with. */
    git_ssh_key_name?: string;
    git_ssh_key_scope?: string;
    git_ssh_key_owner?: string;
    /**
     * git_signing_retired is the project's trusted signing history: keys it
     * signed with before the current one, each trusted only for commits made
     * before its retirement.
     */
    git_signing_retired?: RetiredSigningKey[];
    /** agent_roles maps agent IDs to their assigned role. */
    agent_roles?: Record<string, ProjectRole>;
    /**
     * spend is the project's spend limits: sliding windows in tokens and/or
     * list-price dollars over every turn charged to it. Stored; usage reporting
     * against them and admission enforcement are not wired yet. Absent meters nothing.
     */
    spend?: SpendLimits;
    /** gpu_minutes is the maximum GPU compute budget in minutes (0 = unlimited). */
    gpu_minutes?: number;
    /** max_concurrency is the maximum concurrent workflow runs (0 = unlimited). */
    max_concurrency?: number;
    /** workspace_tier is the storage tier of the sandboxed checkout; absent keeps the host bind mount. */
    workspace_tier?: WorkspaceTier;
    /** workspace_tmpfs_mb sizes the ephemeral tier's tmpfs in MiB (0 or absent = unsized). */
    workspace_tmpfs_mb?: number;
    /** container_memory_mb is the container memory limit in MiB (0 or absent = the runtime's default). */
    container_memory_mb?: number;
    /** session_rotation is the project's session rotation policy; absent means the project never chose one and runs "resume". */
    session_rotation?: SessionRotationPolicy;
    metadata?: Record<string, string>;
    created_at: string;
    updated_at: string;
}
/** CreateProjectRequest is the payload for creating a new project. */
export interface CreateProjectRequest {
    /** tenant_id is the tenant that owns the project. Required. */
    tenant_id: string;
    /** name is the unique project name within the tenant. Required. */
    name: string;
    description?: string;
    workspace_agent_id?: string;
    workspace_path?: string;
    host_affinity?: string;
    /** default_lead_agent is the Agent or pool a job in this project leads with when it names none. */
    default_lead_agent?: AgentRef;
    git_repo_url?: string;
    git_default_branch?: string;
    /** spend is the project's spend limits; absent meters nothing. */
    spend?: SpendLimits;
    gpu_minutes?: number;
    max_concurrency?: number;
    metadata?: Record<string, string>;
    /** default_workflow names the tenant-scoped workflow a Task auto-spawns into; it must be registered in the tenant. */
    default_workflow?: string;
    /** autonomous turns autonomous development on for the new project. */
    autonomous?: boolean;
    /** context_repos are cloned by the worker beside the project checkout. */
    context_repos?: ContextRepo[];
    /** git_author_name / git_author_email are the commit identity of the workspace. Both or neither. */
    git_author_name?: string;
    git_author_email?: string;
    /** git_signing_enabled turns commit signing on; an enabled policy names git_signing_key_name. */
    git_signing_enabled?: boolean;
    /** git_signing_algorithm is ED25519 (default) or ECDSA-P256 (FIPS). */
    git_signing_algorithm?: string;
    git_signing_key_name?: string;
    git_signing_key_scope?: string;
    git_signing_key_owner?: string;
    /** git_ssh_key_* reference the KMS key an ssh:// git_repo_url is reached with. Empty means the signing key. */
    git_ssh_key_name?: string;
    git_ssh_key_scope?: string;
    git_ssh_key_owner?: string;
    /** workspace_tier selects the storage tier of the sandboxed checkout; an unrecognized tier is refused. */
    workspace_tier?: WorkspaceTier;
    /** workspace_tmpfs_mb sizes the ephemeral tier's tmpfs in MiB (ephemeral tier only). */
    workspace_tmpfs_mb?: number;
    /** container_memory_mb is the container memory limit in MiB. */
    container_memory_mb?: number;
    /** session_rotation is what a worker session does when its pooled account rotates; absent runs "resume". */
    session_rotation?: SessionRotationPolicy;
}
/** CreateProjectResponse is the server response after project creation. */
export interface CreateProjectResponse {
    project: Project;
}
/** GetProjectResponse is the server response for a project lookup. */
export interface GetProjectResponse {
    project: Project;
}
/** UpdateProjectRequest is the payload for updating an existing project. */
export interface UpdateProjectRequest {
    /** tenant_id identifies the owning tenant. Required. */
    tenant_id: string;
    /** project_id is the project UUID. Required. */
    project_id: string;
    name?: string;
    description?: string;
    /** clear_description removes the description when true. */
    clear_description?: boolean;
    workspace_agent_id?: string;
    workspace_path?: string;
    host_affinity?: string;
    /** default_lead_agent is the Agent or pool a job in this project leads with when it names none. */
    default_lead_agent?: AgentRef;
    git_repo_url?: string;
    git_default_branch?: string;
    /** git_current_sha records the latest commit SHA observed by an agent. */
    git_current_sha?: string;
    agent_roles?: Record<string, ProjectRole>;
    /** spend replaces the project's spend limits when present; `{}` clears them. */
    spend?: SpendLimits;
    gpu_minutes?: number;
    max_concurrency?: number;
    metadata?: Record<string, string>;
    /** default_workflow sets the auto-spawn workflow when non-empty; clear_default_workflow removes it. */
    default_workflow?: string;
    clear_default_workflow?: boolean;
    /** autonomous updates the autonomous-development switch when present (omit it to leave it unchanged). */
    autonomous?: boolean;
    /** parked parks (true) or resumes (false) the project when present (omit it to leave it unchanged). */
    parked?: boolean;
    /** context_repos replaces the whole list when present; clear_context_repos empties it. */
    context_repos?: ContextRepo[];
    clear_context_repos?: boolean;
    /** git_author_name / git_author_email update the commit identity when non-empty. */
    git_author_name?: string;
    git_author_email?: string;
    /** git_signing_enabled updates the signing switch when present (omit it to leave signing unchanged). */
    git_signing_enabled?: boolean;
    git_signing_algorithm?: string;
    git_signing_key_name?: string;
    git_signing_key_scope?: string;
    git_signing_key_owner?: string;
    /** git_ssh_key_* update the access key reference when non-empty. */
    git_ssh_key_name?: string;
    git_ssh_key_scope?: string;
    git_ssh_key_owner?: string;
    /**
     * git_signing_retire_key_* name a KMS-held key the project signed with
     * before its current one; the server keeps its public half in the trusted
     * signing history, retired at git_signing_retired_at (RFC 3339; omit for
     * now), so commits it signed before then still verify.
     */
    git_signing_retire_key_name?: string;
    git_signing_retire_key_scope?: string;
    git_signing_retire_key_owner?: string;
    git_signing_retired_at?: string;
    /** workspace_tier updates the storage tier when non-empty. */
    workspace_tier?: WorkspaceTier;
    /**
     * workspace_tmpfs_mb / container_memory_mb update the sizes when PRESENT:
     * omit a key to leave it unchanged, send 0 to lift the limit.
     */
    workspace_tmpfs_mb?: number;
    container_memory_mb?: number;
    /** session_rotation updates the session rotation policy when present. */
    session_rotation?: SessionRotationPolicy;
}
/** UpdateProjectResponse is the server response after a project update. */
export interface UpdateProjectResponse {
    project: Project;
}
/** DeleteProjectResponse is the server response after a project deletion. */
export interface DeleteProjectResponse {
    deleted: boolean;
}
/** ListProjectsResponse is the server response for listing projects. */
export interface ListProjectsResponse {
    projects: Project[];
    total: number;
}
/** GrantFleetNodeRoleRequest is the payload for setting a fleet node's project role. */
export interface GrantFleetNodeRoleRequest {
    /** tenant_id identifies the owning tenant. Required. */
    tenant_id: string;
    /** project_id is the project UUID. Required. */
    project_id: string;
    /** node_id is the fleet node ID. Required. */
    node_id: string;
    /** role is the role to assign. Required. */
    role: ProjectRole;
}
/** GrantFleetNodeRoleResponse is the server response after role assignment. */
export interface GrantFleetNodeRoleResponse {
    project_id: string;
    node_id: string;
    role: ProjectRole;
    timestamp: string;
}
/** RevokeFleetNodeRoleRequest is the payload for revoking a fleet node's project access. */
export interface RevokeFleetNodeRoleRequest {
    /** tenant_id identifies the owning tenant. Required. */
    tenant_id: string;
    /** project_id is the project UUID. Required. */
    project_id: string;
    /** node_id is the fleet node ID. Required. */
    node_id: string;
}
/** RevokeFleetNodeRoleResponse is the server response after role revocation. */
export interface RevokeFleetNodeRoleResponse {
    project_id: string;
    node_id: string;
    timestamp: string;
}
/**
 * ProjectsClient exposes project lifecycle management operations.
 * Maps to /api/v1/projects REST endpoints.
 */
export interface ProjectsClient {
    /**
     * createProject creates a new project within a tenant.
     * POST /api/v1/projects
     */
    createProject(req: CreateProjectRequest): Promise<CreateProjectResponse>;
    /**
     * getProject retrieves a project by ID.
     * GET /api/v1/projects/:projectId?tenant_id=...
     */
    getProject(tenantId: string, projectId: string): Promise<GetProjectResponse>;
    /**
     * updateProject updates an existing project.
     * PUT /api/v1/projects/:projectId
     */
    updateProject(req: UpdateProjectRequest): Promise<UpdateProjectResponse>;
    /**
     * deleteProject removes a project by ID.
     * DELETE /api/v1/projects/:projectId?tenant_id=...
     */
    deleteProject(tenantId: string, projectId: string): Promise<DeleteProjectResponse>;
    /**
     * listProjects returns a tenant's projects.
     * GET /api/v1/projects?tenant_id=...
     *
     * ONE PAGE OF THEM since the endpoint gained paging: it sends no `limit`
     * and the server's default page is 25. Use listProjectsPage for the count
     * and the page after.
     */
    listProjects(tenantId: string): Promise<ListProjectsResponse>;
    /**
     * listProjectsPage returns ONE PAGE of a tenant's projects, newest first,
     * with the count of everything that matched.
     * GET /api/v1/projects?tenant_id=...&limit=&offset=&sort=&q=
     */
    listProjectsPage(tenantId: string, page?: ProjectPageParams): Promise<PageResult<Project>>;
    /**
     * grantFleetNodeRole sets or updates the role for an agent within a project.
     * PUT /api/v1/projects/:projectId/permissions/:agentId
     */
    grantFleetNodeRole(req: GrantFleetNodeRoleRequest): Promise<GrantFleetNodeRoleResponse>;
    /**
     * revokeFleetNodeRole sets the agent's role to "denied", blocking all project access.
     * DELETE /api/v1/projects/:projectId/permissions/:agentId
     */
    revokeFleetNodeRole(req: RevokeFleetNodeRoleRequest): Promise<RevokeFleetNodeRoleResponse>;
}
//# sourceMappingURL=projects.d.ts.map