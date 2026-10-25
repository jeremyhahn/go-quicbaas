// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// Client-side checks shared by every transport's ceremony client.

import { CeremonyAdminSelectorError } from './errors.js';
import type { CeremonyInitializeRequest } from './types.js';

/**
 * requireOneAdminSelector throws CeremonyAdminSelectorError unless the request
 * selects exactly one bootstrap administrator credential path, the rule the
 * server enforces on CeremonyInitialize.
 */
export function requireOneAdminSelector(req: CeremonyInitializeRequest): void {
  const selected: string[] = [];
  if (req.admin_csr_pem) selected.push('admin_csr_pem');
  if (req.admin_cert_pem) selected.push('admin_cert_pem');
  if (req.admin_generate) selected.push('admin_generate');
  if (selected.length !== 1) {
    throw new CeremonyAdminSelectorError(selected);
  }
}
