/**
 * PromptAddress names one prompt of a set at a scope. tenant_id, scope and
 * project_id narrow the layer; omitted, the server answers for the caller's
 * own scope.
 */
export interface PromptAddress {
    tenant_id?: string;
    scope?: string;
    project_id?: string;
    set: string;
    name?: string;
}
/** ListPromptsRequest lists the prompts of a set at a scope. */
export interface ListPromptsRequest {
    tenant_id?: string;
    scope?: string;
    project_id?: string;
    set: string;
}
/** GetPromptRequest reads a prompt's record; version 0 reads the latest. */
export interface GetPromptRequest extends PromptAddress {
    version?: number;
}
/** PutPromptRequest writes a new revision; expected_version guards the write. */
export interface PutPromptRequest extends PromptAddress {
    content: string;
    note: string;
    expected_version: number;
}
/** RollbackPromptRequest makes an earlier revision the latest. */
export interface RollbackPromptRequest extends PromptAddress {
    to_version: number;
    note?: string;
    expected_version: number;
}
/** ResetPromptRequest drops this scope's override, so the parent answers. */
export interface ResetPromptRequest extends PromptAddress {
    note?: string;
    expected_version: number;
}
/** PromptVersionRef pins the layer and revision a body came from. */
export interface PromptVersionRef {
    set: string;
    name: string;
    scope: string;
    scope_id?: string;
    version: number;
    hash: string;
}
/** PromptRecord is one stored revision of a prompt at a scope. */
export interface PromptRecord {
    tenant_id: string;
    project_id?: string;
    scope: string;
    set: string;
    name: string;
    version: number;
    content_hash: string;
    content: string;
    follow_parent: boolean;
    updated_at: string;
}
/** PromptListEntry is one prompt of a listed set. */
export interface PromptListEntry {
    name: string;
    description?: string;
    /** overridden says this scope holds an edited record. */
    overridden: boolean;
    version?: number;
    follow_parent?: boolean;
    /** effective is the body a job at this scope runs with. */
    effective: PromptVersionRef;
}
/** ListPromptsResponse is a set's prompts at a scope. */
export interface ListPromptsResponse {
    set: string;
    scope: string;
    prompts: PromptListEntry[];
}
/** PromptLayer is one scope of the resolution chain. */
export interface PromptLayer {
    scope: string;
    scope_id?: string;
    present: boolean;
    version?: number;
    hash?: string;
    follow_parent?: boolean;
    /** answered marks the layer the body came from. */
    answered: boolean;
}
/** EffectivePrompt is the body a job at this scope runs with, and why. */
export interface EffectivePrompt {
    set: string;
    name: string;
    scope: string;
    content: string;
    origin: PromptVersionRef;
    chain: PromptLayer[];
}
/** PromptDiffOp is one line operation of a prompt diff. */
export type PromptDiffOp = 'equal' | 'delete' | 'insert';
/** PromptDiffLine is one line of a prompt diff. */
export interface PromptDiffLine {
    op: PromptDiffOp;
    text: string;
}
/** PromptDiff compares this scope's effective body with its parent's. */
export interface PromptDiff {
    set: string;
    name: string;
    scope: string;
    origin: PromptVersionRef;
    parent_origin: PromptVersionRef;
    same: boolean;
    lines: PromptDiffLine[];
}
/** PromptRevisionSummary is one revision in a prompt's history. */
export interface PromptRevisionSummary {
    version: number;
    content_hash: string;
    author: string;
    note?: string;
    follow_parent: boolean;
    created_at: string;
}
/** PromptHistory is a prompt's revisions at a scope, oldest first, the current revision last, as the server returns them. */
export interface PromptHistory {
    set: string;
    name: string;
    scope: string;
    revisions: PromptRevisionSummary[];
}
/**
 * PromptsClient reads and edits the layered prompt library. Each verb mirrors
 * the Go SDK's prompt.Client and maps to one REST route and one MCP tool.
 */
export interface PromptsClient {
    /** listPrompts: GET /api/v1/prompts/{set}; MCP prompt_list. */
    listPrompts(req: ListPromptsRequest): Promise<ListPromptsResponse>;
    /** getPrompt: GET /api/v1/prompts/{set}/{name}; MCP prompt_get. */
    getPrompt(req: GetPromptRequest): Promise<PromptRecord>;
    /** getEffectivePrompt: GET /api/v1/prompts/{set}/{name}/effective; MCP prompt_effective. */
    getEffectivePrompt(addr: PromptAddress): Promise<EffectivePrompt>;
    /** diffPrompt: GET /api/v1/prompts/{set}/{name}/diff; MCP prompt_diff. */
    diffPrompt(addr: PromptAddress): Promise<PromptDiff>;
    /** getPromptHistory: GET /api/v1/prompts/{set}/{name}/history; MCP prompt_history. */
    getPromptHistory(addr: PromptAddress): Promise<PromptHistory>;
    /** putPrompt: PUT /api/v1/prompts/{set}/{name}; MCP prompt_put. */
    putPrompt(req: PutPromptRequest): Promise<PromptRecord>;
    /** rollbackPrompt: POST /api/v1/prompts/{set}/{name}/rollback; MCP prompt_rollback. */
    rollbackPrompt(req: RollbackPromptRequest): Promise<PromptRecord>;
    /** resetPrompt: DELETE /api/v1/prompts/{set}/{name}; MCP prompt_reset. */
    resetPrompt(req: ResetPromptRequest): Promise<PromptRecord>;
}
/** promptScopeQuery carries the scope fields the REST routes read as query. */
export declare function promptScopeQuery(scope: {
    tenant_id?: string;
    scope?: string;
    project_id?: string;
}): URLSearchParams;
/**
 * promptPath builds /api/v1/prompts/{set}[/{name}]{suffix}[?query]. A verb
 * that addresses one prompt and has no name is refused before any request:
 * the route would silently become the set's listing.
 */
export declare function promptPath(addr: {
    set: string;
    name?: string;
}, suffix: string, query: URLSearchParams, method: string): string;
//# sourceMappingURL=prompts.d.ts.map