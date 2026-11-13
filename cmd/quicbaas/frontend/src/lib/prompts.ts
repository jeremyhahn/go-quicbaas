// Prompts page vocabulary and its link format.
//
// The Prompts page browses one prompt set at one scope. Other pages link into
// it — the job page links each prompt version a job rendered — with a hash
// query naming the set, scope, project, prompt and version. This file is the
// one place that query is built and read, so the two ends cannot drift.

/** PROMPT_SETS are the prompt sets the Prompts page browses. */
export const PROMPT_SETS: readonly string[] = ['workflow', 'sdlc'];

/** PROMPT_SCOPES are the scopes the Prompts page browses, the first being its default. */
export const PROMPT_SCOPES: readonly string[] = ['tenant', 'project', 'system'];

/** PROMPTS_PATH is the Prompts page's route path. */
export const PROMPTS_PATH = '/prompts';

/** PromptLink opens the Prompts page on one prompt at one version. */
export interface PromptLink {
  set: string;
  scope: string;
  /** project is the project id; present only at project scope. */
  project?: string;
  name: string;
  /** version is the scope record's version; 0 names no stored revision. */
  version: number;
}

/** promptLinkHref is the hash that opens the Prompts page on a link. */
export function promptLinkHref(link: PromptLink): string {
  const query = new URLSearchParams({
    set: link.set,
    scope: link.scope,
    name: link.name,
    version: String(link.version),
  });
  if (link.scope === 'project' && link.project) {
    query.set('project', link.project);
  }
  return `#${PROMPTS_PATH}?${query.toString()}`;
}

/**
 * parsePromptLink reads a link from the Prompts page's route params. It
 * answers null when the params name no prompt, or name a set or scope the
 * page does not browse, or a project scope without its project: the page then
 * opens on its defaults instead of on a selection it cannot show. A version
 * that is not a non-negative integer reads as 0.
 */
export function parsePromptLink(params: Record<string, string>): PromptLink | null {
  const name = (params['name'] ?? '').trim();
  const set = params['set'] ?? '';
  const scope = params['scope'] ?? '';
  const project = (params['project'] ?? '').trim();
  if (name === '' || !PROMPT_SETS.includes(set) || !PROMPT_SCOPES.includes(scope)) {
    return null;
  }
  if (scope === 'project' && project === '') {
    return null;
  }
  const version = Number(params['version'] ?? '0');
  return {
    set,
    scope,
    project: scope === 'project' ? project : undefined,
    name,
    version: Number.isInteger(version) && version > 0 ? version : 0,
  };
}

/** HTTP statuses the Prompts page reads as an availability state. */
const HTTP_FORBIDDEN = 403;
const HTTP_UNAVAILABLE = 503;

/** PromptsUnavailable is why the Prompts page cannot show a set. */
export interface PromptsUnavailable {
  kind: 'forbidden' | 'not-running';
  message: string;
}

/**
 * promptAvailability reads a refused call as one of the page's empty
 * states: 403 names the permission the caller lacks, 503 says the prompt
 * store is not running on the node. Any other error answers null: it is a
 * failure of that one call, not of the page.
 */
export function promptAvailability(err: unknown, permission = 'prompts:read'): PromptsUnavailable | null {
  const status = (err as { status?: unknown } | null)?.status;
  if (status === HTTP_FORBIDDEN) {
    return { kind: 'forbidden', message: `You need the ${permission} permission to see prompts here.` };
  }
  if (status === HTTP_UNAVAILABLE) {
    return { kind: 'not-running', message: 'The prompt store is not running on this node.' };
  }
  return null;
}

/** PLACEHOLDER matches a ${name} placeholder a prompt body carries. */
const PLACEHOLDER = /\$\{([A-Za-z_][A-Za-z0-9_.]*)\}/g;

/**
 * promptPlaceholders lists the ${name} placeholders a body carries, each
 * once, in the order they first appear: the editor's legend. The wire does
 * not carry a prompt's declared variables, so the legend is read from the
 * body the edit starts from.
 */
export function promptPlaceholders(body: string): string[] {
  const seen: string[] = [];
  for (const m of body.matchAll(PLACEHOLDER)) {
    const name = m[1] ?? '';
    if (!seen.includes(name)) seen.push(name);
  }
  return seen;
}

/**
 * missingPlaceholders is every placeholder the original body carries that
 * the draft dropped: a pre-check before Save. The server validates the
 * declared variables and markers on save and names what is missing (400).
 */
export function missingPlaceholders(original: string, draft: string): string[] {
  const kept = new Set(promptPlaceholders(draft));
  return promptPlaceholders(original).filter((p) => !kept.has(p));
}

/** PromptOriginSource is the part of a list entry the origin badge reads. */
export interface PromptOriginSource {
  overridden: boolean;
  effective: { scope: string };
}

/**
 * promptOrigin names where the prompt a scope runs comes from: overridden
 * at this scope, or inherited from the layer that answered. The embedded
 * body ships with the system set, so it reads as system.
 */
export function promptOrigin(entry: PromptOriginSource): { label: string; badge: string } {
  if (entry.overridden) {
    return { label: 'overridden here', badge: 'badge-primary' };
  }
  const from = entry.effective.scope === 'embedded' ? 'system' : entry.effective.scope;
  return { label: `inherited from ${from}`, badge: 'badge-ghost' };
}

/** PromptRefSource is a prompt version a job ran with. */
export interface PromptRefSource {
  set: string;
  name: string;
  scope: string;
  scope_id?: string;
  version: number;
  hash: string;
}

/** shortPromptHash is a content hash's first eight hex digits, without its algorithm tag. */
export function shortPromptHash(hash: string): string {
  const colon = hash.indexOf(':');
  return (colon >= 0 ? hash.slice(colon + 1) : hash).slice(0, 8);
}

/** promptRefLabel is the job page's line for a prompt version: set/name · scope · vN · hash8. */
export function promptRefLabel(ref: PromptRefSource): string {
  return `${ref.set}/${ref.name} · ${ref.scope} · v${ref.version} · ${shortPromptHash(ref.hash)}`;
}

/** PromptLine is one line of the job page's Prompts group. */
export interface PromptLine {
  key: string;
  label: string;
  href: string | null;
}

/**
 * jobPromptLines is the job page's Prompts group: every ref the job ran
 * with, in order, each keyed by its position so a repeated ref still
 * renders once per entry.
 */
export function jobPromptLines(job: { prompts?: PromptRefSource[] | null }): PromptLine[] {
  return (job.prompts ?? []).map((ref, i) => ({
    key: `${i}:${ref.set}/${ref.name}`,
    label: promptRefLabel(ref),
    href: promptRefHref(ref),
  }));
}

/**
 * promptRefHref opens the Prompts page on the layer and version a job ran
 * with. The embedded body has no stored revision: it opens the system
 * scope at version 0. A set the page does not browse links nowhere.
 */
export function promptRefHref(ref: PromptRefSource): string | null {
  if (!PROMPT_SETS.includes(ref.set)) {
    return null;
  }
  const embedded = ref.scope === 'embedded';
  const scope = embedded ? 'system' : ref.scope;
  if (!PROMPT_SCOPES.includes(scope) || (scope === 'project' && !ref.scope_id)) {
    return null;
  }
  return promptLinkHref({
    set: ref.set,
    scope,
    project: scope === 'project' ? ref.scope_id : undefined,
    name: ref.name,
    version: embedded ? 0 : ref.version,
  });
}
