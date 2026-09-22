import type { PageParams, PageResult } from './page.js';
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
 * Project is the canonical project entity returned by the server.
 *
 * THE SHAPE IS FLAT, and it is flat because the server's is: every transport
 * serializes `pkg/api/rest.ProjectRESTResponse`, whose git, workspace, quota
 * and permission fields are all top-level `git_*` / `workspace_*` /
 * `token_budget` / `agent_roles` keys. A nested mirror of it here read
 * `project.git.repo_url` off a body that never carried one, so every existing
 * project's Git settings edited as blank (go-quicbaas#797). One shape, and it
 * is the one on the wire.
 */
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
    default_lead_agent?: string;
    /** default_workflow is the tenant-scoped workflow a Task auto-spawns into. */
    default_workflow?: string;
    /** autonomous reports whether autonomous development is on for this project. */
    autonomous?: boolean;
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
    /** agent_roles maps agent IDs to their assigned role. */
    agent_roles?: Record<string, ProjectRole>;
    /** token_budget is the maximum LLM token budget (0 = unlimited). */
    token_budget?: number;
    /** gpu_minutes is the maximum GPU compute budget in minutes (0 = unlimited). */
    gpu_minutes?: number;
    /** max_concurrency is the maximum concurrent workflow runs (0 = unlimited). */
    max_concurrency?: number;
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
    default_lead_agent?: string;
    git_repo_url?: string;
    git_default_branch?: string;
    token_budget?: number;
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
    default_lead_agent?: string;
    git_repo_url?: string;
    git_default_branch?: string;
    /** git_current_sha records the latest commit SHA observed by an agent. */
    git_current_sha?: string;
    agent_roles?: Record<string, ProjectRole>;
    token_budget?: number;
    gpu_minutes?: number;
    max_concurrency?: number;
    metadata?: Record<string, string>;
    /** default_workflow sets the auto-spawn workflow when non-empty; clear_default_workflow removes it. */
    default_workflow?: string;
    clear_default_workflow?: boolean;
    /** autonomous updates the autonomous-development switch when present (omit it to leave it unchanged). */
    autonomous?: boolean;
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
/** GrantAgentRoleRequest is the payload for setting an agent's project role. */
export interface GrantAgentRoleRequest {
    /** tenant_id identifies the owning tenant. Required. */
    tenant_id: string;
    /** project_id is the project UUID. Required. */
    project_id: string;
    /** agent_id is the agent UUID. Required. */
    agent_id: string;
    /** role is the role to assign. Required. */
    role: ProjectRole;
}
/** GrantAgentRoleResponse is the server response after role assignment. */
export interface GrantAgentRoleResponse {
    project_id: string;
    agent_id: string;
    role: ProjectRole;
    timestamp: string;
}
/** RevokeAgentRoleRequest is the payload for revoking an agent's project access. */
export interface RevokeAgentRoleRequest {
    /** tenant_id identifies the owning tenant. Required. */
    tenant_id: string;
    /** project_id is the project UUID. Required. */
    project_id: string;
    /** agent_id is the agent UUID. Required. */
    agent_id: string;
}
/** RevokeAgentRoleResponse is the server response after role revocation. */
export interface RevokeAgentRoleResponse {
    project_id: string;
    agent_id: string;
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
    listProjectsPage(tenantId: string, page?: PageParams): Promise<PageResult<Project>>;
    /**
     * grantAgentRole sets or updates the role for an agent within a project.
     * PUT /api/v1/projects/:projectId/permissions/:agentId
     */
    grantAgentRole(req: GrantAgentRoleRequest): Promise<GrantAgentRoleResponse>;
    /**
     * revokeAgentRole sets the agent's role to "denied", blocking all project access.
     * DELETE /api/v1/projects/:projectId/permissions/:agentId
     */
    revokeAgentRole(req: RevokeAgentRoleRequest): Promise<RevokeAgentRoleResponse>;
}
//# sourceMappingURL=projects.d.ts.map