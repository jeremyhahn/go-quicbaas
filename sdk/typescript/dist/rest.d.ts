import type { Client, ConnectionClient, OrchestratorClient } from './interfaces.js';
import { APIError, ConnectionError, NotConnectedError } from './errors.js';
export declare class RestClient implements Client {
    readonly connection: ConnectionClient;
    readonly orchestrator: OrchestratorClient;
    constructor(baseUrl: string);
}
export { APIError, ConnectionError, NotConnectedError };
//# sourceMappingURL=rest.d.ts.map