import type { CeremonyInitializeRequest } from './types.js';
/**
 * requireOneAdminSelector throws CeremonyAdminSelectorError unless the request
 * selects exactly one bootstrap administrator credential path, the rule the
 * server enforces on CeremonyInitialize.
 */
export declare function requireOneAdminSelector(req: CeremonyInitializeRequest): void;
//# sourceMappingURL=ceremony.d.ts.map