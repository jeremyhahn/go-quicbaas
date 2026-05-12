/** ProjectRole defines the level of access an agent has within a project. */
export type ProjectRole = 'lead' | 'worker' | 'observer' | 'denied';
/** ProjectWorkspace describes the agent workspace associated with a project. */
export interface ProjectWorkspace {
    /** agent_id is the ID of the agent that owns the workspace. */
    agent_id?: string;
    /** path is the filesystem path to the workspace on the agent host. */
    path?: string;
}
/** ProjectGit describes the git repository linked to a project. */
export interface ProjectGit {
    /** repo_url is the remote git repository URL. */
    repo_url?: string;
    /** default_branch is the default git branch (e.g. "main"). */
    default_branch?: string;
    /** current_sha is the commit SHA last recorded by an agent job run. */
    current_sha?: string;
}
/** ProjectPermissions defines agent-level access roles for a project. */
export interface ProjectPermissions {
    /** agent_roles maps agent IDs to their assigned role. */
    agent_roles?: Record<string, ProjectRole>;
}
/** ProjectQuotas defines resource limits for a project. */
export interface ProjectQuotas {
    /** token_budget is the maximum LLM token budget (0 = unlimited). */
    token_budget?: number;
    /** gpu_minutes is the maximum GPU compute budget in minutes (0 = unlimited). */
    gpu_minutes?: number;
    /** max_concurrency is the maximum concurrent workflow runs (0 = unlimited). */
    max_concurrency?: number;
}
/** Project is the canonical project entity returned by the server. */
export interface Project {
    id: string;
    tenant_id: string;
    name: string;
    description?: string;
    workspace?: ProjectWorkspace;
    host_affinity?: string;
    default_lead_agent?: string;
    git?: ProjectGit;
    permissions?: ProjectPermissions;
    quotas?: ProjectQuotas;
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
     * listProjects returns all projects for a given tenant.
     * GET /api/v1/projects?tenant_id=...
     */
    listProjects(tenantId: string): Promise<ListProjectsResponse>;
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