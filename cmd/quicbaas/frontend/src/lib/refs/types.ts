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

/**
 * The universal reference: the shared vocabulary behind every field in this
 * app that names another object this server controls — a secret path, a KMS
 * key, a project ID. `RefPicker` and `RefChip` (components/core) are generic
 * over `RefKind`; everything kind-specific lives in one `RefProvider` per
 * kind, registered once in `providers.ts`.
 *
 * A picker or a chip that only knew "string" would need a special case per
 * field. A picker that knows `RefProvider` needs exactly one implementation,
 * and adding a new referenceable kind is adding one provider, not a new
 * component.
 */


/** RefKind is one type of object this server owns that a form can reference. */
export type RefKind =
  | 'secret'
  | 'kmsKey'
  | 'sshKey'
  | 'certificate'
  | 'account'
  | 'fleetNode'
  | 'project'
  | 'tenant'
  | 'user'
  | 'role'
  | 'schedulerJob'
  | 'workflow';

/**
 * RefStatus is the live state a status dot renders. Not every kind uses
 * every value — a provider reports whichever of these its object actually
 * has (see each provider's comment) and 'unknown' for a kind that reports
 * no live status.
 */
export type RefStatus = 'exists' | 'missing' | 'sealed' | 'expired' | 'disabled' | 'unknown';

/** RefOption is one row a picker's search returns. */
export interface RefOption {
  /** value is what gets stored in the field — a path, name, or ID. */
  value: string;
  /** label is the option's primary display name. */
  label: string;
  /** detail is the secondary line — an algorithm, a host, an email. */
  detail?: string;
  status?: RefStatus;
  /** group heads a section in the picker's grouped results. */
  group?: string;
}

/** RefProviderContext is what a provider's list/search/status/create is asked against. */
export interface RefProviderContext {
  tenantID: string;
  /** extra is provider-specific scoping — the SSH key store's scope/owner pair, for example. */
  [extra: string]: unknown;
}

/** RefCreateField is one extra input the inline create panel collects beyond the typed name/path. */
export interface RefCreateField {
  key: string;
  label: string;
  type: 'text' | 'password' | 'textarea';
  placeholder?: string;
  required?: boolean;
}

/**
 * RefProvider is everything a picker or a chip needs for one kind: how to
 * browse it, search it, resolve one value's live status, optionally create
 * one inline, and optionally link to its detail view.
 */
export interface RefProvider {
  readonly kind: RefKind;
  /** label names the kind in prose — "Secret", "KMS key". */
  readonly label: string;
  // Lucide icons are SvelteComponentTyped classes; use a permissive type,
  // the same convention EmptyState.svelte and StatCard.svelte follow.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  /** glyph is the lucide icon component the picker and the chip render beside every row. */
  readonly glyph: new (...args: any[]) => any;
  /** list returns the browse set — the rows shown before the user types anything. */
  list(ctx: RefProviderContext): Promise<RefOption[]>;
  /** search narrows list() by free text; a kind with no server-side search filters list() itself. */
  search(query: string, ctx: RefProviderContext): Promise<RefOption[]>;
  /**
   * status resolves ONE value's live status — the call a chip makes for a
   * value that was never in a list page, so a stale or hand-typed reference
   * still renders truthfully instead of defaulting to "exists".
   */
  status?(value: string, ctx: RefProviderContext): Promise<RefStatus>;
  /** createLabel is the trailing row's verb — "Create secret". Absent means the kind is not creatable here. */
  createLabel?: string;
  /** createFields are the extra inputs the inline create panel collects beyond the typed name/path. */
  createFields?: RefCreateField[];
  /** create makes a new object named by the typed input plus the extra fields, then the picker selects it. */
  create?(input: string, extra: Record<string, string>, ctx: RefProviderContext): Promise<RefOption>;
  /** detailRoute is the app's hash route for this value, or undefined when the kind has no detail view yet. */
  detailRoute?(value: string, ctx: RefProviderContext): string | undefined;
}
