import type { BarrierClient, BootstrapClient, CeremonyClient, Client } from './interfaces.js';
import type { ClientConfig, Transport } from './types.js';
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
export declare function createClient(config?: ClientConfig): Promise<Client>;
export declare function getClient(config?: ClientConfig): Promise<Client>;
export declare function createBarrierClient(transport?: Transport, config?: ClientConfig): Promise<BarrierClient>;
export declare function createCeremonyClient(transport?: Transport, config?: ClientConfig): Promise<CeremonyClient>;
export declare function createBootstrapClient(transport?: Transport, config?: ClientConfig): Promise<BootstrapClient>;
//# sourceMappingURL=factory.d.ts.map