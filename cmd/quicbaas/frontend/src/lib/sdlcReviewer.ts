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
 * The per-pull-request reviewer choice, as the job view offers it.
 *
 * The choice is an OPERATOR'S RECORD on the SDLC module — never a label or a
 * comment on the pull request — so the view reads the binding's profiles and
 * the current selection, and writes through the module's own verbs.
 */

import type { ProjectBinding, ReviewerSelection } from '@quicbaas/sdk';
import { DEFAULT_REVIEWER_PROFILE } from './sdlcBinding.ts';

/** PROJECT_DEFAULT is the select value that clears the override. */
export const PROJECT_DEFAULT = '';

/** ReviewerOption is one choice in the reviewer select. */
export interface ReviewerOption {
  /** The profile name the override records; PROJECT_DEFAULT clears it. */
  value: string;
  label: string;
  countsTowardMerge: boolean;
}

/**
 * reviewerOptions lists what an operator may pick for one pull request: the
 * project default first (clearing any override), then every profile by name,
 * each labelled with its merge role so an advisory pick is never mistaken for
 * one that can land the change.
 */
export function reviewerOptions(binding: ProjectBinding): ReviewerOption[] {
  const defaultProfile = binding.reviewer;
  if ((defaultProfile?.agent_id ?? '').trim() === '') return [];
  const defaultName = defaultProfile?.name?.trim() || DEFAULT_REVIEWER_PROFILE;
  const defaultCounts = defaultProfile?.counts_toward_merge ?? true;
  const options: ReviewerOption[] = [
    {
      value: PROJECT_DEFAULT,
      label: `Project default (${defaultName})`,
      countsTowardMerge: defaultCounts,
    },
    {
      value: defaultName,
      label: `${defaultName}${roleSuffix(defaultCounts)}`,
      countsTowardMerge: defaultCounts,
    },
  ];
  for (const alternate of binding.reviewers ?? []) {
    const name = alternate.name?.trim() ?? '';
    if (name === '') continue;
    const counts = alternate.counts_toward_merge ?? false;
    options.push({ value: name, label: `${name}${roleSuffix(counts)}`, countsTowardMerge: counts });
  }
  return options;
}

function roleSuffix(counts: boolean): string {
  return counts ? '' : ' (advisory)';
}

/** selectedValue is the select value a selection corresponds to. */
export function selectedValue(selection: ReviewerSelection | null): string {
  if (selection === null || !selection.overridden) return PROJECT_DEFAULT;
  return selection.profile;
}
