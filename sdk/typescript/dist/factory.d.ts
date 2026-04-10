import type { Client } from './interfaces.js';
import type { ClientConfig } from './types.js';
declare global {
    interface Window {
        go?: Record<string, Record<string, unknown>>;
        runtime?: {
            EventsOn(event: string, callback: (...args: unknown[]) => void): () => void;
            EventsOff(event: string): void;
        };
    }
}
export declare function isWailsRuntime(): boolean;
export declare function isDesktop(): boolean;
export declare function getClient(config?: ClientConfig): Promise<Client>;
//# sourceMappingURL=factory.d.ts.map