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
 * The one place a RefStatus becomes a colour and a word — reused by
 * RefPicker's option rows and RefChip's dot, off the same
 * `STATUS_TONES`/`Tone` table StatusBadge already draws from, so a ref's
 * status dot and a StatusBadge pill never drift into two palettes for the
 * same idea.
 */

import type { Tone } from '../theme/tokens.ts';
import type { RefStatus } from './types.ts';

const TONE_FOR_STATUS: Readonly<Record<RefStatus, Tone>> = {
  exists: 'success',
  missing: 'error',
  sealed: 'warning',
  expired: 'warning',
  disabled: 'neutral',
  unknown: 'neutral',
};

const LABEL_FOR_STATUS: Readonly<Record<RefStatus, string>> = {
  exists: 'Exists',
  missing: 'Missing',
  sealed: 'Sealed',
  expired: 'Expired',
  disabled: 'Disabled',
  unknown: 'Unknown',
};

/** refStatusTone maps a status to the tone its dot renders in. */
export function refStatusTone(status: RefStatus | undefined): Tone {
  return TONE_FOR_STATUS[status ?? 'unknown'];
}

/** refStatusLabel is the status's word, for a title/aria-label. */
export function refStatusLabel(status: RefStatus | undefined): string {
  return LABEL_FOR_STATUS[status ?? 'unknown'];
}
