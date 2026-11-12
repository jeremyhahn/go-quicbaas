// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

// ---------------------------------------------------------------------------
// Orchestrator prompt-override domain types.
//
// Mirrors pkg/api/rest/handlers_prompt.go (/api/v1/prompts/*),
// pkg/service/prompt_service.go and pkg/orchestration/prompts. A prompt is
// addressed by a set and a name, and resolves through a chain of layers
// (project -> tenant -> system -> embedded); each override is versioned.
//
// Wire note: timestamps arrive as RFC 3339 strings; int64 values are
// numbers. Every route takes the optional PromptAddress query fields.
// ---------------------------------------------------------------------------

/** PromptScope is the layer a prompt override lives in. */
export type PromptScope = 'project' | 'tenant' | 'system' | 'embedded';

/** PromptAddress selects the layer a request reads or writes (query fields). */
export interface PromptAddress {
  tenant_id?: string;
  scope?: PromptScope;
  project_id?: string;
}

/** PromptVersionRef names one revision of a prompt at one layer. */
export interface PromptVersionRef {
  set: string;
  name: string;
  scope: PromptScope;
  scope_id?: string;
  version: number;
  hash: string;
}

/** PromptListEntry is one prompt of a set, as seen from a layer. */
export interface PromptListEntry {
  name: string;
  description?: string;
  overridden: boolean;
  version?: number;
  follow_parent?: boolean;
  effective: PromptVersionRef;
}

/** PromptList is the GET /api/v1/prompts/{set} response. */
export interface PromptList {
  set: string;
  scope: PromptScope;
  prompts: PromptListEntry[];
}

/** PromptView is one prompt override at one layer. */
export interface PromptView {
  tenant_id: string;
  project_id?: string;
  scope: PromptScope;
  set: string;
  name: string;
  version: number;
  content_hash: string;
  content: string;
  follow_parent: boolean;
  updated_at: string;
}

/** PromptLayer is one layer of the resolution chain. */
export interface PromptLayer {
  scope: PromptScope;
  scope_id?: string;
  present: boolean;
  version?: number;
  hash?: string;
  follow_parent?: boolean;
  answered: boolean;
}

/** EffectivePrompt is the content a layer resolves to, and where it came from. */
export interface EffectivePrompt {
  set: string;
  name: string;
  scope: PromptScope;
  content: string;
  origin: PromptVersionRef;
  chain: PromptLayer[];
}

/** PromptDiffLine is one line of a diff against the parent layer. */
export interface PromptDiffLine {
  op: 'equal' | 'delete' | 'insert';
  text: string;
}

/** PromptDiff compares a layer's effective prompt with its parent's. */
export interface PromptDiff {
  set: string;
  name: string;
  scope: PromptScope;
  origin: PromptVersionRef;
  parent_origin: PromptVersionRef;
  same: boolean;
  lines: PromptDiffLine[];
}

/** PromptRevisionSummary is one entry of a prompt's history. */
export interface PromptRevisionSummary {
  version: number;
  content_hash: string;
  author: string;
  note?: string;
  follow_parent: boolean;
  created_at: string;
}

/** PromptHistory lists a prompt's revisions at one layer. */
export interface PromptHistory {
  set: string;
  name: string;
  scope: PromptScope;
  revisions: PromptRevisionSummary[];
}

/** PutPromptRequest writes a new override revision. */
export interface PutPromptRequest {
  content: string;
  note?: string;
  expected_version?: number;
}

/** RollbackPromptRequest restores an earlier revision as a new one. */
export interface RollbackPromptRequest {
  to_version: number;
  note?: string;
  expected_version?: number;
}

/** GetPromptOptions selects the layer and, optionally, a revision. */
export type GetPromptOptions = PromptAddress & { version?: number };

/** ResetPromptOptions selects the layer, with an optional guard and note. */
export type ResetPromptOptions = PromptAddress & { expected_version?: number; note?: string };

/** PromptsClient reads, overrides, diffs and rolls back orchestrator prompts. */
export interface PromptsClient {
  /** Lists the prompts of a set. */
  listPrompts(set: string, addr?: PromptAddress): Promise<PromptList>;
  /** Reads a prompt override (a given revision when version is set). */
  getPrompt(set: string, name: string, opts?: GetPromptOptions): Promise<PromptView>;
  /** Writes a new override revision. */
  putPrompt(set: string, name: string, req: PutPromptRequest, addr?: PromptAddress): Promise<PromptView>;
  /** Resets the override so the layer follows its parent. */
  resetPrompt(set: string, name: string, opts?: ResetPromptOptions): Promise<PromptView>;
  /** Resolves the effective prompt and its layer chain. */
  getEffectivePrompt(set: string, name: string, addr?: PromptAddress): Promise<EffectivePrompt>;
  /** Diffs the layer's effective prompt against its parent's. */
  diffPrompt(set: string, name: string, addr?: PromptAddress): Promise<PromptDiff>;
  /** Lists the override's revisions. */
  getPromptHistory(set: string, name: string, addr?: PromptAddress): Promise<PromptHistory>;
  /** Restores an earlier revision as a new one. */
  rollbackPrompt(
    set: string,
    name: string,
    req: RollbackPromptRequest,
    addr?: PromptAddress,
  ): Promise<PromptView>;
}
