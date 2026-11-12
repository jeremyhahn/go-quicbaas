// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC

import { UnsupportedArgumentError } from './errors.js';
import type { CeremonyOfficer } from './types.js';

// ceremonyOfficerOptions are the officer fields no ceremony backend accepts
// yet: the pinned go-qrdb ceremony parser behind pkg/api/mcp/tools_ceremony.go
// and the REST handler in pkg/api/rest/handlers_ceremony.go read only
// username and csr_pem.
const ceremonyOfficerOptions: ReadonlyArray<{ field: string; isSet: (o: CeremonyOfficer) => boolean }> = [
  { field: 'role', isSet: (o) => o.role !== undefined && o.role !== '' },
  { field: 'encryption_kem', isSet: (o) => o.encryption_kem !== undefined && o.encryption_kem !== '' },
  { field: 'encryption_key', isSet: (o) => o.encryption_key !== undefined && o.encryption_key !== '' },
  { field: 'factors', isSet: (o) => o.factors !== undefined && o.factors.length > 0 },
  { field: 'designated', isSet: (o) => o.designated !== undefined && o.designated !== '' },
];

// ceremonyOfficerWireForm returns the officer as the backend takes it, or
// throws UnsupportedArgumentError when it sets an option the backend would
// otherwise drop. backend names the transport in the message ("MCP", "REST").
export function ceremonyOfficerWireForm(
  transport: string,
  backend: string,
  officer: CeremonyOfficer,
): { username: string; csr_pem: string } {
  for (const option of ceremonyOfficerOptions) {
    if (option.isSet(officer)) {
      throw new UnsupportedArgumentError(
        transport,
        option.field,
        `ceremony officer "${officer.username}" sets ${option.field}; the ${backend} ceremony backend does not support it yet`,
      );
    }
  }
  return { username: officer.username, csr_pem: officer.csr_pem };
}
